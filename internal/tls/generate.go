package tls

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

const (
	defaultCertDir  = "./certs"
	defaultCertFile = "cert.pem"
	defaultKeyFile  = "key.pem"
	certExpiry      = 10 * 365 * 24 * time.Hour // 10 years
	rsaKeyBits      = 2048
)

// GenerateSelfSigned creates a self-signed certificate and key.
// Returns paths to cert and key files.
func GenerateSelfSigned(certDir string) (certPath, keyPath string, err error) {
	if certDir == "" {
		certDir = defaultCertDir
	}

	certPath = filepath.Join(certDir, defaultCertFile)
	keyPath = filepath.Join(certDir, defaultKeyFile)

	if err := os.MkdirAll(certDir, 0755); err != nil {
		return "", "", fmt.Errorf("create cert dir: %w", err)
	}

	privateKey, err := rsa.GenerateKey(rand.Reader, rsaKeyBits)
	if err != nil {
		return "", "", fmt.Errorf("generate key: %w", err)
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return "", "", fmt.Errorf("generate serial: %w", err)
	}

	now := time.Now()
	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"anthropic-proxy"},
			CommonName:   "localhost",
		},
		NotBefore:             now,
		NotAfter:              now.Add(certExpiry),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
		DNSNames:              []string{"localhost"},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return "", "", fmt.Errorf("create cert: %w", err)
	}

	certFile, err := os.Create(certPath)
	if err != nil {
		return "", "", fmt.Errorf("create cert file: %w", err)
	}
	defer certFile.Close()

	if err := pem.Encode(certFile, &pem.Block{Type: "CERTIFICATE", Bytes: certDER}); err != nil {
		return "", "", fmt.Errorf("encode cert: %w", err)
	}

	keyFile, err := os.Create(keyPath)
	if err != nil {
		return "", "", fmt.Errorf("create key file: %w", err)
	}
	defer keyFile.Close()

	keyDER := x509.MarshalPKCS1PrivateKey(privateKey)
	if err := pem.Encode(keyFile, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: keyDER}); err != nil {
		return "", "", fmt.Errorf("encode key: %w", err)
	}

	return certPath, keyPath, nil
}
