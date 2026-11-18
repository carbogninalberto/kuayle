package service

import (
	"context"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/crewjam/saml"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/pkg/validate"
	dsig "github.com/russellhaering/goxmldsig"
)

const maxMetadataSize = 256 << 10

// LoadSAMLSigningKey loads an optional RSA credential at server startup.
func (s *SSOService) LoadSAMLSigningKey(certFile, keyFile string) error {
	if certFile == "" && keyFile == "" {
		return nil
	}
	pair, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return errors.New("could not load SAML SP certificate and signing key")
	}
	key, ok := pair.PrivateKey.(*rsa.PrivateKey)
	if !ok || key.N.BitLen() < 2048 {
		return errors.New("SAML SP signing requires an RSA key of at least 2048 bits")
	}
	cert, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return errors.New("invalid SAML SP signing certificate")
	}
	s.samlKey, s.samlCert = key, cert
	return nil
}

func (s *SSOService) samlProvider(slug, metadata string) (*saml.ServiceProvider, error) {
	acs, err := url.Parse(s.redirectURI(slug))
	if err != nil {
		return nil, ErrSSOTokenInvalid
	}
	entity, err := url.Parse(s.frontendURL + "/api/auth/sso/" + url.PathEscape(slug) + "/metadata")
	if err != nil {
		return nil, ErrSSOTokenInvalid
	}
	sp := &saml.ServiceProvider{
		EntityID:                         entity.String(),
		MetadataURL:                      *entity,
		AcsURL:                           *acs,
		AuthnNameIDFormat:                saml.PersistentNameIDFormat,
		AssertionConsumerServiceBindings: []string{saml.HTTPPostBinding},
		RejectUnknownConditions:          true,
	}
	if s.samlKey != nil {
		sp.Key, sp.Certificate, sp.SignatureMethod = s.samlKey, s.samlCert, dsig.RSASHA256SignatureMethod
	}
	if metadata != "" {
		var idp saml.EntityDescriptor
		if xml.Unmarshal([]byte(metadata), &idp) != nil {
			return nil, ErrSSOTokenInvalid
		}
		sp.IDPMetadata = &idp
	}
	return sp, nil
}

func (s *SSOService) SAMLMetadata(ctx context.Context, slug string) ([]byte, error) {
	ws, err := s.workspace(ctx, slug)
	if err != nil {
		return nil, err
	}
	sp, err := s.samlProvider(ws.Slug, "")
	if err != nil {
		return nil, err
	}
	md := sp.Metadata()
	// Only plaintext, signed assertions are supported by the ACS.
	role := &md.SPSSODescriptors[0]
	role.KeyDescriptors = slices.DeleteFunc(role.KeyDescriptors, func(k saml.KeyDescriptor) bool { return k.Use != "signing" })
	return xml.Marshal(md)
}

func (s *SSOService) readIDPMetadata(ctx context.Context, req dto.SSOConfigRequest) (string, error) {
	if (req.MetadataXML == "") == (req.MetadataURL == "") {
		return "", ssoInvalidf("Supply IdP metadata XML or a metadata URL")
	}
	data := []byte(req.MetadataXML)
	if req.MetadataURL != "" {
		u, err := validate.OperatorBaseURL(req.MetadataURL)
		if err != nil || (!s.allowPrivate && u.Scheme != "https") {
			return "", ssoInvalidf("Invalid metadata URL")
		}
		get, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return "", ssoInvalidf("Invalid metadata URL")
		}
		resp, err := s.httpClient.Do(get)
		if err != nil {
			return "", ssoInvalidf("Could not fetch IdP metadata")
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return "", ssoInvalidf("Could not fetch IdP metadata")
		}
		data, err = io.ReadAll(io.LimitReader(resp.Body, maxMetadataSize+1))
		if err != nil {
			return "", ssoInvalidf("Could not read IdP metadata")
		}
	}
	var idp saml.EntityDescriptor
	if len(data) > maxMetadataSize || xml.Unmarshal(data, &idp) != nil || idp.EntityID == "" || len(idp.IDPSSODescriptors) != 1 {
		return "", ssoInvalidf("Invalid IdP metadata")
	}
	role := &idp.IDPSSODescriptors[0]
	sp := saml.ServiceProvider{IDPMetadata: &idp}
	location := sp.GetSSOBindingLocation(saml.HTTPRedirectBinding)
	u, err := validate.OperatorBaseURL(location)
	if err != nil || (!s.allowPrivate && u.Scheme != "https") {
		return "", ssoInvalidf("Invalid IdP HTTP-Redirect endpoint")
	}
	count := 0
	for _, k := range role.KeyDescriptors {
		if k.Use != "" && k.Use != "signing" {
			continue
		}
		for _, cert := range k.KeyInfo.X509Data.X509Certificates {
			der, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(cert.Data), ""))
			if err != nil {
				return "", ssoInvalidf("Invalid IdP signing certificate")
			}
			if _, err = x509.ParseCertificate(der); err != nil {
				return "", ssoInvalidf("Invalid IdP signing certificate")
			}
			count++
		}
	}
	if count == 0 {
		return "", ssoInvalidf("IdP metadata needs a signing certificate")
	}
	return string(data), nil
}

