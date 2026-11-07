// Licensed under the Apache License, Version 2.0.

package hardcharger

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// fakeStopped is a Chargewire server whose discovery says state (a resume
// holder's "stopped", or "" for a serving server). It records every request
// and every statement's SQL.
type fakeStopped struct {
	server   *httptest.Server
	state    string
	mu       sync.Mutex
	requests []string
	sql      []string
	created  []map[string]any // the options of each session created
}

func newFakeStopped(t *testing.T, state string) *fakeStopped {
	f := &fakeStopped{state: state}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reply := func(status int, body any) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(body)
		}
		f.mu.Lock()
		f.requests = append(f.requests, r.Method+" "+r.URL.Path)
		f.mu.Unlock()
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/chargewire":
			doc := map[string]any{"protocol": "chargewire", "protocol_versions": []string{"1"},
				"server": map[string]string{"name": "Hardcharger", "version": "2.7.0"}, "capabilities": []string{},
				"auth": map[string]any{"grant_types": []string{"password"}}}
			if f.state != "" {
				doc["state"] = f.state
			}
			reply(http.StatusOK, doc)
		case r.Method == http.MethodPost && r.URL.Path == "/chargewire/v1/auth/token":
			reply(http.StatusOK, map[string]any{"access_token": "tok", "token_type": "Bearer", "expires_in": 3600})
		case r.Method == http.MethodPost && r.URL.Path == "/chargewire/v1/sessions":
			var body struct {
				Options map[string]any `json:"options"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			f.mu.Lock()
			f.created = append(f.created, body.Options)
			f.mu.Unlock()
			reply(http.StatusCreated, map[string]any{"session_id": "s1", "options": map[string]any{}})
		case r.Method == http.MethodPost && r.URL.Path == "/chargewire/v1/sessions/s1/statements":
			var body struct {
				SQL string `json:"sql"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			f.mu.Lock()
			f.sql = append(f.sql, body.SQL)
			f.mu.Unlock()
			reply(http.StatusCreated, map[string]any{"statement_id": "q1", "state": "succeeded",
				"result": map[string]any{"kind": "update", "rows_affected": 0}})
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			reply(http.StatusNotFound, map[string]any{"status": 404, "code": "not_found", "detail": "no"})
		}
	}))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeStopped) sent() ([]string, []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.requests), slices.Clone(f.sql)
}

// openStopped opens a database with a session tag and a setting (which every
// session gets through the new-session hook) and one connection.
func openStopped(t *testing.T, f *fakeStopped) adbc.ConnectionWithContext {
	t.Helper()
	ctx := context.Background()
	db, err := NewDriver(memory.DefaultAllocator).NewDatabaseWithContext(ctx, map[string]string{
		adbc.OptionKeyURI:                     "hardcharger://" + strings.TrimPrefix(f.server.URL, "http://") + "?tls=false",
		adbc.OptionKeyUsername:                "u",
		adbc.OptionKeyPassword:                "p",
		OptionSessionTag:                      `{"app":"pool"}`,
		OptionSettingPrefix + "query_timeout": "12",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close(ctx) })
	cnxn, err := db.Open(ctx)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return cnxn
}

