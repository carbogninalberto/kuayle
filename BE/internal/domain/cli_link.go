package domain

import (
	"crypto/rand"
	"math/big"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

const (
	// CLILinkPrefix identifies one-time connection-link codes. Like the PAT
	// prefix it lets secret-scanning tools spot a leaked code.
	CLILinkPrefix = "kuayle_cl_"

	cliLinkRandomLength = 40
)

// CLIConnectionLink is a short-lived, revocable secret that mints a personal
// access token for kuayle_cli. Only the code's SHA-256 hash is stored; the
// link itself is shown once when it is created.
type CLIConnectionLink struct {
	ID             uuid.UUID      `json:"id" db:"id"`
	UserID         uuid.UUID      `json:"user_id" db:"user_id"`
	Name           string         `json:"name" db:"name"`
	CodeHash       string         `json:"-" db:"code_hash"`
	CodePrefix     string         `json:"code_prefix" db:"code_prefix"`
	Scopes         pq.StringArray `json:"scopes" db:"scopes"`
	WorkspaceSlugs pq.StringArray `json:"workspace_slugs" db:"workspace_slugs"` // nil = all workspaces
	TokenExpiresAt *time.Time     `json:"token_expires_at" db:"token_expires_at"`
	ExpiresAt      time.Time      `json:"expires_at" db:"expires_at"`
	MaxUses        int            `json:"max_uses" db:"max_uses"`
	Uses           int            `json:"uses" db:"uses"`
	RevokedAt      *time.Time     `json:"revoked_at" db:"revoked_at"`
	CreatedAt      time.Time      `json:"created_at" db:"created_at"`
}

// GenerateCLILinkCode creates a new connection-link code. The plaintext is
// returned once and never stored; only its SHA-256 hash persists.
func GenerateCLILinkCode() (plaintext, hash, prefix string, err error) {
	random, err := randomString(cliLinkRandomLength)
	if err != nil {
		return "", "", "", err
	}
	plaintext = CLILinkPrefix + random
	return plaintext, HashToken(plaintext), CLILinkPrefix + random[:tokenPrefixVisible], nil
}

func randomString(length int) (string, error) {
	var b strings.Builder
	b.Grow(length)
	max := big.NewInt(int64(len(tokenAlphabet)))
	for i := 0; i < length; i++ {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		b.WriteByte(tokenAlphabet[n.Int64()])
	}
	return b.String(), nil
}

var secretPattern = regexp.MustCompile(`kuayle_(?:pat|cl)_[A-Za-z0-9]+`)

// RedactSecrets masks Kuayle token and connection-link codes in a message,
// keeping only the recognizable prefix and its 8 display characters.
func RedactSecrets(value string) string {
	return secretPattern.ReplaceAllStringFunc(value, func(match string) string {
		keep := len(TokenPrefix) + tokenPrefixVisible
		if strings.HasPrefix(match, CLILinkPrefix) {
			keep = len(CLILinkPrefix) + tokenPrefixVisible
		}
		if len(match) <= keep {
			return match
		}
		return match[:keep] + "…"
	})
}
