// Licensed under the Apache License, Version 2.0.

//go:build integration

package hardcharger

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// The driver inherits the Chargewire driver's defaults: a large result is split
// into up to 32 partitions, and an upload of two batches or more goes in up to
// 16 parts on a near link, 32 on a far one (`auto`; §6.10, §8.3).

// partitionCount runs sql with ExecutePartitions and returns how many
// partitions the server split its result into.
func partitionCount(t *testing.T, cnxn adbc.ConnectionWithContext, sql string) uint64 {
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
	_, partitions, _, err := stmt.ExecutePartitions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return partitions.NumPartitions
}

const largeResult = "SELECT i, i * 2 AS j FROM range(2000000) t(i)"

func TestIntegrationALargeResultIsSplitByDefault(t *testing.T) {
	if n := partitionCount(t, mustOpen(t, shared, nil), largeResult); n < 2 || n > 32 {
		t.Fatalf("%d partitions, want 2 to 32 (the default asks for up to 32)", n)
	}
	// The twin: one partition when asked for one.
	cnxn := mustOpen(t, shared, map[string]string{"adbc.chargewire.statement.result_partitions": "1"})
	if n := partitionCount(t, cnxn, largeResult); n != 1 {
		t.Fatalf("%d partitions with result_partitions=1", n)
	}
}

// recordingProxy forwards to a server and records each request's method and path.
type recordingProxy struct {
	mu       sync.Mutex
	requests []string
	server   *httptest.Server
}

func newRecordingProxy(t *testing.T, target *testServer) *recordingProxy {
	backend, _ := url.Parse("http://127.0.0.1:" + strconv.Itoa(target.port))
	p := &recordingProxy{}
	forward := httputil.NewSingleHostReverseProxy(backend)
	p.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		p.requests = append(p.requests, r.Method+" "+r.URL.Path)
		p.mu.Unlock()
		forward.ServeHTTP(w, r)
	}))
	t.Cleanup(p.server.Close)
	return p
}

// ingestRequests counts single-stream ingests, partitioned-ingest opens and parts.
func (p *recordingProxy) ingestRequests() (single, opens, parts int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, r := range p.requests {
		switch {
		case strings.HasPrefix(r, "POST ") && strings.HasSuffix(r, "/ingest"):
			single++
		case strings.HasPrefix(r, "POST ") && strings.HasSuffix(r, "/ingests"):
			opens++
		case strings.HasPrefix(r, "POST ") && strings.Contains(r, "/parts/"):
			parts++
		}
	}
	return
}

// ingest uploads 16 batches of 5,000 rows into table and checks they all arrived.
func ingest(t *testing.T, cnxn adbc.ConnectionWithContext, table string) {
	t.Helper()
	ctx := context.Background()
	schema := arrow.NewSchema([]arrow.Field{{Name: "k", Type: arrow.PrimitiveTypes.Int64}}, nil)
	var batches []arrow.RecordBatch
	for b := range 16 {
		builder := array.NewRecordBuilder(memory.DefaultAllocator, schema)
		for r := range 5000 {
			builder.Field(0).(*array.Int64Builder).Append(int64(b*5000 + r))
		}
		batches = append(batches, builder.NewRecordBatch())
		builder.Release()
	}
	reader, err := array.NewRecordReader(schema, batches)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Release()
	for _, b := range batches {
		b.Release()
	}
	// The shared server outlives a test: start clean, so the suite can run with -count.
	execUpdate(t, cnxn, "DROP TABLE IF EXISTS "+table)
	stmt, err := cnxn.NewStatement(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close(ctx)
	for key, value := range map[string]string{adbc.OptionKeyIngestTargetTable: table, adbc.OptionKeyIngestMode: adbc.OptionValueIngestModeCreate} {
		if err := stmt.(adbc.GetSetOptionsWithContext).SetOption(ctx, key, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := stmt.BindStream(ctx, reader); err != nil {
		t.Fatal(err)
	}
	if n, err := stmt.ExecuteUpdate(ctx); err != nil || n != 16*5000 {
		t.Fatalf("ingested %d rows, %v", n, err)
	}
	if got := scalar(t, cnxn, "SELECT count(DISTINCT k) FROM "+table); got != strconv.Itoa(16*5000) {
		t.Fatalf("%s distinct rows", got)
	}
}

func proxiedOpen(t *testing.T, extra map[string]string) (*recordingProxy, adbc.ConnectionWithContext) {
	proxy := newRecordingProxy(t, shared)
	options := map[string]string{adbc.OptionKeyURI: "hardcharger://" + strings.TrimPrefix(proxy.server.URL, "http://") + "?tls=false"}
	for key, value := range extra {
		options[key] = value
	}
	return proxy, mustOpen(t, shared, options)
}

func TestIntegrationAnIngestGoesInPartsByDefault(t *testing.T) {
	proxy, cnxn := proxiedOpen(t, nil)
	ingest(t, cnxn, "default_parts")
	if single, opens, parts := proxy.ingestRequests(); single != 0 || opens != 1 || parts < 2 || parts > 32 {
		t.Fatalf("%d single, %d opens, %d parts; want one partitioned ingest of 2 to 32 parts", single, opens, parts)
	}
}

func TestIntegrationIngestPartsOption(t *testing.T) {
	// Hardcharger's name: one part is one ordered stream (the twin of the default).
	proxy, cnxn := proxiedOpen(t, map[string]string{OptionIngestParts: "1"})
	ingest(t, cnxn, "one_part")
	if single, opens, parts := proxy.ingestRequests(); single != 1 || opens != 0 || parts != 0 {
		t.Fatalf("%d single, %d opens, %d parts; want one stream", single, opens, parts)
	}
	// The Chargewire name works too.
	proxy, cnxn = proxiedOpen(t, map[string]string{"adbc.chargewire.ingest.parts": "2"})
	ingest(t, cnxn, "two_parts")
	if _, opens, parts := proxy.ingestRequests(); opens != 1 || parts != 2 {
		t.Fatalf("%d opens, %d parts; want 2 parts", opens, parts)
	}
	// And on a connection.
	proxy, cnxn = proxiedOpen(t, nil)
	if err := cnxn.(adbc.GetSetOptionsWithContext).SetOption(context.Background(), OptionIngestParts, "3"); err != nil {
		t.Fatal(err)
	}
	ingest(t, cnxn, "three_parts")
	if _, opens, parts := proxy.ingestRequests(); opens != 1 || parts != 3 {
		t.Fatalf("%d opens, %d parts; want 3 parts", opens, parts)
	}
}
