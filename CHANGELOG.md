# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `Connection.access_token` and `Connection.access_token_expires_at` (a UTC datetime, or
  `None`), through warp-sql-adbc `4596835`: the token the connection sends, renewed first when
  due, or the caller's own `access_token` as given (options `adbc.warp.auth.access_token` and
  `adbc.warp.auth.access_token_expires_at`, readable on the database and connection in every
  language). It is a bearer secret; with `session_id` it hands a session to another process.
- Attaching to an existing session, through warp-sql-adbc `2c0af4e`: `connect(session_id=...)`
  (option `adbc.warp.session.id`) uses that session instead of creating one, its open
  transaction included; `Connection.session_id` reads a connection's.
  `connect(close_session_on_release=...)` (option `adbc.warp.session.close_on_release`, also a
  connection option) says whether closing ends the session: by default yes for a session the
  connection created, no for an attached one. A session the server doesn't have, or another
  user's, is `NotFound` with `warp.code` `session_not_found`. With several replicas it works
  only when routing reaches the replica that holds the session (the README says more).
- `connect(subject_token=...)`: sign in with your identity provider's token (an OIDC ID token
  or another JWT the server trusts), exchanged for a Hardcharger access token (Warp SQL's
  `token_exchange` grant, option `adbc.warp.auth.subject_token`). It is refused beside
  `username`, `password`, `access_token`, `auth_type="external"` or `"client_certificate"`
  (also when the subject token comes through `db_kwargs`: the driver would silently use one
  credential), and beside a different `db_kwargs` value for the same option.
- `connect(tls_server_name=...)`: the server's host name for TLS when the URI's host isn't it
  (an IP address, a tunnel, a load balancer), option `adbc.warp.tls.server_name`. It is sent
  as the TLS server name (SNI) on every connection, result partitions and ingest parts too,
  and the certificate is checked against it: GizmoSQL's `tls_override_hostname`. The tests
  read the SNI off the wire through a recording proxy.

### Changed

- A shared browser sign-in whose token the server refuses with `token_revoked`,
  `credentials_changed` or `principal_dropped` no longer starts another browser sign-in on its
  own (Warp SQL §4.2): the error says why. Without a reason, after `token_expired`, or with a
  reason the driver doesn't know, it signs in again once, as before.
- **Warp SQL protocol 1.0.0**, through warp-sql-adbc `b8f6cdd` (needs a Hardcharger server that
  speaks 1.0.0, from its `spec/v1.0` branch; the driver has no fallbacks for the draft's names).
  What changes for Hardcharger users: a server that requires client certificates and gets a
  connection without one is the refusal `certificate_required` (`UNAUTHENTICATED`; it was
  `client_certificate_required`, or an unreachable server, `IO`); codes the driver makes up
  start with `client_` (`client_protocol_error`, and `client_http_error` for an error status
  without a problem document); errors carry `warp.reason` / `warp.cancel_reason` when the server
  gave one; after `invalid_token` with `token_revoked`, `credentials_changed` or
  `principal_dropped` the driver no longer signs in again on its own; a statement cancelled by
  the time the server answers is `CANCELLED`; unknown states and reasons are accepted (§12); a
  partitioned ingest commits with `part_count`; `adbc.warp.statement.query_tag` is sent compact
  and ASCII-escaped, at most 4,096 bytes. The README names the codes.
- Browser sign-in (`auth_type=external`) is Warp SQL's (spec §4.3), on the server's own port and
  URL, run by the Warp SQL driver (`warpsql.BrowserSignIn`, warp-sql-adbc 6ee3a73): the driver
  creates a flow, opens the identity provider's page, polls the flow with its secret and gets
  the server's own access token, which it uses as `adbc.warp.auth.access_token` (the identity
  provider's ID token never reaches the client). A server without browser sign-in is refused
  before any browser opens ("does not offer browser sign-in", `UNAUTHENTICATED`); a refusal
  carries the server's code (`access_denied`, `invalid_credentials`, `identity_provider_error`)
  and detail. A shared sign-in (`share_sign_in`) now shares the access token, renewed from its
  `expires_in`; its key no longer has a sign-in server URL. The token can't be renewed: once it
  expires, a connection's requests are refused (`token_expired`) and a new connection signs in
  again. Needs a Hardcharger server with browser sign-in on its main port.

### Removed

- The client of GizmoSQL 1.x's separate sign-in server (`/oauth/initiate`, `/oauth/token/…` on
  port 31339): the options `adbc.hardcharger.oauth.url` and `adbc.hardcharger.oauth.port`
  (refused now as unknown options), Go's `DefaultOAuthPort`, Python's `connect(oauth_url=...,
  oauth_port=...)`, `DEFAULT_OAUTH_PORT`, `DatabaseOptions.OAUTH_URL` and `OAUTH_PORT`.