// Against a stopped server (discovery "state": "stopped"), opening a
// connection with tags and settings and closing it send nothing but
// discovery: the setup runs when the session opens, at the first statement,
// which then runs after it.
func TestLazyConnectToAStoppedServerRunsTheSetupAtTheFirstStatement(t *testing.T) {
	f := newFakeStopped(t, "stopped")
	ctx := context.Background()
	cnxn := openStopped(t, f)
	if requests, _ := f.sent(); !slices.Equal(requests, []string{"GET /chargewire"}) {
		t.Fatalf("requests after Open %v, want the discovery request only", requests)
	}
	stmt, err := cnxn.NewStatement(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := stmt.SetSqlQuery(ctx, "CREATE TABLE t (x INT)"); err != nil {
		t.Fatal(err)
	}
	if _, err := stmt.ExecuteUpdate(ctx); err != nil {
		t.Fatalf("first statement: %v", err)
	}
	_ = stmt.Close(ctx)
	_, sql := f.sent()
	want := []string{`SET SESSION hardcharger.session_tag = '{"app":"pool"}'`, "SET SESSION hardcharger.query_timeout = '12'",
		"CREATE TABLE t (x INT)"}
	if !slices.Equal(sql, want) {
		t.Fatalf("statements\n%q\nwant\n%q", sql, want)
	}
	second := newFakeStopped(t, "stopped")
	_ = openStopped(t, second).Close(ctx)
	if requests, _ := second.sent(); !slices.Equal(requests, []string{"GET /chargewire"}) {
		t.Fatalf("requests for an open and a close %v, want the discovery request only", requests)
	}
}

// The positive twin: a serving server (no state) gets the sign-in, the
// session and its setup at Open, as before.
func TestConnectToAServingServerSetsTheSessionUpAtOpen(t *testing.T) {
	f := newFakeStopped(t, "")
	openStopped(t, f)
	requests, sql := f.sent()
	if !slices.Equal(requests[:3], []string{"GET /chargewire", "POST /chargewire/v1/auth/token", "POST /chargewire/v1/sessions"}) ||
		len(sql) != 2 {
		t.Fatalf("requests %v and statements %q at Open, want a sign-in, a session and its two SETs", requests, sql)
	}
}

// Options set on a connection to a stopped server before its first request
// (the current catalog, a query tag, a session setting) send nothing either:
// the catalog goes with the request that opens the session, and the tags and
// settings run in the new-session hook right after it, before the first
// statement. The order on the wire: sign-in, session, setup, statement.
func TestLazyConnectDefersConnectionOptionsToTheFirstStatement(t *testing.T) {
	f := newFakeStopped(t, "stopped")
	ctx := context.Background()
	cnxn := openStopped(t, f)
	setter := cnxn.(adbc.GetSetOptionsWithContext)
	for key, value := range map[string]string{
		adbc.OptionKeyCurrentCatalog:          "lake",
		OptionQueryTag:                        `{"job":"nightly"}`,
		OptionSettingPrefix + "query_timeout": "30",
	} {
		if err := setter.SetOption(ctx, key, value); err != nil {
			t.Fatalf("SetOption(%s): %v", key, err)
		}
	}
	if requests, _ := f.sent(); !slices.Equal(requests, []string{"GET /chargewire"}) {
		t.Fatalf("requests after Open and SetOption %v, want the discovery request only", requests)
	}
	stmt, err := cnxn.NewStatement(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := stmt.SetSqlQuery(ctx, "SELECT 1"); err != nil {
		t.Fatal(err)
	}
	if _, err := stmt.ExecuteUpdate(ctx); err != nil {
		t.Fatalf("first statement: %v", err)
	}
	_ = stmt.Close(ctx)
	requests, sql := f.sent()
	want := []string{"GET /chargewire", "POST /chargewire/v1/auth/token", "POST /chargewire/v1/sessions"}
	if !slices.Equal(requests[:min(len(requests), 3)], want) {
		t.Fatalf("requests %v, want them to begin %v", requests, want)
	}
	f.mu.Lock()
	created := f.created
	f.mu.Unlock()
	if len(created) != 1 || created[0]["catalog"] != "lake" {
		t.Fatalf("sessions created with options %v, want one with catalog lake", created)
	}
	wantSQL := []string{`SET SESSION hardcharger.session_tag = '{"app":"pool"}'`,
		`SET SESSION hardcharger.query_tag = '{"job":"nightly"}'`, "SET SESSION hardcharger.query_timeout = '30'", "SELECT 1"}
	if !slices.Equal(sql, wantSQL) {
		t.Fatalf("statements\n%q\nwant\n%q", sql, wantSQL)
	}
}
