package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	deliveryMaxAttempts = 4
	deliveryTimeout     = 12 * time.Second
	deliveryBaseDelay   = 200 * time.Millisecond
	deliveryMaxDelay    = 5 * time.Second
)

type fileState struct {
	Size    int64
	ModTime time.Time
}

type collector struct {
	client           *http.Client
	endpoint         string
	token            string
	machineID        string
	mu               sync.Mutex
	files            map[string]fileState
	initialized      bool
	gitHead          string
	browserCDP       string
	browserLocations map[string]string
	wait             func(context.Context, time.Duration) error
}

type deliveryError struct {
	statusCode int
	attempts   int
	transient  bool
	cause      error
}

func (e *deliveryError) Error() string {
	if e.statusCode != 0 {
		return fmt.Sprintf("backend returned HTTP %d after %d attempt(s)", e.statusCode, e.attempts)
	}
	return fmt.Sprintf("backend delivery failed after %d attempt(s): %v", e.attempts, e.cause)
}

func main() {
	c := &collector{
		client: &http.Client{Timeout: 10 * time.Second}, endpoint: strings.TrimRight(os.Getenv("KUAYLE_INGEST_URL"), "/"),
		token: os.Getenv("KUAYLE_MACHINE_TOKEN"), machineID: os.Getenv("KUAYLE_MACHINE_ID"), files: make(map[string]fileState),
		browserCDP: strings.TrimRight(os.Getenv("KUAYLE_BROWSER_CDP_URL"), "/"), browserLocations: make(map[string]string),
	}
	if c.endpoint == "" || c.token == "" {
		log.Fatal("collector ingest URL and machine token are required")
	}
	go c.poll()
	http.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	http.HandleFunc("/event", c.receiveEvent)
	server := &http.Server{
		Addr: ":8091", Handler: http.DefaultServeMux, ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 64 * 1024,
	}
	log.Fatal(server.ListenAndServe())
}

func (c *collector) poll() {
	fileTicker := time.NewTicker(2 * time.Second)
	heartbeatTicker := time.NewTicker(30 * time.Second)
	defer fileTicker.Stop()
	defer heartbeatTicker.Stop()
	for {
		select {
		case <-fileTicker.C:
			c.scanFiles()
			c.scanGit()
			c.scanBrowser()
		case <-heartbeatTicker.C:
			c.sendBackground("collector", "machine.heartbeat", map[string]any{"machine_id": c.machineID})
		}
	}
}

