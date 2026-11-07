// Licensed under the Apache License, Version 2.0.

package hardcharger

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/gizmodata/chargewire-adbc/go/v2/chargewire"
)

func TestToChargewireURI(t *testing.T) {
	for raw, want := range map[string]string{
		"hardcharger://db.example.com":            "chargewire://db.example.com:31337",
		"hardcharger://db.example.com:443":        "chargewire://db.example.com:443",
		"hardcharger://127.0.0.1?tls=false":       "chargewire://127.0.0.1:31337?tls=false",
		"hardcharger://127.0.0.1:4000?tls=false":  "chargewire://127.0.0.1:4000?tls=false",
		"hardcharger://h/custom/base":             "chargewire://h:31337/custom/base",
		"HARDCHARGER://h":                         "chargewire://h:31337",
		"hardcharger://[::1]":                     "chargewire://[::1]:31337",
		"hardcharger://[::1]:5000?tls=false":      "chargewire://[::1]:5000?tls=false",
		"chargewire://h:1?tls=false":              "chargewire://h:1?tls=false",
		"https://h/chargewire":                    "https://h/chargewire",
		"http://h:8080":                           "http://h:8080",
		"grpc://h:1":                              "grpc://h:1", // the Chargewire driver refuses it
		"hardcharger://joe@h:1":                   "chargewire://joe@h:1",
		"hardcharger://h:1?tls=false&extra=thing": "chargewire://h:1?tls=false&extra=thing",
	} {
		got, err := ToChargewireURI(raw)
		if err != nil || got != want {
			t.Errorf("ToChargewireURI(%q) = %q, %v; want %q", raw, got, err, want)
		}
	}
}

// A GizmoData Cloud host (gizmodata.com or a name under it, in any case, with
// or without a trailing dot) answers on 443, so a hardcharger:// URI without a
// port gets 443 there. An explicit port always wins, and every other host,
// look-alikes that only contain the name included, keeps 31337.
func TestToChargewireURIGizmoDataCloudPort(t *testing.T) {
	for raw, want := range map[string]string{
		"hardcharger://gizmodata.com":                         "chargewire://gizmodata.com:443",
		"hardcharger://acme-prod.us-east-1.aws.gizmodata.com": "chargewire://acme-prod.us-east-1.aws.gizmodata.com:443",
		"hardcharger://ACME.GizmoData.COM":                    "chargewire://ACME.GizmoData.COM:443",
		"hardcharger://acme.gizmodata.com.":                   "chargewire://acme.gizmodata.com.:443",
		"hardcharger://joe@acme.gizmodata.com/base?tls=true":  "chargewire://joe@acme.gizmodata.com:443/base?tls=true",
		"hardcharger://acme.gizmodata.com:31337":              "chargewire://acme.gizmodata.com:31337",
		"hardcharger://gizmodata.com:8443":                    "chargewire://gizmodata.com:8443",
		"hardcharger://evilgizmodata.com":                     "chargewire://evilgizmodata.com:31337",
		"hardcharger://acme.evilgizmodata.com":                "chargewire://acme.evilgizmodata.com:31337",
		"hardcharger://gizmodata.com.example.org":             "chargewire://gizmodata.com.example.org:31337",
		"hardcharger://acme.gizmodata.com.example.org":        "chargewire://acme.gizmodata.com.example.org:31337",
		"hardcharger://gizmodata.co":                          "chargewire://gizmodata.co:31337",
		"hardcharger://xgizmodata.com.":                       "chargewire://xgizmodata.com.:31337",
		"hardcharger://gizmodata.com.":                        "chargewire://gizmodata.com.:443",
		"hardcharger://.gizmodata.com":                        "chargewire://.gizmodata.com:31337", // no label before the dot
	} {
		got, err := ToChargewireURI(raw)
		if err != nil || got != want {
			t.Errorf("ToChargewireURI(%q) = %q, %v; want %q", raw, got, err, want)
		}
	}
}

func wantStatus(t *testing.T, err error, status adbc.Status, text string) {
	t.Helper()
	var adbcErr adbc.Error
	if !errors.As(err, &adbcErr) {
		t.Fatalf("error %v (%T), want an adbc.Error", err, err)
	}
	if adbcErr.Code != status || !strings.Contains(adbcErr.Msg, text) {
		t.Fatalf("error %v (status %v), want status %v and %q", adbcErr.Msg, adbcErr.Code, status, text)
	}
}

