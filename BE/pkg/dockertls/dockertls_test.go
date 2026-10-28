package dockertls

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func parseCertificate(t *testing.T, value string) *x509.Certificate {
	t.Helper()
	block, _ := pem.Decode([]byte(value))
	require.NotNil(t, block)
	certificate, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)
	return certificate
}

// The daemon and its clients authenticate each other with certificates of a
// per-machine CA: the server certificate names only the daemon's hosts, the
// client certificate is only valid for client authentication, and another
// machine's certificates are not accepted.
func TestDockerTLSIsMutualAndScopedToTheMachine(t *testing.T) {
	expires := time.Now().Add(2 * time.Hour)
	material, err := New("0123456789abcdef0123", expires)
	require.NoError(t, err)
	other, err := New("fedcba98765432100123", expires)
	require.NoError(t, err)

	roots := x509.NewCertPool()
	require.True(t, roots.AppendCertsFromPEM([]byte(material.CA)))
	server := parseCertificate(t, material.ServerCert)
	client := parseCertificate(t, material.ClientCert)
	ca := parseCertificate(t, material.CA)
	require.True(t, ca.IsCA)
	require.ElementsMatch(t, []string{"docker", "0123456789abcdef0123-docker", "localhost"}, server.DNSNames)
	require.False(t, server.NotAfter.Before(expires), "valid while the machine may run")
	for _, host := range []string{"docker", "0123456789abcdef0123-docker"} {
		_, err = server.Verify(x509.VerifyOptions{Roots: roots, DNSName: host, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}})
		require.NoError(t, err, host)
	}
	_, err = server.Verify(x509.VerifyOptions{Roots: roots, DNSName: "fedcba98765432100123-docker", KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}})
	require.Error(t, err, "another machine's daemon host")
	_, err = client.Verify(x509.VerifyOptions{Roots: roots, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}})
	require.NoError(t, err)
	_, err = client.Verify(x509.VerifyOptions{Roots: roots, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}})
	require.Error(t, err, "a client certificate cannot serve")
	_, err = parseCertificate(t, other.ClientCert).Verify(x509.VerifyOptions{Roots: roots, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}})
	require.Error(t, err, "another machine's client is rejected")
	_, err = tls.X509KeyPair([]byte(material.ServerCert), []byte(material.ServerKey))
	require.NoError(t, err)
	_, err = tls.X509KeyPair([]byte(material.ClientCert), []byte(material.ClientKey))
	require.NoError(t, err)
}
