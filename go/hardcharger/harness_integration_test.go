// Licensed under the Apache License, Version 2.0.

//go:build integration

// Live-server tests. They start the hardcharger-server binary named by
// HARDCHARGER_ADBC_TEST_SERVER_BINARY on free loopback ports, with a fresh
// admin password containing ':' and '@'. Without it they fail; they never
// skip, and a run in which no integration test ran fails too. The servers get
// a clean environment: no HARDCHARGER_* variable of the shell reaches them,
// and each has a HOME of its own (its test directory), never the user's.
//
//	HARDCHARGER_ADBC_TEST_SERVER_BINARY=/path/to/hardcharger-server go test -race -tags integration ./...

package hardcharger

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

const (
	serverBinaryEnv = "HARDCHARGER_ADBC_TEST_SERVER_BINARY"
	testUsername    = "hc_adbc_test"
)

// integrationTests counts tests that reached a server, so a run where none
// did fails instead of passing vacuously.
var integrationTests atomic.Int64

var shared *testServer

func TestMain(m *testing.M) {
	if os.Getenv(serverBinaryEnv) == "" {
		fmt.Fprintf(os.Stderr, "FAIL: set %s to a hardcharger-server binary to run the integration tests\n", serverBinaryEnv)
		os.Exit(1)
	}
	server, err := launchServer(nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FAIL: %v\n", err)
		os.Exit(1)
	}
	shared = server
	code := m.Run()
	server.stop()
	if code == 0 && integrationTests.Load() == 0 {
		fmt.Fprintln(os.Stderr, "FAIL: no integration test ran")
		code = 1
	}
	os.Exit(code)
}

type testServer struct {
	cmd      *exec.Cmd
	port     int
	password string
	tls      bool
	log      *syncBuffer
	dir      string
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func freePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

func cleanEnvironment() []string {
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "HARDCHARGER_") && !strings.HasPrefix(kv, "HOME=") {
			env = append(env, kv)
		}
	}
	return env
}

func launchServer(extraArgs []string) (*testServer, error) {
	port, err := freePort()
	if err != nil {
		return nil, err
	}
	secret := make([]byte, 9)
	_, _ = rand.Read(secret)
	dir, err := os.MkdirTemp("", "hardcharger-adbc-test-")
	if err != nil {
		return nil, err
	}
	server := &testServer{port: port, password: hex.EncodeToString(secret) + ":p@ss:word", log: &syncBuffer{}, dir: dir}
	// DuckDB's persistent secrets in the test's own directory, never ~/.duckdb's.
	args := []string{"--host", "127.0.0.1", "--port", strconv.Itoa(port), "--health-port", "0",
		"--database", ":memory:", "--admin-username", testUsername, "--print-queries", "false",
		"--init-sql-commands", "SET secret_directory = '" + filepath.ToSlash(filepath.Join(dir, "secrets")) + "'"}
	for i, arg := range extraArgs {
		if arg == "--tls-cert" {
			server.tls = true
		}
		// {base_url}: the server's own URL, for flags that need it before it runs.
		extraArgs[i] = strings.ReplaceAll(arg, "{base_url}", fmt.Sprintf("http://127.0.0.1:%d", port))
	}
	server.cmd = exec.Command(os.Getenv(serverBinaryEnv), append(args, extraArgs...)...)
	server.cmd.Dir = dir
	server.cmd.Env = append(cleanEnvironment(), "HARDCHARGER_ADMIN_PASSWORD="+server.password, "HOME="+dir)
	server.cmd.Stdout, server.cmd.Stderr = server.log, server.log
	if err := server.cmd.Start(); err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 2 * time.Second}
	scheme := "http"
	if server.tls {
		scheme = "https"
		client.Transport = insecureTransport()
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := client.Get(fmt.Sprintf("%s://127.0.0.1:%d/chargewire", scheme, port))
		if err == nil {
			_ = resp.Body.Close()
			return server, nil
		}
		if strings.Contains(err.Error(), "certificate required") {
			return server, nil // it requires client certificates (--mtls-ca-cert), and answered the handshake
		}
		time.Sleep(100 * time.Millisecond)
	}
	server.stop()
	return nil, fmt.Errorf("hardcharger-server did not answer on port %d; its log:\n%s", port, server.log.String())
}

