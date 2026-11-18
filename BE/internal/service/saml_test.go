package service

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"encoding/xml"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beevik/etree"
	"github.com/crewjam/saml"
	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/stretchr/testify/require"
)

func TestSAMLMetadata(t *testing.T) {
	rig := newSSOTestRig(t)
	data, err := rig.service.SAMLMetadata(t.Context(), "acme")
	require.NoError(t, err)
	var metadata saml.EntityDescriptor
	require.NoError(t, xml.Unmarshal(data, &metadata))
	require.Equal(t, "https://app.test/api/auth/sso/acme/metadata", metadata.EntityID)
	sp := metadata.SPSSODescriptors[0]
	require.True(t, *sp.WantAssertionsSigned)
	require.Len(t, sp.AssertionConsumerServices, 1)
	require.Equal(t, saml.HTTPPostBinding, sp.AssertionConsumerServices[0].Binding)
	require.Equal(t, "https://app.test/api/auth/sso/acme/callback", sp.AssertionConsumerServices[0].Location)
}

func TestSAMLSignIn(t *testing.T) {
	tls := httptest.NewTLSServer(nil)
	defer tls.Close()
	u, err := url.Parse(tls.URL)
	require.NoError(t, err)
	idp := saml.IdentityProvider{Key: tls.TLS.Certificates[0].PrivateKey, Certificate: tls.Certificate(), MetadataURL: *u, SSOURL: *u}
	metadata, err := xml.Marshal(idp.Metadata())
	require.NoError(t, err)
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(metadata) }))
	defer source.Close()
	for _, scenario := range []string{"valid", "unsigned envelope", "missing NameID", "empty issuer", "missing audience", "unsigned assertion in signed response", "wrong audience", "expired", "replayed", "other workspace", "different request", "untrusted certificate", "untrusted assertion in trusted response", "unverified email"} {
		t.Run(scenario, func(t *testing.T) {
			rig := newSSOTestRig(t)
			cfg := dto.SSOConfigRequest{Protocol: "saml", MetadataXML: string(metadata), Enabled: true, DefaultRole: "member"}
			if scenario == "valid" {
				cfg.MetadataURL = source.URL
				cfg.MetadataXML = ""
			}
			saved, err := rig.service.SaveConfig(t.Context(), "acme", cfg)
			require.NoError(t, err)
			safe, err := json.Marshal(saved)
			require.NoError(t, err)
			require.NotContains(t, string(safe), "Certificate")
			require.False(t, saved.HasClientSecret)
			target, bind, err := rig.service.Start(t.Context(), "acme", "/acme")
			require.NoError(t, err)
			location, err := url.Parse(target)
			require.NoError(t, err)
			state := location.Query().Get("RelayState")
			sp, err := rig.service.samlProvider("acme", string(metadata))
			require.NoError(t, err)
			desc := sp.Metadata()
			role := &desc.SPSSODescriptors[0]
			request := &saml.IdpAuthnRequest{IDP: &idp, Now: time.Now().UTC(), HTTPRequest: httptest.NewRequest("GET", target, nil), ServiceProviderMetadata: desc, SPSSODescriptor: role, ACSEndpoint: &role.AssertionConsumerServices[0], Request: saml.AuthnRequest{ID: rig.repo.states[hashValue(state)].Nonce}}
			verified := "true"
			if scenario == "unverified email" {
				verified = "false"
			}
			session := &saml.Session{NameID: "user-1", CreateTime: request.Now, CustomAttributes: []saml.Attribute{
				{Name: "email", Values: []saml.AttributeValue{{Value: "person@example.com", Type: "xs:string"}}},
				{Name: "email_verified", Values: []saml.AttributeValue{{Value: verified, Type: "xs:string"}}},
			}}
			require.NoError(t, (saml.DefaultAssertionMaker{}).MakeAssertion(request, session))
			switch scenario {
			case "missing NameID":
				request.Assertion.Subject.NameID = nil
			case "empty issuer":
				request.Assertion.Issuer.Value = ""
			case "missing audience":
				request.Assertion.Conditions.AudienceRestrictions = nil
			case "wrong audience":
				request.Assertion.Conditions.AudienceRestrictions[0].Audience.Value = "another-sp"
			case "expired":
				request.Assertion.Conditions.NotOnOrAfter = request.Now.Add(-time.Second)
			case "unsigned assertion in signed response":
				request.AssertionEl = request.Assertion.Element()
			case "different request":
				request.Assertion.Subject.SubjectConfirmations[0].SubjectConfirmationData.InResponseTo = "other-id"
			case "untrusted certificate", "untrusted assertion in trusted response":
				other := idp
				key, cert := testSAMLCertificate(t)
				other.Key = key
				other.Certificate = cert
				request.IDP = &other
				if scenario == "untrusted assertion in trusted response" {
					require.NoError(t, request.MakeAssertionEl())
					request.IDP = &idp
				}
			}
			require.NoError(t, request.MakeResponse())
			if scenario == "unsigned envelope" {
				signature := request.ResponseEl.SelectElement("ds:Signature")
				require.NotNil(t, signature)
				request.ResponseEl.RemoveChild(signature)
			}
			doc := etree.NewDocument()
			doc.SetRoot(request.ResponseEl)
			data, err := doc.WriteToBytes()
			require.NoError(t, err)
			encoded := base64.StdEncoding.EncodeToString(data)
			slug := "acme"
			if scenario == "other workspace" {
				slug = "other"
				ws := &domain.Workspace{ID: uuid.New(), Slug: slug}
				rig.byslug[slug] = ws
				_, err = rig.service.SaveConfig(t.Context(), slug, cfg)
				require.NoError(t, err)
			}
			callback := func() error {
				_, _, _, _, err := rig.service.SAMLCallback(t.Context(), slug, state, encoded, bind)
				return err
			}
			switch scenario {
			case "valid", "unsigned envelope", "replayed":
				require.NoError(t, callback())
				require.Equal(t, domain.ProviderSAML, rig.repo.provisioned[0].Provider)
				require.Equal(t, rig.workspace.ID, *rig.refresh.created[0].SSOWorkspaceID)
				if scenario == "replayed" {
					require.ErrorIs(t, callback(), ErrSSOStateInvalid)
					fresh, newBind, err := rig.service.Start(t.Context(), slug, "")
					require.NoError(t, err)
					next, err := url.Parse(fresh)
					require.NoError(t, err)
					_, _, _, _, err = rig.service.SAMLCallback(t.Context(), slug, next.Query().Get("RelayState"), encoded, newBind)
					require.ErrorIs(t, err, ErrSSOTokenInvalid)
					require.Len(t, rig.refresh.created, 1)
				}
			default:
				require.Error(t, callback())
				require.Empty(t, rig.refresh.created)
				require.Empty(t, rig.repo.provisioned)
			}
		})
	}
}

