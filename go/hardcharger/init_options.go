// Licensed under the Apache License, Version 2.0.

package hardcharger

import (
	"slices"
	"strings"

	"github.com/apache/arrow-adbc/go/adbc"
)

// InitOptionOrder returns the keys of the connection options a caller set
// before the connection was initialized (the C API allows
// AdbcConnectionSetOption before AdbcConnectionInit), in the order to apply
// them once it is open: the current catalog, then the current schema (which
// is looked up in the current catalog), then the others by name, except that
// where one option is given under several names the one a database would keep
// is applied last, so it wins on a connection too: the names from before the
// ADBC driver style guide first (adbc.warp.*, adbc.hardcharger.*, then
// adbc.chargewire.*), then the Chargewire driver's chargewire.*, then the rest
// (hardcharger.* among them). This package's generated cgo entry points
// (pkg/hardcharger) keep those options in a map and call this instead of
// ranging over it, in the order Go randomizes.
func InitOptionOrder(options map[string]string) []string {
	keys := make([]string, 0, len(options))
	for key := range options {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, func(a, b string) int {
		if ra, rb := initOptionRank(a), initOptionRank(b); ra != rb {
			return ra - rb
		}
		return strings.Compare(a, b)
	})
	return keys
}

func initOptionRank(key string) int {
	switch {
	case key == adbc.OptionKeyCurrentCatalog:
		return 0
	case key == adbc.OptionKeyCurrentDbSchema:
		return 1
	case strings.HasPrefix(key, "adbc.warp."):
		return 2
	case strings.HasPrefix(key, oldOptionPrefix):
		return 3
	case strings.HasPrefix(key, "adbc.chargewire."):
		return 4
	case strings.HasPrefix(key, "chargewire."):
		return 5
	}
	return 6
}
