// Licensed under the Apache License, Version 2.0.

//go:build integration

package hardcharger

import (
	"context"
	"strings"
	"testing"
)

func TestIntegrationReadOnlyContextWithSessionSetup(t *testing.T) {
	db, _, err := open(t, shared, map[string]string{OptionSessionTag: `{"app":"reader"}`})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	reader, err := db.Open(WithReadOnly(ctx))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close(ctx)
	if got := scalar(t, reader, "SELECT 42"); got != "42" {
		t.Fatalf("read returned %s", got)
	}
	if got := scalar(t, reader, "SELECT value FROM hardcharger_settings() WHERE name='hardcharger.session_tag'"); !strings.Contains(got, "reader") {
		t.Fatalf("session tag missing: %s", got)
	}
	statement, err := reader.NewStatement(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer statement.Close(ctx)
	if err := statement.SetSqlQuery(ctx, "CREATE TABLE wrapper_go_readonly_write (n INTEGER)"); err != nil {
		t.Fatal(err)
	}
	if _, err := statement.ExecuteUpdate(ctx); err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("wanted read-only refusal, got %v", err)
	}
}
