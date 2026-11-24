package agent

import (
	"reflect"
	"testing"

	"github.com/kuayle/kuayle-backend/internal/config"
	"github.com/stretchr/testify/require"
)

// A provider built without an image falls back to the image the server's
// configuration defaults to, not to an upstream image Kuayle never runs.
func TestBuiltinProvidersFallBackToTheConfiguredDefaultImages(t *testing.T) {
	defaults := func(field string) string {
		t.Helper()
		structField, ok := reflect.TypeOf(config.DevMachineConfig{}).FieldByName(field)
		require.True(t, ok, field)
		image := structField.Tag.Get("default")
		require.NotEmpty(t, image, field)
		return image
	}
	for field, provider := range map[string]Provider{
		"ClaudeCodeImage": NewClaudeCodeProvider(""),
		"OpenCodeImage":   NewOpenCodeProvider(""),
		"CodexImage":      NewCodexProvider(""),
		"HeadlessImage":   NewGooseProvider(""),
	} {
		require.Equal(t, defaults(field), provider.Metadata().DefaultImage, field)
	}
}
