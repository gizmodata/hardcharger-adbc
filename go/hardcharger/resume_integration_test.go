// Licensed under the Apache License, Version 2.0.

//go:build integration

package hardcharger

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
)

// resumeFor answers the next requests whose path starts with prefix (under
// /chargewire/v1/) with server_resuming.
func (p *sessionProxy) resumeFor(requests int, prefix string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.resuming, p.resumingPrefix = requests, prefix
}

// adbc.hardcharger.resume_timeout is the Chargewire driver's
// adbc.chargewire.resume_timeout: while the server answers server_resuming
// (here a proxy, for the sign-in and the next request), the driver waits each
// answer's Retry-After and sends the request again, until the timeout; with 0
// it fails at once with server_resuming (StatusIO).
func TestIntegrationResumeTimeout(t *testing.T) {
	proxy := newSessionProxy(t, shared)
	proxy.resumeFor(2, "")
	start := time.Now()
	db, cnxn, err := openThrough(t, proxy, map[string]string{OptionResumeTimeout: "30"})
	if err != nil {
		t.Fatalf("Open while the server resumes: %v", err)
	}
	if waited := time.Since(start); waited < 2*time.Second {
		t.Fatalf("Open took %v: the driver did not wait Retry-After (1 s) for each of two answers", waited)
	}
	if got := scalar(t, cnxn, "SELECT 42"); got != "42" {
		t.Fatalf("SELECT 42 = %s", got)
	}
	for _, key := range []string{OptionResumeTimeout, chargewireResumeTimeout} {
		if got, err := db.(adbc.GetSetOptionsWithContext).GetOption(context.Background(), key); err != nil || got != "30" {
			t.Fatalf("GetOption(%s) = %q, %v; want 30", key, got, err)
		}
	}

	// The sign-in only: the first connection's own background requests (freeing
	// its statements) must not take the answer.
	proxy.resumeFor(1, "auth/token")
	_, _, err = openThrough(t, proxy, map[string]string{OptionResumeTimeout: "0"})
	wantStatus(t, err, adbc.StatusIO, "")
	if code := chargewireCode(err); code != "server_resuming" {
		t.Fatalf("code %q (%v), want server_resuming", code, err)
	}
}

func openThrough(t *testing.T, proxy *sessionProxy, extra map[string]string) (adbc.DatabaseWithContext, adbc.ConnectionWithContext, error) {
	t.Helper()
	options := map[string]string{adbc.OptionKeyURI: "hardcharger://" + strings.TrimPrefix(proxy.server.URL, "http://") + "?tls=false"}
	for key, value := range extra {
		options[key] = value
	}
	return open(t, shared, options)
}
