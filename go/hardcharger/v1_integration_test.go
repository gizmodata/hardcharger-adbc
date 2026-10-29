// Licensed under the Apache License, Version 2.0.

//go:build integration

// Chargewire protocol 1.0.0, through the Hardcharger driver against a live
// hardcharger-server.

package hardcharger

import (
	"context"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
)

const optionSessionTransaction = "adbc.chargewire.session.transaction"

func execUpdate(t *testing.T, cnxn adbc.ConnectionWithContext, sql string) {
	t.Helper()
	ctx := context.Background()
	stmt, err := cnxn.NewStatement(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close(ctx)
	if err := stmt.SetSqlQuery(ctx, sql); err != nil {
		t.Fatal(err)
	}
	if _, err := stmt.ExecuteUpdate(ctx); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

func sessionTransaction(t *testing.T, cnxn adbc.ConnectionWithContext) string {
	t.Helper()
	value, err := cnxn.(adbc.GetSetOptionsWithContext).GetOption(context.Background(), optionSessionTransaction)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

// The session's transaction is the server's view (§5.2), so it sees SQL
// BEGIN and ROLLBACK, which the driver never tracks.
func TestIntegrationSessionTransactionIsTheServers(t *testing.T) {
	cnxn := mustOpen(t, shared, nil)
	if got := sessionTransaction(t, cnxn); got != "none" {
		t.Fatalf("before BEGIN: %q", got)
	}
	execUpdate(t, cnxn, "BEGIN TRANSACTION")
	if got := sessionTransaction(t, cnxn); got != "open" {
		t.Fatalf("after BEGIN: %q", got)
	}
	execUpdate(t, cnxn, "ROLLBACK")
	if got := sessionTransaction(t, cnxn); got != "none" {
		t.Fatalf("after ROLLBACK: %q", got)
	}
}

// A server that verifies client certificates refuses a connection without
// one at the handshake (TLS alert certificate_required). That is the
// refusal certificate_required (§11; it was client_certificate_required),
// not an unreachable server. The twin, a password sign-in over a
// certificate, is TestIntegrationClientCertificateSignInOffOnTheServer.
func TestIntegrationNoClientCertificateIsCertificateRequired(t *testing.T) {
	ca := newCertificateAuthority(t)
	server := startServer(t, ca.mtlsServerArgs()...)
	_, _, err := open(t, server, map[string]string{"adbc.chargewire.tls.root_certs": ca.caPEM})
	wantStatus(t, err, adbc.StatusUnauthenticated, "certificate")
	if code := chargewireCode(err); code != "certificate_required" {
		t.Fatalf("chargewire.code %q: %v", code, err)
	}
}
