package codexlogin

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const credentials = `{"auth_mode":"chatgpt","tokens":{"id_token":"<test-id>","access_token":"<test-access>","refresh_token":"<test-refresh>"}}`

func TestStartAndWait(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "outside-api-key")
	t.Setenv("CODEX_HOME", "/outside-codex-home")
	t.Setenv("XDG_CONFIG_HOME", "/outside-config")
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	executable, homeFile := fakeCodex(t, `
printf '%s' '`+credentials+`' > "$CODEX_HOME/auth.json"
printf '%s\n' '{"method":"account/login/completed","params":{"loginId":"unrelated","success":false}}'
printf '%s\n' '{"method":"account/login/completed","params":{"loginId":"attempt","success":true}}'
`+fakeDevicePrompt+keepReading)
	login, err := start(ctx, executable)
	if err != nil {
		t.Fatal(err)
	}
	defer login.Close()
	if login.VerificationURL != "https://auth.openai.com/codex/device" || login.UserCode != "ABCD-1234" {
		t.Fatal("incorrect prompt")
	}
	auth, err := login.Wait()
	if err != nil {
		t.Fatal(err)
	}
	if string(auth) != credentials {
		t.Fatal("incorrect credentials")
	}
	assertHomeRemoved(t, homeFile)
}

func TestCloseCancelsAndCleansUp(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	executable, homeFile := fakeCodex(t, fakeDevicePrompt+`
printf '%s' '`+credentials+`' > "$CODEX_HOME/auth.json"
printf '%s\n' '{"method":"account/login/completed","params":{"loginId":"unrelated","success":true}}'
`+keepReading)
	login, err := start(ctx, executable)
	if err != nil {
		t.Fatal(err)
	}
	login.Close()
	login.Close()
	auth, err := login.Wait()
	if !errors.Is(err, context.Canceled) || len(auth) != 0 {
		t.Fatal("canceled login returned credentials or incorrect error")
	}
	assertHomeRemoved(t, homeFile)
}

func TestWaitRequiresMatchingSuccessAndChatGPTCredentials(t *testing.T) {
	for _, tt := range []struct {
		name string
		tail string
	}{
		{"upstream failure", `
printf '%s' '` + credentials + `' > "$CODEX_HOME/auth.json"
printf '%s\n' '{"method":"account/login/completed","params":{"loginId":"attempt","success":false,"error":"test-secret"}}'
`},
		{"invalid credentials", `
printf '%s' '{"auth_mode":"apiKey","tokens":{}}' > "$CODEX_HOME/auth.json"
printf '%s\n' '{"method":"account/login/completed","params":{"loginId":"attempt","success":true}}'
`},
		{"missing credentials", `
printf '%s\n' '{"method":"account/login/completed","params":{"loginId":"attempt","success":true}}'
`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			executable, homeFile := fakeCodex(t, fakeDevicePrompt+tt.tail+keepReading)
			login, err := start(ctx, executable)
			if err != nil {
				t.Fatal(err)
			}
			defer login.Close()
			auth, err := login.Wait()
			if err == nil || len(auth) != 0 || strings.Contains(err.Error(), "test-secret") {
				t.Fatal("failed login exposed upstream data or returned success")
			}
			assertHomeRemoved(t, homeFile)
		})
	}
}

func TestStartRejectsUnexpectedVerificationURL(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	executable, homeFile := fakeCodex(t, strings.Replace(fakeDevicePrompt, "https://auth.openai.com/codex/device", "https://auth.openai.com.evil.example/codex/device", 1)+keepReading)
	login, err := start(ctx, executable)
	if err == nil || login != nil {
		t.Fatal("accepted unexpected verification URL")
	}
	assertHomeRemoved(t, homeFile)
}

const fakeDevicePrompt = `
printf '%s\n' '{"id":2,"result":{"type":"chatgptDeviceCode","loginId":"attempt","verificationUrl":"https://auth.openai.com/codex/device","userCode":"ABCD-1234"}}'
`

const keepReading = `
while IFS= read -r req; do :; done
`

func fakeCodex(t *testing.T, tail string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	homeFile := filepath.Join(dir, "home")
	script := `#!/bin/sh
set -eu
[ "$*" = 'app-server --listen stdio:// -c cli_auth_credentials_store="file"' ]
[ "${OPENAI_API_KEY-unset}" = unset ]
[ "${XDG_CONFIG_HOME-unset}" = unset ]
[ "$HOME" = "$CODEX_HOME" ]
[ "$HOME" = "$TMPDIR" ]
[ "$HOME" -ef . ]
[ "$HOME" != "/outside-codex-home" ]
printf '%s' "$CODEX_HOME" > "$(dirname "$0")/home"
IFS= read -r req
case "$req" in *'"id":1'*'"method":"initialize"'*'"clientInfo"'*) ;; *) exit 1 ;; esac
printf '%s\n' '{"method":"configWarning","params":{}}'
printf '%s\n' '{"id":1,"result":{}}'
IFS= read -r req
[ "$req" = '{"method":"initialized"}' ]
IFS= read -r req
case "$req" in *'"id":2'*'"method":"account/login/start"'*'"type":"chatgptDeviceCode"'*) ;; *) exit 1 ;; esac
` + tail
	executable := filepath.Join(dir, "fake-codex")
	if err := os.WriteFile(executable, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return executable, homeFile
}

func assertHomeRemoved(t *testing.T, file string) {
	t.Helper()
	home, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(string(home)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("login home was not removed")
	}
}