func (s *testServer) stop() {
	if s.cmd.Process != nil {
		_ = s.cmd.Process.Signal(syscall.SIGTERM)
		_ = s.cmd.Process.Signal(syscall.SIGTERM) // the second stops without draining
		done := make(chan struct{})
		go func() { _ = s.cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(20 * time.Second):
			_ = s.cmd.Process.Kill()
		}
	}
	_ = os.RemoveAll(s.dir)
}

func (s *testServer) uri() string {
	if s.tls {
		return fmt.Sprintf("hardcharger://127.0.0.1:%d", s.port)
	}
	return fmt.Sprintf("hardcharger://127.0.0.1:%d?tls=false", s.port)
}

func startServer(t *testing.T, extraArgs ...string) *testServer {
	t.Helper()
	server, err := launchServer(extraArgs)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.stop)
	return server
}

// open connects to server with the admin's credentials and extra options.
func open(t *testing.T, server *testServer, extra map[string]string) (adbc.DatabaseWithContext, adbc.ConnectionWithContext, error) {
	t.Helper()
	integrationTests.Add(1)
	opts := map[string]string{adbc.OptionKeyURI: server.uri(), adbc.OptionKeyUsername: testUsername,
		adbc.OptionKeyPassword: server.password}
	for key, value := range extra {
		if value == "<unset>" {
			delete(opts, key)
		} else {
			opts[key] = value
		}
	}
	db, err := NewDriver(memory.NewCheckedAllocator(memory.DefaultAllocator)).NewDatabaseWithContext(context.Background(), opts)
	if err != nil {
		return nil, nil, err
	}
	t.Cleanup(func() { _ = db.Close(context.Background()) })
	cnxn, err := db.Open(context.Background())
	if err != nil {
		return db, nil, err
	}
	t.Cleanup(func() { _ = cnxn.Close(context.Background()) })
	return db, cnxn, nil
}

func mustOpen(t *testing.T, server *testServer, extra map[string]string) adbc.ConnectionWithContext {
	t.Helper()
	_, cnxn, err := open(t, server, extra)
	if err != nil {
		t.Fatalf("%v\nserver log:\n%s", err, server.log.String())
	}
	return cnxn
}

// scalar runs sql and returns the first column of its first row as text.
func scalar(t *testing.T, cnxn adbc.ConnectionWithContext, sql string) string {
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
	reader, _, err := stmt.ExecuteQuery(ctx)
	if err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	defer reader.Release()
	for reader.Next() {
		rec := reader.RecordBatch()
		if rec.NumRows() == 0 {
			continue
		}
		if rec.Column(0).IsNull(0) {
			return "<null>"
		}
		return rec.Column(0).ValueStr(0)
	}
	if err := reader.Err(); err != nil {
		t.Fatal(err)
	}
	return "<no rows>"
}

// eventually polls sql until it answers want (instrumentation is written
// asynchronously).
func eventually(t *testing.T, cnxn adbc.ConnectionWithContext, sql, want string) {
	t.Helper()
	var got string
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		if got = scalar(t, cnxn, sql); got == want {
			return
		}
	}
	t.Fatalf("%s -> %q, want %q", sql, got, want)
}

// info returns GetInfo's string values.
func info(t *testing.T, cnxn adbc.ConnectionWithContext) map[adbc.InfoCode]string {
	t.Helper()
	reader, err := cnxn.GetInfo(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Release()
	values := map[adbc.InfoCode]string{}
	for reader.Next() {
		rec := reader.RecordBatch()
		codes := rec.Column(0).(*array.Uint32)
		union := rec.Column(1).(*array.DenseUnion)
		for i := range int(rec.NumRows()) {
			if s, ok := union.Field(union.ChildID(i)).(*array.String); ok {
				values[adbc.InfoCode(codes.Value(i))] = s.Value(int(union.ValueOffset(i)))
			}
		}
	}
	return values
}

// chargewireCode is the Chargewire error code the driver put in an error's details.
func chargewireCode(err error) string {
	var adbcErr adbc.Error
	if !errorsAsADBC(err, &adbcErr) {
		return ""
	}
	for _, detail := range adbcErr.Details {
		if detail.Key() == "chargewire.code" {
			if value, err := detail.Serialize(); err == nil {
				return string(value)
			}
		}
	}
	return ""
}
