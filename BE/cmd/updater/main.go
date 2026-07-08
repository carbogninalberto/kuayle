package main

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
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

	mu      sync.Mutex
	running bool
	message string
}

type updateResponse struct {
	Running bool   `json:"running"`
	Message string `json:"message"`
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

	u.mu.Lock()
	defer u.mu.Unlock()
	writeJSON(w, http.StatusOK, updateResponse{Running: u.running, Message: u.message})
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
	u.running = true
	u.message = "System update is running"
	u.mu.Unlock()

	go u.runUpdate()
	writeJSON(w, http.StatusAccepted, updateResponse{Running: true, Message: "System update started"})
}

func (u *updater) runUpdate() {
	log.Printf("starting update script %s", u.script)
	cmd := exec.Command("bash", u.script)
	cmd.Dir = u.repoRoot
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()

	u.mu.Lock()
	defer u.mu.Unlock()
	u.running = false
	if err != nil {
		u.message = "Last update failed"
		log.Printf("update failed: %v", err)
		return
	}
	u.message = "Last update completed successfully"
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
