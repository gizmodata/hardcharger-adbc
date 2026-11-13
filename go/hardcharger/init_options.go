// Licensed under the Apache License, Version 2.0.

package hardcharger

import "github.com/gizmodata/chargewire-adbc/go/v2/chargewire"

// InitOptionOrder is the Chargewire driver's chargewire.InitOptionOrder: the
// keys of the connection options a caller set before the connection was
// initialized (the C API allows AdbcConnectionSetOption before
// AdbcConnectionInit), in the order to apply them once it is open: the
// current catalog, then the current schema, then the others by name. The
// rule is the Chargewire driver's; this package only gives it to its own
// generated cgo entry points (pkg/hardcharger), which keep those options in a
// map and used to range over it, in the order Go randomizes, so a schema set
// with a catalog was usually looked up in the server's default catalog.
func InitOptionOrder(options map[string]string) []string {
	return chargewire.InitOptionOrder(options)
}
