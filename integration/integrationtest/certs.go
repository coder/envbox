package integrationtest

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func GenerateTLSCertificate(t testing.TB, commonName string, ipAddr string) tls.Certificate {
	t.Helper()

	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Acme Co"},
			CommonName:   commonName,
		},
		DNSNames:  []string{commonName},
		NotBefore: time.Now(),
		NotAfter:  time.Now().Add(time.Hour * 24 * 180),

		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP(ipAddr)},
		IsCA:                  true,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	require.NoError(t, err)
	var certFile bytes.Buffer
	require.NoError(t, err)
	_, err = certFile.Write(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes}))
	require.NoError(t, err)
	privateKeyBytes, err := x509.MarshalPKCS8PrivateKey(privateKey)
	require.NoError(t, err)
	var keyFile bytes.Buffer
	err = pem.Encode(&keyFile, &pem.Block{Type: "PRIVATE KEY", Bytes: privateKeyBytes})
	require.NoError(t, err)
	cert, err := tls.X509KeyPair(certFile.Bytes(), keyFile.Bytes())
	require.NoError(t, err)
	return cert
}

func writePEM(t testing.TB, path string, typ string, contents []byte) {
	t.Helper()

	f, err := os.Create(path)
	require.NoError(t, err)
	defer f.Close()

	err = pem.Encode(f, &pem.Block{
		Type:  typ,
		Bytes: contents,
	})
	require.NoError(t, err)
}

func WriteCertificate(t testing.TB, c tls.Certificate, certPath, keyPath string) {
	require.Len(t, c.Certificate, 1, "expecting 1 certificate")
	key, err := x509.MarshalPKCS8PrivateKey(c.PrivateKey)
	require.NoError(t, err)

	cert := c.Certificate[0]

	writePEM(t, keyPath, "PRIVATE KEY", key)
	writePEM(t, certPath, "CERTIFICATE", cert)
}
