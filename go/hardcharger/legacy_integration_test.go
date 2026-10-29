// Licensed under the Apache License, Version 2.0.

//go:build integration

// Names and servers from before the protocol was renamed to Chargewire. The
// old URI scheme and option keys run against the suite's own server; a
// server from before the rename (one already running, e.g. the
// ghcr.io/gizmodata/hardcharger:2.2.0 image) is named by
// HARDCHARGER_ADBC_TEST_LEGACY_URI, with the admin's password in
// HARDCHARGER_ADBC_TEST_LEGACY_PASSWORD (and user name in
// HARDCHARGER_ADBC_TEST_LEGACY_USERNAME, default hardcharger). Without the
// URI those tests skip; with it, they fail when the driver can't use it.
//
//	docker run -d -p 127.0.0.1:42100:31337 -e HARDCHARGER_ADMIN_PASSWORD=… ghcr.io/gizmodata/hardcharger:2.2.0
//	HARDCHARGER_ADBC_TEST_LEGACY_URI='hardcharger://127.0.0.1:42100?tls=false' HARDCHARGER_ADBC_TEST_LEGACY_PASSWORD=… \
//	  go test -race -tags integration -run Legacy ./...

package hardcharger

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

const (
	legacyURIEnv      = "HARDCHARGER_ADBC_TEST_LEGACY_URI"
	legacyPasswordEnv = "HARDCHARGER_ADBC_TEST_LEGACY_PASSWORD"
	legacyUsernameEnv = "HARDCHARGER_ADBC_TEST_LEGACY_USERNAME"
)

// The old scheme and an old option key, against the suite's server: the
// option takes effect (the server stops a long statement at its timeout).
func TestIntegrationLegacySchemeAndOptionKeys(t *testing.T) {
	ctx := context.Background()
	cnxn := mustOpen(t, shared, map[string]string{
		adbc.OptionKeyURI:        "warp://127.0.0.1:" + strconv.Itoa(shared.port) + "?tls=false",
		"adbc.warp.ingest.parts": "1",
	})
	if got := scalar(t, cnxn, "SELECT 42"); got != "42" {
		t.Fatal(got)
	}
	if got, err := cnxn.(adbc.GetSetOptionsWithContext).GetOption(ctx, OptionIngestParts); err != nil || got != "1" {
		t.Fatalf("ingest parts %q, %v", got, err)
	}
	stmt, err := cnxn.NewStatement(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close(ctx)
	if err := stmt.(adbc.GetSetOptionsWithContext).SetOption(ctx, "adbc.warp.statement.timeout_seconds", "0.3"); err != nil {
		t.Fatal(err)
	}
	if err := stmt.SetSqlQuery(ctx, "SELECT sum(a.range * b.range) FROM range(100000000) a, range(100000) b"); err != nil {
		t.Fatal(err)
	}
	_, err = stmt.ExecuteUpdate(ctx)
	wantStatus(t, err, adbc.StatusTimeout, "statement_timeout")
	// The session is still usable.
	if got := scalar(t, cnxn, "SELECT 1"); got != "1" {
		t.Fatal(got)
	}
}

// legacyServer is the server from before the rename, or a skip.
func legacyServer(t *testing.T) map[string]string {
	t.Helper()
	uri := os.Getenv(legacyURIEnv)
	if uri == "" {
		t.Skipf("set %s to a server from before the rename", legacyURIEnv)
	}
	password := os.Getenv(legacyPasswordEnv)
	if password == "" {
		t.Fatalf("%s is set: set %s too", legacyURIEnv, legacyPasswordEnv)
	}
	username := os.Getenv(legacyUsernameEnv)
	if username == "" {
		username = "hardcharger"
	}
	return map[string]string{adbc.OptionKeyURI: uri, adbc.OptionKeyUsername: username, adbc.OptionKeyPassword: password}
}

func openLegacy(t *testing.T, extra map[string]string) adbc.ConnectionWithContext {
	t.Helper()
	integrationTests.Add(1)
	opts := legacyServer(t)
	for key, value := range extra {
		opts[key] = value
	}
	ctx := context.Background()
	db, err := NewDriver(memory.NewCheckedAllocator(memory.DefaultAllocator)).NewDatabaseWithContext(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close(ctx) })
	cnxn, err := db.Open(ctx)
	if err != nil {
		t.Fatalf("a server from before the rename at %s: %v", opts[adbc.OptionKeyURI], err)
	}
	t.Cleanup(func() { _ = cnxn.Close(ctx) })
	return cnxn
}

