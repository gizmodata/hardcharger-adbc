// Licensed under the Apache License, Version 2.0.

//go:build integration

package hardcharger

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
)

// A session that replaces a lost one gets the connection's tags and
// adbc.hardcharger.setting.* settings, as they were when the session was
// lost (the database's, changed by the connection since), before anything
// else runs in it: for every way a session is replaced (an idle eviction,
// session_ended with token_expired or idle_in_transaction, a session lost
// inside a transaction), and also when the statement that found the loss is
// refused because a transaction was lost with the session.

const (
	sessionIDOption    = "adbc.chargewire.session.id"
	replacementsOption = "adbc.chargewire.session.replacements"
	setupSessionTag    = `{"app":"replay"}`
	setupQueryTag      = `{"job":"connect"}`
	laterQueryTag      = `{"job":"later"}`
)

// setupOptions are the tags and settings each test connects with; the
// idle-in-transaction timeout (seconds) is the test's.
func setupOptions(idleInTransaction string) map[string]string {
	return map[string]string{
		OptionSettingPrefix + "query_timeout":                       "12",
		OptionSettingPrefix + "idle_in_transaction_session_timeout": idleInTransaction,
		OptionSessionTag: setupSessionTag,
		OptionQueryTag:   setupQueryTag,
	}
}

func connectionOption(t *testing.T, cnxn adbc.ConnectionWithContext, key string) string {
	t.Helper()
	value, err := cnxn.(adbc.GetSetOptionsWithContext).GetOption(context.Background(), key)
	if err != nil {
		t.Fatalf("GetOption(%s): %v", key, err)
	}
	return value
}

// laterTag changes the query tag on the open connection: a replacement must
// get this one, not the database's.
func laterTag(t *testing.T, cnxn adbc.ConnectionWithContext) {
	t.Helper()
	if err := cnxn.(adbc.GetSetOptionsWithContext).SetOption(context.Background(), OptionQueryTag, laterQueryTag); err != nil {
		t.Fatal(err)
	}
}

// assertSetUp checks, on the connection's current session, what the server
// says of the settings and tags (hardcharger_settings()), and that the
// session is the replacement of oldID.
func assertSetUp(t *testing.T, cnxn adbc.ConnectionWithContext, oldID, idleInTransaction string) {
	t.Helper()
	if got := connectionOption(t, cnxn, replacementsOption); got != "1" {
		t.Fatalf("%s replacements, want 1: the session was not replaced", got)
	}
	if id := connectionOption(t, cnxn, sessionIDOption); id == oldID {
		t.Fatalf("still on session %s", id)
	}
	// Read in SQL too, not only through GetOption.
	if got := scalar(t, cnxn, "SELECT CAST(value AS VARCHAR) FROM hardcharger_settings() WHERE name = 'hardcharger.query_timeout'"); got != "12" {
		t.Errorf("hardcharger.query_timeout = %s on the replacement, want 12", got)
	}
	for key, want := range map[string]string{
		OptionSettingPrefix + "query_timeout":                       "12",
		OptionSettingPrefix + "idle_in_transaction_session_timeout": idleInTransaction,
		OptionSessionTag: setupSessionTag,
		OptionQueryTag:   laterQueryTag,
	} {
		if got := connectionOption(t, cnxn, key); got != want {
			t.Errorf("%s = %q on the replacement, want %q", key, got, want)
		}
	}
}

// An idle eviction: the server forgets a session idle past
// --session-idle-timeout (session_not_found), no transaction open.
func TestIntegrationSettingsSurviveAnIdleEviction(t *testing.T) {
	server := startServer(t, "--session-idle-timeout", "2")
	cnxn := mustOpen(t, server, setupOptions("30"))
	laterTag(t, cnxn)
	old := connectionOption(t, cnxn, sessionIDOption)
	// Evicted within 5 s after the 2 s limit.
	time.Sleep(9 * time.Second)
	if got := scalar(t, cnxn, "SELECT 41 + 1"); got != "42" {
		t.Fatalf("SELECT 41 + 1 = %s", got)
	}
	assertSetUp(t, cnxn, old, "30")
}