func TestPlusHTTPSchemeIsRefusedWithTheSpellingToUse(t *testing.T) {
	_, err := ToChargewireURI("hardcharger+http://127.0.0.1:31337")
	wantStatus(t, err, adbc.StatusInvalidArgument, "hardcharger://host:port?tls=false for plain HTTP")
	// The twin: the plain-HTTP spelling.
	got, err := ToChargewireURI("hardcharger://127.0.0.1:31337?tls=false")
	if err != nil || got != "chargewire://127.0.0.1:31337?tls=false" || usesTLS(got) {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestBadURIs(t *testing.T) {
	_, err := ToChargewireURI("")
	wantStatus(t, err, adbc.StatusInvalidArgument, "a uri is required")
	_, err = ToChargewireURI("hardcharger:///base")
	wantStatus(t, err, adbc.StatusInvalidArgument, "no host")
	_, err = ToChargewireURI("hardcharger://joe:secret@h:bad")
	wantStatus(t, err, adbc.StatusInvalidArgument, "invalid uri")
	if strings.Contains(err.Error(), "secret") {
		t.Fatalf("the password leaked: %v", err)
	}
}

func TestUsesTLS(t *testing.T) {
	for uri, want := range map[string]bool{
		"chargewire://h:1": true, "chargewire://h:1?tls=false": false, "chargewire://h:1?tls=0": false,
		"chargewire://h:1?tls=true": true, "https://h": true, "http://h": false,
	} {
		if got := usesTLS(uri); got != want {
			t.Errorf("usesTLS(%q) = %v", uri, got)
		}
	}
}

func TestSettingNames(t *testing.T) {
	for _, name := range []string{"query_timeout", "hardcharger.query_timeout", "x_1"} {
		if _, err := settingName(name); err != nil {
			t.Errorf("%q: %v", name, err)
		}
	}
	for _, name := range []string{"query_timeout; DROP TABLE t", "Query_Timeout", "", "a.b", "1abc", "a'b"} {
		_, err := settingName(name)
		wantStatus(t, err, adbc.StatusInvalidArgument, "invalid Hardcharger setting name")
	}
}

func TestSetStatement(t *testing.T) {
	for _, tc := range []struct {
		name, value string
		global      bool
		want        string
	}{
		{"query_timeout", "30", false, "SET SESSION hardcharger.query_timeout = '30'"},
		{"query_tag", `{"a":"it's"}`, false, `SET SESSION hardcharger.query_tag = '{"a":"it''s"}'`},
		{"graceful_shutdown", "false", true, "SET GLOBAL hardcharger.graceful_shutdown = 'false'"},
		{"query_timeout", "", false, "RESET SESSION hardcharger.query_timeout"},
		{"max_concurrent_statements", "", true, "RESET GLOBAL hardcharger.max_concurrent_statements"},
	} {
		if got := setStatement(tc.name, tc.value, tc.global); got != tc.want {
			t.Errorf("got %s, want %s", got, tc.want)
		}
	}
}

func newDB(t *testing.T, opts map[string]string) (*database, error) {
	t.Helper()
	db, err := NewDriver(memory.DefaultAllocator).NewDatabaseWithContext(context.Background(), opts)
	if err != nil {
		return nil, err
	}
	t.Cleanup(func() { _ = db.Close(context.Background()) })
	return db.(*database), nil
}

func TestDatabaseOptions(t *testing.T) {
	ctx := context.Background()
	db, err := newDB(t, map[string]string{
		adbc.OptionKeyURI:                                "hardcharger://h?tls=false",
		OptionAuthType:                                   AuthTypeExternal,
		OptionOAuthTimeoutSeconds:                        "2.5",
		OptionQueryTag:                                   `{"job":"x"}`,
		OptionSettingPrefix + "query_timeout":            "30",
		OptionSettingPrefix + "hardcharger.bypass_queue": "true",
	})
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{
		adbc.OptionKeyURI:                     "hardcharger://h?tls=false",
		OptionAuthType:                        AuthTypeExternal,
		OptionOAuthTimeoutSeconds:             "2.5",
		OptionQueryTag:                        `{"job":"x"}`,
		OptionSettingPrefix + "query_timeout": "30",
		OptionSettingPrefix + "bypass_queue":  "true",
	} {
		if got, err := db.GetOption(ctx, key); err != nil || got != want {
			t.Errorf("GetOption(%s) = %q, %v; want %q", key, got, err, want)
		}
	}
	// The Chargewire driver got the translated URI and none of Hardcharger's options.
	if db.chargewireURI != "chargewire://h:31337?tls=false" {
		t.Errorf("chargewire uri %q", db.chargewireURI)
	}
	_, err = db.GetOption(ctx, OptionSessionTag)
	wantStatus(t, err, adbc.StatusNotFound, "is not set")
}

func TestBadDatabaseOptionsAreRefused(t *testing.T) {
	for opts, text := range map[[2]string]string{
		{OptionAuthType, "oauth"}:                              "expected password, external or client_certificate (mtls)",
		{OptionOAuthTimeoutSeconds, "soon"}:                    "expected a number of seconds",
		{OptionSettingPrefix + "bad name", "1"}:                "invalid Hardcharger setting name",
		{OptionGlobalSettingPrefix + "graceful_shutdown", "1"}: "is a connection option",
		{"adbc.hardcharger.nope", "1"}:                         "unknown option",
		{adbc.OptionKeyURI, "hardcharger+https://h"}:           "is not a Hardcharger URI scheme",
	} {
		_, err := newDB(t, map[string]string{adbc.OptionKeyURI: "hardcharger://h", opts[0]: opts[1]})
		if err == nil || !strings.Contains(err.Error(), text) {
			t.Errorf("%s=%s: error %v, want %q", opts[0], opts[1], err, text)
		}
	}
	// The twin: the same keys with good values.
	if _, err := newDB(t, map[string]string{adbc.OptionKeyURI: "hardcharger://h", OptionAuthType: AuthTypePassword,
		OptionOAuthTimeoutSeconds: "30", OptionSettingPrefix + "query_timeout": "1"}); err != nil {
		t.Fatal(err)
	}
}

func TestSetOptionAfterCreation(t *testing.T) {
	ctx := context.Background()
	db, err := newDB(t, map[string]string{adbc.OptionKeyURI: "hardcharger://h2"})
	if err != nil {
		t.Fatal(err)
	}
	if db.chargewireURI != "chargewire://h2:31337" {
		t.Fatalf("chargewire uri %q", db.chargewireURI)
	}
	if got, _ := db.GetOption(ctx, adbc.OptionKeyURI); got != "hardcharger://h2" {
		t.Fatalf("uri %q", got)
	}
	if err := db.SetOption(ctx, OptionQueryTag, `{"a":1}`); err != nil {
		t.Fatal(err)
	}
	if got, _ := db.GetOption(ctx, OptionQueryTag); got != `{"a":1}` {
		t.Fatalf("query tag %q", got)
	}
	wantStatus(t, db.SetOption(ctx, OptionAuthType, "nope"), adbc.StatusInvalidArgument, "expected password, external or client_certificate (mtls)")
	// The Chargewire driver's options reach it after creation too (driverbase's
	// SetOptions refused them as unknown), and it validates them.
	if err := db.SetOption(ctx, "adbc.chargewire.retry.max_attempts", "3"); err != nil {
		t.Fatal(err)
	}
	if got, _ := db.GetOption(ctx, "adbc.chargewire.retry.max_attempts"); got != "3" {
		t.Fatalf("max attempts %q", got)
	}
	if err := db.SetOptions(ctx, map[string]string{OptionIngestParts: "2", "adbc.chargewire.tls.skip_verify": "true"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := db.GetOption(ctx, "adbc.chargewire.tls.skip_verify"); got != "true" {
		t.Fatalf("skip verify %q", got)
	}
	wantStatus(t, db.SetOption(ctx, "adbc.chargewire.retry.max_attempts", "zero"), adbc.StatusInvalidArgument, "adbc.chargewire.retry.max_attempts")
}

// fakeSignIn is a Chargewire server with browser sign-in (spec §4.3): discovery,
// a flow, and polls answered from the queued answers (a pending flow after
// them). A poll without the flow's secret gets 404 flow_not_found.
type fakeSignIn struct {
	mu       sync.Mutex
	polls    []fakeAnswer
	offer    bool
	requests []string
	secrets  []string
	sessions []string // the Authorization of each session created
	server   *httptest.Server
	// state is the discovery document's state ("stopped", or "" for a
	// serving server); revoked are tokens refused with token_revoked.
	state   string
	revoked map[string]bool
}

type fakeAnswer struct {
	status int
	body   map[string]any
}

const fakePollSecret = "fake-poll-secret"

func newFakeSignIn(t *testing.T, polls ...fakeAnswer) *fakeSignIn {
	f := &fakeSignIn{polls: polls, offer: true}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.requests = append(f.requests, r.Method+" "+r.URL.Path)
		reply := func(status int, body any) {
			w.Header().Set("Content-Type", "application/json")
			if status >= 400 {
				w.Header().Set("Content-Type", "application/problem+json")
			}
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(body)
		}
		if f.revoked[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")] {
			reply(401, map[string]any{"code": "invalid_token", "reason": "token_revoked", "status": 401,
				"detail": "the token was revoked"})
			return
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/chargewire":
			capabilities := []string{}
			if f.offer {
				capabilities = append(capabilities, "oauth_browser")
			}
			doc := map[string]any{"protocol": "chargewire", "protocol_versions": []string{"1"},
				"server": map[string]string{"name": "Hardcharger", "version": "0"}, "capabilities": capabilities,
				"auth": map[string]any{"grant_types": []string{"password"}, "oauth_browser": map[string]string{}}}
			if f.state != "" {
				doc["state"] = f.state
			}
			reply(200, doc)
		case r.Method == http.MethodPost && r.URL.Path == "/chargewire/v1/auth/oauth/flows":
			reply(201, map[string]any{"flow_id": "f_1", "poll_secret": fakePollSecret,
				"authorize_url": "http://idp/authorize?state=s", "poll_interval_s": 1, "expires_at": "2099-01-01T00:00:00Z"})
		case r.Method == http.MethodPost && r.URL.Path == "/chargewire/v1/auth/oauth/flows/f_1":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			f.secrets = append(f.secrets, body["poll_secret"])
			answer := fakeAnswer{200, map[string]any{"state": "pending"}}
			if body["poll_secret"] != fakePollSecret {
				answer = fakeAnswer{404, map[string]any{"code": "flow_not_found", "status": 404, "detail": "no such flow"}}
			} else if len(f.polls) > 0 {
				answer, f.polls = f.polls[0], f.polls[1:]
			}
			reply(answer.status, answer.body)
		case r.Method == http.MethodPost && r.URL.Path == "/chargewire/v1/sessions":
			f.sessions = append(f.sessions, r.Header.Get("Authorization"))
			reply(201, map[string]any{"session_id": "s1", "options": map[string]any{}})
		case r.Method == http.MethodPost && r.URL.Path == "/chargewire/v1/sessions/s1/statements":
			reply(201, map[string]any{"statement_id": "q1", "state": "succeeded",
				"result": map[string]any{"kind": "update", "rows_affected": 0}})
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			reply(404, map[string]any{"code": "not_found", "status": 404})
		}
	}))
	t.Cleanup(f.server.Close)
	return f
}

// withBrowser replaces the browser: fn gets the opener's command and the URL.
func withBrowser(t *testing.T, fn func(command, url string) error) {
	t.Helper()
	saved := openBrowser
	openBrowser = func(command string) chargewire.BrowserOpener {
		return func(_ context.Context, url string) error { return fn(command, url) }
	}
	t.Cleanup(func() { openBrowser = saved })
}

func (f *fakeSignIn) options(extra ...string) map[string]string {
	u, _ := url.Parse(f.server.URL)
	options := map[string]string{adbc.OptionKeyURI: "hardcharger://" + u.Host + "?tls=false", OptionAuthType: AuthTypeExternal}
	for i := 0; i+1 < len(extra); i += 2 {
		options[extra[i]] = extra[i+1]
	}
	return options
}

var completeAnswer = fakeAnswer{200, map[string]any{"state": "complete", "access_token": "server-token",
	"token_type": "Bearer", "expires_in": 3600}}

// The browser sign-in runs the protocol's flow on the server's own port and
// opens sessions with the server's token.
func TestBrowserSignInCompletesAfterPending(t *testing.T) {
	var opened []string
	withBrowser(t, func(command, url string) error { opened = append(opened, command+" "+url); return nil })
	fake := newFakeSignIn(t, fakeAnswer{200, map[string]any{"state": "pending"}}, completeAnswer)
	db, err := newDB(t, fake.options(OptionOAuthBrowser, "my-browser --new"))
	if err != nil {
		t.Fatal(err)
	}
	cnxn, err := db.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer cnxn.Close(context.Background())
	if len(opened) != 1 || opened[0] != "my-browser --new http://idp/authorize?state=s" {
		t.Fatalf("opened %q", opened)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if got := strings.Join(fake.requests[:4], ", "); got !=
		"GET /chargewire, POST /chargewire/v1/auth/oauth/flows, POST /chargewire/v1/auth/oauth/flows/f_1, POST /chargewire/v1/auth/oauth/flows/f_1" {
		t.Fatalf("requests %q", fake.requests)
	}
	for _, secret := range fake.secrets {
		if secret != fakePollSecret {
			t.Fatalf("polled with %q", secret)
		}
	}
	if len(fake.sessions) != 1 || fake.sessions[0] != "Bearer server-token" {
		t.Fatalf("sessions opened with %q", fake.sessions)
	}
	for _, request := range fake.requests {
		if strings.Contains(request, "/oauth/initiate") || strings.Contains(request, "/oauth/token") {
			t.Fatalf("a 1.x sign-in route was called: %s", request)
		}
	}
}

func TestBrowserSignInFailures(t *testing.T) {
	withBrowser(t, func(string, string) error { return nil })
	for _, tc := range []struct {
		name    string
		polls   []fakeAnswer
		offer   bool
		timeout string
		status  adbc.Status
		text    string
	}{
		{"refused", []fakeAnswer{{200, map[string]any{"state": "failed", "error": map[string]string{
			"code": "access_denied", "detail": "access_denied: the user said no"}}}},
			true, "", adbc.StatusUnauthenticated, "the user said no (access_denied"},
		{"not authorized", []fakeAnswer{{200, map[string]any{"state": "failed", "error": map[string]string{
			"code": "invalid_credentials", "detail": "User 'mallory@evil.com' is not authorized"}}}},
			true, "", adbc.StatusUnauthenticated, "User 'mallory@evil.com' is not authorized (invalid_credentials"},
		{"expired", []fakeAnswer{{404, map[string]any{"code": "flow_not_found", "status": 404, "detail": "no such flow"}}},
			true, "", adbc.StatusUnauthenticated, "expired or was already collected"},
		{"timeout", nil, true, "0.05", adbc.StatusTimeout, "not completed within 50ms"},
		{"not configured", nil, false, "", adbc.StatusUnauthenticated, "does not offer browser sign-in"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := newFakeSignIn(t, tc.polls...)
			fake.offer = tc.offer
			options := fake.options()
			if tc.timeout != "" {
				options[OptionOAuthTimeoutSeconds] = tc.timeout
			}
			db, err := newDB(t, options)
			if err != nil {
				t.Fatal(err)
			}
			_, err = db.Open(context.Background())
			wantStatus(t, err, tc.status, tc.text)
			if len(fake.sessions) != 0 {
				t.Fatal("a session was opened")
			}
		})
	}
	db, err := newDB(t, map[string]string{adbc.OptionKeyURI: "hardcharger://127.0.0.1:1?tls=false",
		OptionAuthType: AuthTypeExternal})
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Open(context.Background())
	wantStatus(t, err, adbc.StatusIO, "connecting to")
}

func TestBrowserNoneOnlyPrintsTheURL(t *testing.T) {
	var commands []string
	withBrowser(t, func(command, _ string) error { commands = append(commands, command); return nil })
	fake := newFakeSignIn(t, completeAnswer)
	db, err := newDB(t, fake.options(OptionOAuthBrowser, "none"))
	if err != nil {
		t.Fatal(err)
	}
	cnxn, err := db.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_ = cnxn.Close(context.Background())
	// "none" reaches the Chargewire driver's opener, which prints the URL instead.
	if len(commands) != 1 || commands[0] != chargewire.BrowserNone {
		t.Fatalf("commands %q", commands)
	}
}

// HARDCHARGER_BROWSER is the default browser command.
func TestBrowserCommandFromTheEnvironment(t *testing.T) {
	t.Setenv("HARDCHARGER_BROWSER", "env-browser")
	var commands []string
	withBrowser(t, func(command, _ string) error { commands = append(commands, command); return nil })
	fake := newFakeSignIn(t, completeAnswer)
	db, err := newDB(t, fake.options())
	if err != nil {
		t.Fatal(err)
	}
	cnxn, err := db.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_ = cnxn.Close(context.Background())
	if len(commands) != 1 || commands[0] != "env-browser" {
		t.Fatalf("commands %q", commands)
	}
}

// The 1.x sign-in server's options are gone.
func TestSideSignInServerOptionsAreGone(t *testing.T) {
	for _, key := range []string{"adbc.hardcharger.oauth.url", "adbc.hardcharger.oauth.port"} {
		_, err := newDB(t, map[string]string{adbc.OptionKeyURI: "hardcharger://h", key: "x"})
		wantStatus(t, err, adbc.StatusNotImplemented, "unknown option "+key)
	}
}

func TestIngestPartsIsTheChargewireOption(t *testing.T) {
	ctx := context.Background()
	db, err := newDB(t, map[string]string{adbc.OptionKeyURI: "hardcharger://h", OptionIngestParts: "2"})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{OptionIngestParts, chargewireIngestParts} {
		if got, err := db.GetOption(ctx, key); err != nil || got != "2" {
			t.Errorf("GetOption(%s) = %q, %v", key, got, err)
		}
	}
	// The Chargewire driver validates it, under either name; the twin is above.
	_, err = newDB(t, map[string]string{adbc.OptionKeyURI: "hardcharger://h", OptionIngestParts: "none"})
	wantStatus(t, err, adbc.StatusInvalidArgument, chargewireIngestParts)
	// Unset, it is the Chargewire driver's default: auto (16 parts on a near link, 32 on a far one).
	db, err = newDB(t, map[string]string{adbc.OptionKeyURI: "hardcharger://h"})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := db.GetOption(ctx, OptionIngestParts); err != nil || got != "auto" {
		t.Errorf("default ingest parts %q, %v", got, err)
	}
	if got, err := db.GetOption(ctx, "adbc.chargewire.statement.result_partitions"); err != nil || got != "32" {
		t.Errorf("default result partitions %q, %v", got, err)
	}
}

// selfSignedPEM is a throwaway client certificate and key, for options that
// only need to parse.
func selfSignedPEM(t *testing.T) (certPEM, keyPEM string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "unit"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		EmailAddresses: []string{"unit@example.com"}, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, _ := x509.MarshalPKCS8PrivateKey(key)
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}))
}

