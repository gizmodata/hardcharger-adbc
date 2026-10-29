// Licensed under the Apache License, Version 2.0.

package hardcharger

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/gizmodata/chargewire-adbc/go/v2/chargewire"
)

// --- shared browser sign-in -------------------------------------------------------------------

func TestRenewTime(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	for name, tc := range map[string]struct {
		lifetime time.Duration
		want     time.Time
	}{
		"an hour":          {time.Hour, now.Add(time.Hour - time.Minute)},
		"short: a quarter": {8 * time.Second, now.Add(6 * time.Second)},
		"unknown":          {0, time.Time{}},
	} {
		if got := renewTime(tc.lifetime, now); !got.Equal(tc.want) {
			t.Errorf("%s: %v, want %v", name, got, tc.want)
		}
	}
}

func TestSharedSignInRunsOnceForConcurrentCallers(t *testing.T) {
	shared := &sharedSignIn{running: make(chan struct{}, 1)}
	var runs atomic.Int64
	token := "server-token"
	signIn := func(context.Context) (string, time.Duration, error) {
		runs.Add(1)
		time.Sleep(50 * time.Millisecond) // the user, in the browser
		return token, time.Hour, nil
	}
	var wg sync.WaitGroup
	var freshCount atomic.Int64
	for range 16 {
		wg.Go(func() {
			got, fresh, err := shared.get(context.Background(), signIn)
			if err != nil || got != token {
				t.Errorf("get: %q, %v", got, err)
			}
			if fresh {
				freshCount.Add(1)
			}
		})
	}
	wg.Wait()
	if runs.Load() != 1 || freshCount.Load() != 1 {
		t.Fatalf("%d sign-ins (%d fresh), want 1", runs.Load(), freshCount.Load())
	}
}

func TestSharedSignInRunsAgainNearExpiryAndAfterForget(t *testing.T) {
	shared := &sharedSignIn{running: make(chan struct{}, 1)}
	runs := 0
	lifetime := 400 * time.Millisecond
	signIn := func(context.Context) (string, time.Duration, error) {
		runs++
		return fmt.Sprintf("token-%d", runs), lifetime, nil
	}
	first, _, _ := shared.get(context.Background(), signIn)
	if again, fresh, _ := shared.get(context.Background(), signIn); again != first || fresh {
		t.Fatal("a token far from its expiry was not reused")
	}
	time.Sleep(lifetime * 3 / 4) // past exp - lifetime/4
	second, fresh, _ := shared.get(context.Background(), signIn)
	if second == first || !fresh || runs != 2 {
		t.Fatalf("a token close to its expiry was handed out (runs %d)", runs)
	}
	shared.forget(first) // an old token: no effect
	if got, _, _ := shared.get(context.Background(), signIn); got != second {
		t.Fatal("forgetting an old token dropped the current one")
	}
	shared.forget(second)
	if got, fresh, _ := shared.get(context.Background(), signIn); got == second || !fresh || runs != 3 {
		t.Fatalf("a forgotten token was handed out (runs %d)", runs)
	}
}

func TestSharedSignInFailureIsNotShared(t *testing.T) {
	shared := &sharedSignIn{running: make(chan struct{}, 1)}
	runs := 0
	failing := func(context.Context) (string, time.Duration, error) { runs++; return "", 0, errors.New("no") }
	for range 2 {
		if _, _, err := shared.get(context.Background(), failing); err == nil {
			t.Fatal("a failed sign-in succeeded")
		}
	}
	if runs != 2 {
		t.Fatalf("%d sign-ins, want each caller to try", runs)
	}
}