// session_ended with idle_in_transaction, the limit being the session's own
// setting: the statement that finds the session ended is refused (its
// transaction was rolled back), and the replacement is set up all the same.
// The replacement's own limit then ends it again, which it would not with the
// server's default (0, never).
func TestIntegrationSettingsSurviveIdleInTransaction(t *testing.T) {
	cnxn := mustOpen(t, shared, setupOptions("1"))
	laterTag(t, cnxn)
	old := connectionOption(t, cnxn, sessionIDOption)
	execUpdate(t, cnxn, "BEGIN")
	time.Sleep(7 * time.Second) // ended within 5 s after the 1 s limit
	err := tryUpdate(t, cnxn, "SELECT 1")
	if code := chargewireCode(err); code != "session_ended" || !strings.Contains(err.Error(), "idle_in_transaction") {
		t.Fatalf("got %v (code %q), want session_ended with idle_in_transaction", err, code)
	}
	assertSetUp(t, cnxn, old, "1")

	execUpdate(t, cnxn, "BEGIN")
	time.Sleep(7 * time.Second)
	err = tryUpdate(t, cnxn, "SELECT 1")
	if code := chargewireCode(err); code != "session_ended" || !strings.Contains(err.Error(), "idle_in_transaction") {
		t.Fatalf("got %v (code %q): the replacement did not keep the session's idle_in_transaction_session_timeout", err, code)
	}
}

// A session lost (session_not_found) with a transaction begun in SQL: the
// statement that finds it gone is refused, and the replacement is set up.
func TestIntegrationSettingsSurviveALostTransaction(t *testing.T) {
	cnxn := mustOpen(t, shared, setupOptions("30"))
	laterTag(t, cnxn)
	old := connectionOption(t, cnxn, sessionIDOption)
	execUpdate(t, cnxn, "BEGIN")
	endSession(t, shared, old)
	err := tryUpdate(t, cnxn, "SELECT 1")
	if code := chargewireCode(err); code != "session_not_found" || !strings.Contains(err.Error(), "did not run again") {
		t.Fatalf("got %v (code %q), want session_not_found, not run again", err, code)
	}
	assertSetUp(t, cnxn, old, "30")
}

// session_ended with token_expired. Hardcharger ends a session so only when
// it was opened with a scoped token, which the driver can't renew (it then
// opens no replacement), so a proxy in front of the real server answers so
// once for the session: the driver signs in again, and the replacement is a
// real session of the server, set up by the driver.
func TestIntegrationSettingsSurviveTokenExpired(t *testing.T) {
	proxy := newSessionProxy(t, shared)
	cnxn := mustOpenThrough(t, proxy, setupOptions("30"))
	laterTag(t, cnxn)
	old := connectionOption(t, cnxn, sessionIDOption)
	proxy.endOnce(old, "session_ended", "token_expired")
	if got := scalar(t, cnxn, "SELECT 41 + 1"); got != "42" {
		t.Fatalf("SELECT 41 + 1 = %s", got)
	}
	assertSetUp(t, cnxn, old, "30")
	if proxy.tokens() < 2 {
		t.Fatalf("%d sign-ins: the driver must get a new token before replacing a token_expired session", proxy.tokens())
	}
}

// When setting up a replacement fails, nothing runs in it: the request that
// found the session lost fails with the reason, and the next one opens and
// sets up another session.
func TestIntegrationAFailedSetupIsAnError(t *testing.T) {
	proxy := newSessionProxy(t, shared)
	cnxn := mustOpenThrough(t, proxy, setupOptions("30"))
	laterTag(t, cnxn)
	old := connectionOption(t, cnxn, sessionIDOption)
	proxy.refuseSetupOnce("SET SESSION hardcharger.query_timeout")
	endSession(t, shared, old)
	err := tryUpdate(t, cnxn, "CREATE OR REPLACE TABLE hc_setup_failed AS SELECT 1 AS v")
	wantStatus(t, err, adbc.StatusIO, "setting hardcharger.query_timeout")
	if !strings.Contains(err.Error(), "next request opens and sets up another session") {
		t.Fatalf("the error does not say the next request tries again: %v", err)
	}
	other := mustOpen(t, shared, nil)
	if got := scalar(t, other, "SELECT count(*) FROM duckdb_tables() WHERE table_name = 'hc_setup_failed'"); got != "0" {
		t.Fatal("the statement ran in a session that was not set up")
	}
	if got := connectionOption(t, cnxn, replacementsOption); got != "0" {
		t.Fatalf("%s replacements after the failed setup, want 0", got)
	}
	if got := scalar(t, cnxn, "SELECT 7"); got != "7" {
		t.Fatalf("SELECT 7 = %s on the next request", got)
	}
	assertSetUp(t, cnxn, old, "30")
}