func (c *collector) receiveEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var event struct {
		Source    string `json:"source"`
		EventType string `json:"event_type"`
		Payload   any    `json:"payload"`
	}
	if json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&event) != nil || event.Source == "" || event.EventType == "" {
		http.Error(w, "invalid event", http.StatusBadRequest)
		return
	}
	if err := c.send(r.Context(), event.Source, event.EventType, event.Payload); err != nil {
		log.Printf("collector event delivery failed: %v", err)
		if failure, ok := err.(*deliveryError); ok && failure.transient {
			w.Header().Set("Retry-After", "1")
			http.Error(w, "event delivery temporarily unavailable", http.StatusServiceUnavailable)
			return
		}
		http.Error(w, "event delivery rejected", http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (c *collector) scanFiles() {
	next := make(map[string]fileState)
	_ = filepath.WalkDir("/workspace", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() && (entry.Name() == ".git" || entry.Name() == "node_modules") {
			return filepath.SkipDir
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return nil
		}
		relative, _ := filepath.Rel("/workspace", path)
		next[relative] = fileState{Size: info.Size(), ModTime: info.ModTime()}
		return nil
	})
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.initialized {
		c.files = next
		c.initialized = true
		return
	}
	for path, state := range next {
		previous, exists := c.files[path]
		if !exists {
			c.sendBackground("filesystem", "file.created", map[string]any{"path": path, "size_bytes": state.Size})
		} else if previous.Size != state.Size || !previous.ModTime.Equal(state.ModTime) {
			c.sendBackground("filesystem", "file.modified", map[string]any{"path": path, "size_bytes": state.Size})
		}
	}
	for path := range c.files {
		if _, exists := next[path]; !exists {
			c.sendBackground("filesystem", "file.deleted", map[string]any{"path": path})
		}
	}
	c.files = next
}

func (c *collector) scanGit() {
	command := exec.Command("git", "-C", "/workspace", "rev-parse", "HEAD")
	output, err := command.Output()
	if err != nil {
		return
	}
	head := strings.TrimSpace(string(output))
	if c.gitHead != "" && c.gitHead != head {
		c.sendBackground("git", "git.commit_created", map[string]any{"commit": head})
	}
	c.gitHead = head
}

func (c *collector) scanBrowser() {
	if c.browserCDP == "" {
		return
	}
	request, err := http.NewRequest(http.MethodGet, c.browserCDP+"/json", nil)
	if err != nil {
		return
	}
	// Chrome rejects internal DNS names in the Host header as a DNS-rebinding defense.
	request.Host = "127.0.0.1"
	response, err := c.client.Do(request)
	if err != nil {
		return
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return
	}
	var targets []struct {
		ID    string `json:"id"`
		Title string `json:"title"`
		URL   string `json:"url"`
		Type  string `json:"type"`
	}
	if json.NewDecoder(io.LimitReader(response.Body, 1024*1024)).Decode(&targets) != nil {
		return
	}
	for _, target := range targets {
		if target.Type != "page" || target.URL == "" || strings.HasPrefix(target.URL, "about:") {
			continue
		}
		parsed, err := url.Parse(target.URL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			continue
		}
		parsed.RawQuery = ""
		parsed.Fragment = ""
		parsed.User = nil
		location := parsed.String()
		if c.browserLocations[target.ID] != location {
			c.browserLocations[target.ID] = location
			c.sendBackground("browser", "browser.navigation", map[string]any{"url": location, "title": target.Title})
		}
	}
}

func (c *collector) sendBackground(source, eventType string, payload any) {
	if err := c.send(context.Background(), source, eventType, payload); err != nil {
		log.Printf("collector event delivery failed: %v", err)
	}
}

func (c *collector) send(ctx context.Context, source, eventType string, payload any) error {
	body, err := json.Marshal(map[string]any{"source": source, "event_type": eventType, "payload": payload, "occurred_at": time.Now().UTC()})
	if err != nil {
		return &deliveryError{attempts: 1, cause: fmt.Errorf("encode event: %w", err)}
	}
	deliveryContext, cancel := context.WithTimeout(ctx, deliveryTimeout)
	defer cancel()

	var failure *deliveryError
	for attempt := 1; attempt <= deliveryMaxAttempts; attempt++ {
		request, err := http.NewRequestWithContext(deliveryContext, http.MethodPost, c.endpoint+"/events", bytes.NewReader(body))
		if err != nil {
			return &deliveryError{attempts: attempt, cause: fmt.Errorf("create request: %w", err)}
		}
		request.Header.Set("Authorization", "Bearer "+c.token)
		request.Header.Set("Content-Type", "application/json")

		response, requestErr := c.client.Do(request)
		retryAfter := ""
		if requestErr != nil {
			failure = &deliveryError{attempts: attempt, transient: true, cause: requestErr}
		} else {
			retryAfter = response.Header.Get("Retry-After")
			_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
			_ = response.Body.Close()
			if response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices {
				return nil
			}
			failure = &deliveryError{
				statusCode: response.StatusCode,
				attempts:   attempt,
				transient:  retryableDeliveryStatus(response.StatusCode),
			}
			if !failure.transient {
				return failure
			}
		}
		if attempt == deliveryMaxAttempts || deliveryContext.Err() != nil {
			return failure
		}
		if err := c.waitForRetry(deliveryContext, deliveryRetryDelay(attempt, retryAfter, time.Now())); err != nil {
			failure.cause = err
			return failure
		}
	}
	return failure
}

func retryableDeliveryStatus(status int) bool {
	return status == http.StatusRequestTimeout || status == http.StatusTooEarly || status == http.StatusTooManyRequests || status >= http.StatusInternalServerError
}

func deliveryRetryDelay(attempt int, retryAfter string, now time.Time) time.Duration {
	if delay, ok := parseRetryAfter(retryAfter, now); ok {
		return min(delay, deliveryMaxDelay)
	}
	delay := deliveryBaseDelay * time.Duration(1<<min(attempt-1, 10))
	jitter := time.Duration(rand.Int63n(max(1, int64(delay/2))))
	return min(delay+jitter, deliveryMaxDelay)
}

func parseRetryAfter(value string, now time.Time) (time.Duration, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil && seconds >= 0 {
		if seconds >= int64(deliveryMaxDelay/time.Second) {
			return deliveryMaxDelay, true
		}
		return time.Duration(seconds) * time.Second, true
	}
	when, err := http.ParseTime(value)
	if err != nil {
		return 0, false
	}
	return max(0, when.Sub(now)), true
}

func (c *collector) waitForRetry(ctx context.Context, delay time.Duration) error {
	if c.wait != nil {
		return c.wait(ctx, delay)
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
