// Licensed under the Apache License, Version 2.0.

package hardcharger

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
)

// A shared browser sign-in: with OptionOAuthShareSignIn, the databases of one
// process that sign in through the browser to the same server, with the same
// TLS trust and the same OptionOAuthSignInKey, share one sign-in. The first to open a connection runs it (the others wait
// for it); the rest reuse the server's access token it yielded, until the token
// is close to its expiry (its expires_in), when the next database to open runs
// a new sign-in. A shared token that the server refuses
// is forgotten, and that database signs in again (once). Tokens never cross
// servers or sign-in keys, and live only in this process's memory.

// sharedSignInMargin is how long before its expiry a shared token stops being
// handed out (at most; a quarter of a shorter lifetime), so that a database
// that takes it still has time to use it.
const sharedSignInMargin = time.Minute

// signInKey says which databases may share a sign-in.
type signInKey struct {
	server string // the Chargewire server: scheme, host, port and base path
	trust  string // the TLS trust (root certificates, skip_verify, server name)
	label  string // OptionOAuthSignInKey
}

// sharedSignIn is one key's sign-in.
type sharedSignIn struct {
	// running is held (a 1-buffered channel, so a waiter can give up) by the
	// database whose sign-in is running.
	running chan struct{}

	mu      sync.Mutex
	token   string
	renewAt time.Time // when to stop handing the token out; zero when unknown
}

var sharedSignIns = struct {
	mu      sync.Mutex
	entries map[signInKey]*sharedSignIn
}{entries: map[signInKey]*sharedSignIn{}}

// ForgetSharedSignIns drops every shared browser sign-in of this process
// (OptionOAuthShareSignIn), for example when the user signs out: the next
// database to open a connection signs in again. Open connections keep their
// sessions and access tokens.
func ForgetSharedSignIns() {
	sharedSignIns.mu.Lock()
	defer sharedSignIns.mu.Unlock()
	sharedSignIns.entries = map[signInKey]*sharedSignIn{}
}

func sharedSignInFor(key signInKey) *sharedSignIn {
	sharedSignIns.mu.Lock()
	defer sharedSignIns.mu.Unlock()
	entry := sharedSignIns.entries[key]
	if entry == nil {
		entry = &sharedSignIn{running: make(chan struct{}, 1)}
		sharedSignIns.entries[key] = entry
	}
	return entry
}

// get returns the key's access token, running signIn when there is none that
// is still good. fresh reports that this call signed in. Concurrent callers
// wait for one sign-in, or until ctx ends.
func (s *sharedSignIn) get(ctx context.Context,
	signIn func(context.Context) (string, time.Duration, error)) (token string, fresh bool, err error) {
	select {
	case s.running <- struct{}{}:
	case <-ctx.Done():
		status := adbc.StatusCancelled
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			status = adbc.StatusTimeout
		}
		return "", false, oauthError(status, "gave up waiting for the shared sign-in: %v", ctx.Err())
	}
	defer func() { <-s.running }()
	if token := s.current(time.Now()); token != "" {
		return token, false, nil
	}
	token, lifetime, err := signIn(ctx)
	if err != nil {
		return "", false, err
	}
	s.mu.Lock()
	s.token, s.renewAt = token, renewTime(lifetime, time.Now())
	s.mu.Unlock()
	return token, true, nil
}

// current is the token when it is still good at now, else "".
func (s *sharedSignIn) current(now time.Time) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.token == "" || (!s.renewAt.IsZero() && !now.Before(s.renewAt)) {
		return ""
	}
	return s.token
}

// forget drops token, unless another sign-in already replaced it.
func (s *sharedSignIn) forget(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.token == token {
		s.token, s.renewAt = "", time.Time{}
	}
}

// renewTime is when a token with this lifetime, issued at now, stops being
// shared: its expiry less a margin. It is zero for an unknown lifetime; such a
// token is shared until the server refuses it.
func renewTime(lifetime time.Duration, now time.Time) time.Time {
	if lifetime <= 0 {
		return time.Time{}
	}
	return now.Add(lifetime - min(sharedSignInMargin, lifetime/4))
}

// signInKeyFor is the key of a database's browser sign-in.
func signInKeyFor(chargewireURI string, chargewireOptions map[string]string, label string) signInKey {
	server := chargewireURI
	if u, err := url.Parse(chargewireURI); err == nil {
		tls := "tls"
		if !usesTLS(chargewireURI) {
			tls = "plain"
		}
		server = tls + "://" + strings.ToLower(u.Host) + strings.TrimSuffix(u.Path, "/")
	}
	trust := sha256.New()
	for _, option := range []string{"adbc.chargewire.tls.root_certs", "adbc.chargewire.tls.skip_verify", "adbc.chargewire.tls.server_name"} {
		trust.Write([]byte(option + "=" + chargewireOptions[option] + "\x00"))
	}
	return signInKey{server: server, trust: hex.EncodeToString(trust.Sum(nil)), label: label}
}
