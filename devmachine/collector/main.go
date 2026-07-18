package main

import (
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
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
			c.send("collector", "machine.heartbeat", map[string]any{"machine_id": c.machineID})
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
	c.send(event.Source, event.EventType, event.Payload)
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
			c.send("filesystem", "file.created", map[string]any{"path": path, "size_bytes": state.Size})
		} else if previous.Size != state.Size || !previous.ModTime.Equal(state.ModTime) {
			c.send("filesystem", "file.modified", map[string]any{"path": path, "size_bytes": state.Size})
		}
	}
	for path := range c.files {
		if _, exists := next[path]; !exists {
			c.send("filesystem", "file.deleted", map[string]any{"path": path})
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
		c.send("git", "git.commit_created", map[string]any{"commit": head})
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
			c.send("browser", "browser.navigation", map[string]any{"url": location, "title": target.Title})
		}
	}
}

func (c *collector) send(source, eventType string, payload any) {
	body, _ := json.Marshal(map[string]any{"source": source, "event_type": eventType, "payload": payload, "occurred_at": time.Now().UTC()})
	request, _ := http.NewRequest(http.MethodPost, c.endpoint+"/events", bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Content-Type", "application/json")
	response, err := c.client.Do(request)
	if err != nil {
		log.Printf("event delivery failed: %v", err)
		return
	}
	response.Body.Close()
}