func (s *SSOService) startSAML(slug, metadata, state string) (string, string, error) {
	sp, err := s.samlProvider(slug, metadata)
	if err != nil {
		return "", "", err
	}
	req, err := sp.MakeAuthenticationRequest(sp.GetSSOBindingLocation(saml.HTTPRedirectBinding), saml.HTTPRedirectBinding, saml.HTTPPostBinding)
	if err != nil {
		return "", "", ErrSSOTokenInvalid
	}
	target, err := req.Redirect(state, sp)
	if err != nil {
		return "", "", ErrSSOTokenInvalid
	}
	return target.String(), req.ID, nil
}

func (s *SSOService) verifySAML(slug, metadata, requestID, encoded string) (*ssoClaims, error) {
	sp, err := s.samlProvider(slug, metadata)
	if err != nil {
		return nil, err
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, ErrSSOTokenInvalid
	}
	// The library also permits a signed envelope containing unsigned assertions.
	// Require exactly one plaintext assertion with its own XML signature before
	// asking the library to verify it and extract the signed claims.
	var envelope struct {
		XMLName    xml.Name `xml:"urn:oasis:names:tc:SAML:2.0:protocol Response"`
		Assertions []struct {
			Signature []struct{} `xml:"http://www.w3.org/2000/09/xmldsig# Signature"`
		} `xml:"urn:oasis:names:tc:SAML:2.0:assertion Assertion"`
		Encrypted []struct{} `xml:"urn:oasis:names:tc:SAML:2.0:assertion EncryptedAssertion"`
	}
	if xml.Unmarshal(data, &envelope) != nil || len(envelope.Assertions) != 1 || len(envelope.Assertions[0].Signature) != 1 || len(envelope.Encrypted) != 0 {
		return nil, ErrSSOTokenInvalid
	}
	a, err := sp.ParseXMLResponse(data, []string{requestID}, sp.AcsURL)
	if err != nil || a.Subject.NameID == nil || strings.TrimSpace(a.Subject.NameID.Value) == "" {
		return nil, ErrSSOTokenInvalid
	}
	// Bind the signed assertion to exactly one request, even when the outer
	// response is unsigned. Strict deadlines also cover the library's skew allowance.
	if len(a.Subject.SubjectConfirmations) != 1 {
		return nil, ErrSSOTokenInvalid
	}
	bearer := a.Subject.SubjectConfirmations[0].SubjectConfirmationData
	now := s.now()
	if bearer == nil || bearer.InResponseTo != requestID || !now.Before(bearer.NotOnOrAfter) || !now.Before(a.Conditions.NotOnOrAfter) {
		return nil, ErrSSOTokenInvalid
	}
	// OIDC issuers are HTTP(S) URLs. Namespace SAML entities so a protocol
	// switch cannot reinterpret an OIDC subject as a SAML NameID.
	claims := &ssoClaims{Provider: domain.ProviderSAML, Issuer: "saml:" + a.Issuer.Value, Subject: a.Subject.NameID.Value}
	for _, statement := range a.AttributeStatements {
		for _, attr := range statement.Attributes {
			if len(attr.Values) != 1 {
				continue
			}
			switch attr.Name {
			case "email":
				claims.Email = attr.Values[0].Value
			case "email_verified":
				claims.EmailVerified = attr.Values[0].Value
			case "name":
				claims.Name = attr.Values[0].Value
			}
		}
	}
	return claims, nil
}
