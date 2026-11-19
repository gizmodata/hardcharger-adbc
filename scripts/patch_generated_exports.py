"""Re-apply this repository's changes to the generated cgo entry points (`make -C go regenerate`).

go/pkg/hardcharger/driver.go comes from driverbase-go's template, with changes of ours.
Each must match the template's text exactly once: when the template changes, this script
stops, and the change has to be looked at again.

1. The Arrow C Data export of extension arrays (exportRecordBatch, in this package).

2. Options set before AdbcConnectionInit. The template keeps them in a map and applies them by
   ranging over it, in the order Go randomizes. The current schema is looked up in the current
   catalog, so it has to be applied after the catalog: hardcharger.InitOptionOrder, which is the
   Chargewire driver's chargewire.InitOptionOrder, gives that order (chargewire-adbc's
   go/chargewire/init_options.go says what went wrong without it).

3. Read-only is one-way before initialization and reaches Database.Open before any setup.

4. No forced garbage collection in AdbcStatementRelease and AdbcArrayStreamRelease: the
   template runs runtime.GC() in both, two full collections per statement (about 0.5 ms of a
   1 ms SELECT 1 through the C ABI). AdbcConnectionRelease and AdbcDatabaseRelease keep theirs.
"""

from pathlib import Path

CHANGES = [
    (
        "the extension-array export fix (exportRecordBatch)",
        "cdata.ExportArrowRecordBatch(cStream.rdr.RecordBatch(), toCdataArray(array), nil)",
        "exportRecordBatch(cStream.rdr.RecordBatch(), toCdataArray(array))",
    ),
    (
        "one-way read-only before connection initialization",
        "\t\tconn.initArgs[k] = v\n",
        """		if e := hardcharger.SetInitOption(conn.initArgs, k, v); e != nil {
			return C.AdbcStatusCode(errToAdbcErr(err, e))
		}
""",
    ),
    (
        "read-only in the creation request",
        "\tc, e := cdb.db.Open(context.Background())\n",
        """	openCtx, e := hardcharger.ConnectionInitContext(context.Background(), conn.initArgs)
	if e != nil {
		return C.AdbcStatusCode(errToAdbcErr(err, e))
	}
	c, e := cdb.db.Open(openCtx)
""",
    ),
    (
        "the options set before AdbcConnectionInit (hardcharger.InitOptionOrder)",
        """		for k, v := range conn.initArgs {
			rawCode := errToAdbcErr(err, conn.cnxn.SetOption(ctx, k, v))
""",
        """		for _, k := range hardcharger.InitOptionOrder(conn.initArgs) {
			if k == adbc.OptionKeyReadOnly {
				continue // already carried in the session's creation request
			}
			rawCode := errToAdbcErr(err, conn.cnxn.SetOption(ctx, k, conn.initArgs[k]))
""",
    ),
    (
        "no forced GC in AdbcArrayStreamRelease",
        """		C.free(unsafe.Pointer(cStream.adbcErr))
	}
	runtime.GC()
}
""",
        """		C.free(unsafe.Pointer(cStream.adbcErr))
	}
	// No runtime.GC() here: see AdbcStatementRelease.
}
""",
    ),
    (
        "no forced GC in AdbcStatementRelease",
        """		st.stmt = nil
		// manually trigger GC for two reasons:
		//  1. ASAN expects the release callback to be called before
		//     the process ends, but GC is not deterministic. So by manually
		//     triggering the GC we ensure the release callback gets called.
		//  2. Creates deterministic GC behavior by all Release functions
		//     triggering a garbage collection
		runtime.GC()
""",
        """		st.stmt = nil
		// The template forces a full garbage collection here and in
		// AdbcArrayStreamRelease, so that release callbacks run before the
		// process ends (for ASAN). That is two collections per statement,
		// together about half a millisecond, which a loop of small queries
		// pays on every statement. The Go runtime collects by itself, Arrow data
		// handed across the C ABI is released by its own callbacks, and
		// AdbcConnectionRelease and AdbcDatabaseRelease still collect, so
		// everything is released before a process that closes its
		// connection ends.
""",
    ),
]

driver = Path(__file__).resolve().parents[1] / "go/pkg/hardcharger/driver.go"
source = driver.read_text()
for what, original, replacement in CHANGES:
    if source.count(original) != 1:
        raise SystemExit(f"driverbase's template changed: review {what}")
    source = source.replace(original, replacement)
driver.write_text(source)