### Documentation

- Trademark notices: Hardcharger™ and GizmoData™ on the README titles and first mentions, package
  metadata and the footer, which says both are trademarks of GizmoData LLC.

### Added

- `adbc.warp.session.transaction` (read-only, connection; Python
  `ConnectionOptions.SESSION_TRANSACTION`): the session's transaction as the server reports it
  (`none`, `open`, `aborted`, `unknown`), which sees SQL `BEGIN` and `ROLLBACK` too. Live tests
  (Go, Python) for it and for `certificate_required` at the handshake; both fail with the
  previous warp-sql-adbc.
- The live-test servers (Go and Python) run with a `HOME` of their own. The Python ingest tests'
  recording proxy listens with a backlog of 64: with the default 5, on macOS, a part upload was
  sometimes reset and sent again, and the request count came out one too high (about 1 run in
  10 under load; 0 in 120 after). The Go ingest tests drop their table first, so the live suite
  runs with `-count`.
- One browser sign-in for many connections: `adbc.hardcharger.oauth.share_sign_in=true` (Python
  `connect(auth_type="external", share_sign_in=True)`) shares a browser sign-in among the
  process's databases that sign in to the same server, through the same sign-in server, with the
  same TLS trust and the same `adbc.hardcharger.oauth.sign_in_key` (Python: `share_sign_in="name"`).
  The first to open a connection signs in and concurrent ones wait for it; the rest reuse its ID
  token (each exchanging it for its own access token) until a minute (or a quarter of a shorter
  lifetime) before it expires, then the next one signs in again; a shared token the server refuses
  is dropped and that database signs in again, once. Go: `ForgetSharedSignIns()`. One `dbt run`
  opened a browser window per connection (five for one model); dbt-hardcharger had to share a
  driver-manager database through `adbc_driver_manager` internals instead. Sharing needs
  `auth_type=external` and is refused otherwise. Live tests (Go, Python) against
  `hardcharger-server` with a mock identity provider: 5 sequential and 8 (Go) or 6 (Python, from
  threads) concurrent connections sign in once; without sharing each signs in; another key or
  server signs in separately; a token close to its expiry is not handed out and the next
  connection signs in again; a refused token is replaced (and a failed sign-in after it fails,
  with no loop).
- Client certificate sign-in (mutual TLS, Warp SQL §4.4): `adbc.hardcharger.auth_type=client_certificate`
  (also `mtls`), Python `connect(auth_type="client_certificate", tls_client_cert=..., tls_client_key=...)`.
  It is the Warp SQL driver's `adbc.warp.auth.type`: the certificate is the credential, the token is
  bound to it, and result partitions and ingest parts present it too. Needs a server with
  `--mtls-ca-cert` and `--mtls-certificate-sign-in`. Live tests in Go and Python against
  `hardcharger-server`: sign-in (an e-mail identity matching `--mtls-admin-identities` ignoring
  case), a split result and a partitioned ingest (row count, distinct keys and sum checked
  exactly), an unknown identity (`unknown_certificate_identity`), a bound token over another
  certificate (`certificate_mismatch`) and its twin, a password beside the certificate, and a server
  with certificate sign-in off (capability `mtls_identity`) with its password twin.

### Changed

- Built on warp-sql-adbc `56483f5` (fix/driver-polish: the plain-HTTP hint and best-effort close),
  from `b74dfe2` (client certificate sign-in), from `b3d2ab8`.

### Fixed

- A user's SQL that failed while running (`execution_error`, HTTP 422: a division by zero, a failed
  conversion, `error()`) was ADBC status `INTERNAL`, so Python raised `InternalError`, which says
  the driver or server is broken. It is now `INVALID_ARGUMENT` (`ProgrammingError`), like a SQL
  error before it runs; a violated constraint (SQLSTATE `23000`) is `INTEGRITY` (`IntegrityError`).
  Built on warp-sql-adbc `15ff049` (fix/adbc-error-status), which maps every Warp SQL error code to
  an ADBC status. Live Go and Python tests check the status and DB-API class of a division by
  zero, a conversion, a failure mid-stream, a syntax error, an unknown table, a duplicate key, a
  cancel (`OperationalError`), a timeout (`OperationalError`) and a wrong password
  (`UNAUTHENTICATED`).
