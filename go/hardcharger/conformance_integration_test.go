// Licensed under the Apache License, Version 2.0.

//go:build integration

package hardcharger

import (
	"context"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// The statuses conformance.go gives (the ADBC Driver Foundry's validation
// suite checks them too, in validation/), each next to the case that must keep
// its own status.

func TestIntegrationUnknownDatabaseOptionAfterOpenIsNotImplemented(t *testing.T) {
	db, _, err := open(t, shared, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	opts := db.(adbc.GetSetOptionsWithContext)
	wantStatus(t, opts.SetOption(ctx, "this_option_does_not_exist", "value"), adbc.StatusNotImplemented,
		"this_option_does_not_exist")
	// A known option is still too late to set once a connection has opened.
	wantStatus(t, opts.SetOption(ctx, adbc.OptionKeyUsername, "someone"), adbc.StatusInvalidState,
		adbc.OptionKeyUsername)
}

func TestIntegrationMissingCurrentCatalogOrSchemaIsNotFound(t *testing.T) {
	cnxn := mustOpen(t, shared, nil)
	ctx := context.Background()
	opts := cnxn.(adbc.GetSetOptionsWithContext)
	if err := run(t, cnxn, "CREATE SCHEMA IF NOT EXISTS conformance_schema"); err != nil {
		t.Fatal(err)
	}
	catalog, err := opts.GetOption(ctx, adbc.OptionKeyCurrentCatalog)
	if err != nil {
		t.Fatal(err)
	}

	wantStatus(t, opts.SetOption(ctx, adbc.OptionKeyCurrentCatalog, "conformance_no_catalog"), adbc.StatusNotFound,
		"conformance_no_catalog")
	wantStatus(t, opts.SetOption(ctx, adbc.OptionKeyCurrentDbSchema, "conformance_no_schema"), adbc.StatusNotFound,
		"conformance_no_schema")
	// GetObjects' filters are patterns: a name that only matches as one is still missing.
	wantStatus(t, opts.SetOption(ctx, adbc.OptionKeyCurrentDbSchema, "conformance_schem_"), adbc.StatusNotFound,
		"conformance_schem_")

	// The twins: names that exist are set.
	if err := opts.SetOption(ctx, adbc.OptionKeyCurrentDbSchema, "conformance_schema"); err != nil {
		t.Fatal(err)
	}
	if got := scalar(t, cnxn, "SELECT current_schema()"); got != "conformance_schema" {
		t.Errorf("current schema %q, want conformance_schema", got)
	}
	if err := opts.SetOption(ctx, adbc.OptionKeyCurrentCatalog, catalog); err != nil {
		t.Fatal(err)
	}
}

func TestIntegrationAppendWithAColumnTheTableLacksIsAlreadyExists(t *testing.T) {
	cnxn := mustOpen(t, shared, nil)
	if err := run(t, cnxn, "CREATE OR REPLACE TABLE conformance_append (a BIGINT, b VARCHAR DEFAULT 'none')"); err != nil {
		t.Fatal(err)
	}
	ingest := func(mode string, columns map[string]arrow.Array) error {
		t.Helper()
		ctx := context.Background()
		stmt, err := cnxn.NewStatement(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer stmt.Close(ctx)
		var fields []arrow.Field
		var arrays []arrow.Array
		for _, name := range []string{"a", "b", "c", "A"} {
			if column, ok := columns[name]; ok {
				fields = append(fields, arrow.Field{Name: name, Type: column.DataType(), Nullable: true})
				arrays = append(arrays, column)
			}
		}
		rec := array.NewRecordBatch(arrow.NewSchema(fields, nil), arrays, int64(arrays[0].Len()))
		defer rec.Release()
		for key, value := range map[string]string{adbc.OptionKeyIngestTargetTable: "conformance_append",
			adbc.OptionKeyIngestMode: mode} {
			if err := stmt.SetOption(ctx, key, value); err != nil {
				t.Fatal(err)
			}
		}
		if err := stmt.Bind(ctx, rec); err != nil {
			t.Fatal(err)
		}
		_, err = stmt.ExecuteUpdate(ctx)
		return err
	}
	ints := func(values ...int64) arrow.Array {
		b := array.NewInt64Builder(memory.DefaultAllocator)
		defer b.Release()
		b.AppendValues(values, nil)
		return b.NewArray()
	}
	strs := func(values ...string) arrow.Array {
		b := array.NewStringBuilder(memory.DefaultAllocator)
		defer b.Release()
		b.AppendValues(values, nil)
		return b.NewArray()
	}

	for _, mode := range []string{adbc.OptionValueIngestModeAppend, adbc.OptionValueIngestModeCreateAppend} {
		err := ingest(mode, map[string]arrow.Array{"a": ints(1), "c": ints(2)})
		wantStatus(t, err, adbc.StatusAlreadyExists, `"c"`)
	}
	// The twins: a column that exists with a value that doesn't convert is the
	// data's error; a subset of the columns, in any case or order, appends.
	wantStatus(t, ingest(adbc.OptionValueIngestModeAppend, map[string]arrow.Array{"a": strs("x")}),
		adbc.StatusInvalidArgument, "Conversion")
	if err := ingest(adbc.OptionValueIngestModeAppend, map[string]arrow.Array{"A": ints(7)}); err != nil {
		t.Fatal(err)
	}
	if got := scalar(t, cnxn, "SELECT a || '/' || b FROM conformance_append"); got != "7/none" {
		t.Errorf("appended row %q, want 7/none", got)
	}
}

// run runs sql without reading a result.
func run(t *testing.T, cnxn adbc.ConnectionWithContext, sql string) error {
	t.Helper()
	ctx := context.Background()
	stmt, err := cnxn.NewStatement(ctx)
	if err != nil {
		return err
	}
	defer stmt.Close(ctx)
	if err := stmt.SetSqlQuery(ctx, sql); err != nil {
		return err
	}
	_, err = stmt.ExecuteUpdate(ctx)
	return err
}