func testSAMLCertificate(t *testing.T) (*rsa.PrivateKey, *x509.Certificate) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	template := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return key, cert
}

func TestSAMLSigningCredential(t *testing.T) {
	rig := newSSOTestRig(t)
	key, cert := testSAMLCertificate(t)
	dir := t.TempDir()
	certFile, keyFile := filepath.Join(dir, "sp.crt"), filepath.Join(dir, "sp.key")
	require.NoError(t, os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}), 0600))
	secret := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	require.NoError(t, os.WriteFile(keyFile, secret, 0600))
	require.NoError(t, rig.service.LoadSAMLSigningKey(certFile, keyFile))
	data, err := rig.service.SAMLMetadata(t.Context(), "acme")
	require.NoError(t, err)
	require.NotContains(t, string(data), "PRIVATE KEY")
	require.NotContains(t, string(data), base64.StdEncoding.EncodeToString(x509.MarshalPKCS1PrivateKey(key)))
	var md saml.EntityDescriptor
	require.NoError(t, xml.Unmarshal(data, &md))
	role := md.SPSSODescriptors[0]
	require.True(t, *role.AuthnRequestsSigned)
	require.Len(t, role.KeyDescriptors, 1)
	require.Equal(t, "signing", role.KeyDescriptors[0].Use)
	require.Equal(t, base64.StdEncoding.EncodeToString(cert.Raw), role.KeyDescriptors[0].KeyInfo.X509Data.X509Certificates[0].Data)
	require.Error(t, rig.service.LoadSAMLSigningKey(certFile, ""))
}

func TestSAMLMetadataImportFailuresPreserveConfig(t *testing.T) {
	rig := newSSOTestRig(t)
	calls := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(http.StatusOK) }))
	defer target.Close()
	redirect := httptest.NewServer(http.RedirectHandler(target.URL, http.StatusFound))
	defer redirect.Close()
	for _, source := range []dto.SSOConfigRequest{
		{MetadataXML: "<EntityDescriptor/>"},
		{MetadataURL: redirect.URL},
		{MetadataXML: "<xml/>", MetadataURL: target.URL},
		{MetadataXML: strings.Repeat("a", maxMetadataSize+1)},
		{MetadataURL: "https://example.test/path?secret=canary"},
	} {
		source.Protocol = "saml"
		source.DefaultRole = "member"
		source.Enabled = true
		_, err := rig.service.SaveConfig(t.Context(), "acme", source)
		require.ErrorIs(t, err, ErrSSOInvalidInput)
		require.NotContains(t, err.Error(), "canary")
		require.Empty(t, rig.repo.configs[rig.workspace.ID].SAMLMetadata)
	}
	require.Zero(t, calls)
}
