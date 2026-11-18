// Package codexlogin runs one ChatGPT device login in an isolated Codex app-server.
package codexlogin

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
)

const (
	officialVerificationURL = "https://auth.openai.com/codex/device"
	maxCredentialsSize      = 64 * 1024
	completionMethod        = "account/login/completed"
)

// Login is a single device authorization attempt.
type Login struct {
	VerificationURL string
	UserCode        string

	cancel context.CancelFunc
	done   chan struct{}
	auth   []byte
	err    error
}

// Start launches Codex and waits for the device prompt. The context must stay
// active until the authorization completes or is canceled.
func Start(ctx context.Context) (*Login, error) {
	return start(ctx, "codex")
}

func start(ctx context.Context, executable string) (*Login, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	home, err := os.MkdirTemp("", "kuayle-codex-login-")
	if err != nil {
		return nil, errors.New("could not create Codex login home")
	}
	ctx, cancel := context.WithCancel(ctx)
	cmd, stdin, stdout, err := launch(ctx, executable, home)
	if err != nil {
		contextErr := ctx.Err()
		cancel()
		_ = os.RemoveAll(home)
		if contextErr != nil {
			return nil, contextErr
		}
		return nil, errors.New("could not start Codex app-server")
	}

	login := &Login{cancel: cancel, done: make(chan struct{})}
	prompt := make(chan devicePrompt, 1)
	go func() {
		defer close(prompt)
		defer close(login.done)
		// Closing both pipes also releases I/O if a launcher inherited a pipe
		// before the app-server was canceled.
		stopClosing := context.AfterFunc(ctx, func() {
			_ = stdout.Close()
			_ = stdin.Close()
		})
		login.auth, login.err = authorize(home, stdin, stdout, prompt)
		if ctx.Err() != nil {
			login.auth, login.err = nil, ctx.Err()
		}

		stopClosing()
		cancel()
		_ = stdout.Close()
		_ = stdin.Close()
		_ = cmd.Wait()
		if err := os.RemoveAll(home); err != nil {
			login.auth, login.err = nil, errors.New("could not remove Codex login home")
		}
	}()
	result, ok := <-prompt
	if !ok {
		return nil, login.err
	}
	login.VerificationURL, login.UserCode = result.VerificationURL, result.UserCode
	return login, nil
}

func launch(ctx context.Context, executable, home string) (*exec.Cmd, *os.File, *os.File, error) {
	childStdin, stdin, err := os.Pipe()
	if err != nil {
		return nil, nil, nil, err
	}
	defer childStdin.Close()
	stdout, childStdout, err := os.Pipe()
	if err != nil {
		_ = stdin.Close()
		return nil, nil, nil, err
	}
	defer childStdout.Close()

	cmd := exec.CommandContext(ctx, executable, "app-server", "--listen", "stdio://", "-c", `cli_auth_credentials_store="file"`)
	cmd.Dir = home
	cmd.Env = []string{"HOME=" + home, "CODEX_HOME=" + home, "TMPDIR=" + home, "PATH=/usr/local/bin:/usr/bin:/bin"}
	cmd.Stdin = childStdin
	cmd.Stdout = childStdout
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, nil, nil, err
	}
	return cmd, stdin, stdout, nil
}

// Wait returns the Codex auth file after successful approval and cleanup.
func (l *Login) Wait() ([]byte, error) {
	<-l.done
	return l.auth, l.err
}

// Close cancels the attempt and waits for the process and its files to be cleaned up.
func (l *Login) Close() {
	l.cancel()
	<-l.done
}

type devicePrompt struct {
	Type            string `json:"type"`
	LoginID         string `json:"loginId"`
	VerificationURL string `json:"verificationUrl"`
	UserCode        string `json:"userCode"`
}

type message struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
}

type loginCompletion struct {
	LoginID string `json:"loginId"`
	Success bool   `json:"success"`
}

