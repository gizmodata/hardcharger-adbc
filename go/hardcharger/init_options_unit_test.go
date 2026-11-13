// Licensed under the Apache License, Version 2.0.

package hardcharger

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
)

// The Chargewire driver's order (its own tests cover it in full): the catalog,
// then the schema, then the others, every time.
func TestInitOptionOrderIsTheChargewireDrivers(t *testing.T) {
	options := map[string]string{
		adbc.OptionKeyCurrentDbSchema: "reports",
		OptionQueryTag:                `{"job": "nightly"}`,
		adbc.OptionKeyAutoCommit:      adbc.OptionValueDisabled,
		adbc.OptionKeyCurrentCatalog:  "lake",
	}
	want := []string{adbc.OptionKeyCurrentCatalog, adbc.OptionKeyCurrentDbSchema}
	for range 200 {
		if got := InitOptionOrder(options); len(got) != len(options) || !slices.Equal(got[:2], want) {
			t.Fatalf("InitOptionOrder = %v, want the catalog, then the schema, then the others", got)
		}
	}
}

// The generated cgo entry points apply the options set before
// AdbcConnectionInit in InitOptionOrder. `make regenerate` writes the file
// again from driverbase's template, which ranges over the map; this fails
// when a regenerated file lost the change (scripts/patch_generated_exports.py
// re-applies it).
func TestGeneratedConnectionInitAppliesOptionsInOrder(t *testing.T) {
	source, err := os.ReadFile("../pkg/hardcharger/driver.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(source), "range hardcharger.InitOptionOrder(conn.initArgs)") {
		t.Error("pkg/hardcharger/driver.go doesn't apply the connection's init options in hardcharger.InitOptionOrder")
	}
	if strings.Contains(string(source), "range conn.initArgs") {
		t.Error("pkg/hardcharger/driver.go ranges over conn.initArgs: the current schema can be applied before the current catalog")
	}
}
