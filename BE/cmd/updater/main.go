package main

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type updater struct {
	token    string
	repoRoot string
	script   string

	mu         sync.Mutex
	running    bool
	message    string
	state      string
	startedAt  *time.Time
	finishedAt *time.Time
	failure    string
	output     *outputTail
}

const (
	stateIdle      = "idle"
	stateRunning   = "running"
	stateSucceeded = "succeeded"
	stateFailed    = "failed"

	// The backend reads at most 64 KiB of updater JSON.
	maxOutputBytes = 16 * 1024
)

type updateResponse struct {
	Running    bool       `json:"running"`
	Message    string     `json:"message"`
	State      string     `json:"state,omitempty"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	Error      string     `json:"error,omitempty"`
	Output     string     `json:"output,omitempty"`
}

// outputTail keeps the most recent script output for failure diagnosis.
type outputTail struct {
	mu   sync.Mutex
	data []byte
}

func (t *outputTail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.data = append(t.data, p...)
	if extra := len(t.data) - maxOutputBytes; extra > 0 {
		t.data = append([]byte(nil), t.data[extra:]...)
	}
	return len(p), nil
}

func (t *outputTail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.ToValidUTF8(string(t.data), "")
}

// lastLine returns the final non-empty output line, usually the failure reason.
func (t *outputTail) lastLine() string {
	lines := strings.Split(strings.TrimSpace(t.String()), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

func main() {
	token := strings.TrimSpace(os.Getenv("UPDATER_TOKEN"))
	if len(token) < 32 {
		log.Fatal("UPDATER_TOKEN must be at least 32 characters")
	}

	repoRoot := envOrDefault("REPO_ROOT", "/repo")
	script := envOrDefault("UPDATE_SCRIPT", filepath.Join(repoRoot, "selfhosting", "update.sh"))
	repoRoot, script = cleanPaths(repoRoot, script)

	if err := validateScript(repoRoot, script); err != nil {
		log.Fatal(err)
	}

	u := &updater{
		token:    token,
		repoRoot: repoRoot,
		script:   script,
		message:  "Idle",
		state:    stateIdle,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/status", u.withAuth(u.handleStatus))
	mux.HandleFunc("/update", u.withAuth(u.handleUpdate))

	port := envOrDefault("PORT", "8081")
	server := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("kuayle updater listening on :%s", port)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func (u *updater) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, updateResponse{Message: "Method not allowed"})
		return
	}

	writeJSON(w, http.StatusOK, u.status())
}

func (u *updater) status() updateResponse {
	u.mu.Lock()
	defer u.mu.Unlock()
	status := updateResponse{
		Running:    u.running,
		Message:    u.message,
		State:      u.state,
		StartedAt:  u.startedAt,
		FinishedAt: u.finishedAt,
		Error:      u.failure,
	}
	if u.state == stateFailed && u.output != nil {
		status.Output = u.output.String()
	}
	return status
}

func (u *updater) handleUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, updateResponse{Message: "Method not allowed"})
		return
	}

	u.mu.Lock()
	if u.running {
		msg := u.message
		u.mu.Unlock()
		writeJSON(w, http.StatusConflict, updateResponse{Running: true, Message: msg})
		return
	}
	now := time.Now().UTC()
	u.running = true
	u.message = "System update is running"
	u.state = stateRunning
	u.startedAt = &now
	u.finishedAt = nil
	u.failure = ""
	u.output = &outputTail{}
	output := u.output
	u.mu.Unlock()

	go u.runUpdate(func() error {
		log.Printf("starting update script %s", u.script)
		cmd := exec.Command("bash", u.script)
		cmd.Dir = u.repoRoot
		cmd.Stdout = io.MultiWriter(os.Stdout, output)
		cmd.Stderr = io.MultiWriter(os.Stderr, output)
		return cmd.Run()
	})
	writeJSON(w, http.StatusAccepted, updateResponse{Running: true, Message: "System update started", State: stateRunning, StartedAt: &now})
}

func (u *updater) runUpdate(run func() error) {
	err := run()

	u.mu.Lock()
	defer u.mu.Unlock()
	now := time.Now().UTC()
	u.running = false
	u.finishedAt = &now
	if err != nil {
		u.message = "Last update failed"
		u.state = stateFailed
		u.failure = u.output.lastLine()
		if u.failure == "" {
			u.failure = "Update script failed: " + err.Error()
		}
		log.Printf("update failed: %v", err)
		return
	}
	u.message = "Last update completed successfully"
	u.state = stateSucceeded
	log.Print("update completed successfully")
}

func (u *updater) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		expected := "Bearer " + u.token
		actual := r.Header.Get("Authorization")
		if subtle.ConstantTimeCompare([]byte(actual), []byte(expected)) != 1 {
			writeJSON(w, http.StatusUnauthorized, updateResponse{Message: "Unauthorized"})
			return
		}
		next(w, r)
	}
}

func envOrDefault(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func cleanPaths(repoRoot, script string) (string, string) {
	repoRootAbs, err := filepath.Abs(repoRoot)
	if err == nil {
		repoRoot = repoRootAbs
	}
	scriptAbs, err := filepath.Abs(script)
	if err == nil {
		script = scriptAbs
	}
	return filepath.Clean(repoRoot), filepath.Clean(script)
}

func validateScript(repoRoot, script string) error {
	if script != repoRoot && !strings.HasPrefix(script, repoRoot+string(os.PathSeparator)) {
		return fmt.Errorf("UPDATE_SCRIPT must be inside REPO_ROOT")
	}
	info, err := os.Stat(script)
	if err != nil {
		return fmt.Errorf("UPDATE_SCRIPT is not available: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("UPDATE_SCRIPT must be a file")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