// endSession ends session id on server from outside, as an eviction would
// (the server then answers session_not_found).
func endSession(t *testing.T, server *testServer, id string) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"grant_type": "password", "username": testUsername, "password": server.password})
	resp, err := http.Post("http://127.0.0.1:"+strconv.Itoa(server.port)+"/chargewire/v1/auth/token", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var answer struct {
		AccessToken string `json:"access_token"`
	}
	err = json.NewDecoder(resp.Body).Decode(&answer)
	_ = resp.Body.Close()
	if err != nil || answer.AccessToken == "" {
		t.Fatalf("token: %v", err)
	}
	req, _ := http.NewRequest(http.MethodDelete, "http://127.0.0.1:"+strconv.Itoa(server.port)+"/chargewire/v1/sessions/"+id, nil)
	req.Header.Set("Authorization", "Bearer "+answer.AccessToken)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("ending session %s: HTTP %d", id, resp.StatusCode)
	}
}

// sessionProxy forwards to a server and can answer for it: end a session once
// (a problem for the next request naming it), or refuse one setup statement.
type sessionProxy struct {
	server *httptest.Server
	mu     sync.Mutex
	end    map[string][2]string // session id -> code, reason
	refuse string               // a statement starting so is refused once
	signIn int
	// resuming: how many more requests (not discovery) get 503
	// server_resuming, as a stopped GizmoData Cloud cluster's address answers
	// while it starts again.
	resuming       int
	resumingPrefix string
}

func newSessionProxy(t *testing.T, target *testServer) *sessionProxy {
	t.Helper()
	backend, _ := url.Parse("http://127.0.0.1:" + strconv.Itoa(target.port))
	forward := httputil.NewSingleHostReverseProxy(backend)
	p := &sessionProxy{end: map[string][2]string{}}
	p.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p.intercept(w, r) {
			return
		}
		forward.ServeHTTP(w, r)
	}))
	t.Cleanup(p.server.Close)
	return p
}

func (p *sessionProxy) endOnce(id, code, reason string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.end[id] = [2]string{code, reason}
}

func (p *sessionProxy) refuseSetupOnce(prefix string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.refuse = prefix
}

func (p *sessionProxy) tokens() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.signIn
}

func (p *sessionProxy) intercept(w http.ResponseWriter, r *http.Request) bool {
	path := strings.TrimPrefix(r.URL.Path, "/chargewire/v1/")
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.resuming > 0 && strings.HasPrefix(r.URL.Path, "/chargewire/v1/") && strings.HasPrefix(path, p.resumingPrefix) {
		p.resuming--
		w.Header().Set("Retry-After", "1")
		problem(w, http.StatusServiceUnavailable, "server_resuming", "")
		return true
	}
	if r.Method == http.MethodPost && path == "auth/token" {
		p.signIn++
		return false
	}
	rest, ok := strings.CutPrefix(path, "sessions/")
	if !ok {
		return false
	}
	id, _, _ := strings.Cut(rest, "/")
	if ended, ok := p.end[id]; ok {
		delete(p.end, id)
		problem(w, http.StatusUnauthorized, ended[0], ended[1])
		return true
	}
	if p.refuse != "" && r.Method == http.MethodPost && strings.HasSuffix(rest, "/statements") {
		body, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(body))
		var doc struct {
			SQL string `json:"sql"`
		}
		_ = json.Unmarshal(body, &doc)
		if strings.HasPrefix(doc.SQL, p.refuse) {
			p.refuse = ""
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"statement_id": "refused", "state": "failed",
				"error": map[string]any{"code": "execution_error", "status": 422, "detail": "refused by the test proxy"}})
			return true
		}
	}
	return false
}

func problem(w http.ResponseWriter, status int, code, reason string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"type": "urn:chargewire:error:" + code, "title": code,
		"status": status, "code": code, "reason": reason, "detail": "answered by the test proxy"})
}

func mustOpenThrough(t *testing.T, proxy *sessionProxy, extra map[string]string) adbc.ConnectionWithContext {
	t.Helper()
	options := map[string]string{adbc.OptionKeyURI: "hardcharger://" + strings.TrimPrefix(proxy.server.URL, "http://") + "?tls=false"}
	for key, value := range extra {
		options[key] = value
	}
	return mustOpen(t, shared, options)
}
