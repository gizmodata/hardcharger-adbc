<p align="center">
  <a href="https://github.com/gizmodata/hardcharger">
    <picture>
      <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/gizmodata/hardcharger-adbc/main/docs/assets/hardcharger-banner-dark.png">
      <img src="https://raw.githubusercontent.com/gizmodata/hardcharger-adbc/main/docs/assets/hardcharger-banner-light.png" alt="Hardcharger™" width="400">
    </picture>
  </a>
</p>

<p align="center">
  <a href="https://github.com/gizmodata/hardcharger-adbc/actions/workflows/ci.yml"><img src="https://github.com/gizmodata/hardcharger-adbc/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://pypi.org/project/adbc-driver-hardcharger/"><img src="https://img.shields.io/pypi/v/adbc-driver-hardcharger?logo=pypi" alt="PyPI version"></a>
  <a href="https://pypi.org/project/adbc-driver-hardcharger/"><img src="https://img.shields.io/badge/Python-3.10%2B-3776AB?logo=python&amp;logoColor=white" alt="Python 3.10 or later"></a>
  <a href="https://pypi.org/project/adbc-driver-hardcharger/"><img src="https://img.shields.io/pepy/dt/adbc-driver-hardcharger.svg" alt="PyPI downloads"></a>
  <a href="https://docs.hardcharger.gizmodata.com/clients/python/"><img src="https://img.shields.io/badge/docs-Hardcharger-2563eb.svg" alt="Hardcharger Python client documentation"></a>
  <a href="https://arrow.apache.org/adbc/"><img src="https://img.shields.io/badge/ADBC-1.12-f15a24.svg" alt="ADBC 1.12"></a>
  <a href="https://github.com/gizmodata/hardcharger-adbc/blob/main/validation/README.md"><img src="https://img.shields.io/badge/ADBC-conformance%20suite%20passing-2ea44f.svg" alt="ADBC conformance suite passing"></a>
  <a href="https://adbc-drivers.org/building-drivers/style-guide.html"><img src="https://img.shields.io/badge/ADBC%20style%20guide-conforms-2ea44f.svg" alt="Follows the ADBC driver style guide"></a>
  <a href="https://github.com/gizmodata/chargewire"><img src="https://img.shields.io/badge/protocol-Chargewire-31a8db.svg" alt="Chargewire protocol"></a>
  <img src="https://img.shields.io/badge/core-Go-00ADD8.svg?logo=go&logoColor=white" alt="Go core">
  <img src="https://img.shields.io/badge/bindings-Python-3776AB.svg?logo=python&logoColor=white" alt="Python bindings">
  <a href="https://github.com/gizmodata/hardcharger"><img src="https://img.shields.io/badge/server-Hardcharger-f15a24.svg" alt="Hardcharger"></a>
  <a href="https://github.com/gizmodata/hardcharger-adbc/blob/main/LICENSE"><img src="https://img.shields.io/badge/license-Apache%202.0-brightgreen.svg" alt="Apache 2.0 license"></a>
  <a href="https://gizmodata.com"><img src="https://img.shields.io/badge/by-GizmoData%E2%84%A2-f15a24.svg" alt="GizmoData™"></a>
</p>

<h3 align="center">Query Hardcharger from Python, C/C++, Go, R, Rust or any ADBC language.</h3>

---

## 🔌 What is it?

