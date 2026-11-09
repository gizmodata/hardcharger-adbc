# ADBC validation (conformance) suite

The [ADBC Driver Foundry's validation suite](https://github.com/adbc-drivers/validation), run
against this driver and a Hardcharger server. It is a test dependency only, pinned in
`requirements.txt`; `tests/hardcharger.py` says what the suite should expect of the driver (every
feature it implements is on, and each one switched off says why), and `queries/` holds DuckDB's
versions of the suite's type cases. CI runs it on every push and before every release
(`.github/workflows/ci.yml`, job `validation`).

It fails, never skips, without its server.

## Run it

```bash
make -C go lib    # or scripts/build_lib.sh; HARDCHARGER_ADBC_TEST_DRIVER_PATH names another library

# A server whose catalog is `validation` and whose TimeZone is UTC, on a free port of 127.0.0.1:
docker run -d --name hc-adbc-validation -p 127.0.0.1:31461:31337 \
  -e TZ=UTC -e HARDCHARGER_ADMIN_PASSWORD="$PASSWORD" ghcr.io/gizmodata/hardcharger:main \
  --admin-username hc_adbc_validation --database /tmp/validation.duckdb

cd validation
uv venv .venv --python 3.14
uv pip install --python .venv/bin/python -r requirements.txt
HARDCHARGER_ADBC_TEST_URI='hardcharger://127.0.0.1:31461?tls=false' \
HARDCHARGER_ADBC_TEST_USERNAME=hc_adbc_validation \
HARDCHARGER_ADBC_TEST_PASSWORD="$PASSWORD" \
  .venv/bin/python -m pytest -rfEsxX
```

## What is skipped or expected to fail, and why

- `get_objects_constraints_check`, `get_objects_constraints_unique`: GetObjects reads
  constraints from Chargewire's metadata endpoints, which carry primary and foreign keys only.
- `test_get_statistics`: GetStatistics is not implemented; Chargewire has no statistics endpoint.
- `ingest/decimal_scale_negative`: DuckDB has no negative-scale DECIMAL (and Hardcharger fails
  such an ingest with a DuckDB INTERNAL error rather than refusing the type).
- `type/bind/float16` (expected to fail): Hardcharger refuses halffloat parameters
  (`unsupported_arrow_type`).
- `test_not_null` (expected to fail): Hardcharger creates a bulk ingest's table with
  `CREATE TABLE ... AS SELECT`, which drops a non-nullable field's `NOT NULL`.

Expected failures are strict: when the server is fixed they fail, and the mark comes off.
