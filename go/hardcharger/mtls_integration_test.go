// Licensed under the Apache License, Version 2.0.

//go:build integration

package hardcharger

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// Client certificate sign-in (Chargewire §4.4) against hardcharger-server with
// --mtls-ca-cert and --mtls-certificate-sign-in: auth_type=client_certificate.

// certificateAuthority is a throwaway CA, written to PEM files, that issues a
// server certificate for 127.0.0.1 and client certificates with e-mail SANs.
type certificateAuthority struct {
	cert                                  *x509.Certificate
	key                                   *ecdsa.PrivateKey
	caPEM                                 string
	caFile, serverCertFile, serverKeyFile string
	serial                                atomic.Int64
}

func newCertificateAuthority(t *testing.T) *certificateAuthority {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "hardcharger-adbc mTLS test CA"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour), IsCA: true,
		BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, _ := x509.ParseCertificate(der)
	ca := &certificateAuthority{cert: cert, key: key, caPEM: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))}
	ca.serial.Store(1)
	dir := t.TempDir()
	write := func(name, content string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	serverCert, serverKey := ca.issue(t, "", true)
	ca.caFile, ca.serverCertFile, ca.serverKeyFile = write("ca.pem", ca.caPEM), write("server.pem", serverCert), write("server-key.pem", serverKey)
	return ca
}

// issue returns a certificate and its key (PEM): a server certificate for
// 127.0.0.1, or a client certificate whose identity is the e-mail SAN email.
func (ca *certificateAuthority) issue(t *testing.T, email string, server bool) (string, string) {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	template := &x509.Certificate{SerialNumber: big.NewInt(ca.serial.Add(1)), Subject: pkix.Name{CommonName: "client"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
	if server {
		template.Subject.CommonName = "localhost"
		template.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
		template.DNSNames, template.IPAddresses = []string{"localhost"}, []net.IP{net.ParseIP("127.0.0.1")}
	} else {
		template.EmailAddresses = []string{email}
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, _ := x509.MarshalPKCS8PrivateKey(key)
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}))
}

// mtlsServerArgs are the flags of a server that verifies client certificates
// against ca, plus extra.
func (ca *certificateAuthority) mtlsServerArgs(extra ...string) []string {
	return append([]string{"--tls-cert", ca.serverCertFile, "--tls-key", ca.serverKeyFile, "--mtls-ca-cert", ca.caFile}, extra...)
}

// certificateOptions sign in with cert/key alone: no user name or password.
func certificateOptions(ca *certificateAuthority, cert, key string) map[string]string {
	return map[string]string{adbc.OptionKeyUsername: "<unset>", adbc.OptionKeyPassword: "<unset>",
		OptionAuthType: AuthTypeClientCertificate, "adbc.chargewire.tls.root_certs": ca.caPEM,
		"adbc.chargewire.tls.client_cert": cert, "adbc.chargewire.tls.client_key": key}
}

const opsIdentity = "OPS@example.com" // configured spelling; the certificate says ops@example.com

func startCertificateServer(t *testing.T, ca *certificateAuthority) *testServer {
	return startServer(t, ca.mtlsServerArgs("--mtls-certificate-sign-in", "--mtls-admin-identities", opsIdentity)...)
}

func TestIntegrationClientCertificateSignIn(t *testing.T) {
	ca := newCertificateAuthority(t)
	server := startCertificateServer(t, ca)
	cert, key := ca.issue(t, "ops@example.com", false)
	cnxn := mustOpen(t, server, certificateOptions(ca, cert, key))
	// An e-mail identity matches ignoring case, and the session is the
	// configured identity's, signed in by certificate.
	if got := scalar(t, cnxn, "SELECT HARDCHARGER_USER()"); got != opsIdentity {
		t.Fatalf("signed in as %q", got)
	}
	if got := scalar(t, cnxn, "SELECT auth_method FROM hardcharger_sessions() WHERE is_current"); got != "client_certificate" {
		t.Fatalf("auth method %q", got)
	}
	// "mtls" is the same.
	opts := certificateOptions(ca, cert, key)
	opts[OptionAuthType] = "mtls"
	if got := scalar(t, mustOpen(t, server, opts), "SELECT HARDCHARGER_USER()"); got != opsIdentity {
		t.Fatalf("mtls: signed in as %q", got)
	}
}

