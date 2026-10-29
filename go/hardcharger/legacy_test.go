// Licensed under the Apache License, Version 2.0.

package hardcharger

import (
	"context"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
)

// The URI helper's name from before the rename keeps its result: the old scheme.
func TestLegacyURIHelper(t *testing.T) {
	for raw, want := range map[string]string{
		"hardcharger://db.example.com":      "warp://db.example.com:31337",
		"hardcharger://127.0.0.1?tls=false": "warp://127.0.0.1:31337?tls=false",
		"chargewire://h:1":                  "warp://h:1",
		"warp://h:1?tls=false":              "warp://h:1?tls=false",
		"https://h/x":                       "https://h/x",
	} {
		got, err := ToWarpURI(raw)
		if err != nil || got != want {
			t.Errorf("ToWarpURI(%q) = %q, %v; want %q", raw, got, err, want)
		}
	}
	_, err := ToWarpURI("hardcharger:///base")
	wantStatus(t, err, adbc.StatusInvalidArgument, "no host")
}

// The old URI scheme reads as the current one, so this package's own checks
// (TLS or not, the shared sign-in's server) see it as such.
func TestLegacySchemeReadsAsCurrent(t *testing.T) {
	for raw, want := range map[string]string{
		"warp://h:1?tls=false":  "chargewire://h:1?tls=false",
		"WARP://h:1":            "chargewire://h:1",
		"warp://joe@h:1/warp":   "chargewire://joe@h:1/warp", // the driver maps the old base path
		"warpx://h:1":           "warpx://h:1",
		"chargewire://h:1/warp": "chargewire://h:1/warp",
	} {
		got, err := ToChargewireURI(raw)
		if err != nil || got != want {
			t.Errorf("ToChargewireURI(%q) = %q, %v; want %q", raw, got, err, want)
		}
	}
	got, _ := ToChargewireURI("warp://h:1?tls=false")
	if usesTLS(got) {
		t.Fatalf("%s uses TLS", got)
	}
	if signInKeyFor(got, nil, "") != signInKeyFor("chargewire://h:1?tls=false", nil, "") {
		t.Fatal("the old scheme names another sign-in server")
	}
}

// Old option keys read as the current ones, also where this package reads
// them itself: ingest parts under Hardcharger's name, and the TLS trust a
// shared sign-in is keyed on.
func TestLegacyOptionKeysReadAsCurrent(t *testing.T) {
	ctx := context.Background()
	db, err := newDB(t, map[string]string{adbc.OptionKeyURI: "hardcharger://h",
		"adbc.warp.ingest.parts": "2", "adbc.warp.tls.server_name": "x"})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{OptionIngestParts, chargewireIngestParts, "adbc.warp.ingest.parts"} {
		if got, err := db.GetOption(ctx, key); err != nil || got != "2" {
			t.Errorf("GetOption(%s) = %q, %v", key, got, err)
		}
	}
	if got := db.chargewireOptions["adbc.chargewire.tls.server_name"]; got != "x" {
		t.Errorf("server name %q", got)
	}
	current, err := newDB(t, map[string]string{adbc.OptionKeyURI: "hardcharger://h",
		"adbc.chargewire.tls.server_name": "x"})
	if err != nil {
		t.Fatal(err)
	}
	if signInKeyFor(db.chargewireURI, db.chargewireOptions, "") !=
		signInKeyFor(current.chargewireURI, current.chargewireOptions, "") {
		t.Fatal("an old TLS option key names another sign-in")
	}
	// Both spellings of one key: the current one wins.
	db, err = newDB(t, map[string]string{adbc.OptionKeyURI: "hardcharger://h",
		"adbc.warp.ingest.parts": "2", "adbc.chargewire.ingest.parts": "3"})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := db.GetOption(ctx, OptionIngestParts); err != nil || got != "3" {
		t.Errorf("both spellings: %q, %v", got, err)
	}
	// After creation too.
	if err := db.SetOptions(ctx, map[string]string{"adbc.warp.ingest.parts": "4"}); err != nil {
		t.Fatal(err)
	}
	if got, err := db.GetOption(ctx, OptionIngestParts); err != nil || got != "4" {
		t.Errorf("SetOptions: %q, %v", got, err)
	}
}
