"""Re-apply this repository's changes to the generated cgo entry points (`make -C go regenerate`).

go/pkg/hardcharger/driver.go comes from driverbase-go's template, with two changes of ours.
Each must match the template's text exactly once: when the template changes, this script
stops, and the change has to be looked at again.

1. The Arrow C Data export of extension arrays (exportRecordBatch, in this package).

2. Options set before AdbcConnectionInit. The template keeps them in a map and applies them by
   ranging over it, in the order Go randomizes. The current schema is looked up in the current
   catalog, so it has to be applied after the catalog: hardcharger.InitOptionOrder, which is the
   Chargewire driver's chargewire.InitOptionOrder, gives that order (chargewire-adbc's
   go/chargewire/init_options.go says what went wrong without it).
"""

from pathlib import Path

CHANGES = [
    (
        "the extension-array export fix (exportRecordBatch)",
        "cdata.ExportArrowRecordBatch(cStream.rdr.RecordBatch(), toCdataArray(array), nil)",
        "exportRecordBatch(cStream.rdr.RecordBatch(), toCdataArray(array))",
    ),
    (
        "the options set before AdbcConnectionInit (hardcharger.InitOptionOrder)",
        """		for k, v := range conn.initArgs {
			rawCode := errToAdbcErr(err, conn.cnxn.SetOption(ctx, k, v))
""",
        """		for _, k := range hardcharger.InitOptionOrder(conn.initArgs) {
			rawCode := errToAdbcErr(err, conn.cnxn.SetOption(ctx, k, conn.initArgs[k]))
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