func authorize(home string, stdin io.Writer, stdout io.Reader, started chan<- devicePrompt) ([]byte, error) {
	encoder := json.NewEncoder(stdin)
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 4096), 1<<20)

	if err := encoder.Encode(map[string]any{
		"id": 1, "method": "initialize",
		"params": map[string]any{
			"clientInfo": map[string]string{"name": "kuayle", "version": "1"},
		},
	}); err != nil {
		return nil, errors.New("could not initialize Codex app-server")
	}
	if _, err := readResponse(scanner, "1", nil); err != nil {
		return nil, errors.New("could not initialize Codex app-server")
	}
	if err := encoder.Encode(map[string]string{"method": "initialized"}); err != nil {
		return nil, errors.New("could not initialize Codex app-server")
	}
	if err := encoder.Encode(map[string]any{
		"id": 2, "method": "account/login/start",
		"params": map[string]string{"type": "chatgptDeviceCode"},
	}); err != nil {
		return nil, errors.New("could not request a Codex device code")
	}

	// Codex starts polling before it publishes the prompt response. Keep any
	// completion delivered in that window and match it once we know the login ID.
	earlyCompletions := make(map[string]bool)
	result, err := readResponse(scanner, "2", func(msg message) {
		if completion, ok := parseCompletion(msg); ok && len(earlyCompletions) < 16 {
			earlyCompletions[completion.LoginID] = completion.Success
		}
	})
	if err != nil {
		return nil, errors.New("could not request a Codex device code")
	}
	var prompt devicePrompt
	if json.Unmarshal(result, &prompt) != nil || prompt.Type != "chatgptDeviceCode" ||
		prompt.LoginID == "" || prompt.VerificationURL != officialVerificationURL || !validCode(prompt.UserCode) {
		return nil, errors.New("Codex returned an invalid device prompt")
	}
	started <- prompt

	if success, ok := earlyCompletions[prompt.LoginID]; ok {
		return finish(home, success)
	}
	for scanner.Scan() {
		var msg message
		if json.Unmarshal(scanner.Bytes(), &msg) != nil {
			return nil, errors.New("Codex returned an invalid login message")
		}
		completion, ok := parseCompletion(msg)
		if ok && completion.LoginID == prompt.LoginID {
			return finish(home, completion.Success)
		}
	}
	return nil, errors.New("Codex app-server stopped before sign-in completed")
}

func readResponse(scanner *bufio.Scanner, id string, onOther func(message)) (json.RawMessage, error) {
	for scanner.Scan() {
		var msg message
		if json.Unmarshal(scanner.Bytes(), &msg) != nil {
			break
		}
		if string(msg.ID) == id && msg.Method == "" {
			if len(msg.Error) == 0 && len(msg.Result) > 0 && string(msg.Result) != "null" {
				return msg.Result, nil
			}
			break
		}
		if onOther != nil {
			onOther(msg)
		}
	}
	return nil, errors.New("Codex returned no successful response")
}

func parseCompletion(msg message) (loginCompletion, bool) {
	var completion loginCompletion
	if msg.Method != completionMethod || len(msg.ID) != 0 {
		return completion, false
	}
	err := json.Unmarshal(msg.Params, &completion)
	return completion, err == nil && completion.LoginID != ""
}

func validCode(code string) bool {
	return len(code) > 0 && len(code) <= 64 && !strings.ContainsFunc(code, func(r rune) bool {
		return r < '!' || r > '~'
	})
}

func finish(home string, success bool) ([]byte, error) {
	if !success {
		return nil, errors.New("Codex sign-in was unsuccessful")
	}
	auth, err := readCredentials(home)
	if err != nil {
		return nil, errors.New("Codex did not produce valid ChatGPT credentials")
	}
	return auth, nil
}

func readCredentials(home string) ([]byte, error) {
	root, err := os.OpenRoot(home)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	info, err := root.Lstat("auth.json")
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxCredentialsSize {
		return nil, errors.New("invalid Codex credentials file")
	}
	file, err := root.Open("auth.json")
	if err != nil {
		return nil, err
	}
	defer file.Close()
	auth, err := io.ReadAll(io.LimitReader(file, maxCredentialsSize+1))
	if err != nil {
		return nil, err
	}
	var data struct {
		AuthMode string `json:"auth_mode"`
		Tokens   struct {
			IDToken      string `json:"id_token"`
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
		} `json:"tokens"`
	}
	if len(auth) > maxCredentialsSize || json.Unmarshal(auth, &data) != nil ||
		data.AuthMode != "chatgpt" || data.Tokens.IDToken == "" ||
		data.Tokens.AccessToken == "" || data.Tokens.RefreshToken == "" {
		return nil, errors.New("invalid Codex credentials file")
	}
	return auth, nil
}
