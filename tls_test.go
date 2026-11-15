package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeTestCertificate(t *testing.T) (certFile, keyFile string, roots *x509.CertPool) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "respkv test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1)},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}

	dir := t.TempDir()
	certFile = filepath.Join(dir, "cert.pem")
	keyFile = filepath.Join(dir, "key.pem")
	writePEM(t, certFile, "CERTIFICATE", certDER)
	writePEM(t, keyFile, "PRIVATE KEY", keyDER)

	roots = x509.NewCertPool()
	roots.AddCert(cert)
	return certFile, keyFile, roots
}

func writePEM(t *testing.T, path, blockType string, der []byte) {
	t.Helper()

	data := pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: der})
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestLoadTLSConfig(t *testing.T) {
	certFile, keyFile, _ := writeTestCertificate(t)

	config, err := loadTLSConfig(certFile, keyFile)
	if err != nil {
		t.Fatalf("loadTLSConfig() err = %v", err)
	}
	if len(config.Certificates) != 1 {
		t.Errorf("config has %d certificates, want 1", len(config.Certificates))
	}
	if config.MinVersion != tls.VersionTLS12 {
		t.Errorf("MinVersion = %x, want TLS 1.2 (%x)", config.MinVersion, tls.VersionTLS12)
	}
}

func TestLoadTLSConfigWithoutFiles(t *testing.T) {
	config, err := loadTLSConfig("", "")
	if config != nil || err != nil {
		t.Errorf("loadTLSConfig(\"\", \"\") = %v, %v, want nil, nil", config, err)
	}
}

func TestLoadTLSConfigErrors(t *testing.T) {
	certFile, keyFile, _ := writeTestCertificate(t)
	missing := filepath.Join(t.TempDir(), "missing.pem")

	tests := []struct {
		name     string
		certFile string
		keyFile  string
		want     error
	}{
		{name: "only certificate", certFile: certFile, want: errTLSFlags},
		{name: "only key", keyFile: keyFile, want: errTLSFlags},
		{name: "missing certificate", certFile: missing, keyFile: keyFile, want: os.ErrNotExist},
		{name: "key in place of certificate", certFile: keyFile, keyFile: keyFile},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, err := loadTLSConfig(tt.certFile, tt.keyFile)
			if err == nil {
				t.Fatalf("loadTLSConfig() = %v, nil, want an error", config)
			}
			if tt.want != nil && !errors.Is(err, tt.want) {
				t.Errorf("loadTLSConfig() err = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestServerSpeaksTLS(t *testing.T) {
	certFile, keyFile, roots := writeTestCertificate(t)
	s := newTestServer()
	config, err := loadTLSConfig(certFile, keyFile)
	if err != nil {
		t.Fatalf("loadTLSConfig() err = %v", err)
	}
	s.tlsConfig = config
	addr, _ := startServer(t, s)

	dialer := &net.Dialer{Timeout: testTimeout}
	conn, err := tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{RootCAs: roots})
	if err != nil {
		t.Fatalf("TLS dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	if err := conn.SetDeadline(time.Now().Add(testTimeout)); err != nil {
		t.Fatalf("set deadline: %v", err)
	}

	assertReply(t, conn, "PING\r\n", "+PONG\r\n")
	assertReply(t, conn, "*3\r\n$3\r\nSET\r\n$1\r\nk\r\n$1\r\nv\r\n", "+OK\r\n")
	assertReply(t, conn, "GET k\r\n", "$1\r\nv\r\n")
}

func TestTLSServerClosesPlainTextConnection(t *testing.T) {
	certFile, keyFile, _ := writeTestCertificate(t)
	s := newTestServer()
	config, err := loadTLSConfig(certFile, keyFile)
	if err != nil {
		t.Fatalf("loadTLSConfig() err = %v", err)
	}
	s.tlsConfig = config
	addr, _ := startServer(t, s)
	conn := dial(t, addr)

	if _, err := io.WriteString(conn, "PING\r\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := io.ReadAll(conn)
	if errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatal("server kept the plain-text connection open")
	}
	if strings.Contains(string(got), "PONG") {
		t.Errorf("server answered a plain-text command: %q", got)
	}
}