func TestSharedSignInWaiterGivesUp(t *testing.T) {
	shared := &sharedSignIn{running: make(chan struct{}, 1)}
	release := make(chan struct{})
	go func() {
		_, _, _ = shared.get(context.Background(), func(context.Context) (string, time.Duration, error) {
			<-release
			return "t", time.Hour, nil
		})
	}()
	time.Sleep(20 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, _, err := shared.get(ctx, func(context.Context) (string, time.Duration, error) { return "other", time.Hour, nil })
	wantStatus(t, err, adbc.StatusTimeout, "gave up waiting for the shared sign-in")
	close(release)
}

func TestSignInKeys(t *testing.T) {
	base := signInKeyFor("chargewire://h:31337", nil, "")
	same := []signInKey{
		signInKeyFor("chargewire://H:31337/", map[string]string{"adbc.chargewire.retry.max_attempts": "2"}, ""),
	}
	different := map[string]signInKey{
		"another server":    signInKeyFor("chargewire://h:31338", nil, ""),
		"another host":      signInKeyFor("chargewire://other:31337", nil, ""),
		"plain HTTP":        signInKeyFor("chargewire://h:31337?tls=false", nil, ""),
		"another base path": signInKeyFor("chargewire://h:31337/x", nil, ""),
		"other roots":       signInKeyFor("chargewire://h:31337", map[string]string{"adbc.chargewire.tls.root_certs": "pem"}, ""),
		"skip verify":       signInKeyFor("chargewire://h:31337", map[string]string{"adbc.chargewire.tls.skip_verify": "true"}, ""),
		"server name":       signInKeyFor("chargewire://h:31337", map[string]string{"adbc.chargewire.tls.server_name": "x"}, ""),
		"another key":       signInKeyFor("chargewire://h:31337", nil, "alice"),
	}
	for _, key := range same {
		if key != base {
			t.Errorf("%+v != %+v", key, base)
		}
	}
	for name, key := range different {
		if key == base {
			t.Errorf("%s: same key", name)
		}
	}
}

func TestShareSignInOptions(t *testing.T) {
	ctx := context.Background()
	db, err := NewDriver(memory.DefaultAllocator).NewDatabaseWithContext(ctx, map[string]string{
		"uri": "hardcharger://127.0.0.1:1", OptionAuthType: AuthTypeExternal,
		OptionOAuthShareSignIn: "true", OptionOAuthSignInKey: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close(ctx)
	for key, want := range map[string]string{OptionOAuthShareSignIn: "true", OptionOAuthSignInKey: "alice"} {
		if got, err := db.(*database).GetOption(ctx, key); err != nil || got != want {
			t.Errorf("%s = %q, %v; want %q", key, got, err, want)
		}
	}
	_, err = NewDriver(memory.DefaultAllocator).NewDatabaseWithContext(ctx, map[string]string{
		"uri": "hardcharger://127.0.0.1:1", OptionOAuthShareSignIn: "sometimes"})
	wantStatus(t, err, adbc.StatusInvalidArgument, "expected true or false")
	// Sharing is for browser sign-ins: with a password it is refused, before anything is sent.
	db, err = NewDriver(memory.DefaultAllocator).NewDatabaseWithContext(ctx, map[string]string{
		"uri": "hardcharger://127.0.0.1:1", "username": "u", "password": "p", OptionOAuthShareSignIn: "true"})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close(ctx)
	_, err = db.Open(ctx)
	wantStatus(t, err, adbc.StatusInvalidArgument, "needs adbc.hardcharger.auth_type=external")
}

// --- TLS to a plain-HTTP server -----------------------------------------------------------------

// plainHTTPListener answers anything with an HTTP 400 and records what it got.
type plainHTTPListener struct {
	listener net.Listener
	accepted atomic.Int64
	mu       sync.Mutex
	received bytes.Buffer
}

func newPlainHTTPListener(t *testing.T) *plainHTTPListener {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &plainHTTPListener{listener: listener}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			p.accepted.Add(1)
			go func() {
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
				buf := make([]byte, 4096)
				for first := true; ; first = false {
					n, err := conn.Read(buf)
					p.mu.Lock()
					p.received.Write(buf[:n])
					p.mu.Unlock()
					if first {
						_, _ = io.WriteString(conn, "HTTP/1.1 400 Bad Request\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
					}
					if err != nil {
						return
					}
				}
			}()
		}
	}()
	t.Cleanup(func() { _ = listener.Close() })
	return p
}

func (p *plainHTTPListener) port() int { return p.listener.Addr().(*net.TCPAddr).Port }

func (p *plainHTTPListener) bytes() []byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	return bytes.Clone(p.received.Bytes())
}

func TestTLSToAPlainHTTPServerSaysToTurnTLSOff(t *testing.T) {
	server := newPlainHTTPListener(t)
	ctx := context.Background()
	const password = "pl41n:p@ss-never-sent"
	db, err := NewDriver(memory.DefaultAllocator).NewDatabaseWithContext(ctx, map[string]string{
		"uri": fmt.Sprintf("hardcharger://127.0.0.1:%d", server.port()), "username": "plain_user", "password": password})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close(ctx)
	_, err = db.Open(ctx)
	want := fmt.Sprintf("[chargewire] The server at 127.0.0.1:%d speaks plain HTTP, not TLS. "+
		"Add ?tls=false to the URI to connect without encryption.", server.port())
	var adbcErr adbc.Error
	if !errors.As(err, &adbcErr) || adbcErr.Code != adbc.StatusIO || adbcErr.Msg != want {
		t.Fatalf("error %v, want %q", err, want)
	}
	if got := server.accepted.Load(); got != 1 {
		t.Errorf("%d connections, want 1 (no retries, no plaintext fallback)", got)
	}
	received := server.bytes()
	if len(received) == 0 || received[0] != 0x16 {
		t.Errorf("the server got %q, want a TLS hello only", received)
	}
	for _, secret := range []string{password, "plain_user", "auth/token"} {
		if bytes.Contains(received, []byte(secret)) {
			t.Errorf("%q reached the plain-HTTP server", secret)
		}
	}
}

// Browser sign-in over TLS to a server that speaks plain HTTP: the Chargewire
// driver's hint, before any flow is created or a browser opened.
func TestBrowserSignInToAPlainHTTPServer(t *testing.T) {
	server := newPlainHTTPListener(t)
	opened := 0
	withBrowser(t, func(string, string) error { opened++; return nil })
	ctx := context.Background()
	db, err := NewDriver(memory.DefaultAllocator).NewDatabaseWithContext(ctx, map[string]string{
		"uri": fmt.Sprintf("hardcharger://127.0.0.1:%d", server.port()), OptionAuthType: AuthTypeExternal})
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Open(ctx)
	_ = db.Close(ctx)
	wantStatus(t, err, adbc.StatusIO, fmt.Sprintf("The server at 127.0.0.1:%d speaks plain HTTP, not TLS. "+
		"Add ?tls=false to the URI to connect without encryption.", server.port()))
	if opened != 0 {
		t.Fatalf("the browser was opened %d times", opened)
	}
	if bytes.Contains(server.bytes(), []byte("oauth")) {
		t.Fatal("a sign-in request reached the plain-HTTP server")
	}
}

func TestARefusedSharedTokenIsReplacedOnlyForRenewableReasons(t *testing.T) {
	// Chargewire §4.2: sign in again after no reason, token_expired or an unknown reason;
	// not after token_revoked, credentials_changed or principal_dropped.
	refusal := func(reason string) adbc.Error {
		err := adbc.Error{Code: adbc.StatusUnauthenticated, Msg: "refused"}
		if reason != "" {
			err.Details = []adbc.ErrorDetail{&adbc.TextErrorDetail{Name: chargewire.ErrorDetailReason, Detail: reason}}
		}
		return err
	}
	for reason, want := range map[string]bool{
		"": true, "token_expired": true, "some_later_reason": true,
		"token_revoked": false, "credentials_changed": false, "principal_dropped": false,
	} {
		if got := renewableRefusal(refusal(reason)); got != want {
			t.Errorf("renewableRefusal(%q) = %v, want %v", reason, got, want)
		}
	}
}
