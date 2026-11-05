// Licensed under the Apache License, Version 2.0.

//go:build integration

// Chargewire protocol 1.0.0, through the Hardcharger driver against a live
// hardcharger-server.

package hardcharger

import (
	"context"
	"strings"
	"testing"
	"time"

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

// tryUpdate runs sql with ExecuteUpdate and returns its error.
func tryUpdate(t *testing.T, cnxn adbc.ConnectionWithContext, sql string) error {
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
	_, err = stmt.ExecuteUpdate(ctx)
	return err
}

// A session Hardcharger ends for idling in a transaction
// (hardcharger.idle_in_transaction_session_timeout, set for the session
// through adbc.hardcharger.setting.*) is replaced: the statement that finds it
// ended fails with session_ended (idle_in_transaction) instead of running
// again in autocommit after the transaction was rolled back, and the
// connection works afterwards (chargewire-adbc 2.1.0; with 2.0.0 every later
// call failed too).
func TestIntegrationIdleInTransactionSessionIsReplaced(t *testing.T) {
	cnxn := mustOpen(t, shared, map[string]string{OptionSettingPrefix + "idle_in_transaction_session_timeout": "1"})
	execUpdate(t, cnxn, "CREATE OR REPLACE TABLE hc_idle_tx (v INTEGER)")
	execUpdate(t, cnxn, "BEGIN")
	execUpdate(t, cnxn, "INSERT INTO hc_idle_tx VALUES (1)")
	// The server ends the session within 5 s after the 1 s limit.
	time.Sleep(7 * time.Second)
	err := tryUpdate(t, cnxn, "INSERT INTO hc_idle_tx VALUES (2)")
	if code := chargewireCode(err); code != "session_ended" || !strings.Contains(err.Error(), "idle_in_transaction") ||
		!strings.Contains(err.Error(), "did not run again") {
		t.Fatalf("got %v (code %q), want session_ended with idle_in_transaction, not run again", err, code)
	}
	other := mustOpen(t, shared, nil)
	if got := scalar(t, other, "SELECT count(*) FROM hc_idle_tx"); got != "0" {
		t.Fatalf("%s rows: the statement ran again in autocommit after its transaction was rolled back", got)
	}
	execUpdate(t, cnxn, "INSERT INTO hc_idle_tx VALUES (4)")
	if got := scalar(t, other, "SELECT sum(v) FROM hc_idle_tx"); got != "4" {
		t.Fatalf("sum = %s, want 4", got)
	}
}
