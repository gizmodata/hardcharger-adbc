<p align="center">
  <a href="https://github.com/gizmodata/hardcharger">
    <img src="docs/assets/hardcharger-adbc-banner.svg" alt="Hardcharger™ ADBC: one native driver for every ADBC language. By GizmoData™." width="100%">
  </a>
</p>

<p align="center">
  <a href="https://pypi.org/project/adbc-driver-hardcharger/"><img src="https://img.shields.io/badge/PyPI-adbc--driver--hardcharger-blue.svg?logo=PyPI" alt="PyPI"></a>
  <a href="https://arrow.apache.org/adbc/"><img src="https://img.shields.io/badge/ADBC-1.12-f15a24.svg" alt="ADBC 1.12"></a>
  <a href="https://github.com/gizmodata/warp-sql"><img src="https://img.shields.io/badge/protocol-Warp%20SQL-31a8db.svg" alt="Warp SQL protocol"></a>
  <img src="https://img.shields.io/badge/core-Go-00ADD8.svg?logo=go&logoColor=white" alt="Go core">
  <img src="https://img.shields.io/badge/bindings-Python-3776AB.svg?logo=python&logoColor=white" alt="Python bindings">
  <a href="https://github.com/gizmodata/hardcharger"><img src="https://img.shields.io/badge/server-Hardcharger-f15a24.svg" alt="Hardcharger"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache%202.0-brightgreen.svg" alt="Apache 2.0 license"></a>
  <a href="https://gizmodata.com"><img src="https://img.shields.io/badge/by-GizmoData%E2%84%A2-f15a24.svg" alt="GizmoData™"></a>
</p>

<h3 align="center">Query Hardcharger from Python, C/C++, Go, R, Rust or any ADBC language.</h3>

---

## 🔌 What is it?