// auth_type=client_certificate is the Chargewire driver's adbc.chargewire.auth.type:
// the Chargewire driver's own checks run on it (they need a certificate, TLS
// and no password), so a refusal from it proves the option reached it.
func TestClientCertificateAuthTypeIsTheChargewireOption(t *testing.T) {
	ctx := context.Background()
	cert, key := selfSignedPEM(t)
	withCertificate := func(extra map[string]string) map[string]string {
		opts := map[string]string{adbc.OptionKeyURI: "hardcharger://h", OptionAuthType: AuthTypeClientCertificate,
			"adbc.chargewire.tls.client_cert": cert, "adbc.chargewire.tls.client_key": key}
		for k, v := range extra {
			if v == "<unset>" {
				delete(opts, k)
			} else {
				opts[k] = v
			}
		}
		return opts
	}
	for name, tc := range map[string]struct {
		opts map[string]string
		text string
	}{
		"no certificate": {withCertificate(map[string]string{"adbc.chargewire.tls.client_cert": "<unset>",
			"adbc.chargewire.tls.client_key": "<unset>"}), "needs a client certificate: set adbc.chargewire.tls.client_cert and adbc.chargewire.tls.client_key"},
		"a password beside it": {withCertificate(map[string]string{adbc.OptionKeyUsername: "joe", adbc.OptionKeyPassword: "pw"}),
			"signs in with the client certificate alone; do not also set username, password"},
		"plain HTTP": {withCertificate(map[string]string{adbc.OptionKeyURI: "hardcharger://h?tls=false"}), "needs TLS"},
	} {
		_, err := newDB(t, tc.opts)
		if err == nil {
			t.Errorf("%s: accepted", name)
			continue
		}
		wantStatus(t, err, adbc.StatusInvalidArgument, tc.text)
	}
	// The twin: certificate and key over TLS, under either spelling.
	for _, spelling := range []string{AuthTypeClientCertificate, "mtls"} {
		db, err := newDB(t, withCertificate(map[string]string{OptionAuthType: spelling}))
		if err != nil {
			t.Fatalf("%s: %v", spelling, err)
		}
		for _, k := range []string{OptionAuthType, chargewireAuthType} {
			if got, err := db.GetOption(ctx, k); err != nil || got != AuthTypeClientCertificate {
				t.Errorf("%s: GetOption(%s) = %q, %v", spelling, k, got, err)
			}
		}
	}
	// Back to a password: the Chargewire driver is told too.
	db, err := newDB(t, withCertificate(nil))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetOption(ctx, OptionAuthType, AuthTypePassword); err != nil {
		t.Fatal(err)
	}
	if got, _ := db.GetOption(ctx, chargewireAuthType); got != AuthTypePassword {
		t.Errorf("chargewire auth type %q after auth_type=password", got)
	}
}

