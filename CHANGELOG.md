# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [2.4.8] - 2026-11-09

Tested with Hardcharger 2.10.0, on chargewire-adbc 2.2.9.

### Changed

- **On chargewire-adbc 2.2.9**, which now gives the three ADBC statuses 2.4.6 added here itself:
  an option the driver doesn't know, set on a database after its first connection, is
  `NOT_IMPLEMENTED`; making a catalog or schema that doesn't exist current is `NOT_FOUND`; an
  append (or `create_append`) with a column the table doesn't have is `ALREADY_EXISTS`. This
  driver's own copies (`go/hardcharger/conformance.go` and its hooks in `driver.go`) are gone;
  the behavior is unchanged, and the same tests and the ADBC validation suite check it.
- **Dependencies**: chargewire-adbc 2.2.9 (its shared library also exports only `Adbc*` and its
  macOS wheels say macOS 13, as this driver's have since 2.4.7 and 2.4.6).

## [2.4.7] - 2026-11-09

Tested with Hardcharger 2.10.0, on chargewire-adbc 2.2.8.

### Fixed

- **The shared library exports only the ADBC entrypoints**, as the
  [ADBC shared-library requirements](https://adbc-drivers.org/building-drivers/shared-library-requirements.html)
  ask: `AdbcDriverInit`, `AdbcDriverHardchargerInit` and the other `Adbc*` functions, 57 in all.
  2.0.0 through 2.4.6 also exported every Go `//export` function and the cgo glue
  (`HardchargerDatabaseNew`, arrow-go's `streamGetNext`, `_cgo_*`, `crosscall2`, ...): 70 to 255
  extra names depending on the platform, which could clash with another library's in the same
  process. The export lists are `go/pkg/hardcharger/exports_linux.map`, `exports_darwin.txt` and
  `exports_windows.def`, passed by `scripts/build_lib.sh` and `make -C go lib`. Nothing that loads
  the driver changes: the entrypoints the driver manager and the manifest use are still there.

### Added

- **`go/internal/checkexports`**: checks a built library (ELF, Mach-O or PE) for `AdbcDriverInit`,
  the entrypoint its file name implies, and no export without the `Adbc` prefix. CI and the
  release run it on every library they build, on every platform, Windows arm64 included.

## [2.4.6] - 2026-11-09

Tested with Hardcharger 2.10.0, on chargewire-adbc 2.2.8. The driver now passes the ADBC Driver
Foundry's validation (conformance) suite.

### Fixed

- **Setting an option the driver doesn't know on a database that has opened a connection** is
  `NOT_IMPLEMENTED`, as before the first connection; it was `INVALID_STATE` ("must be set before
  the first connection is opened"), which said the option exists. A known option is still
  `INVALID_STATE` then.
- **Making a catalog or schema that doesn't exist current** (`adbc.connection.catalog`,
  `adbc.connection.db_schema`) is `NOT_FOUND`, the ADBC status for it; it was `INVALID_ARGUMENT`.
- **Appending (or `create_append`) data with a column the existing table doesn't have** is
  `ALREADY_EXISTS`, ADBC's status for a table that exists with another schema; it was
  `INVALID_ARGUMENT`. A value that doesn't convert is still the data's error (`INVALID_ARGUMENT`).
- **The library's `GetInfo` driver version** in builds from a checkout (`make -C go lib`,
  `scripts/build_lib.sh`) said `go/vX.Y.Z`, the Go module's tag, or, in a clone without tags (CI),
  a bare commit hash, neither a version; it now comes from the release tag (`vX.Y.Z-N-gHASH`), or
  is `v0.0.0-HASH` without one. Released libraries were not affected.
- **The macOS wheels said they run on macOS 11**: they are tagged `macosx_13_0` and the library's
  minimum macOS is 13.0, because the Go 1.27 runtime it is built with requires macOS 13 Ventura.
  2.0.0 through 2.4.5 were tagged `macosx_11_0`, so pip would install them on macOS 11 and 12,
  where the Go runtime isn't supported. The release now checks that the library's minimum macOS
  matches the wheel's tag, and `scripts/build_lib.sh` passes the target in the cgo flags, so Go's
  build cache (which ignores `MACOSX_DEPLOYMENT_TARGET`) can't reuse a build for another target.

### Added

- **`validation/`**: the [ADBC validation suite](https://github.com/adbc-drivers/validation)
  (pinned, a test dependency only) configured for this driver, with DuckDB's versions of its type
  cases, and a CI job that runs it against a server on every push and before every release (it
  fails, never skips, without its server). 332 passed, 4 skipped, 2 expected failures; each skip
  and expected failure says why in `validation/README.md`.

### Changed

- **Dependencies**: chargewire-adbc 2.2.8 (its browser re-sign-in after a Stop is for its own
  `oauth_browser` auth type; this driver's `auth_type="external"` keeps its own), apache
  arrow-adbc Go 1.13.0, driverbase-go of 2026-11-09 (the generated cgo exports are unchanged),
  grpc 1.85.0 and the golang.org/x modules; Go toolchain 1.27.3 (the module still needs Go
  1.27.2). Python: `adbc-driver-manager>=1.13.0`, `pyarrow>=26.0.1`; test and dev extras
  `pytest>=9.2.0`, `ruff>=0.17.4`. CI: the C example's ADBC driver manager from
  apache-arrow-adbc-25, setup-uv 10.3.3.

## [2.4.5] - 2026-11-07

For Hardcharger 2.7.0, on chargewire-adbc 2.2.7.

### Fixed

- **A browser sign-in database's connections opened after a Stop could never start the cluster.**
  Stopping a GizmoData Cloud cluster ends the tokens issued until then (`invalid_token`, reason
  `token_revoked`). With `auth_type="external"` the database kept its browser sign-in's token, and
  2.4.4's fresh sign-in needs a password, so every connection opened after the Stop failed at its
  first statement. Such a connection now runs the browser sign-in again, once (usually silent with
  single sign-on), drops a shared sign-in's old token first, and runs the statement with the new
  token, which starts the cluster; a second refusal is the error. Connections opened before the
  Stop still never sign in again on their own.

### Changed

- **A token supplied from outside (`access_token`)** that a connection opened after a Stop finds
  revoked still fails, now saying the token was likely issued before the server was stopped and to
  supply a new one (chargewire-adbc 2.2.7).

## [2.4.4] - 2026-11-07

For Hardcharger 2.7.0, on chargewire-adbc 2.2.6.

### Fixed

- **A connection opened after a person stopped the cluster could never start it again**
  (chargewire-adbc 2.2.6). Stopping a GizmoData Cloud cluster ends the tokens issued until then
  (`invalid_token`, reason `token_revoked`). A database that had signed in while the cluster was
  serving kept its token, so a connection opened after the stop sent it at its first statement,
  was refused, and didn't sign in again: every new connection of that database failed. A
  connection opened while the cluster is stopped now signs in afresh at its first statement, which
  starts the cluster. Connections opened before the stop still don't sign in again on their own.

## [2.4.3] - 2026-11-07

For Hardcharger 2.7.0, on chargewire-adbc 2.2.5.

### Fixed

- **A pool that kept one database still started a stopped cluster at its refills**
  (chargewire-adbc 2.2.5). 2.4.2 decided whether to connect lazily from the database's first
  discovery, so a Go app that keeps one database and pools its connections went on signing in
  and setting up sessions at every refill after the cluster stopped, starting it again. Every
  Open after the database's first connection now reads discovery again (one small `GET`, which
  never wakes a cluster) and connects lazily while the cluster is stopped, at once while it
  serves. Python's `connect()` makes its own database each time and was not affected.

## [2.4.2] - 2026-11-07

For Hardcharger 2.7.0, on chargewire-adbc 2.2.4.

### Added

- **Connecting doesn't start a stopped GizmoData Cloud cluster** (chargewire-adbc 2.2.4). A
  connection pool opens connections on a timer, and each new connection signed in and opened a
  session, which starts a stopped auto-resume cluster: a pooled app woke an idle-stopped cluster
  at every refill, and right after a person's Stop. When discovery says the server is stopped
  (`"state": "stopped"`), opening a connection now signs in and opens nothing. The connection
  signs in, opens its session and applies the session tag, query tag and
  `adbc.hardcharger.setting.*` settings at its first request that does work. Closing a connection
  that did nothing sends nothing. A wrong password fails that first request instead of the
  connect. Options set on the connection before that request wait for it too: the current
  catalog and schema (`adbc.connection.catalog`/`db_schema`, Python's `catalog=`/`db_schema=`)
  go with the request that opens the session, and `adbc.hardcharger.session_tag`, `query_tag`
  and `setting.*` set on the connection run with the setup, after the session opens and before
  the first statement; a refused value fails that statement. (Global settings,
  `adbc.hardcharger.global_setting.*`, still run when set.) A serving server is connected to as
  before. `Open` now runs the setup itself only for a session it attached to
  (`adbc.chargewire.session.id`); any other session gets it from the new-session hook when it
  opens.

## [2.4.1] - 2026-11-06

For Hardcharger 2.7.0, on chargewire-adbc 2.2.3.

### Fixed

- **Closing a connection could wake a stopped GizmoData Cloud cluster** (chargewire-adbc 2.2.3).
  The driver renews its access token with the saved credentials shortly before it expires, on
  whatever request comes next, and a close or cancel was such a request: closing a connection late
  in its token's life signed in again, which starts a stopped auto-resume cluster for nobody, or
  undoes a person's Stop. Closes, cancels and ingest aborts now never sign in, before sending or
  after an `invalid_token` refusal, and don't wait out a resume (`server_resuming`): they carry the
  token the driver has, and a close the server refuses for its token is done (the session is gone
  or idles out). Aborting an ingest of a lost session no longer opens a replacement session either.
  Requests that do work renew the token as before.

## [2.4.0] - 2026-11-05

For Hardcharger 2.7.0, on chargewire-adbc 2.2.2.

### Added

- **GizmoData Cloud hosts default to port 443.** A `hardcharger://` URI without a port now
  connects to 443 when its host is `gizmodata.com` or a name under it (any case, a trailing dot
  allowed), where GizmoData Cloud clusters answer: `hardcharger://acme.us-east-1.aws.gizmodata.com`
  is `acme.us-east-1.aws.gizmodata.com:443`. An explicit port always wins, and every other host,
  look-alikes such as `evilgizmodata.com` or `gizmodata.com.example.org` included, keeps 31337.
  The Hardcharger CLI applies the same rule. Go: `hardcharger.IsGizmoDataCloudHost`,
  `hardcharger.GizmoDataCloudPort`; Python: `adbc_driver_hardcharger.GIZMODATA_CLOUD_PORT`.

### Fixed

- **A statement could run again, in autocommit, after its transaction was lost** (chargewire-adbc
  2.2.1). Hardcharger servers through 2.7.0 can answer `BEGIN` before the session document shows
  the transaction, so the driver could read `"none"` right after it; when the session was then
  lost (a server restart, an idle eviction), the next statement ran again on the replacement
  session on its own: after `BEGIN; INSERT 1;` and a restart, `INSERT 2` was committed alone. The
  driver now assumes a transaction is open after any statement that can begin one (`BEGIN`,
  `START`, `TRANSACTION`, `AUTOCOMMIT`, `EXECUTE`) until a statement that can end it (`COMMIT`,
  `ROLLBACK`, `END`, `ABORT`) runs and the server then says none is open; until then a request
  that finds the session lost fails with the transaction named as lost instead of running again.
  This holds against servers already deployed.
- **A request ran again without the lost session's `USE` and settings** (chargewire-adbc 2.2.2).
  After a lost session with no transaction open, the driver ran the request again in the new
  session, which has none of what SQL set in the old one: after `USE other_schema`, an
  `INSERT INTO t` landed in the connection's own schema's `t`, without an error. After a statement
  whose first word is `USE`, `SET`, `RESET`, `PRAGMA` or `EXECUTE`, a request that finds the
  session lost now fails with a message saying the session's settings and `USE` are gone, and the
  next one runs in the new session. The connection's own settings and tags
  (`adbc.hardcharger.setting.*`, the session and query tags, Python's `set_setting()`, ...) don't
  count: the driver sets them up again in every new session, so a request still runs again after
  them.

## [2.3.0] - 2026-11-05

For Hardcharger 2.7.0, on chargewire-adbc 2.2.0 (Chargewire 2.2.0's `server_resuming`, spec §11.2).

### Fixed

- **A replacement session gets the connection's settings and tags.** When the server lost a
  session (an idle eviction, `session_ended` with `token_expired` or `idle_in_transaction`), the
  session that replaced it had none of the `adbc.hardcharger.setting.*` settings or the session
  and query tags, which are set in SQL: `query_timeout` went from 12 to 0, and a session's own
  `idle_in_transaction_session_timeout` fell back to the server's. The driver now sets them up in
  every session it opens for a connection, the first and each replacement, before anything else
  runs there, through the Chargewire driver's new-session hook; a statement refused because its
  transaction was lost with the old session still leaves the new one set up. They are the
  database's options as the connection changed them since (`SetOption`, Python's
  `set_setting()`, `set_query_tag()`, ...). When setting up a replacement fails, the driver ends
  it instead of using it with the server's defaults: the request that found the loss fails with
  the setting and the server's reason (`StatusIO`), and the next request tries again with another
  session. The README's "open a new connection to get them back" limitation is gone.

### Changed

- Built on the Chargewire ADBC driver 2.2.0 (`github.com/gizmodata/chargewire-adbc/go/v2` v2.2.0),
  for its new-session hook and its waiting for a resuming server (`server_resuming`).

### Added

- `adbc.hardcharger.resume_timeout` (Python `connect(resume_timeout=...)`,
  `DatabaseOptions.RESUME_TIMEOUT`): the Chargewire driver's new `adbc.chargewire.resume_timeout`
  under Hardcharger's name. While a stopped GizmoData Cloud cluster starts again because a client
  connected, its address answers `server_resuming` (503 with `Retry-After`); the driver waits and
  sends the same request again, the sign-in included, for up to this many seconds (default 600;
  `0`: fail at once).

## [2.2.0] - 2026-11-05

For Hardcharger 2.7.0 (Chargewire 2.1.0).

### Changed

- Built on the Chargewire ADBC driver 2.1.0 (`github.com/gizmodata/chargewire-adbc/go/v2` v2.1.0),
  which brings:
  - **No statement runs again after its transaction was lost.** A session lost (idle eviction, a
    restart) while a transaction begun with SQL `BEGIN` was open was replaced and the statement
    that found it gone ran again in the new session, in autocommit, committing it on its own after
    the transaction's earlier statements had been rolled back (prepared statements too). It now
    fails with the transaction named as lost, and the next statement runs in the new session.
    Metadata and option calls that found such a session lost now fail too, instead of running
    again quietly and hiding the loss.
  - **`idle_in_transaction`** (Hardcharger 2.7.0's `idle_in_transaction_session_timeout`): a
    session the server ended for idling in a transaction is replaced, and the statement that found
    it ended fails rather than run again; before, the connection stayed on the ended session and
    every later call failed.
  - Nested schemas (Hardcharger 2.7.0, `CREATE SCHEMA sales.payroll`) appear in GetObjects as
    `sales.payroll`, and GetTableSchema finds their tables.
- The README says that a replacement session lacks the connection's `adbc.hardcharger.setting.*`
  settings and tags (set in SQL when a connection opens).

### Added

- A live test: a session Hardcharger ends for idling in a transaction
  (`adbc.hardcharger.setting.idle_in_transaction_session_timeout`) is replaced, the statement that
  found it ended fails with `session_ended` and is not run again, and the connection works
  afterwards.

## [2.1.0] - 2026-10-29

### Changed

- The driver now uses the Chargewire protocol and is built on the Chargewire ADBC driver (Go
  module `github.com/gizmodata/chargewire-adbc/go/v2`). The options it passes through are now
  `adbc.chargewire.*` (as named by `DatabaseOptions`, `ConnectionOptions` and
  `StatementOptions`), `chargewire://` URIs pass through as they are, the Go URI helper is
  `ToChargewireURI`, and errors carry their details as `chargewire.code`,
  `chargewire.request_id`, `chargewire.reason` and `chargewire.cancel_reason`: code that reads
  an error's details by key must use these. Connection strings, option keys and the Go URI
  helper's name from before the rename keep working.
- It still works with Hardcharger servers released before the rename (2.2.0 and earlier): at the
  default base path the driver talks to such a server in its own spelling of the protocol, for
  every connection of the database. CI runs the live suites against the 2.2.0 image as well.
- Built on the released Chargewire ADBC driver, `github.com/gizmodata/chargewire-adbc/go/v2`
  v2.0.0.

## [2.0.2] - 2026-10-22

### Fixed

- Export Arrow extension arrays through the C Data interface using their physical storage,
  preserving the original extension metadata. DuckDB 2.0's `duckdb_settings().typed_value`
  and other `VARIANT` results now work in Python and other C ADBC consumers, including
  variants nested in structs, lists and dictionaries. Sliced struct storage keeps the
  correct offsets and validity. Regression tests exercise live-server results and
  zero-copy C Data round trips with memory accounting.

## [2.0.1] - 2026-10-22

### Added

- A total PyPI downloads badge matching the GizmoSQL ADBC driver and a documentation badge
  linking to the Python client guide, in both the repository README and PyPI description.
- A Documentation project link in the PyPI package metadata.

### Changed

- The PyPI description now mirrors the full repository README, with absolute image and file
  URLs that work on both sites. `scripts/sync_python_readme.sh` updates the package copy;
  CI checks that the two remain identical before publishing.
- Update the build requirements to setuptools 84.2.1 and wheel 0.48.0, and the Go protobuf
  API/RPC modules to their October 19, 2026 revisions. Python runtime dependencies are current.

## [2.0.0] - 2026-10-20

The first release, with Hardcharger 2.0.0: the ADBC driver for Hardcharger, speaking Chargewire
protocol 1.0.0 through chargewire-adbc 1.0.0, for Python (`adbc-driver-hardcharger` on PyPI), Go
(`github.com/gizmodata/hardcharger-adbc/go/v2`) and every other ADBC language through the shared
library. Earlier builds (0.1.0) were never published; the sections below are the whole history,
newest first, for anyone who installed one from git.
### Changed

- Build on the released Chargewire ADBC Go module v1.0.0 instead of an internal pseudo-version.

### Fixed

- Python connections using a standard ADBC profile with `driver = "hardcharger"` resolve the
  installed driver manifest, avoiding a mismatch with the bundled library path. Named profiles,
  `profile://` URIs and absolute paths are documented alongside generic driver-manager usage.

### Added

- The Go module `github.com/gizmodata/hardcharger-adbc/go/v2` is versioned by `go/vX.Y.Z` tags on each
  release's commit (`go-module.yml` checks the pair, refuses a pseudo-version of chargewire-adbc, and
  warms the module proxy); `RELEASING.md` has the order, chargewire-adbc first.
- `THIRD_PARTY_NOTICES` (the Go runtime's and every linked Go module's license, chargewire-adbc's
  included), generated by `scripts/third_party_notices.sh`, checked by CI, inside every wheel and
  library tarball.
- Readable access tokens and expiry on Python connections, with session handoff examples.
- Attaching to an existing session, through chargewire-adbc `ca7845d`: `connect(session_id=...)`
  (option `adbc.chargewire.session.id`) uses that session instead of creating one, its open
  transaction included; `Connection.session_id` reads a connection's.
  `connect(close_session_on_release=...)` (option `adbc.chargewire.session.close_on_release`, also a
  connection option) says whether closing ends the session: by default yes for a session the
  connection created, no for an attached one. A session the server doesn't have, or another
  user's, is `NotFound` with `chargewire.code` `session_not_found`. With several replicas it works
  only when routing reaches the replica that holds the session (the README says more).
- `connect(subject_token=...)`: sign in with your identity provider's token (an OIDC ID token
  or another JWT the server trusts), exchanged for a Hardcharger access token (Chargewire's
  `token_exchange` grant, option `adbc.chargewire.auth.subject_token`). It is refused beside
  `username`, `password`, `access_token`, `auth_type="external"` or `"client_certificate"`
  (also when the subject token comes through `db_kwargs`: the driver would silently use one
  credential), and beside a different `db_kwargs` value for the same option.
- `connect(tls_server_name=...)`: the server's host name for TLS when the URI's host isn't it
  (an IP address, a tunnel, a load balancer), option `adbc.chargewire.tls.server_name`. It is sent
  as the TLS server name (SNI) on every connection, result partitions and ingest parts too,
  and the certificate is checked against it: GizmoSQL's `tls_override_hostname`. The tests
  read the SNI off the wire through a recording proxy.

### Changed

- A shared browser sign-in whose token the server refuses with `token_revoked`,
  `credentials_changed` or `principal_dropped` no longer starts another browser sign-in on its
  own (Chargewire §4.2): the error says why. Without a reason, after `token_expired`, or with a
  reason the driver doesn't know, it signs in again once, as before.
- **Chargewire protocol 1.0.0**, through chargewire-adbc `b8f6cdd` (needs a Hardcharger server that
  speaks 1.0.0, from its `spec/v1.0` branch; the driver has no fallbacks for the draft's names).
  What changes for Hardcharger users: a server that requires client certificates and gets a
  connection without one is the refusal `certificate_required` (`UNAUTHENTICATED`; it was
  `client_certificate_required`, or an unreachable server, `IO`); codes the driver makes up
  start with `client_` (`client_protocol_error`, and `client_http_error` for an error status
  without a problem document); errors carry `chargewire.reason` / `chargewire.cancel_reason` when the server
  gave one; after `invalid_token` with `token_revoked`, `credentials_changed` or
  `principal_dropped` the driver no longer signs in again on its own; a statement cancelled by
  the time the server answers is `CANCELLED`; unknown states and reasons are accepted (§12); a
  partitioned ingest commits with `part_count`; `adbc.chargewire.statement.query_tag` is sent compact
  and ASCII-escaped, at most 4,096 bytes. The README names the codes.
- Browser sign-in (`auth_type=external`) is Chargewire's (spec §4.3), on the server's own port and
  URL, run by the Chargewire driver (`chargewire.BrowserSignIn`, chargewire-adbc 6ee3a73): the driver
  creates a flow, opens the identity provider's page, polls the flow with its secret and gets
  the server's own access token, which it uses as `adbc.chargewire.auth.access_token` (the identity
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

- The README banner is the new Hardcharger™ logo, the bull mascot lockup in a light and a dark
  version that follows the reader's color scheme, in place of the GizmoData banner card.
- Trademark notices: Hardcharger™ and GizmoData™ on the README titles and first mentions, package
  metadata and the footer, which says both are trademarks of GizmoData LLC.

### Added

- `adbc.chargewire.session.transaction` (read-only, connection; Python
  `ConnectionOptions.SESSION_TRANSACTION`): the session's transaction as the server reports it
  (`none`, `open`, `aborted`, `unknown`), which sees SQL `BEGIN` and `ROLLBACK` too. Live tests
  (Go, Python) for it and for `certificate_required` at the handshake; both fail with the
  previous chargewire-adbc.
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
- Client certificate sign-in (mutual TLS, Chargewire §4.4): `adbc.hardcharger.auth_type=client_certificate`
  (also `mtls`), Python `connect(auth_type="client_certificate", tls_client_cert=..., tls_client_key=...)`.
  It is the Chargewire driver's `adbc.chargewire.auth.type`: the certificate is the credential, the token is
  bound to it, and result partitions and ingest parts present it too. Needs a server with
  `--mtls-ca-cert` and `--mtls-certificate-sign-in`. Live tests in Go and Python against
  `hardcharger-server`: sign-in (an e-mail identity matching `--mtls-admin-identities` ignoring
  case), a split result and a partitioned ingest (row count, distinct keys and sum checked
  exactly), an unknown identity (`unknown_certificate_identity`), a bound token over another
  certificate (`certificate_mismatch`) and its twin, a password beside the certificate, and a server
  with certificate sign-in off (capability `mtls_identity`) with its password twin.

### Changed

- Built on chargewire-adbc `56483f5` (fix/driver-polish: the plain-HTTP hint and best-effort close),
  from `b74dfe2` (client certificate sign-in), from `b3d2ab8`.

### Fixed

- A user's SQL that failed while running (`execution_error`, HTTP 422: a division by zero, a failed
  conversion, `error()`) was ADBC status `INTERNAL`, so Python raised `InternalError`, which says
  the driver or server is broken. It is now `INVALID_ARGUMENT` (`ProgrammingError`), like a SQL
  error before it runs; a violated constraint (SQLSTATE `23000`) is `INTEGRITY` (`IntegrityError`).
  Built on chargewire-adbc `15ff049` (fix/adbc-error-status), which maps every Chargewire error code to
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

- In Go, Chargewire options set on a database after it was created (`db.SetOption`,
  `db.SetOptions`, e.g. `adbc.chargewire.retry.max_attempts`) were refused as unknown: they went to
  driverbase's `SetOptions`, which knows none of them. They now reach the Chargewire driver one by one.
  (Options given at creation, and every option set through the C API or Python, were not affected.)

- The `dbapi` module exports PEP 249's exception classes (`Error`, `Warning`, `InterfaceError`,
  `DatabaseError`, `DataError`, `OperationalError`, `IntegrityError`, `InternalError`,
  `ProgrammingError`, `NotSupportedError`), as PEP 249 requires of a driver module. They are
  `adbc_driver_manager`'s own classes, so `except dbapi.ProgrammingError` and
  `except adbc_driver_manager.ProgrammingError` catch the same errors.

### Added

- The Hardcharger ADBC driver: a Go package (`github.com/gizmodata/hardcharger-adbc/go/hardcharger`)
  on the Chargewire ADBC driver (chargewire-adbc b3d2ab8, `chargewire.NewDriverWithName`), compiled to the C shared library
  `libadbc_driver_hardcharger` (entry point `AdbcDriverHardchargerInit`), so every ADBC
  language gets:
  - `hardcharger://host[:port][/base-path][?tls=false]` URIs: Chargewire's `chargewire://` with port
    31337 by default; `chargewire://`, `https://` and `http://` pass through; `hardcharger+http://`
    is refused with the spelling to use;
  - "ADBC Hardcharger Driver - Go" as GetInfo's driver name;
  - the `adbc.hardcharger.*` options: query and session tags, `hardcharger.*` session settings
    on every session (database) or one (connection; read back with GetOption), global
    settings, and browser sign-in (`auth_type=external`, exchanging the identity provider's ID
    token for a Chargewire access token).
- The Chargewire driver's defaults, inherited: a large result is split into up to 32 partitions
  read at once (`adbc.chargewire.statement.result_partitions`), and a bulk ingest of two batches or
  more goes in parts, `auto`: up to 16 on a near link, 32 on a far one (`adbc.chargewire.ingest.parts`,
  also `adbc.hardcharger.ingest.parts` on the database or a connection). Tests count the partitions (`ExecutePartitions`) and the
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
