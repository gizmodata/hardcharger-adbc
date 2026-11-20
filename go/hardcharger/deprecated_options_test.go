// Licensed under the Apache License, Version 2.0.

package hardcharger

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/gizmodata/chargewire-adbc/go/v2/chargewire"
)

func captureDeprecations(t *testing.T) *bytes.Buffer {
	t.Helper()
	var out bytes.Buffer
	previous := chargewire.SetDeprecationOutput(&out)
	t.Cleanup(func() { chargewire.SetDeprecationOutput(previous) })
	return &out
}

// Every option name from before the ADBC driver style guide reads as its
// current one: Hardcharger's own under hardcharger.*, the Chargewire driver's
// as chargewire.* (which this package passes on).
func TestOldOptionNames(t *testing.T) {
	captureDeprecations(t)
	for old, want := range map[string]string{
		"adbc.hardcharger.auth_type":                  OptionAuthType,
		"adbc.hardcharger.oauth.timeout_seconds":      OptionOAuthTimeoutSeconds,
		"adbc.hardcharger.oauth.browser":              OptionOAuthBrowser,
		"adbc.hardcharger.oauth.share_sign_in":        OptionOAuthShareSignIn,
		"adbc.hardcharger.oauth.sign_in_key":          OptionOAuthSignInKey,
		"adbc.hardcharger.session_tag":                OptionSessionTag,
		"adbc.hardcharger.query_tag":                  OptionQueryTag,
		"adbc.hardcharger.setting.query_timeout":      OptionSettingPrefix + "query_timeout",
		"adbc.hardcharger.global_setting.max_queries": OptionGlobalSettingPrefix + "max_queries",
		"adbc.hardcharger.ingest.parts":               chargewire.OptionIngestParts,
		"adbc.hardcharger.resume_timeout":             chargewire.OptionResumeTimeout,
		"adbc.chargewire.statement.timeout_seconds":   chargewire.OptionStatementTimeout,
		"adbc.chargewire.tls.root_certs":              chargewire.OptionTLSRootCerts,
		"adbc.warp.statement.query_tag":               chargewire.OptionQueryTag,
		// Current names.
		OptionQueryTag:                      OptionQueryTag,
		OptionAuthType:                      OptionAuthType,
		OptionOAuthBrowser:                  OptionOAuthBrowser,
		OptionIngestParts:                   chargewire.OptionIngestParts,
		OptionResumeTimeout:                 chargewire.OptionResumeTimeout,
		"hardcharger.query.timeout_seconds": chargewire.OptionStatementTimeout,
		"hardcharger.query.tag":             chargewire.OptionQueryTag,
		chargewire.OptionStatementTimeout:   chargewire.OptionStatementTimeout,
		// Not options: kept as written, so the error names them.
		"adbc.hardcharger.no_such_option":    "adbc.hardcharger.no_such_option",
		"adbc.hardcharger.setting.":          "adbc.hardcharger.setting.",
		"adbc.chargewire.tls.no_such_option": "adbc.chargewire.tls.no_such_option",
		"hardcharger.no_such_option":         "hardcharger.no_such_option",
		"uri":                                "uri",
	} {
		if got := currentOptionKey(old); got != want {
			t.Errorf("currentOptionKey(%q) = %q, want %q", old, got, want)
		}
	}
	// The current names are the style guide's.
	for _, key := range []string{OptionAuthType, OptionOAuthTimeoutSeconds, OptionOAuthBrowser,
		OptionOAuthShareSignIn, OptionOAuthSignInKey, OptionSessionTag, OptionQueryTag,
		OptionSettingPrefix, OptionGlobalSettingPrefix, OptionIngestParts, OptionResumeTimeout} {
		if !strings.HasPrefix(key, "hardcharger.") {
			t.Errorf("option %q doesn't start with hardcharger.", key)
		}
	}
}

// Old names warn once per process, in Hardcharger's terms, naming keys and
// never values; they set and read the same options as the current names.
func TestOldOptionNamesWarnOnce(t *testing.T) {
	out := captureDeprecations(t)
	ctx := context.Background()
	const secret = "s3cr3t-token-never-logged"
	for range 2 {
		db, err := newDB(t, map[string]string{
			adbc.OptionKeyURI:                   "hardcharger://h?tls=false",
			"adbc.hardcharger.query_tag":        `{"job": "old"}`,
			"adbc.hardcharger.resume_timeout":   "12",
			"adbc.chargewire.auth.access_token": secret,
			"adbc.warp.retry.max_attempts":      "3",
		})
		if err != nil {
			t.Fatal(err)
		}
		for key, want := range map[string]string{
			OptionQueryTag:                       `{"job": "old"}`,
			"adbc.hardcharger.query_tag":         `{"job": "old"}`,
			OptionResumeTimeout:                  "12",
			chargewire.OptionResumeTimeout:       "12",
			"hardcharger.retry.max_attempts":     "3",
			"adbc.chargewire.retry.max_attempts": "3",
		} {
			if got, err := db.GetOption(ctx, key); err != nil || got != want {
				t.Errorf("GetOption(%s) = %q, %v; want %q", key, got, err, want)
			}
		}
	}
	warnings := out.String()
	if strings.Contains(warnings, secret) {
		t.Fatalf("a deprecation warning printed an option value: %q", warnings)
	}
	for old, current := range map[string]string{
		"adbc.hardcharger.query_tag":         OptionQueryTag,
		"adbc.hardcharger.resume_timeout":    OptionResumeTimeout,
		"adbc.chargewire.auth.access_token":  "hardcharger.auth.access_token",
		"adbc.warp.retry.max_attempts":       "hardcharger.retry.max_attempts",
		"adbc.chargewire.retry.max_attempts": "hardcharger.retry.max_attempts",
	} {
		line := `[hardcharger] option "` + old + `" is deprecated; use "` + current + `"` + "\n"
		if n := strings.Count(warnings, line); n != 1 {
			t.Errorf("warning %q printed %d times, want once; all: %q", line, n, warnings)
		}
	}
	if strings.Contains(warnings, "[chargewire]") {
		t.Errorf("a warning in the Chargewire driver's terms: %q", warnings)
	}
}