// Split results and partitioned ingests open connections of their own; each
// presents the certificate the token is bound to.
func TestIntegrationClientCertificateSplitResultAndPartitionedIngest(t *testing.T) {
	ca := newCertificateAuthority(t)
	server := startCertificateServer(t, ca)
	cert, key := ca.issue(t, "ops@example.com", false)
	opts := certificateOptions(ca, cert, key)
	opts[OptionIngestParts] = "4"
	cnxn := mustOpen(t, server, opts)
	if n := partitionCount(t, cnxn, largeResult); n < 2 {
		t.Fatalf("%d partitions: the result was not split, so no partition connection was tested", n)
	}
	if got := scalar(t, cnxn, "SELECT count(*) FROM ("+largeResult+")"); got != "2000000" {
		t.Fatalf("count %s", got)
	}

	ctx := context.Background()
	schema := arrow.NewSchema([]arrow.Field{{Name: "k", Type: arrow.PrimitiveTypes.Int64}}, nil)
	var batches []arrow.RecordBatch
	for b := range 4 {
		builder := array.NewRecordBuilder(memory.DefaultAllocator, schema)
		for i := range 25000 {
			builder.Field(0).(*array.Int64Builder).Append(int64(b*25000 + i))
		}
		batches = append(batches, builder.NewRecordBatch())
		builder.Release()
	}
	reader, err := array.NewRecordReader(schema, batches)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Release()
	for _, b := range batches {
		b.Release()
	}
	stmt, err := cnxn.NewStatement(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close(ctx)
	if err := stmt.SetOption(ctx, adbc.OptionKeyIngestTargetTable, "certificate_parts"); err != nil {
		t.Fatal(err)
	}
	if err := stmt.BindStream(ctx, reader); err != nil {
		t.Fatal(err)
	}
	if n, err := stmt.ExecuteUpdate(ctx); err != nil || n != 100000 {
		t.Fatalf("ingested %d: %v", n, err)
	}
	// Exactly the rows sent, each once.
	if got := scalar(t, cnxn, "SELECT count(*) || ' ' || count(DISTINCT k) || ' ' || sum(k) FROM certificate_parts"); got != "100000 100000 4999950000" {
		t.Fatalf("count, distinct, sum: %s", got)
	}
}

func TestIntegrationClientCertificateUnknownIdentityIsRefused(t *testing.T) {
	ca := newCertificateAuthority(t)
	server := startCertificateServer(t, ca)
	cert, key := ca.issue(t, "stranger@example.com", false)
	_, _, err := open(t, server, certificateOptions(ca, cert, key))
	wantStatus(t, err, adbc.StatusUnauthenticated, "stranger@example.com")
	if code := chargewireCode(err); code != "unknown_certificate_identity" {
		t.Fatalf("chargewire.code %q: %v", code, err)
	}
	// The twin, ops@example.com, is TestIntegrationClientCertificateSignIn.
}

// With --mtls-ca-cert alone a certificate only admits the connection: the
// server does not offer the grant, and the driver says so before trying it.
func TestIntegrationClientCertificateSignInOffOnTheServer(t *testing.T) {
	ca := newCertificateAuthority(t)
	server := startServer(t, ca.mtlsServerArgs()...)
	cert, key := ca.issue(t, "ops@example.com", false)
	_, _, err := open(t, server, certificateOptions(ca, cert, key))
	wantStatus(t, err, adbc.StatusUnauthenticated, "does not offer client certificate sign-in (capability mtls_identity)")
	// The twin: a password sign-in over the same certificate.
	opts := certificateOptions(ca, cert, key)
	opts[OptionAuthType], opts[adbc.OptionKeyUsername], opts[adbc.OptionKeyPassword] = AuthTypePassword, testUsername, server.password
	if got := scalar(t, mustOpen(t, server, opts), "SELECT HARDCHARGER_USER()"); got != testUsername {
		t.Fatalf("signed in as %q", got)
	}
}

// certificateToken signs in with the client_certificate grant the way another
// tool would, returning the token bound to cert.
func certificateToken(t *testing.T, server *testServer, ca *certificateAuthority, cert, key string) string {
	t.Helper()
	pair, err := tls.X509KeyPair([]byte(cert), []byte(key))
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM([]byte(ca.caPEM))
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, Certificates: []tls.Certificate{pair}}}}
	defer client.CloseIdleConnections()
	resp, err := client.Post("https://127.0.0.1:"+strconv.Itoa(server.port)+"/chargewire/v1/auth/token", "application/json",
		strings.NewReader(`{"grant_type":"client_certificate"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var answer struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&answer); err != nil || answer.AccessToken == "" {
		t.Fatalf("certificate sign-in: HTTP %d, %v", resp.StatusCode, err)
	}
	return answer.AccessToken
}

// A token bound to one certificate is refused over another (§4.4).
func TestIntegrationClientCertificateBoundTokenOverAnotherCertificate(t *testing.T) {
	ca := newCertificateAuthority(t)
	server := startCertificateServer(t, ca)
	ops, opsKey := ca.issue(t, "ops@example.com", false)
	other, otherKey := ca.issue(t, "other@example.com", false)
	token := certificateToken(t, server, ca, ops, opsKey)
	withToken := func(cert, key string) map[string]string {
		opts := certificateOptions(ca, cert, key)
		opts[OptionAuthType], opts["adbc.chargewire.auth.access_token"] = AuthTypePassword, token
		return opts
	}
	_, _, err := open(t, server, withToken(other, otherKey))
	wantStatus(t, err, adbc.StatusUnauthenticated, "")
	if code := chargewireCode(err); code != "certificate_mismatch" {
		t.Fatalf("chargewire.code %q: %v", code, err)
	}
	// The twin: the token over its own certificate.
	if got := scalar(t, mustOpen(t, server, withToken(ops, opsKey)), "SELECT HARDCHARGER_USER()"); got != opsIdentity {
		t.Fatalf("signed in as %q", got)
	}
}
