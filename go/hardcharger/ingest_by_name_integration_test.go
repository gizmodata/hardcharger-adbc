// Licensed under the Apache License, Version 2.0.

//go:build integration

package hardcharger

import (
	"context"
	"strings"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// ingestBatch bulk-loads one batch into table with the given ADBC mode.
func ingestBatch(t *testing.T, cnxn adbc.ConnectionWithContext, table, mode string, rec arrow.RecordBatch) error {
	t.Helper()
	ctx := context.Background()
	stmt, err := cnxn.NewStatement(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close(ctx)
	for key, value := range map[string]string{adbc.OptionKeyIngestTargetTable: table, adbc.OptionKeyIngestMode: mode} {
		if err := stmt.SetOption(ctx, key, value); err != nil {
			t.Fatalf("SetOption(%s): %v", key, err)
		}
	}
	if err := stmt.Bind(ctx, rec); err != nil {
		t.Fatal(err)
	}
	_, err = stmt.ExecuteUpdate(ctx)
	return err
}

func int32Batch(columns []string, values [][]int32) arrow.RecordBatch {
	fields := make([]arrow.Field, len(columns))
	for i, name := range columns {
		fields[i] = arrow.Field{Name: name, Type: arrow.PrimitiveTypes.Int32, Nullable: true}
	}
	builder := array.NewRecordBuilder(memory.DefaultAllocator, arrow.NewSchema(fields, nil))
	defer builder.Release()
	for i, column := range values {
		builder.Field(i).(*array.Int32Builder).AppendValues(column, nil)
	}
	return builder.NewRecordBatch()
}

// An append binds the upload's columns to the table's by name (Warp SQL §8.1), so
// two INTEGER columns uploaded the other way round land where their names say, an
// omitted column takes its default, and a column the table lacks is refused.
// Hardcharger 2.0.0 bound by position and stored the swapped columns crosswise.
func TestIntegrationIngestAppendBindsColumnsByName(t *testing.T) {
	cnxn := mustOpen(t, shared, nil)
	execUpdate(t, cnxn, "CREATE OR REPLACE TABLE by_name (a INTEGER, b INTEGER, c VARCHAR DEFAULT 'dflt')")
	swapped := int32Batch([]string{"b", "a"}, [][]int32{{10, 20}, {1, 2}})
	defer swapped.Release()
	if err := ingestBatch(t, cnxn, "by_name", adbc.OptionValueIngestModeAppend, swapped); err != nil {
		t.Fatalf("append: %v", err)
	}
	if got := scalar(t, cnxn, "SELECT string_agg(a || ':' || b || ':' || c, ',' ORDER BY a) FROM by_name"); got != "1:10:dflt,2:20:dflt" {
		t.Fatalf("stored %q; want the values under their own names", got)
	}
	extra := int32Batch([]string{"a", "nope"}, [][]int32{{3}, {0}})
	defer extra.Release()
	err := ingestBatch(t, cnxn, "by_name", adbc.OptionValueIngestModeAppend, extra)
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("a column the table lacks: %v; want a refusal naming it", err)
	}
	if got := scalar(t, cnxn, "SELECT count(*) FROM by_name"); got != "2" {
		t.Fatalf("after the refused upload: %s rows", got)
	}
}
