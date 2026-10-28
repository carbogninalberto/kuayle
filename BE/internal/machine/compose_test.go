package machine

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var officialImages = []string{"docker.io/library"}

func TestParseComposeAcceptsTheSupportedSubsetInDependencyOrder(t *testing.T) {
	services, err := ParseCompose([]byte(`
name: app
services:
  api:
    image: ghcr.io/acme/api:1
    depends_on:
      db: {condition: service_healthy}
    ports: ["8080:8080"]
  db:
    image: postgres:16
    restart: unless-stopped
    environment:
      POSTGRES_DB: ${POSTGRES_DB:-fixture}
      HOME_LEAK: ${HOME}
      EMPTY:
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U $$USER"]
      interval: 2s
      retries: 5
    command: postgres -c fsync=off
`), append(officialImages, "ghcr.io/acme"))
	require.NoError(t, err)
	require.Len(t, services, 2)
	db, api := services[0], services[1]
	require.Equal(t, "db", db.Name)
	require.Equal(t, "docker.io/library/postgres:16", db.Image)
	require.Equal(t, []string{"POSTGRES_DB=fixture", "HOME_LEAK=", "EMPTY="}, db.Env)
	require.Equal(t, []string{"postgres", "-c", "fsync=off"}, db.Command)
	require.Equal(t, &ComposeHealth{Test: []string{"CMD-SHELL", "pg_isready -U $USER"}, Interval: 2 * time.Second, Retries: 5}, db.Health)
	require.Equal(t, "api", api.Name)
	require.Equal(t, "ghcr.io/acme/api:1", api.Image)
}

func TestParseComposeRejectsWhatWouldWeakenIsolation(t *testing.T) {
	cases := map[string]string{
		"build":            "services:\n  app:\n    build: .\n",
		"no image":         "services:\n  app:\n    command: [sh]\n",
		"bind mount":       "services:\n  app:\n    image: redis\n    volumes: [\"/:/host\"]\n",
		"privileged":       "services:\n  app:\n    image: redis\n    privileged: true\n",
		"host network":     "services:\n  app:\n    image: redis\n    network_mode: host\n",
		"capabilities":     "services:\n  app:\n    image: redis\n    cap_add: [SYS_ADMIN]\n",
		"devices":          "services:\n  app:\n    image: redis\n    devices: [\"/dev/kvm\"]\n",
		"env file":         "services:\n  app:\n    image: redis\n    env_file: .env\n",
		"top-level volume": "services:\n  app:\n    image: redis\nvolumes:\n  data: {}\n",
		"include":          "include: [other.yaml]\nservices:\n  app:\n    image: redis\n",
		"unlisted image":   "services:\n  app:\n    image: attacker.example/exfil:latest\n",
		"user image":       "services:\n  app:\n    image: someone/redis\n",
		"anchors":          "x: &a {image: redis}\nservices:\n  app: *a\n",
		"egress alias":     "services:\n  machine-egress:\n    image: redis\n",
		"validation alias": "services:\n  validation-1:\n    image: redis\n",
		"required var":     "services:\n  app:\n    image: redis\n    environment: {A: \"${A:?missing}\"}\n",
		"cycle":            "services:\n  a:\n    image: redis\n    depends_on: [b]\n  b:\n    image: redis\n    depends_on: [a]\n",
		"unknown dep":      "services:\n  a:\n    image: redis\n    depends_on: [b]\n",
		"completion dep":   "services:\n  a:\n    image: redis\n    depends_on: {b: {condition: service_completed_successfully}}\n  b:\n    image: redis\n",
		"empty":            "services: {}\n",
		"not yaml":         "services: [\n",
	}
	for name, file := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ParseCompose([]byte(file), officialImages)
			var composeError *ComposeError
			require.True(t, errors.As(err, &composeError), "%v", err)
		})
	}
	_, err := ParseCompose([]byte(strings.Repeat("#", maxComposeFileBytes+1)), officialImages)
	require.Error(t, err)
	many := "services:\n"
	for i := 0; i <= maxComposeServices; i++ {
		many += "  s" + string(rune('a'+i)) + ":\n    image: redis\n"
	}
	_, err = ParseCompose([]byte(many), officialImages)
	require.ErrorContains(t, err, "at most")
}