func TestResumeTimeoutIsTheChargewireOption(t *testing.T) {
	ctx := context.Background()
	db, err := newDB(t, map[string]string{adbc.OptionKeyURI: "hardcharger://h", OptionResumeTimeout: "45"})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{OptionResumeTimeout, chargewireResumeTimeout} {
		if got, err := db.GetOption(ctx, key); err != nil || got != "45" {
			t.Errorf("GetOption(%s) = %q, %v", key, got, err)
		}
	}
	// The Chargewire driver validates it, under either name; the twin is above.
	_, err = newDB(t, map[string]string{adbc.OptionKeyURI: "hardcharger://h", OptionResumeTimeout: "soon"})
	wantStatus(t, err, adbc.StatusInvalidArgument, chargewireResumeTimeout)
	// Unset, it is the Chargewire driver's default, 600 s.
	db, err = newDB(t, map[string]string{adbc.OptionKeyURI: "hardcharger://h"})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := db.GetOption(ctx, OptionResumeTimeout); err != nil || got != "600" {
		t.Errorf("default resume timeout %q, %v", got, err)
	}
}

// The setup a replacement session gets: the database's tags and settings, then
// what the connection set since (a later value of a setting replaces the
// earlier one in place; "" resets), in a stable order.
func TestSessionSetupStatements(t *testing.T) {
	cfg := newConfig()
	for _, kv := range [][2]string{{OptionSettingPrefix + "query_timeout", "12"}, {OptionQueryTag, `{"a":1}`},
		{OptionSettingPrefix + "bypass_queue", "true"}, {OptionSettingPrefix + "query_timeout", "13"}} {
		if err := cfg.set(kv[0], kv[1]); err != nil {
			t.Fatal(err)
		}
	}
	setup := newSessionSetup(cfg)
	setup.record("query_tag", `{"a":2}`)
	setup.record("bypass_queue", "")
	setup.record("memory_limit_per_query", "1GB")
	var got []string
	for _, s := range setup.statements() {
		got = append(got, setStatement(s.name, s.value, false))
	}
	want := []string{
		`SET SESSION hardcharger.query_tag = '{"a":2}'`,
		"SET SESSION hardcharger.query_timeout = '13'",
		"RESET SESSION hardcharger.bypass_queue",
		"SET SESSION hardcharger.memory_limit_per_query = '1GB'",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("statements\n%q\nwant\n%q", got, want)
	}
}

