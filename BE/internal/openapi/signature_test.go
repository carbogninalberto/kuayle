package openapi

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/pkg/crypto"
	"github.com/stretchr/testify/require"
)

// The guide's verification sample must accept a body signed the way
// WebhookService.Dispatch signs it, and reject a tampered one.
func TestSignatureSampleVerifiesARealDelivery(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	body, err := json.Marshal(dto.WebhookDelivery{Event: "issue.created", Payload: map[string]string{"identifier": "ENG-1"}, Timestamp: "2026-11-10T00:00:00Z"})
	require.NoError(t, err)
	secret := "a-webhook-secret"
	header := "sha256=" + crypto.HMACSha256(body, secret)

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "verify.mjs"), []byte(signatureSample), 0o600))
	script := `import { isFromKuayle } from './verify.mjs';
const [body, header, secret] = process.argv.slice(2);
const raw = Buffer.from(body, 'base64');
if (!isFromKuayle(raw, header, secret)) process.exit(3);
if (isFromKuayle(Buffer.concat([raw, Buffer.from(' ')]), header, secret)) process.exit(4);
if (isFromKuayle(raw, header, secret + 'x')) process.exit(5);
if (isFromKuayle(raw, undefined, secret)) process.exit(6);`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "run.mjs"), []byte(script), 0o600))
	out, err := exec.Command(node, filepath.Join(dir, "run.mjs"), base64.StdEncoding.EncodeToString(body), header, secret).CombinedOutput()
	require.NoError(t, err, string(out))
}
