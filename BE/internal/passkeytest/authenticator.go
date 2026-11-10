// Package passkeytest is a software WebAuthn authenticator and client for
// tests. It answers the options the server sends the way a browser and an
// authenticator would, with knobs for the misbehaviour the server must refuse.
package passkeytest

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"

	"github.com/fxamacker/cbor/v2"
)

const (
	flagUserPresent    = 0x01
	flagUserVerified   = 0x04
	flagBackupEligible = 0x08
	flagBackupState    = 0x10
	flagAttested       = 0x40
)

// Authenticator holds at most one discoverable credential.
type Authenticator struct {
	// Origin is what the client reports in clientDataJSON; RPID is hashed
	// into the authenticator data. Both default to the server's options.
	Origin string
	RPID   string
	AAGUID [16]byte
	// Attestation is "none" (default) or "packed" for self attestation.
	Attestation string
	// UserVerified is whether the user passes verification (PIN, biometric).
	UserVerified bool
	// Synced marks a multi-device passkey: backed up and with no counter.
	Synced bool
	// OmitUserHandle leaves the user handle out of assertions.
	OmitUserHandle bool

	CredentialID []byte
	UserHandle   []byte
	// SignCount is the counter of the last signature; it increases with
	// every assertion unless the passkey is Synced.
	SignCount uint32
	key       *ecdsa.PrivateKey
}

// New returns an authenticator used from origin that verifies its user.
func New(origin string) *Authenticator {
	a := &Authenticator{Origin: origin, UserVerified: true}
	_, _ = rand.Read(a.AAGUID[:])
	return a
}

// Clone returns a copy sharing the credential's private key, as a cloned
// hardware key would.
func (a *Authenticator) Clone() *Authenticator {
	clone := *a
	return &clone
}

type options struct {
	PublicKey struct {
		Challenge string `json:"challenge"`
		RPID      string `json:"rpId"`
		RP        struct {
			ID string `json:"id"`
		} `json:"rp"`
		User struct {
			ID string `json:"id"`
		} `json:"user"`
	} `json:"publicKey"`
}

func parseOptions(optionsJSON []byte) (*options, error) {
	var o options
	if err := json.Unmarshal(optionsJSON, &o); err != nil {
		return nil, err
	}
	if o.PublicKey.Challenge == "" {
		return nil, fmt.Errorf("options carry no challenge: %s", optionsJSON)
	}
	return &o, nil
}

func b64(data []byte) string { return base64.RawURLEncoding.EncodeToString(data) }

func (a *Authenticator) clientData(ceremony, challenge string) []byte {
	data, _ := json.Marshal(map[string]any{"type": ceremony, "challenge": challenge, "origin": a.Origin, "crossOrigin": false})
	return data
}

func (a *Authenticator) flags() byte {
	flags := byte(flagUserPresent)
	if a.UserVerified {
		flags |= flagUserVerified
	}
	if a.Synced {
		flags |= flagBackupEligible | flagBackupState
	}
	return flags
}

func (a *Authenticator) authData(rpID string, flags byte) []byte {
	hash := sha256.Sum256([]byte(rpID))
	data := append([]byte{}, hash[:]...)
	data = append(data, flags)
	return binary.BigEndian.AppendUint32(data, a.SignCount)
}

func (a *Authenticator) sign(authData, clientData []byte) ([]byte, error) {
	clientHash := sha256.Sum256(clientData)
	digest := sha256.Sum256(append(append([]byte{}, authData...), clientHash[:]...))
	return ecdsa.SignASN1(rand.Reader, a.key, digest[:])
}

// Create answers navigator.credentials.create: it makes a new discoverable
// credential and returns the PublicKeyCredential JSON for the server.
func (a *Authenticator) Create(optionsJSON []byte) ([]byte, error) {
	o, err := parseOptions(optionsJSON)
	if err != nil {
		return nil, err
	}
	rpID := a.RPID
	if rpID == "" {
		rpID = o.PublicKey.RP.ID
	}
	if a.key, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader); err != nil {
		return nil, err
	}
	a.CredentialID = make([]byte, 32)
	_, _ = rand.Read(a.CredentialID)
	if a.UserHandle, err = base64.RawURLEncoding.DecodeString(o.PublicKey.User.ID); err != nil {
		return nil, err
	}
	a.SignCount = 0

	encoder, err := cbor.CTAP2EncOptions().EncMode()
	if err != nil {
		return nil, err
	}
	x, y := make([]byte, 32), make([]byte, 32)
	a.key.X.FillBytes(x)
	a.key.Y.FillBytes(y)
	coseKey, err := encoder.Marshal(map[int]any{1: 2, 3: -7, -1: 1, -2: x, -3: y})
	if err != nil {
		return nil, err
	}
	authData := a.authData(rpID, a.flags()|flagAttested)
	authData = append(authData, a.AAGUID[:]...)
	authData = binary.BigEndian.AppendUint16(authData, uint16(len(a.CredentialID)))
	authData = append(authData, a.CredentialID...)
	authData = append(authData, coseKey...)

	clientData := a.clientData("webauthn.create", o.PublicKey.Challenge)
	format, statement := "none", map[string]any{}
	if a.Attestation == "packed" {
		signature, err := a.sign(authData, clientData)
		if err != nil {
			return nil, err
		}
		format, statement = "packed", map[string]any{"alg": -7, "sig": signature}
	}
	attestation, err := encoder.Marshal(map[string]any{"fmt": format, "attStmt": statement, "authData": authData})
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{
		"id": b64(a.CredentialID), "rawId": b64(a.CredentialID), "type": "public-key",
		"authenticatorAttachment": "platform",
		"response": map[string]any{
			"clientDataJSON":    b64(clientData),
			"attestationObject": b64(attestation),
			"transports":        []string{"internal"},
		},
		"clientExtensionResults": map[string]any{},
	})
}

// Get answers navigator.credentials.get with an assertion by the credential.
func (a *Authenticator) Get(optionsJSON []byte) ([]byte, error) {
	if a.key == nil {
		return nil, fmt.Errorf("authenticator holds no credential")
	}
	o, err := parseOptions(optionsJSON)
	if err != nil {
		return nil, err
	}
	rpID := a.RPID
	if rpID == "" {
		rpID = o.PublicKey.RPID
	}
	if !a.Synced {
		a.SignCount++
	}
	authData := a.authData(rpID, a.flags())
	clientData := a.clientData("webauthn.get", o.PublicKey.Challenge)
	signature, err := a.sign(authData, clientData)
	if err != nil {
		return nil, err
	}
	response := map[string]any{
		"clientDataJSON":    b64(clientData),
		"authenticatorData": b64(authData),
		"signature":         b64(signature),
	}
	if !a.OmitUserHandle {
		response["userHandle"] = b64(a.UserHandle)
	}
	return json.Marshal(map[string]any{
		"id": b64(a.CredentialID), "rawId": b64(a.CredentialID), "type": "public-key",
		"authenticatorAttachment": "platform",
		"response":                response,
		"clientExtensionResults":  map[string]any{},
	})
}