// stop makes the fake a stopped cluster's stand-in that refuses tokens (a
// person stopped the cluster, which ended the tokens issued until then).
func (f *fakeSignIn) stop(tokens ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.state = "stopped"
	if f.revoked == nil {
		f.revoked = map[string]bool{}
	}
	for _, token := range tokens {
		f.revoked[token] = true
	}
}

func (f *fakeSignIn) sessionsOpened() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.sessions)
}

func completeWith(token string) fakeAnswer {
	return fakeAnswer{200, map[string]any{"state": "complete", "access_token": token, "token_type": "Bearer",
		"expires_in": 3600}}
}

func runUpdate(ctx context.Context, cnxn adbc.ConnectionWithContext) error {
	stmt, err := cnxn.NewStatement(ctx)
	if err != nil {
		return err
	}
	defer stmt.Close(ctx)
	if err := stmt.SetSqlQuery(ctx, "CREATE TABLE t (x INT)"); err != nil {
		return err
	}
	_, err = stmt.ExecuteUpdate(ctx)
	return err
}

// A browser sign-in's token, from before a person stopped the cluster, is
// refused (token_revoked) at the first statement of a connection opened
// after the stop: the driver signs in through the browser again, once (with
// single sign-on, at once), and the statement runs with the new token.
func TestNewConnectionAfterAStopSignsInThroughTheBrowserAgain(t *testing.T) {
	opened := 0
	withBrowser(t, func(string, string) error { opened++; return nil })
	fake := newFakeSignIn(t, completeWith("token-1"), completeWith("token-2"))
	db, err := newDB(t, fake.options())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	first, err := db.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_ = first.Close(ctx)
	fake.stop("token-1")
	second, err := db.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close(ctx)
	if opened != 1 {
		t.Fatalf("browser opened %d times by the Opens, want once", opened)
	}
	if err := runUpdate(ctx, second); err != nil {
		t.Fatalf("first statement after the stop: %v", err)
	}
	if opened != 2 {
		t.Fatalf("browser opened %d times, want 2 (the sign-in again)", opened)
	}
	if got := fake.sessionsOpened(); !slices.Equal(got, []string{"Bearer token-1", "Bearer token-2"}) {
		t.Fatalf("sessions opened with %q, want token-1 then token-2", got)
	}
}

