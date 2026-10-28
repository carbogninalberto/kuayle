// Package dockertls issues the mutual-TLS material of a Dev Machine's own
// Docker daemon: a per-machine CA (whose key is discarded), the daemon's
// server certificate and one client certificate.
package dockertls

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"time"
)

// Material is a machine's daemon TLS material: a per-machine CA, the
// daemon's server certificate and one client certificate, all PEM encoded.
// The CA key is discarded, so no further certificate can be issued.
type Material struct {
	CA         string
	ServerCert string
	ServerKey  string
	ClientCert string
	ClientKey  string
}

// New issues the TLS material of the daemon reachable as "docker"
// and "<routingKey>-docker" on the machine network, valid a day beyond the
// machine's expiry.
func New(routingKey string, expiresAt time.Time) (Material, error) {
	notBefore := time.Now().Add(-time.Hour)
	notAfter := expiresAt.Add(24 * time.Hour)
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return Material{}, err
	}
	caTemplate := &x509.Certificate{
		SerialNumber: randomSerial(), Subject: pkix.Name{CommonName: "Kuayle machine " + routingKey + " Docker CA"},
		NotBefore: notBefore, NotAfter: notAfter, IsCA: true, BasicConstraintsValid: true, MaxPathLenZero: true,
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		return Material{}, err
	}
	ca, err := x509.ParseCertificate(caDER)
	if err != nil {
		return Material{}, err
	}
	issue := func(name string, usage x509.ExtKeyUsage, dnsNames []string) (string, string, error) {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return "", "", err
		}
		template := &x509.Certificate{
			SerialNumber: randomSerial(), Subject: pkix.Name{CommonName: name}, NotBefore: notBefore, NotAfter: notAfter,
			KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{usage}, DNSNames: dnsNames,
		}
		der, err := x509.CreateCertificate(rand.Reader, template, ca, &key.PublicKey, caKey)
		if err != nil {
			return "", "", err
		}
		keyDER, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			return "", "", err
		}
		return encodePEM("CERTIFICATE", der), encodePEM("PRIVATE KEY", keyDER), nil
	}
	material := Material{CA: encodePEM("CERTIFICATE", caDER)}
	if material.ServerCert, material.ServerKey, err = issue("docker", x509.ExtKeyUsageServerAuth, []string{"docker", routingKey + "-docker", "localhost"}); err != nil {
		return Material{}, err
	}
	if material.ClientCert, material.ClientKey, err = issue("kuayle-machine-client", x509.ExtKeyUsageClientAuth, nil); err != nil {
		return Material{}, err
	}
	return material, nil
}

func randomSerial() *big.Int {
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		return big.NewInt(time.Now().UnixNano())
	}
	return serial
}

func encodePEM(kind string, der []byte) string {
	return string(pem.EncodeToMemory(&pem.Block{Type: kind, Bytes: der}))
}