- A TLS URI (`hardcharger://`, the default) pointed at a server that speaks plain HTTP failed with
  Go's "server gave HTTP response to HTTPS client". It now fails at once with the `hardcharger`
  CLI's hint, in the driver's terms: `The server at HOST:PORT speaks plain HTTP, not TLS. Add
  ?tls=false to the URI to connect without encryption.` (status `IO`); only the TLS hello is sent,
  no credential (live tests record the bytes through a TCP proxy), and nothing falls back to
  plaintext. The browser sign-in says the same of a plain-HTTP sign-in server (with
  `adbc.hardcharger.oauth.url` given: use an `http://` URL there), before opening any browser.
- Closing a connection after the server stopped raised an error, and in Python left the
  database handle open. Ending the session is best-effort now (at most 5 s; a stopped or hung
  server is not an error) and Close always releases the driver's handles. Live tests stop the
  server, then close statements (prepared, and with a result being read), connections and the
  database: no error, no leaked goroutine (goleak), no Arrow memory left, and in Python the
  driver-manager handles closed.

- Python 3.11 is the minimum (`requires-python = ">=3.11"`): pyarrow 26, the floor,
  needs 3.11, so on 3.10 the package could never be installed.

- In Go, Warp SQL options set on a database after it was created (`db.SetOption`,
  `db.SetOptions`, e.g. `adbc.warp.retry.max_attempts`) were refused as unknown: they went to
  driverbase's `SetOptions`, which knows none of them. They now reach the Warp SQL driver one by one.
  (Options given at creation, and every option set through the C API or Python, were not affected.)

- The `dbapi` module exports PEP 249's exception classes (`Error`, `Warning`, `InterfaceError`,
  `DatabaseError`, `DataError`, `OperationalError`, `IntegrityError`, `InternalError`,
  `ProgrammingError`, `NotSupportedError`), as PEP 249 requires of a driver module. They are
  `adbc_driver_manager`'s own classes, so `except dbapi.ProgrammingError` and
  `except adbc_driver_manager.ProgrammingError` catch the same errors.

### Added

- The Hardcharger ADBC driver: a Go package (`github.com/gizmodata/hardcharger-adbc/go/hardcharger`)
  on the Warp SQL ADBC driver (warp-sql-adbc b3d2ab8, `warpsql.NewDriverWithName`), compiled to the C shared library
  `libadbc_driver_hardcharger` (entry point `AdbcDriverHardchargerInit`), so every ADBC
  language gets:
  - `hardcharger://host[:port][/base-path][?tls=false]` URIs: Warp SQL's `warp://` with port
    31337 by default; `warp://`, `https://` and `http://` pass through; `hardcharger+http://`
    is refused with the spelling to use;
  - "ADBC Hardcharger Driver - Go" as GetInfo's driver name;
  - the `adbc.hardcharger.*` options: query and session tags, `hardcharger.*` session settings
    on every session (database) or one (connection; read back with GetOption), global
    settings, and browser sign-in (`auth_type=external`, exchanging the identity provider's ID
    token for a Warp SQL access token).
- The Warp SQL driver's defaults, inherited: a large result is split into up to 8 partitions
  read at once (`adbc.warp.statement.result_partitions`), and a bulk ingest of two batches or
  more goes in up to 8 parts (`adbc.warp.ingest.parts`, also `adbc.hardcharger.ingest.parts`
  on the database or a connection). Tests count the partitions (`ExecutePartitions`) and the
  ingest's part uploads (through a recording proxy), in Go and Python, each with a twin set to 1.
- The driver manifest for `driver = "hardcharger"` (`packaging/hardcharger.toml.in`,
  `python -m adbc_driver_hardcharger.manifest`), and C and Go examples.
- `adbc-driver-hardcharger`, the Python bindings: platform wheels carrying the library,
  `from adbc_driver_hardcharger import dbapi as hardcharger`, `hardcharger.connect(...)`, and
  helpers (`set_setting`, `get_setting`, `reset_setting`, `settings`, `set_query_tag`,
  `set_session_tag`, `current_user`, `current_session`, `server_version`) on the driver's options.
- Tests: Go unit and live suites (race detector) and Python unit and live suites against real
  `hardcharger-server` processes (plain HTTP, TLS, instrumentation, browser sign-in with a mock
  identity provider), the library loaded by manifest through the driver manager with a
  `hardcharger://` URI; all fail without a server. CI also runs the C example through the ADBC
  C driver manager and the Go example. `release.yml` (v* tags) gates on all of it, then builds
  the library for six platforms and wheels for five, a GitHub Release, and PyPI from the
  `pypi` environment.