// A refusal after signing in again is the error: one browser sign-in again,
// not a loop.
func TestNewConnectionAfterAStopReportsASecondRefusal(t *testing.T) {
	opened := 0
	withBrowser(t, func(string, string) error { opened++; return nil })
	fake := newFakeSignIn(t, completeWith("token-1"), completeWith("token-2"), completeWith("token-3"))
	db, err := newDB(t, fake.options())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	first, err := db.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_ = first.Close(ctx)
	fake.stop("token-1", "token-2")
	second, err := db.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close(ctx)
	if err := runUpdate(ctx, second); err == nil || !strings.Contains(err.Error(), "token_revoked") {
		t.Fatalf("first statement: %v, want the second token_revoked refusal", err)
	}
	if opened != 2 {
		t.Fatalf("browser opened %d times, want 2 (one sign-in again)", opened)
	}
}

// The twin: a connection opened before the stop never signs in again on its
// own (§4.2): its refusal is the error, and no browser opens.
func TestConnectionFromBeforeAStopDoesNotOpenTheBrowser(t *testing.T) {
	opened := 0
	withBrowser(t, func(string, string) error { opened++; return nil })
	fake := newFakeSignIn(t, completeWith("token-1"), completeWith("token-2"))
	db, err := newDB(t, fake.options())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	cnxn, err := db.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer cnxn.Close(ctx)
	fake.stop("token-1")
	if err := runUpdate(ctx, cnxn); err == nil || !strings.Contains(err.Error(), "token_revoked") {
		t.Fatalf("statement after the stop: %v, want the token_revoked refusal", err)
	}
	if opened != 1 {
		t.Fatalf("browser opened %d times, want once (the first sign-in)", opened)
	}
}

// With a shared browser sign-in, the token from before the stop is dropped
// from the share before signing in again, so the new connection gets a new
// token rather than the shared old one.
func TestNewConnectionAfterAStopDropsTheSharedToken(t *testing.T) {
	ForgetSharedSignIns()
	t.Cleanup(ForgetSharedSignIns)
	opened := 0
	withBrowser(t, func(string, string) error { opened++; return nil })
	fake := newFakeSignIn(t, completeWith("token-1"), completeWith("token-2"))
	db, err := newDB(t, fake.options(OptionOAuthShareSignIn, "true", OptionOAuthSignInKey, "reauth-test"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	first, err := db.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_ = first.Close(ctx)
	fake.stop("token-1")
	second, err := db.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close(ctx)
	if err := runUpdate(ctx, second); err != nil {
		t.Fatalf("first statement after the stop: %v", err)
	}
	if got := fake.sessionsOpened(); opened != 2 || !slices.Equal(got, []string{"Bearer token-1", "Bearer token-2"}) {
		t.Fatalf("browser opened %d times, sessions opened with %q; want 2 and token-1 then token-2", opened, got)
	}
}
