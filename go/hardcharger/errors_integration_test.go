// Licensed under the Apache License, Version 2.0.

//go:build integration

package hardcharger

import (
	"context"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
)

// A user's SQL that fails is the user's error, never StatusInternal (which
// says the driver or the server is broken), whether it fails before it runs
// (sql_error) or while it runs (execution_error, HTTP 422).
func TestIntegrationSQLErrorsAreNotInternal(t *testing.T) {
	cnxn := mustOpen(t, shared, nil)
	ctx := context.Background()
	run := func(sql string) error {
		t.Helper()
		stmt, err := cnxn.NewStatement(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer stmt.Close(ctx)
		if err := stmt.SetSqlQuery(ctx, sql); err != nil {
			t.Fatal(err)
		}
		reader, _, err := stmt.ExecuteQuery(ctx)
		if err == nil {
			for reader.Next() {
			}
			err = reader.Err()
			reader.Release()
		}
		return err
	}
	if err := run("CREATE OR REPLACE TABLE errstatus_keys (id INTEGER PRIMARY KEY); INSERT INTO errstatus_keys VALUES (1)"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		sql    string
		status adbc.Status
		code   string
		text   string
	}{
		{"SELECT 1 // 0", adbc.StatusInvalidArgument, "execution_error", "ivision by zero"},
		{"SELECT CAST(s AS INTEGER) FROM (VALUES ('1'), ('x')) t(s)", adbc.StatusInvalidArgument, "execution_error", "Conversion Error"},
		{"SELECT CASE WHEN i < 300000 THEN i ELSE error('boom') END FROM range(1000000) t(i)", adbc.StatusInvalidArgument, "execution_error", "boom"},
		{"SELEC 1", adbc.StatusInvalidArgument, "sql_error", "Parser Error"},
		{"SELECT * FROM errstatus_no_such_table", adbc.StatusNotFound, "sql_error", "errstatus_no_such_table"},
		{"INSERT INTO errstatus_keys VALUES (1)", adbc.StatusIntegrity, "execution_error", "onstraint"},
	} {
		err := run(tc.sql)
		if err == nil {
			t.Errorf("%s succeeded; want an error", tc.sql)
			continue
		}
		wantStatus(t, err, tc.status, tc.text)
		if code := chargewireCode(err); code != tc.code {
			t.Errorf("%s: code %q, want %q", tc.sql, code, tc.code)
		}
	}
	if got := scalar(t, cnxn, "SELECT 42"); got != "42" {
		t.Errorf("the session answers %s after the failures", got)
	}
}