The [ADBC](https://arrow.apache.org/adbc/) driver for
[Hardcharger™](https://github.com/gizmodata/hardcharger), GizmoData™'s DuckDB 2.0 server. It is
a native Go driver built on the generic [Warp SQL ADBC driver](https://github.com/gizmodata/warp-sql-adbc),
which does the work (Arrow IPC streaming, bind parameters, cancel, retries, TLS, and by
default results split into up to 8 partitions read at once and bulk ingests sent in up to 8
parts), compiled to one C shared library, `libadbc_driver_hardcharger`, that
every ADBC language loads. On top of Warp SQL it adds what is particular to Hardcharger, for
every language at once:

- 🏎️ **`hardcharger://` URIs**, with Hardcharger's port 31337 as the default;
- 🏷️ **query and session tags**, recorded with every statement in Hardcharger's instrumentation;
- ⚙️ **`hardcharger.*` settings**: set them for every session, or read, set and reset them on one;
- 🌐 **browser sign-in** through the server's identity provider (SSO);
- 🪪 **client certificate sign-in** (mutual TLS): the certificate is the credential, no password;
- 🧾 **"ADBC Hardcharger Driver - Go"** as the driver name in GetInfo (the vendor name,
  "Hardcharger", comes from the server);
- 🧩 **a driver manifest**, so `driver = "hardcharger"` finds it in any ADBC driver manager.

> **Status: in development, unreleased (0.1.0).** Speaks the Warp SQL protocol 1.0.0, built on
> warp-sql-adbc `b8f6cdd`; tested against `hardcharger-server` built from Hardcharger's
> `spec/v1.0` branch (`b4c813b`).

| Package | What it is |
|---|---|
| `pip install adbc-driver-hardcharger` | this driver: Python bindings, with the library in the wheel |
| `pip install hardcharger` | the Hardcharger **server**, started from Python |
| `pip install "hardcharger[adbc]"` | both |
| `go get github.com/gizmodata/hardcharger-adbc/go` | the Go package |
| [Releases](https://github.com/gizmodata/hardcharger-adbc/releases) | the library and the manifest template, for C/C++, R, Rust, ... |

## 🚀 Quick start

### Python

```python
from adbc_driver_hardcharger import dbapi as hardcharger

with hardcharger.connect(uri="hardcharger://localhost?tls=false", username="hardcharger", password="change-me") as conn:
    with conn.cursor() as cur:
        cur.execute(operation="SELECT 42 AS answer, ? AS greeting", parameters=["hello"])
        print(cur.fetch_arrow_table())
```

The connection and cursor are DB-API 2.0 objects with ADBC's extensions:
`fetch_arrow_table()`, `fetchall()`, `adbc_ingest()` for bulk loads, `adbc_get_objects()` and
the rest.

### C/C++ (through the ADBC driver manager)

With the manifest installed (see [Driver manifest](#-driver-manifest)), the driver manager
finds the driver by name:

```c
#include <arrow-adbc/adbc.h>
#include <arrow-adbc/adbc_driver_manager.h>

// (error handling elided; see examples/c/hello.c)
AdbcDatabaseNew(&database, &error);
// Search ADBC_DRIVER_PATH and the user and system directories for hardcharger.toml.
AdbcDriverManagerDatabaseSetLoadFlags(&database, ADBC_LOAD_FLAG_DEFAULT, &error);
AdbcDatabaseSetOption(&database, "driver", "hardcharger", &error);
AdbcDatabaseSetOption(&database, "uri", "hardcharger://localhost?tls=false", &error);
AdbcDatabaseSetOption(&database, "username", "hardcharger", &error);
AdbcDatabaseSetOption(&database, "password", "change-me", &error);
AdbcDatabaseSetOption(&database, "adbc.hardcharger.query_tag", "{\"app\": \"hello.c\"}", &error);
AdbcDatabaseInit(&database, &error);
```

[examples/c/hello.c](examples/c/hello.c) is the whole program (it prints `answer = 42`):

```bash
cc examples/c/hello.c -I"$ADBC/include" -L"$ADBC/lib" -ladbc_driver_manager -o hello && ./hello
```

R (`adbcdrivermanager`), Rust (`adbc_driver_manager`) and the other driver managers load it
the same way, by the name `hardcharger`, with the same URIs and options.

### Go

```go
db, err := hardcharger.NewDriver(memory.DefaultAllocator).NewDatabaseWithContext(ctx, map[string]string{
	"uri":                      "hardcharger://localhost?tls=false",
	"username":                 "hardcharger",
	"password":                 "change-me",
	hardcharger.OptionQueryTag: `{"app": "hello.go"}`,
})
cnxn, err := db.Open(ctx)
```

[go/examples/hello](go/examples/hello/main.go) is the whole program (`go run ./examples/hello`
in `go/`). The package is `github.com/gizmodata/hardcharger-adbc/go/hardcharger`.

## 🔗 URIs

| URI | Meaning |
|---|---|
| `hardcharger://host` | HTTPS to `host:31337` |
| `hardcharger://host:port` | HTTPS to another port |
| `hardcharger://host?tls=false` | Plain HTTP (development, loopback) |
| `hardcharger://host:port/custom/prefix` | The server mounts Warp SQL under `/custom/prefix` |
| `warp://…`, `https://…`, `http://…` | The Warp SQL driver's own spellings, as they are |

`hardcharger://` means exactly what Warp SQL's `warp://` means, so TLS is on unless the URI
says `?tls=false`. Pointed at a server that speaks plain HTTP, a TLS URI fails at once, having
sent nothing but the TLS hello, with `The server at HOST:PORT speaks plain HTTP, not TLS. Add
?tls=false to the URI to connect without encryption.`; the driver never falls back to plaintext. There is deliberately no `hardcharger+http://`: one spelling for plain HTTP
keeps URIs the same in every driver (JDBC, the `hardcharger` command-line client), and the
driver refuses `hardcharger+http://` with a message naming `?tls=false`.

## ⚙️ Options

Every option of the [Warp SQL driver](https://github.com/gizmodata/warp-sql-adbc#%EF%B8%8F-options)
(`adbc.warp.*`: TLS, tokens, retries, compression, ...) applies, with its defaults. Two shape
throughput, and the driver keeps the Warp SQL driver's defaults:

| Option | Default | Meaning |
|---|---|---|
| `adbc.warp.statement.result_partitions` | 8 | A large result is split into up to this many partitions, read at once over their own connections (the server decides; a result of fewer than two batches is one stream). `1`: never split |
| `adbc.warp.ingest.parts` (also `adbc.hardcharger.ingest.parts`) | 8 | A bulk ingest of two batches or more goes over up to this many connections, committed as one. Its rows land in no defined order; `1`: one stream, in the upload's order |

Hardcharger's own:

| Option | Where | Meaning |
|---|---|---|
| `adbc.hardcharger.query_tag` | database, connection | A JSON object recorded with every following statement (`hardcharger.query_tag`); `""` removes it |
| `adbc.hardcharger.session_tag` | database, connection | A JSON object tagging the session (`hardcharger.session_tag`) |
| `adbc.hardcharger.setting.NAME` | database, connection | `SET SESSION hardcharger.NAME`: on every new session (database) or now (connection; `""` resets, and GetOption reads the value that applies) |
| `adbc.hardcharger.global_setting.NAME` | connection | `SET GLOBAL hardcharger.NAME` (admins); `""` resets |
| `adbc.hardcharger.ingest.parts` | database, connection | `adbc.warp.ingest.parts` under Hardcharger's name (on a statement, use the Warp SQL name) |
| `adbc.hardcharger.auth_type` | database | `password` (default), `external` (browser sign-in), or `client_certificate` (also `mtls`): sign in with the client certificate alone; it is the Warp SQL driver's `adbc.warp.auth.type` |
| `adbc.hardcharger.oauth.timeout_seconds` | database | How long to wait for the sign-in (default 300) |
| `adbc.hardcharger.oauth.browser` | database | Command that opens the sign-in page (the URL is appended); default `$HARDCHARGER_BROWSER`, else the system's; `none` prints the URL |
| `adbc.hardcharger.oauth.share_sign_in` | database | `true`: share one browser sign-in among this process's databases that sign in to the same server (see [below](#one-sign-in-for-many-connections)); default `false` |
| `adbc.hardcharger.oauth.sign_in_key` | database | Names a shared sign-in (default `""`): only databases with the same key share one |

Tags are stored in `_hardcharger_instr.sql_statements.query_tag` and
`_hardcharger_instr.sessions.session_tag` when the server runs with `--enable-instrumentation`;
a tag that isn't a JSON object is refused with `Invalid JSON for query_tag`.

`adbc.warp.session.transaction` (read-only, connection; Python
`ConnectionOptions.SESSION_TRANSACTION`) is the session's transaction as the server reports it:
`none`, `open`, `aborted`, or `unknown`. It sees SQL `BEGIN` and `COMMIT` too.

Errors carry the server's error code in the detail `warp.code` (`sql_error`,
`permission_denied`, ...), or a code beginning with `client_` for a failure the driver found
in the server's answer (`client_protocol_error`; `client_http_error` for an error status
without a problem document, such as a proxy's page). `warp.reason` and `warp.cancel_reason`
say why a token or session stopped working or a statement was cancelled (`token_revoked`,
`killed`, ...). After `token_revoked`, `credentials_changed` or `principal_dropped` the driver
does not sign in again on its own.

## 🐍 Python helpers

```python
with hardcharger.connect(uri="hardcharger://localhost?tls=false", username="hardcharger", password="change-me",
                         session_tag={"app": "nightly-etl"}, query_tag={"job": "load-orders"},
                         settings={"query_timeout": 60}) as conn:
    conn.set_query_tag(tag={"job": "refresh-rollups"})          # every later statement
    conn.set_setting(name="query_timeout", value=120)           # SET SESSION hardcharger.query_timeout
    print(conn.get_setting(name="query_timeout"))                # '120'
    conn.reset_setting(name="query_timeout")
    conn.set_setting(name="max_concurrent_statements", value=8, scope="global")  # admins only
    conn.reset_setting(name="max_concurrent_statements", scope="global")
    print(conn.current_user(), conn.server_version())          # e.g. hardcharger 0.2.0
    print(conn.settings())                                       # hardcharger_settings(), as Arrow
```

They map to the options above, so they behave exactly as in C or Go.

## 🌐 Browser sign-in (SSO)

When the server offers browser sign-in (`--oauth-client-id` and `--oauth-client-secret`, with
the identity provider in `--token-allowed-issuer`; it then lists the Warp SQL capability
`oauth_browser`), sign in as yourself:

```python
with hardcharger.connect(uri="hardcharger://db.example.com", auth_type="external") as conn:
    print(conn.current_user())  # the e-mail address you signed in with
```

When the first connection opens, the driver asks the server for a sign-in flow (Warp SQL's
browser sign-in, on the server's own port and URL: nothing else to configure on the client),
opens the identity provider's page in your browser, waits, and receives the server's own
access token. The identity provider's settings stay on the server. `open_browser=False`
(option `oauth.browser=none`) prints the URL instead, for a remote shell; a string is the
command to open it with. The token can't be renewed: when it expires, open a new connection
and sign in again.

### One sign-in for many connections

Each database signs in on its own, and the DB-API's `connect()` makes a database per
connection, so a program that opens several connections (dbt, a pool, a notebook) would open a
browser window for each. `share_sign_in=True` (option `adbc.hardcharger.oauth.share_sign_in`)
shares one sign-in among the process's databases that sign in to the same server, with the same
TLS trust:

```python
for model in ("orders", "customers", "rollups"):
    with hardcharger.connect(uri="hardcharger://db.example.com", auth_type="external",
                             share_sign_in=True) as conn:   # one browser sign-in for all three
        ...
```

The first connection signs in (others opened meanwhile wait for it); the rest reuse the
server's access token it got, until shortly before it expires (a minute, or a quarter of a
shorter lifetime), when the next connection signs in again. A shared token the server refuses is dropped and that connection signs in again, once.
The token lives only in the process's memory and never goes to another server. To sign in as
two people (or roles) to one server in one process, give each its own key:
`share_sign_in="alice"` (option `adbc.hardcharger.oauth.sign_in_key`). In Go,
`hardcharger.ForgetSharedSignIns()` drops them all, for a sign-out.

## 🪪 Client certificate sign-in (mutual TLS)

When the server verifies client certificates and lets them sign in (`--mtls-ca-cert` with
`--mtls-certificate-sign-in`), the certificate is the credential: no user name or password.

```bash
HARDCHARGER_ADMIN_PASSWORD=change-me hardcharger-server --tls-cert server.pem --tls-key server-key.pem \
  --mtls-ca-cert client-ca.pem --mtls-certificate-sign-in --mtls-admin-identities ops@example.com
```

```python
with hardcharger.connect(uri="hardcharger://db.example.com", auth_type="client_certificate",
                         tls_client_cert="client.pem", tls_client_key="client-key.pem",
                         tls_root_certs="server-ca.pem") as conn:  # when the server's CA is private
    print(conn.current_user())  # the certificate's identity: ops@example.com
```

In C, Go and every other language, set `adbc.hardcharger.auth_type` to `client_certificate`
with the Warp SQL driver's `adbc.warp.tls.client_cert` and `adbc.warp.tls.client_key` (PEM
text; `adbc.warp.tls.root_certs` too when the server's certificate comes from a private CA):

```go
db, err := hardcharger.NewDriver(memory.DefaultAllocator).NewDatabaseWithContext(ctx, map[string]string{
	"uri":                      "hardcharger://db.example.com",
	hardcharger.OptionAuthType: hardcharger.AuthTypeClientCertificate,
	"adbc.warp.tls.client_cert": certPEM,
	"adbc.warp.tls.client_key":  keyPEM,
})
```

The identity is the certificate's e-mail SAN unless the server says otherwise
(`--mtls-identity-source`); it must be one of `--mtls-admin-identities` or, under access
control, a user of the store. The token the driver gets is bound to the certificate, and every
connection it opens (result partitions, ingest parts) presents the same one. The driver refuses
a user name, password or token beside `client_certificate`, a missing certificate or key, and
plain HTTP before sending anything, and a server that doesn't offer certificate sign-in
(capability `mtls_identity`) before trying it. The server's refusals carry its code in
`warp.code`: `unknown_certificate_identity`, `certificate_mismatch` (a token bound to another
certificate), `certificate_required` (also when the server refuses a connection without a
certificate at the TLS handshake), `certificate_revoked`, and `certificate_identity_mismatch`.

## 🎫 Token exchange (your identity provider's tokens)

A program that already holds a token from your identity provider (an OpenID Connect ID token
or another JWT: a workload identity, a CI job's token, a token your web app got at sign-in)
signs in with it directly: the driver exchanges it for a Hardcharger access token (Warp SQL's
`token_exchange` grant), with no password and no browser. The server checks the token against
the provider's keys, issuer and audience:

```bash
HARDCHARGER_ADMIN_PASSWORD=change-me hardcharger-server --tls-cert server.pem --tls-key server-key.pem \
  --token-jwks-uri https://login.example.com/oauth2/v1/keys \
  --token-allowed-issuer https://login.example.com --token-allowed-audience hardcharger \
  --token-authorized-emails '*@example.com' --token-default-role user
```

```python
with hardcharger.connect(uri="hardcharger://db.example.com", subject_token=id_token) as conn:
    print(conn.current_user())  # the token's e-mail address
```

`subject_token=` is the Warp SQL driver's `adbc.warp.auth.subject_token` (in C, Go and every
other language, set that option). Python refuses it beside `username`, `password`,
`access_token`, `auth_type="external"` or `"client_certificate"`, and a different value for the
same option in `db_kwargs`: the driver would otherwise use one credential and ignore the
others. A token the server doesn't accept (another issuer or audience, expired, a bad
signature, an e-mail address outside `--token-authorized-emails`) is refused with
`UNAUTHENTICATED` and `warp.code` `invalid_credentials`. The driver exchanges the token when
the first connection opens, and again shortly before Hardcharger's access token expires, as
long as your provider's token is still valid. Once that token has expired the server refuses
the next exchange (`invalid_credentials`), so open a new connection with a fresh
`subject_token`.

## 🏷️ TLS server name

When the URI's host isn't the name on the server's certificate (you connect by IP address, or
through a tunnel, port forward or load balancer), give the name with `tls_server_name=` (option
`adbc.warp.tls.server_name`). The driver sends it as the TLS server name (SNI), so a proxy or
ingress that routes on SNI sends the connection to the right server, and checks the
certificate against it instead of the URI's host:

```python
with hardcharger.connect(uri="hardcharger://10.0.0.7", username="hardcharger", password="change-me",
                         tls_server_name="db.example.com",
                         tls_root_certs="server-ca.pem") as conn:  # when the server's CA is private
    print(conn.current_user())
```

Without it, a connection by IP address sends no server name at all (TLS never carries an IP
address as one) and the certificate must list that IP address. It is what GizmoSQL's
`tls_override_hostname` was. Every connection the driver opens (result partitions, ingest
parts) sends the same name; `tls_skip_verify=True` still sends it, without checking the
certificate. Python refuses a different value for the same option in `db_kwargs`.

## 🔁 Attaching to an existing session

A session (its open transaction, temporary tables and settings) can outlive its connection
and be picked up by another process: a job runner that begins a transaction in one step and
commits it in the next, or a worker that restarts. The first connection keeps its session
when it closes (`close_session_on_release=False`) and passes on `conn.session_id` with its
`conn.access_token` (see [below](#-reading-the-access-token)); the second attaches with
`session_id=` and that token, since the session belongs to the user who created it:

```python
# Process 1: begin a transaction, then hand the session on.
with hardcharger.connect(uri="hardcharger://db.example.com", username="hardcharger", password="change-me",
                         close_session_on_release=False) as conn:
    with conn.cursor() as cur:
        cur.execute(operation="BEGIN TRANSACTION")
        cur.execute(operation="INSERT INTO orders VALUES (1, 'pending')")
    session_id, token = conn.session_id, conn.access_token  # pass both on; the token is a secret

# Process 2: attach, see the uncommitted row, commit.
with hardcharger.connect(uri="hardcharger://db.example.com", access_token=token,
                         session_id=session_id) as conn:
    with conn.cursor() as cur:
        cur.execute(operation="SELECT count(*) FROM orders")
        print(cur.fetchone())  # (1,)
        cur.execute(operation="COMMIT")
```

They are the Warp SQL driver's database options `adbc.warp.session.id` and
`adbc.warp.session.close_on_release` (a connection option too, settable any time), in every
language. Opening checks the session (`GET /warp/v1/sessions/{id}`) and creates none: an id the
server doesn't have, or another user's session, fails with `NotFound` and `warp.code`
`session_not_found` (the server answers both the same way). Closing an attached connection
leaves the session (`close_session_on_release` defaults to `False` for it, `True` for a
session the connection created); it ends when a connection with `True` closes or when it idles
out (`--session-idle-timeout`). An attached session the server loses is not replaced.

**Several replicas:** a session lives on the one Hardcharger replica that created it.
Attaching from another client works only if its requests are routed to that replica. Affinity
by client IP address won't do it when the second client's address differs from the first's
(another host, NAT or CI runner): the attach then fails with `session_not_found`. Route on
something both clients send, or connect both to that replica's own address.

## 🔑 Reading the access token

A connection's access token is readable: the token the driver got when it signed in (password,
`subject_token`, browser), renewed first when it is about to expire, so after a renewal you get
the new one; or the `access_token` you gave `connect()`, as it is.

```python
with hardcharger.connect(uri="hardcharger://db.example.com", username="hardcharger", password="change-me") as conn:
    token = conn.access_token              # a bearer secret: never log it
    print(conn.access_token_expires_at)    # e.g. 2026-10-20 16:14:13+00:00 (None when unknown)
    print(conn.adbc_connection.get_option(key="adbc.warp.auth.access_token_expires_at"))
    # '2026-10-20T16:14:13Z'
```

They are the Warp SQL driver's `adbc.warp.auth.access_token` and
`adbc.warp.auth.access_token_expires_at` (read-only; RFC 3339 UTC, from the token response's
`expires_in` or the token's own `exp`, `""` when unknown), readable on the database and the
connection in every language. The token works as `Authorization: Bearer <token>` on the
server's HTTP API, and with `session_id` it lets another process carry on in this connection's
session ([above](#-attaching-to-an-existing-session)).

**It is a bearer secret:** whoever holds it acts as the user until it expires (the server's
`--token-lifetime-seconds`). Keep it in memory or store it encrypted, send it only over TLS,
and never log it, print it or put it in a URL. A token handed to another process can't be
renewed there (it was issued elsewhere): give that process a fresh one, or its own sign-in,
before this one expires.

## 🧩 Driver manifest

`driver = "hardcharger"` works in any ADBC driver manager once `hardcharger.toml` is in one of
its search directories:

```bash
python -m adbc_driver_hardcharger.manifest          # for this user, pointing at the wheel's library
python -m adbc_driver_hardcharger.manifest --print  # or print it
```

Without Python, fill in [packaging/hardcharger.toml.in](packaging/hardcharger.toml.in) with the
directory of the library from a [release](https://github.com/gizmodata/hardcharger-adbc/releases)
tarball, and install it as `hardcharger.toml` in `~/.config/adbc/drivers/` (Linux),
`~/Library/Application Support/ADBC/Drivers/` (macOS), `%LOCALAPPDATA%\ADBC\Drivers\`
(Windows), or a directory on `ADBC_DRIVER_PATH`.

## 📂 Layout

```
go/hardcharger/      the driver (package hardcharger), on warp-sql-adbc's go/warpsql
go/pkg/hardcharger/  cgo exports for the C shared library (generated: make -C go regenerate)
go/examples/hello/   the Go example
python/              adbc-driver-hardcharger: ships the library in its wheel, adds dbapi.connect()
examples/c/          the C example, through the ADBC driver manager
packaging/           the driver manifest template
```

## 🛠️ Building and testing

Go 1.27 with cgo (and, on Linux, `zig` for the glibc 2.17 build), Python 3.11+. The Warp SQL
driver is a Go module in an internal repository until launch:
`GOPRIVATE=github.com/gizmodata/*` with git access to `gizmodata/warp-sql-adbc`.

```bash
scripts/build_lib.sh                       # go/build/libadbc_driver_hardcharger.{so,dylib,dll}
make -C go test                            # Go unit tests (race detector)
cd python && uv venv .venv && uv pip install --python .venv/bin/python -e ".[test,dev]"

export HARDCHARGER_ADBC_TEST_SERVER_BINARY=/path/to/hardcharger-server
make -C go integration-test                # Go live suite (race detector)
.venv/bin/python -m pytest                 # Python: unit and live
```

The live suites start real `hardcharger-server` processes on free loopback ports (plain HTTP,
TLS, instrumentation, browser sign-in against a mock identity provider; the defaults: a large
result arrives in several partitions and an ingest goes in several parts, counted through a
recording proxy) and, in Python, load
the library by manifest through the driver manager. Without a server binary they fail, never
skip, and a Go run in which no live test ran fails too. CI also builds the ADBC C driver
manager and runs the C and Go examples against a server on the default port. CI's server is
the `ghcr.io/gizmodata/hardcharger:main` image (Hardcharger's main branch), pulled with
`GITHUB_TOKEN`; pin a `:sha-<short>` tag in `ci.yml` to reproduce an old run.

## 📦 Releasing

Releases follow [semantic versioning](https://semver.org) and [CHANGELOG.md](CHANGELOG.md).
Pushing a `vX.Y.Z` tag runs every suite on the tagged commit, then builds the library for Linux
amd64/arm64 (glibc 2.17; installed and loaded on Oracle Linux 8), macOS arm64/amd64 and Windows
amd64/arm64, wheels for all of them but Windows arm64 (pyarrow and adbc-driver-manager publish
no wheels there), a GitHub Release with the libraries, wheels and manifest template, and PyPI
from the `pypi` environment (trusted publishing, after an approval).

## 📄 License

Apache License 2.0; see [LICENSE](LICENSE).

---

<p align="center">
  <a href="https://gizmodata.com"><img src="docs/assets/gizmodata-logo.png" alt="GizmoData™" width="56"></a><br>
  <sub>Built by <a href="https://gizmodata.com">GizmoData™</a></sub>
</p>

<p align="center"><sub>Hardcharger™ and GizmoData™ are trademarks of GizmoData LLC.</sub></p>