The Python [ADBC](https://arrow.apache.org/adbc/) driver for
[Hardcharger™](https://github.com/gizmodata/hardcharger), GizmoData™'s DuckDB 2.0 server: a
DB-API 2.0 `connect()` with Arrow results, bind parameters, bulk ingest, cancel, and
Hardcharger's tags, settings, browser sign-in (SSO), client certificate sign-in and connection
profiles. The wheel carries the native driver library (Go, built on the
[Chargewire ADBC driver](https://github.com/gizmodata/chargewire-adbc)), so there is nothing else
to install.

This page is about Python. C/C++, Go, R, Rust and the other ADBC languages, installing the
driver library without Python, and the full option reference are in the
[repository README](https://github.com/gizmodata/hardcharger-adbc#readme).

## 📥 Install

```bash
pip install adbc-driver-hardcharger
```

Python 3.10 or newer, with pyarrow 16.0.0+ and adbc-driver-manager 1.8.0+ (pip installs both;
connection profiles need adbc-driver-manager 1.11.0+). Wheels for Linux (amd64, arm64; glibc 2.17
or newer), macOS 13 or newer (arm64, amd64) and Windows (amd64).
`pip install hardcharger` is the Hardcharger **server**, started from Python;
`pip install "hardcharger[adbc]"` installs both.

## 🚀 Quick start

```python
from adbc_driver_hardcharger import dbapi as hardcharger

with hardcharger.connect(
    uri="hardcharger://localhost?tls=false", username="hardcharger", password="change-me"
) as conn:
    with conn.cursor() as cur:
        cur.execute(operation="SELECT 42 AS answer, ? AS greeting", parameters=["hello"])
        print(cur.fetch_arrow_table())
```

The connection and cursor are DB-API 2.0 objects with ADBC's extensions:
`fetch_arrow_table()`, `fetchall()`, `adbc_ingest()` for bulk loads, `adbc_get_objects()` and
the rest. Errors are the DB-API exceptions (an SQL error is a `ProgrammingError`).

`catalog=` and `db_schema=` make a catalog and a schema current when connecting. `db_schema` is
one schema name in `catalog` (in the server's default catalog without it) and is never split at a
dot, because a dot can be part of a schema's name: for the schema `reports` of the catalog `lake`,
pass `catalog="lake", db_schema="reports"`. On an open connection, set
`conn.adbc_current_catalog` first, then `conn.adbc_current_db_schema`: setting the catalog moves
to that catalog's default schema, as SQL `USE` does.

## 🔗 URIs

| URI | Meaning |
|---|---|
| `hardcharger://host` | HTTPS to `host:31337` |
| `hardcharger://cluster.gizmodata.com` | HTTPS to port 443: a GizmoData Cloud host (`gizmodata.com` or any name under it) defaults to 443 |
| `hardcharger://host:port` | HTTPS to another port (an explicit port always wins) |
| `hardcharger://host?tls=false` | Plain HTTP (development, loopback) |
| `profile://name` | An ADBC connection profile (see Connection profiles below) |

TLS is on unless the URI says `?tls=false`, and the driver never falls back to plain HTTP.

## ⚙️ `connect()` arguments

| Argument | Meaning |
|---|---|
| `uri`, `profile` | Where to connect: a URI, or a connection profile's name or path |
| `username`, `password` | Password sign-in |
| `auth_type` | `"password"` (default), `"external"` (browser sign-in), `"client_certificate"` (also `"mtls"`) |
| `access_token`, `subject_token` | A Hardcharger access token, or your identity provider's token to exchange for one |
| `oauth_timeout`, `open_browser`, `share_sign_in` | Browser sign-in: how long to wait (default 300 s), how to open the page, one sign-in for many connections |
| `tls_root_certs`, `tls_client_cert`, `tls_client_key` | PEM text or file paths: a private CA to trust, a client certificate and its key |
| `tls_server_name`, `tls_skip_verify` | The TLS server name when it isn't the URI's host; no certificate check (development only) |
| `session_tag`, `query_tag` | JSON objects (a `dict`) recorded in Hardcharger's instrumentation |
| `settings` | `hardcharger.*` session settings, e.g. `{"query_timeout": 60}` |
| `catalog`, `db_schema` | The current catalog and schema |
| `session_id`, `close_session_on_release` | Attach to an existing session; keep a session when closing |
| `resume_timeout` | Seconds to wait for a stopped GizmoData Cloud cluster that is starting again (default 600) |
| `autocommit` | Default `True` |
| `db_kwargs`, `conn_kwargs` | Any other ADBC option (`hardcharger.*`; the names from before 2.7.0 still work, with a deprecation warning), listed in the [repository README](https://github.com/gizmodata/hardcharger-adbc#readme) |

`help(adbc_driver_hardcharger.dbapi.connect)` describes each one.

## 🎛️ Tags and settings

```python
with hardcharger.connect(
    uri="hardcharger://localhost?tls=false",
    username="hardcharger",
    password="change-me",
    session_tag={"app": "nightly-etl"},
    query_tag={"job": "load-orders"},
    settings={"query_timeout": 60},
) as conn:
    conn.set_query_tag(tag={"job": "refresh-rollups"})  # every later statement
    conn.set_setting(name="query_timeout", value=120)  # SET SESSION hardcharger.query_timeout
    print(conn.get_setting(name="query_timeout"))  # '120'
    conn.reset_setting(name="query_timeout")
    conn.set_setting(name="max_concurrent_statements", value=8, scope="global")  # admins only
    conn.reset_setting(name="max_concurrent_statements", scope="global")
    print(conn.current_user(), conn.server_version())
    print(conn.settings())  # hardcharger_settings(), as Arrow
```

Tags are stored in `_hardcharger_instr.sql_statements.query_tag` and
`_hardcharger_instr.sessions.session_tag` when the server runs with `--enable-instrumentation`.
Settings and tags apply to every session the connection opens, including one that replaces a
session the server lost.

## 🔒 Read-only connections

Ask the server to enforce read-only before anything runs on the connection (Hardcharger 2.13.0
or newer):

```python
with hardcharger.connect(
    uri="hardcharger://localhost?tls=false",
    username="hardcharger",
    password="change-me",
    conn_kwargs={"adbc.connection.readonly": "true"},
) as conn:
    with conn.cursor() as cur:
        cur.execute(operation="SELECT 42 AS answer")
        print(cur.fetchone())  # (42,)
```

The restriction is one-way; open a separate connection to write. A server that rejects or
ignores the option is an error, with no writable fallback.

## 🌐 Browser sign-in (SSO)

When the server offers browser sign-in (it was started with `--oauth-client-id` and
`--oauth-client-secret`), sign in as yourself:

```python
with hardcharger.connect(uri="hardcharger://db.example.com", auth_type="external") as conn:
    print(conn.current_user())  # the e-mail address you signed in with
```

The driver opens the identity provider's page in your browser, waits, and receives the server's
own access token; nothing else is configured on the client. `open_browser=False` prints the URL
instead, for a remote shell; a string is the command to open it with.

Each `connect()` signs in on its own, so a program that opens several connections (dbt, a pool, a
notebook) would open a browser window for each. `share_sign_in=True` shares one sign-in among the
process's connections to the same server:

```python
for model in ("orders", "customers", "rollups"):
    with hardcharger.connect(
        uri="hardcharger://db.example.com", auth_type="external", share_sign_in=True
    ) as conn:  # one browser sign-in for all three
        ...
```

The token lives only in the process's memory. To sign in as two people (or roles) to one server
in one process, give each its own key: `share_sign_in="alice"`.

## 🪪 Client certificate sign-in (mutual TLS)

On a server that lets client certificates sign in (`--mtls-ca-cert` with
`--mtls-certificate-sign-in`), the certificate is the credential:

```python
with hardcharger.connect(
    uri="hardcharger://db.example.com",
    auth_type="client_certificate",
    tls_client_cert="client.pem",
    tls_client_key="client-key.pem",
    tls_root_certs="server-ca.pem",
) as conn:  # when the server's CA is private
    print(conn.current_user())  # the certificate's identity, e.g. ops@example.com
```

## 🎫 Token exchange (your identity provider's tokens)

A program that already holds a token from your identity provider (an OpenID Connect ID token or
another JWT the server trusts: `--token-jwks-uri`, with its issuer and audience) signs in with it
directly, with no password and no browser:

```python
with hardcharger.connect(uri="hardcharger://db.example.com", subject_token=id_token) as conn:
    print(conn.current_user())  # the token's e-mail address
```

The driver exchanges it for a Hardcharger access token, and again shortly before that expires,
while your provider's token is still valid.

## 🏷️ TLS server name

When the URI's host isn't the name on the server's certificate (an IP address, a tunnel, a port
forward), give the name with `tls_server_name=`; the driver sends it as the TLS server name (SNI)
and checks the certificate against it:

```python
with hardcharger.connect(
    uri="hardcharger://10.0.0.7",
    username="hardcharger",
    password="change-me",
    tls_server_name="db.example.com",
    tls_root_certs="server-ca.pem",
) as conn:  # when the server's CA is private
    print(conn.current_user())
```

## 🗂️ Connection profiles

[ADBC connection profiles](https://arrow.apache.org/adbc/current/format/connection_profiles.html)
keep a reusable connection in a TOML file (adbc-driver-manager 1.11.0 or newer). Install the
driver manifest once, so the driver manager finds `driver = "hardcharger"`:

```bash
python -m adbc_driver_hardcharger.manifest
```

Save `hardcharger_prod.toml` in your profile directory (`~/.config/adbc/profiles/` on Linux,
`~/Library/Application Support/ADBC/Profiles/` on macOS, `%LOCALAPPDATA%\ADBC\Profiles\` on
Windows, or a directory in `ADBC_PROFILE_PATH`):

```toml
profile_version = 1
driver = "hardcharger"

[Options]
uri = "hardcharger://db.example.com:31337"
username = "{{ env_var(HARDCHARGER_USERNAME) }}"
password = "{{ env_var(HARDCHARGER_PASSWORD) }}"
```

Set those environment variables, then:

```python
from adbc_driver_hardcharger import dbapi as hardcharger

with hardcharger.connect(profile="hardcharger_prod") as conn, conn.cursor() as cur:
    cur.execute(operation="SELECT 42 AS answer")
    print(cur.fetch_arrow_table())

# Equivalent forms:
# hardcharger.connect(uri="profile://hardcharger_prod")
# hardcharger.connect(profile="/absolute/path/hardcharger_prod.toml")
```

Arguments given to `connect()` override the profile's `[Options]`. The same profile works through
`adbc_driver_manager.dbapi.connect(profile="hardcharger_prod")`, without importing this package.

## 🔁 Attaching to an existing session

A session (its open transaction, temporary tables and settings) can outlive its connection and be
picked up by another process. The first connection keeps its session when it closes and passes on
its id and access token; the second attaches with both:

```python
# Process 1: begin a transaction, then hand the session on.
with hardcharger.connect(
    uri="hardcharger://db.example.com",
    username="hardcharger",
    password="change-me",
    close_session_on_release=False,
) as conn:
    with conn.cursor() as cur:
        cur.execute(operation="BEGIN TRANSACTION")
        cur.execute(operation="INSERT INTO orders VALUES (1, 'pending')")
    session_id, token = conn.session_id, conn.access_token  # pass both on; the token is a secret

# Process 2: attach, see the uncommitted row, commit.
with hardcharger.connect(
    uri="hardcharger://db.example.com", access_token=token, session_id=session_id
) as conn:
    with conn.cursor() as cur:
        cur.execute(operation="SELECT count(*) FROM orders")
        print(cur.fetchone())  # (1,)
        cur.execute(operation="COMMIT")
```

An id the server doesn't have, or another user's session, fails with `session_not_found`. A
session lives on the server replica that created it, so behind a load balancer both clients must
reach that replica.

## 🔑 Reading the access token

```python
with hardcharger.connect(
    uri="hardcharger://db.example.com", username="hardcharger", password="change-me"
) as conn:
    token = conn.access_token  # a bearer secret: never log it
    print(conn.access_token_expires_at)  # e.g. 2026-10-20 16:14:13+00:00 (None when unknown)
```

The token is renewed first when it is about to expire, so you get a current one. Whoever holds it
acts as the user until it expires: keep it in memory, send it only over TLS, and never log it.

## 🧩 Driver manifest (other ADBC languages on this machine)

The wheel's library also serves every other ADBC driver manager on the machine (C/C++, R, Rust,
Go) once a manifest names it:

```bash
python -m adbc_driver_hardcharger.manifest          # for this user, pointing at the wheel's library
python -m adbc_driver_hardcharger.manifest --print  # or print it
```

`--dir DIR` installs it into another directory. Without Python, see the
[repository README](https://github.com/gizmodata/hardcharger-adbc#readme) for installing the
driver library and its manifest.

## 📄 License

Apache License 2.0; see [LICENSE](https://github.com/gizmodata/hardcharger-adbc/blob/main/LICENSE).
The driver library links the Go runtime and the Go modules listed, with their licenses, in
[THIRD_PARTY_NOTICES](https://github.com/gizmodata/hardcharger-adbc/blob/main/python/src/adbc_driver_hardcharger/THIRD_PARTY_NOTICES),
which ships in every wheel.

---

<p align="center">
  <a href="https://gizmodata.com"><img src="https://raw.githubusercontent.com/gizmodata/hardcharger-adbc/main/docs/assets/gizmodata-logo.png" alt="GizmoData™" width="56"></a><br>
  <sub>Built by <a href="https://gizmodata.com">GizmoData™</a></sub>
</p>

<p align="center"><sub>Hardcharger™ and GizmoData™ are trademarks of GizmoData LLC.</sub></p>