// A Hardcharger URI at the default base path reaches a server from before
// the rename: queries, Hardcharger's settings, a prepared statement, a bulk
// ingest and a SQL error's code.
func TestIntegrationLegacyServer(t *testing.T) {
	ctx := context.Background()
	cnxn := openLegacy(t, map[string]string{OptionQueryTag: `{"job": "legacy"}`})
	if got := scalar(t, cnxn, "SELECT 42"); got != "42" {
		t.Fatal(got)
	}
	if got := scalar(t, cnxn, "SELECT sum(i) FROM range(10) t(i)"); got != "45" {
		t.Fatal(got)
	}
	if info(t, cnxn)[adbc.InfoVendorName] != "Hardcharger" {
		t.Fatalf("info %v", info(t, cnxn))
	}
	opts := cnxn.(adbc.GetSetOptionsWithContext)
	if err := opts.SetOption(ctx, OptionSettingPrefix+"query_timeout", "30"); err != nil {
		t.Fatal(err)
	}
	if got, err := opts.GetOption(ctx, OptionSettingPrefix+"query_timeout"); err != nil || got != "30" {
		t.Fatalf("query_timeout %q, %v", got, err)
	}

	// A prepared statement with a parameter.
	stmt, err := cnxn.NewStatement(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close(ctx)
	if err := stmt.SetSqlQuery(ctx, "SELECT ? + 1 AS n"); err != nil {
		t.Fatal(err)
	}
	if err := stmt.Prepare(ctx); err != nil {
		t.Fatal(err)
	}
	builder := array.NewRecordBuilder(memory.DefaultAllocator,
		arrow.NewSchema([]arrow.Field{{Name: "p", Type: arrow.PrimitiveTypes.Int64}}, nil))
	defer builder.Release()
	builder.Field(0).(*array.Int64Builder).Append(41)
	params := builder.NewRecordBatch()
	defer params.Release()
	if err := stmt.Bind(ctx, params); err != nil {
		t.Fatal(err)
	}
	reader, _, err := stmt.ExecuteQuery(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var got string
	for reader.Next() {
		if rec := reader.RecordBatch(); rec.NumRows() > 0 {
			got = rec.Column(0).ValueStr(0)
		}
	}
	if err := reader.Err(); err != nil {
		t.Fatal(err)
	}
	reader.Release()
	if got != "42" {
		t.Fatalf("prepared: %q", got)
	}

	// A bulk ingest (in parts, the default) into a temporary table.
	table := "legacy_ingest_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	ingest(t, cnxn, table)
	execUpdate(t, cnxn, "DROP TABLE "+table)

	// A SQL error keeps its status and code.
	err = opts.SetOption(ctx, OptionSettingPrefix+"query_timeout", "soon")
	wantStatus(t, err, adbc.StatusInvalidArgument, "is not a whole number")
	if code := chargewireCode(err); code != "sql_error" {
		t.Fatalf("chargewire.code %q", code)
	}
}

// The old scheme and option keys against a server from before the rename.
func TestIntegrationLegacyServerWithLegacyNames(t *testing.T) {
	uri := legacyServer(t)[adbc.OptionKeyURI]
	translated, err := ToChargewireURI(uri)
	if err != nil {
		t.Fatal(err)
	}
	cnxn := openLegacy(t, map[string]string{
		adbc.OptionKeyURI:                       "warp" + strings.TrimPrefix(translated, currentScheme),
		"adbc.warp.retry.max_attempts":          "2",
		"adbc.warp.statement.result_partitions": "1",
	})
	if got := scalar(t, cnxn, "SELECT 42"); got != "42" {
		t.Fatal(got)
	}
}