// An unknown key under either prefix is refused under the name the caller
// wrote; the same option given under several names takes the current one,
// whatever the map's order.
func TestOptionNamesUnknownAndBoth(t *testing.T) {
	captureDeprecations(t)
	for _, key := range []string{"adbc.hardcharger.no_such_option", "hardcharger.no_such_option", "hardcharger.oauth.nonsense"} {
		_, err := newDB(t, map[string]string{adbc.OptionKeyURI: "hardcharger://h", key: "1"})
		wantStatus(t, err, adbc.StatusNotImplemented, "unknown option "+key)
	}
	_, err := newDB(t, map[string]string{adbc.OptionKeyURI: "hardcharger://h", "adbc.chargewire.tls.no_such_option": "1"})
	if err == nil || !strings.Contains(err.Error(), "adbc.chargewire.tls.no_such_option") {
		t.Errorf("unknown old Chargewire key: %v", err)
	}
	ctx := context.Background()
	for range 30 {
		db, err := newDB(t, map[string]string{adbc.OptionKeyURI: "hardcharger://h",
			"adbc.hardcharger.ingest.parts": "2", "adbc.chargewire.ingest.parts": "3",
			chargewire.OptionIngestParts: "4", OptionIngestParts: "5",
			"adbc.hardcharger.query_tag": `{"a": 1}`, OptionQueryTag: `{"a": 2}`})
		if err != nil {
			t.Fatal(err)
		}
		if got, _ := db.GetOption(ctx, OptionIngestParts); got != "5" {
			t.Fatalf("ingest parts %q, want hardcharger.ingest.parts' 5", got)
		}
		if got, _ := db.GetOption(ctx, OptionQueryTag); got != `{"a": 2}` {
			t.Fatalf("query tag %q, want the current name's", got)
		}
	}
}

// Hardcharger's own auth.type and oauth.* are not the Chargewire driver's
// options of the same names (other values): those stay chargewire.X, and
// their old names say so.
func TestOwnOptionsAreNotChargewires(t *testing.T) {
	out := captureDeprecations(t)
	for key, want := range map[string]string{
		OptionAuthType:                          OptionAuthType,
		OptionOAuthBrowser:                      OptionOAuthBrowser,
		"adbc.chargewire.auth.type":             chargewire.OptionAuthType,
		"adbc.chargewire.oauth.browser":         chargewire.OptionOAuthBrowser,
		"adbc.chargewire.oauth.timeout_seconds": chargewire.OptionOAuthTimeoutSeconds,
		chargewire.OptionAuthType:               chargewire.OptionAuthType,
	} {
		if got := currentOptionKey(key); got != want {
			t.Errorf("currentOptionKey(%q) = %q, want %q", key, got, want)
		}
	}
	for _, line := range []string{
		`[hardcharger] option "adbc.chargewire.auth.type" is deprecated; use "chargewire.auth.type"`,
		`[hardcharger] option "adbc.chargewire.oauth.browser" is deprecated; use "chargewire.oauth.browser"`,
	} {
		if !strings.Contains(out.String(), line+"\n") {
			t.Errorf("no warning %q in %q", line, out.String())
		}
	}
	// hardcharger.auth.type takes Hardcharger's values, not the Chargewire driver's.
	_, err := newDB(t, map[string]string{adbc.OptionKeyURI: "hardcharger://h", OptionAuthType: "oauth_browser"})
	wantStatus(t, err, adbc.StatusInvalidArgument, "expected password, external or client_certificate")
}

// A uri that doesn't parse may hold a password: no part of it reaches the error.
func TestUnparseableURIIsNotEchoed(t *testing.T) {
	for _, uri := range []string{"hardcharger://u:Pa%ssword@h:1", "hardcharger://h:1/%zzsecret", "chargewire://u:Pa%ssword@h:1"} {
		_, err := newDB(t, map[string]string{adbc.OptionKeyURI: uri})
		if err == nil {
			t.Errorf("%q accepted", uri)
			continue
		}
		for _, secret := range []string{"%ss", "ssword", "%zz", "secret"} {
			if strings.Contains(uri, secret) && strings.Contains(err.Error(), secret) {
				t.Errorf("the error for %q quotes %q: %v", uri, secret, err)
			}
		}
	}
}
