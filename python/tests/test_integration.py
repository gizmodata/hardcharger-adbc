"""Integration tests against real hardcharger-server processes (see conftest.py)."""

from __future__ import annotations

import json
import os
import pathlib
import re
import subprocess
import sys
import time

import adbc_driver_manager
import adbc_driver_manager.dbapi
import pytest

from adbc_driver_hardcharger import dbapi as hardcharger
from adbc_driver_hardcharger import manifest
from conftest import USERNAME, make_certificates, start_server, stop_server

pytestmark = pytest.mark.integration


def chargewire_code(error: Exception) -> str | None:
    """The Chargewire error code the driver put in the error's details."""
    for key, value in getattr(error, "details", None) or []:
        if (key.decode() if isinstance(key, bytes) else key) == "chargewire.code":
            return value.decode() if isinstance(value, bytes) else value
    return None


def eventually(conn, sql: str, predicate, timeout: float = 30):
    """Instrumentation is written asynchronously: poll until ``predicate(value)``."""
    deadline = time.monotonic() + timeout
    value = None
    while time.monotonic() < deadline:
        with conn.cursor() as cur:
            cur.execute(operation=sql)
            row = cur.fetchone()
        value = None if row is None else row[0]
        if predicate(value):
            return value
        time.sleep(0.1)
    raise AssertionError(f"{sql} -> {value!r}")


# --- connecting ----------------------------------------------------------------------------


def test_select_through_a_hardcharger_uri(conn):
    with conn.cursor() as cur:
        cur.execute(operation="SELECT 42 AS answer, ? AS greeting", parameters=["hello"])
        assert cur.fetch_arrow_table().to_pylist() == [{"answer": 42, "greeting": "hello"}]


def test_the_server_reports_itself_as_hardcharger(conn):
    info = conn.adbc_get_info()
    assert info["vendor_name"] == "Hardcharger"
    assert info["driver_name"] == "ADBC Hardcharger Driver - Go"


def test_duckdb_settings_preserves_variant_storage_and_metadata(conn):
    # DuckDB 2.0 adds typed_value VARIANT. The C Data bridge used to export
    # its struct schema with zero array children, failing on the first batch.
    with conn.cursor() as cur:
        cur.execute(operation="SELECT * FROM duckdb_settings()")
        table = cur.fetch_arrow_table()
    assert table.num_rows > 100
    field = table.schema.field("typed_value")
    assert field.metadata[b"ARROW:extension:name"] == b"arrow.parquet.variant"
    assert [child.name for child in field.type] == ["metadata", "value"]
    rows = table.to_pylist()
    assert any(row["name"] == "access_mode" for row in rows)
    assert any(isinstance(row["typed_value"]["value"], bytes) for row in rows)


@pytest.mark.parametrize(
    "expression",
    ["42::VARIANT", "{'v': 42::VARIANT}", "[42::VARIANT, NULL::VARIANT]"],
)
def test_variant_results_round_trip_through_c_data(conn, expression):
    with conn.cursor() as cur:
        cur.execute(operation=f"SELECT {expression} AS v FROM range(7)")
        table = cur.fetch_record_batch().read_all()
    assert table.num_rows == 7
    # Reading the storage values exercises every child, including nested arrays.
    values = table.column("v").to_pylist()
    assert values[0] == values[-1]
    value = values[0]
    if isinstance(value, list):
        assert value[1] is None
        value = value[0]
    elif "v" in value:
        value = value["v"]
    assert value == {"metadata": b"\x01\x00\x00", "value": b"\x14\x2a\x00\x00\x00"}


def test_connection_helpers(conn):
    assert isinstance(conn, hardcharger.Connection)
    assert conn.current_user() == USERNAME
    # The plain semantic version, as hardcharger-server --version prints it ("0.2.0"); servers
    # built before the docs-findings fixes still answer "v0.2.0".
    assert re.fullmatch(r"v?\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?", conn.server_version()), (
        conn.server_version()
    )
    # The session's own id (the Chargewire one, s_..., from Hardcharger 0.4.0; a UUID before).
    assert re.fullmatch(r"s_[a-z2-7]{32}|[0-9a-f-]{36}", conn.current_session()), (
        conn.current_session()
    )


def test_wrong_password_is_refused(hc_server):
    with pytest.raises(expected_exception=adbc_driver_manager.ProgrammingError) as error:
        hardcharger.connect(uri=hc_server.uri, username=USERNAME, password="not-the-password")
    assert chargewire_code(error=error.value) == "invalid_credentials"
    assert "the user name or password is not correct" in str(error.value)


def test_right_password_twin(hc_server):
    with hardcharger.connect(
        uri=hc_server.uri, username=USERNAME, password=hc_server.password
    ) as conn:
        assert conn.current_user() == USERNAME


def test_chargewire_uri_works_too(hc_server):
    uri = f"chargewire://{hc_server.host}:{hc_server.port}?tls=false"
    with hardcharger.connect(uri=uri, username=USERNAME, password=hc_server.password) as conn:
        assert isinstance(conn, hardcharger.Connection)
        assert conn.current_user() == USERNAME


def test_plain_http_spelling_must_be_tls_false(hc_server):
    with pytest.raises(expected_exception=adbc_driver_manager.Error) as error:
        hardcharger.connect(
            uri=f"hardcharger+http://127.0.0.1:{hc_server.port}",
            username=USERNAME,
            password=hc_server.password,
        )
    assert error.value.status_code == adbc_driver_manager.AdbcStatusCode.INVALID_ARGUMENT
    assert "hardcharger://host:port?tls=false for plain HTTP" in str(error.value)


def test_ddl_dml_and_bulk_ingest(conn):
    import pyarrow as pa

    with conn.cursor() as cur:
        cur.execute(operation="CREATE OR REPLACE TABLE trips (id INTEGER, city VARCHAR)")
        assert cur.description is None
        rows = cur.adbc_ingest(
            table_name="trips",
            data=pa.table({"id": [1, 2], "city": ["Boston", "Denver"]}),
            mode="append",
        )
        assert rows == 2
        cur.execute(operation="SELECT city FROM trips ORDER BY id")
        assert cur.fetchall() == [("Boston",), ("Denver",)]


# --- TLS -----------------------------------------------------------------------------------


@pytest.fixture(scope="module")
def tls_server(tmp_path_factory):
    workdir = tmp_path_factory.mktemp("tls")
    certs = make_certificates(out_dir=workdir)
    server = start_server(
        workdir=workdir,
        extra_args=["--tls-cert", str(certs["cert"]), "--tls-key", str(certs["key"])],
    )
    server.certs = certs
    yield server
    stop_server(server=server)


def test_tls_is_the_default_and_verifies(tls_server):
    assert tls_server.uri == f"hardcharger://127.0.0.1:{tls_server.port}"
    with hardcharger.connect(
        uri=tls_server.uri,
        username=USERNAME,
        password=tls_server.password,
        tls_root_certs=tls_server.certs["ca"],
    ) as conn:
        assert conn.current_user() == USERNAME


def test_tls_refuses_an_untrusted_certificate(tls_server):
    with pytest.raises(expected_exception=adbc_driver_manager.Error) as error:
        hardcharger.connect(uri=tls_server.uri, username=USERNAME, password=tls_server.password)
    assert "certificate" in str(error.value)


# --- TLS server name (SNI) ----------------------------------------------------------------

SERVER_NAME = "hc-1.gizmodata.test"


class SNIRecorder:
    """sni_recorder.py in front of a TLS server whose certificate names SERVER_NAME alone (no
    IP address): the TLS server name each connection's ClientHello carried, read off the
    wire."""

    def __init__(self, server, process: subprocess.Popen, port: int, log: pathlib.Path):
        self.server = server
        self.process = process
        self.uri = f"hardcharger://127.0.0.1:{port}"
        self.log = log

    def take(self) -> list:
        """The server names recorded since the last call (None: no SNI)."""
        records = [json.loads(s=line) for line in self.log.read_text().splitlines()]
        self.log.write_text(data="")
        assert all("error" not in record for record in records), records
        return [record["server_name"] for record in records]


@pytest.fixture(scope="module")
def sni_recorder(tmp_path_factory):
    workdir = tmp_path_factory.mktemp("sni")
    certs = make_certificates(out_dir=workdir, server_dns_names=[SERVER_NAME])
    server = start_server(
        workdir=workdir,
        extra_args=["--tls-cert", str(certs["cert"]), "--tls-key", str(certs["key"])],
    )
    server.certs = certs
    log = workdir / "sni.jsonl"
    log.write_text(data="")
    process = subprocess.Popen(
        args=[
            sys.executable,
            str(pathlib.Path(__file__).with_name("sni_recorder.py")),
            str(server.port),
            str(log),
        ],
        stdout=subprocess.PIPE,
        text=True,
    )
    port = int(process.stdout.readline())
    yield SNIRecorder(server=server, process=process, port=port, log=log)
    process.kill()
    process.wait()
    stop_server(server=server)


def _named_connect(recorder: SNIRecorder, **kwargs):
    return hardcharger.connect(
        uri=recorder.uri,
        username=USERNAME,
        password=recorder.server.password,
        tls_root_certs=recorder.server.certs["ca"],
        **kwargs,
    )


def test_tls_server_name_is_the_sni_and_the_verified_name(sni_recorder):
    # Connected by IP address: the name goes into every ClientHello (result partitions'
    # connections too) and the certificate, issued to the name alone, is accepted.
    sni_recorder.take()
    with _named_connect(recorder=sni_recorder, tls_server_name=SERVER_NAME) as conn:
        assert conn.current_user() == USERNAME
        with conn.cursor() as cur:
            cur.execute(operation="SELECT i FROM range(2000000) t(i) ORDER BY i")
            assert cur.fetch_arrow_table().num_rows == 2000000
    names = sni_recorder.take()
    # More than one: the split result's partitions came over connections of their own.
    assert len(names) > 1 and set(names) == {SERVER_NAME}, names


def test_without_tls_server_name_an_ip_sends_no_sni_and_fails_verification(sni_recorder):
    sni_recorder.take()
    with pytest.raises(expected_exception=adbc_driver_manager.Error) as error:
        _named_connect(recorder=sni_recorder)
    assert error.value.status_code == adbc_driver_manager.AdbcStatusCode.IO
    assert "cannot validate certificate for 127.0.0.1 because it doesn't contain any IP SANs" in (
        str(error.value)
    )
    names = sni_recorder.take()
    assert names and set(names) == {None}, names  # an IP literal is never an SNI name


def test_a_tls_server_name_the_certificate_lacks_fails_verification(sni_recorder):
    sni_recorder.take()
    with pytest.raises(expected_exception=adbc_driver_manager.Error) as error:
        _named_connect(recorder=sni_recorder, tls_server_name="other.gizmodata.test")
    assert error.value.status_code == adbc_driver_manager.AdbcStatusCode.IO
    assert f"certificate is valid for {SERVER_NAME}, not other.gizmodata.test" in str(error.value)
    names = sni_recorder.take()
    assert names and set(names) == {"other.gizmodata.test"}, names


# --- token exchange (subject_token) --------------------------------------------------------

EXCHANGE_ISSUER = "https://idp.gizmodata.test"
EXCHANGE_AUDIENCE = "hardcharger-adbc-exchange"


@pytest.fixture(scope="module")
def exchange_server(tmp_path_factory):
    """A server that signs in holders of RS256 tokens from EXCHANGE_ISSUER, checked with the
    issuer's public key (``--token-signature-verify-cert-path``)."""
    from cryptography.hazmat.primitives import serialization
    from cryptography.hazmat.primitives.asymmetric import rsa

    workdir = tmp_path_factory.mktemp("exchange")
    key = rsa.generate_private_key(public_exponent=65537, key_size=2048)
    public = workdir / "idp-public.pem"
    public.write_bytes(
        data=key.public_key().public_bytes(
            encoding=serialization.Encoding.PEM,
            format=serialization.PublicFormat.SubjectPublicKeyInfo,
        )
    )
    exchange_args = [
        "--token-signature-verify-cert-path",
        str(public),
        "--token-allowed-issuer",
        EXCHANGE_ISSUER,
        "--token-allowed-audience",
        EXCHANGE_AUDIENCE,
        "--token-default-role",
        "admin",
        "--token-authorized-emails",
        "*@gizmodata.com",
    ]
    server = start_server(workdir=workdir, extra_args=exchange_args)
    server.idp_key = key
    server.exchange_args = exchange_args
    yield server
    stop_server(server=server)


def _idp_token(server, email: str, issuer: str = EXCHANGE_ISSUER) -> str:
    import datetime

    import jwt

    now = datetime.datetime.now(tz=datetime.timezone.utc)
    return jwt.encode(
        payload={
            "iss": issuer,
            "aud": EXCHANGE_AUDIENCE,
            "sub": email,
            "email": email,
            "iat": now,
            "exp": now + datetime.timedelta(minutes=5),
        },
        key=server.idp_key,
        algorithm="RS256",
    )


def test_subject_token_signs_in_by_token_exchange(exchange_server):
    token = _idp_token(server=exchange_server, email="analyst@gizmodata.com")
    with hardcharger.connect(uri=exchange_server.uri, subject_token=token) as conn:
        assert conn.current_user() == "analyst@gizmodata.com"
        with conn.cursor() as cur:
            cur.execute(operation="SELECT auth_method FROM hardcharger_sessions() WHERE is_current")
            assert cur.fetchone() == ("token",)


def test_subject_token_renews_the_access_token(tmp_path, exchange_server):
    # The driver exchanges the subject token again when Hardcharger's access token runs out.
    server = start_server(
        workdir=tmp_path,
        extra_args=[
            "--token-lifetime-seconds",
            "2",
            *exchange_server.exchange_args,
        ],
    )
    server.idp_key = exchange_server.idp_key
    try:
        token = _idp_token(server=server, email="analyst@gizmodata.com")
        with hardcharger.connect(uri=server.uri, subject_token=token) as conn:
            for _ in range(2):
                time.sleep(2.5)
                assert conn.current_user() == "analyst@gizmodata.com"
    finally:
        stop_server(server=server)


def test_subject_token_from_another_issuer_is_refused(exchange_server):
    token = _idp_token(
        server=exchange_server, email="analyst@gizmodata.com", issuer="https://evil.example"
    )
    with pytest.raises(expected_exception=adbc_driver_manager.Error) as error:
        hardcharger.connect(uri=exchange_server.uri, subject_token=token)
    assert error.value.status_code == adbc_driver_manager.AdbcStatusCode.UNAUTHENTICATED
    assert chargewire_code(error=error.value) == "invalid_credentials"
    assert "the sign-in token is not accepted" in str(error.value)


# --- attaching to a session ------------------------------------------------------------------


def access_token(server, username: str = USERNAME, password: str | None = None) -> str:
    """An access token from the server's token endpoint (the password grant), as a program
    that hands it to several processes would get one."""
    import urllib.request

    request = urllib.request.Request(
        url=f"{server.base_url}/chargewire/v1/auth/token",
        data=json.dumps(
            obj={
                "grant_type": "password",
                "username": username,
                "password": server.password if password is None else password,
            }
        ).encode(),
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    with urllib.request.urlopen(url=request, timeout=10) as response:
        return json.loads(s=response.read())["access_token"]


def _count(conn, sql: str) -> int:
    with conn.cursor() as cur:
        cur.execute(operation=sql)
        return cur.fetchone()[0]


def test_a_transaction_moves_to_another_process(hc_server):
    token = access_token(server=hc_server)
    with hardcharger.connect(uri=hc_server.uri, access_token=token) as conn:
        with conn.cursor() as cur:
            cur.execute(operation="CREATE OR REPLACE TABLE handoff (i INTEGER)")
    # Process 1 begins, inserts, and exits leaving the session (and the transaction) open.
    first = subprocess.run(
        args=[
            sys.executable,
            str(pathlib.Path(__file__).with_name("session_handoff.py")),
            hc_server.uri,
            "HANDOFF_TEST_TOKEN",
        ],
        env={**os.environ, "HANDOFF_TEST_TOKEN": token},
        capture_output=True,
        text=True,
        timeout=60,
    )
    assert first.returncode == 0, first.stderr
    session_id = first.stdout.strip()
    with hardcharger.connect(uri=hc_server.uri, access_token=token) as outside:
        assert _count(conn=outside, sql="SELECT count(*) FROM handoff") == 0
        # Process 2 (this one) attaches: it sees the uncommitted row, and commits it.
        with hardcharger.connect(
            uri=hc_server.uri, access_token=token, session_id=session_id
        ) as attached:
            assert attached.session_id == session_id
            assert attached.adbc_connection.get_option(
                key="adbc.chargewire.session.transaction"
            ) == ("open")
            assert _count(conn=attached, sql="SELECT count(*) FROM handoff") == 1
            with attached.cursor() as cur:
                cur.execute(operation="COMMIT")
        assert _count(conn=outside, sql="SELECT count(*) FROM handoff") == 1


def test_attaching_signs_in_as_the_sessions_user_with_a_password_too(hc_server):
    # The session belongs to the user, not to the token: a password sign-in attaches too.
    with hardcharger.connect(
        uri=hc_server.uri,
        username=USERNAME,
        password=hc_server.password,
        close_session_on_release=False,
    ) as first:
        session_id = first.session_id
    with hardcharger.connect(
        uri=hc_server.uri,
        username=USERNAME,
        password=hc_server.password,
        session_id=session_id,
        close_session_on_release=True,
    ) as attached:
        assert attached.session_id == session_id


def _attach_error(server, session_id: str, **credentials) -> adbc_driver_manager.Error:
    with pytest.raises(expected_exception=adbc_driver_manager.Error) as error:
        hardcharger.connect(uri=server.uri, session_id=session_id, **credentials)
    return error.value


def test_attaching_to_a_missing_session_is_session_not_found(hc_server):
    error = _attach_error(
        server=hc_server,
        session_id="s_does_not_exist",
        username=USERNAME,
        password=hc_server.password,
    )
    assert error.status_code == adbc_driver_manager.AdbcStatusCode.NOT_FOUND
    assert chargewire_code(error=error) == "session_not_found"
    assert "attaching to session s_does_not_exist" in str(error)


def test_close_session_on_release(hc_server):
    credentials = {"username": USERNAME, "password": hc_server.password}
    # A created session ends when its connection closes...
    with hardcharger.connect(uri=hc_server.uri, **credentials) as conn:
        created = conn.session_id
        assert conn.adbc_connection.get_option(key="adbc.chargewire.session.close_on_release") == (
            "true"
        )
    error = _attach_error(server=hc_server, session_id=created, **credentials)
    assert chargewire_code(error=error) == "session_not_found"
    # ... unless told otherwise; an attached one stays when its connection closes...
    with hardcharger.connect(uri=hc_server.uri, close_session_on_release=False, **credentials) as c:
        kept = c.session_id
    for _ in range(2):
        with hardcharger.connect(uri=hc_server.uri, session_id=kept, **credentials) as attached:
            assert attached.adbc_connection.get_option(
                key="adbc.chargewire.session.close_on_release"
            ) == ("false")
            assert attached.session_id == kept
    # ... until a connection that may end it closes.
    with hardcharger.connect(uri=hc_server.uri, session_id=kept, **credentials) as attached:
        attached.adbc_connection.set_options(**{"adbc.chargewire.session.close_on_release": "true"})
    error = _attach_error(server=hc_server, session_id=kept, **credentials)
    assert chargewire_code(error=error) == "session_not_found"


def _bearer_status(server, token: str, session_id: str) -> int:
    """The HTTP status of reading the session with ``token`` as a raw bearer token."""
    import urllib.error
    import urllib.request

    request = urllib.request.Request(
        url=f"{server.base_url}/chargewire/v1/sessions/{session_id}",
        headers={"Authorization": f"Bearer {token}"},
    )
    try:
        with urllib.request.urlopen(url=request, timeout=10) as response:
            return response.status
    except urllib.error.HTTPError as error:
        return error.code


@pytest.fixture(scope="module")
def lifetime_server(tmp_path_factory):
    server = start_server(
        workdir=tmp_path_factory.mktemp("lifetime"),
        extra_args=["--token-lifetime-seconds", "600"],
    )
    yield server
    stop_server(server=server)


def test_the_access_token_is_readable_and_works(lifetime_server):
    import datetime

    with hardcharger.connect(
        uri=lifetime_server.uri, username=USERNAME, password=lifetime_server.password
    ) as conn:
        token = conn.access_token
        assert token
        assert _bearer_status(server=lifetime_server, token=token, session_id=conn.session_id) == (
            200
        )
        # The twin: a token the server didn't issue is refused.
        assert (
            _bearer_status(server=lifetime_server, token=token + "x", session_id=conn.session_id)
            == 401
        )
        expires_at = conn.access_token_expires_at
        assert expires_at.tzinfo is not None and expires_at.utcoffset() == datetime.timedelta(
            seconds=0
        )
        left = expires_at - datetime.datetime.now(tz=datetime.timezone.utc)
        assert datetime.timedelta(seconds=590) < left <= datetime.timedelta(seconds=601), left
        text = conn.adbc_connection.get_option(key="adbc.chargewire.auth.access_token_expires_at")
        assert text.endswith("Z") and datetime.datetime.fromisoformat(text) == expires_at


def test_the_access_token_after_a_renewal(tmp_path):
    server = start_server(workdir=tmp_path, extra_args=["--token-lifetime-seconds", "2"])
    try:
        with hardcharger.connect(
            uri=server.uri, username=USERNAME, password=server.password
        ) as conn:
            first = conn.access_token
            time.sleep(2.5)
            second = conn.access_token
            assert second and second != first
            assert _bearer_status(server=server, token=second, session_id=conn.session_id) == 200
            assert conn.current_user() == USERNAME
    finally:
        stop_server(server=server)


def test_a_callers_own_access_token_reads_back_as_given(lifetime_server):
    token = access_token(server=lifetime_server)
    with hardcharger.connect(uri=lifetime_server.uri, access_token=token) as conn:
        assert conn.access_token == token
        # Hardcharger's tokens are JWTs: the expiry comes from their exp claim.
        assert conn.access_token_expires_at is not None


def test_a_session_and_its_token_go_to_the_next_connection(hc_server):
    with hardcharger.connect(
        uri=hc_server.uri, username=USERNAME, password=hc_server.password
    ) as c:
        with c.cursor() as cur:
            cur.execute(operation="CREATE OR REPLACE TABLE token_handoff (i INTEGER)")
    with hardcharger.connect(
        uri=hc_server.uri,
        username=USERNAME,
        password=hc_server.password,
        close_session_on_release=False,
    ) as first:
        with first.cursor() as cur:
            cur.execute(operation="BEGIN TRANSACTION")
            cur.execute(operation="INSERT INTO token_handoff VALUES (1)")
        session_id, token = first.session_id, first.access_token
    with hardcharger.connect(
        uri=hc_server.uri, access_token=token, session_id=session_id
    ) as attached:
        assert _count(conn=attached, sql="SELECT count(*) FROM token_handoff") == 1
        with attached.cursor() as cur:
            cur.execute(operation="COMMIT")


@pytest.fixture(scope="module")
def rbac_server(tmp_path_factory):
    """A server under access control (a DuckDB-file store) with a second user, bob."""
    workdir = tmp_path_factory.mktemp("rbac")
    server = start_server(
        workdir=workdir,
        extra_args=["--rbac-store-uri", f"duckdb:{(workdir / 'rbac.duckdb').as_posix()}"],
    )
    server.bob_password = "bob-" + server.password
    try:
        with hardcharger.connect(
            uri=server.uri, username=USERNAME, password=server.password
        ) as conn:
            with conn.cursor() as cur:
                cur.execute(operation=f"CREATE USER bob WITH PASSWORD '{server.bob_password}'")
    except Exception:
        stop_server(server=server)
        raise
    yield server
    stop_server(server=server)


def test_another_users_session_is_refused(rbac_server):
    with hardcharger.connect(
        uri=rbac_server.uri,
        username=USERNAME,
        password=rbac_server.password,
        close_session_on_release=False,
    ) as admin:
        admins = admin.session_id
    error = _attach_error(
        server=rbac_server, session_id=admins, username="bob", password=rbac_server.bob_password
    )
    # The server answers another user's session as it answers a missing one (§9.2).
    assert error.status_code == adbc_driver_manager.AdbcStatusCode.NOT_FOUND
    assert chargewire_code(error=error) == "session_not_found"
    # The twin: the session's own user attaches.
    with hardcharger.connect(
        uri=rbac_server.uri,
        username=USERNAME,
        password=rbac_server.password,
        session_id=admins,
        close_session_on_release=True,
    ) as attached:
        assert attached.session_id == admins


# --- client certificate sign-in (mutual TLS) -----------------------------------------------

#: The administrator's identity as the server is configured with it; the certificate says
#: ops@example.com (an e-mail identity matches ignoring case).
OPS_IDENTITY = "OPS@example.com"


@pytest.fixture(scope="module")
def mtls_certs(tmp_path_factory):
    return make_certificates(
        out_dir=tmp_path_factory.mktemp("mtls-certs"),
        clients={"ops": "ops@example.com", "stranger": "stranger@example.com"},
    )


def _mtls_args(certs) -> list[str]:
    return [
        "--tls-cert",
        str(certs["cert"]),
        "--tls-key",
        str(certs["key"]),
        "--mtls-ca-cert",
        str(certs["ca"]),
    ]


@pytest.fixture(scope="module")
def mtls_server(tmp_path_factory, mtls_certs):
    server = start_server(
        workdir=tmp_path_factory.mktemp("mtls"),
        extra_args=[
            *_mtls_args(certs=mtls_certs),
            "--mtls-certificate-sign-in",
            "--mtls-admin-identities",
            OPS_IDENTITY,
        ],
    )
    yield server
    stop_server(server=server)


def _certificate_connect(server, certs, client: str, auth_type: str = "client_certificate"):
    return hardcharger.connect(
        uri=server.uri,
        auth_type=auth_type,
        tls_root_certs=certs["ca"],
        tls_client_cert=certs[client],
        tls_client_key=certs[client + "-key"],
    )


@pytest.mark.parametrize(argnames="auth_type", argvalues=["client_certificate", "mtls"])
def test_client_certificate_sign_in(mtls_server, mtls_certs, auth_type):
    with _certificate_connect(
        server=mtls_server, certs=mtls_certs, client="ops", auth_type=auth_type
    ) as conn:
        assert conn.current_user() == OPS_IDENTITY
        with conn.cursor() as cur:
            cur.execute(operation="SELECT auth_method FROM hardcharger_sessions() WHERE is_current")
            assert cur.fetchone() == ("client_certificate",)


def test_client_certificate_split_result_and_partitioned_ingest(mtls_server, mtls_certs):
    # Partitions and ingest parts go over connections of their own, each presenting the
    # certificate the token is bound to.
    import pyarrow

    with _certificate_connect(server=mtls_server, certs=mtls_certs, client="ops") as conn:
        with conn.cursor() as cur:
            cur.execute(operation="SELECT i FROM range(2000000) t(i) ORDER BY i")
            assert cur.fetch_arrow_table().column(i=0).to_pylist() == list(range(2000000))
            table = pyarrow.table(data={"k": list(range(100000))})
            cur.adbc_ingest(
                table_name="certificate_parts",
                data=table.to_batches(max_chunksize=25000),
                mode="create",
            )
            cur.execute(
                operation="SELECT count(*), count(DISTINCT k), sum(k) FROM certificate_parts"
            )
            assert cur.fetchone() == (100000, 100000, 4999950000)


def test_client_certificate_unknown_identity_is_refused(mtls_server, mtls_certs):
    with pytest.raises(expected_exception=adbc_driver_manager.Error) as error:
        _certificate_connect(server=mtls_server, certs=mtls_certs, client="stranger")
    assert error.value.status_code == adbc_driver_manager.AdbcStatusCode.UNAUTHENTICATED
    assert chargewire_code(error=error.value) == "unknown_certificate_identity"
    assert "stranger@example.com" in str(error.value)


def test_client_certificate_with_a_password_is_refused(mtls_server, mtls_certs):
    with pytest.raises(expected_exception=adbc_driver_manager.Error) as error:
        hardcharger.connect(
            uri=mtls_server.uri,
            auth_type="client_certificate",
            username=USERNAME,
            password=mtls_server.password,
            tls_root_certs=mtls_certs["ca"],
            tls_client_cert=mtls_certs["ops"],
            tls_client_key=mtls_certs["ops-key"],
        )
    assert error.value.status_code == adbc_driver_manager.AdbcStatusCode.INVALID_ARGUMENT
    assert "signs in with the client certificate alone" in str(error.value)


def test_client_certificate_sign_in_off_on_the_server(tmp_path, mtls_certs):
    # --mtls-ca-cert alone: the certificate only admits the connection.
    server = start_server(workdir=tmp_path, extra_args=_mtls_args(certs=mtls_certs))
    try:
        with pytest.raises(expected_exception=adbc_driver_manager.Error) as error:
            _certificate_connect(server=server, certs=mtls_certs, client="ops")
        assert error.value.status_code == adbc_driver_manager.AdbcStatusCode.UNAUTHENTICATED
        assert "capability mtls_identity" in str(error.value)
        # The twin: a password sign-in over the same certificate.
        with hardcharger.connect(
            uri=server.uri,
            username=USERNAME,
            password=server.password,
            tls_root_certs=mtls_certs["ca"],
            tls_client_cert=mtls_certs["ops"],
            tls_client_key=mtls_certs["ops-key"],
        ) as conn:
            assert conn.current_user() == USERNAME
    finally:
        stop_server(server=server)


def test_no_client_certificate_is_certificate_required(mtls_server, mtls_certs):
    # The server refuses the handshake (TLS alert certificate_required): that is the refusal
    # certificate_required, not an unreachable server. The twin, a password sign-in over a
    # certificate, is test_client_certificate_sign_in_off_on_the_server.
    with pytest.raises(expected_exception=adbc_driver_manager.Error) as error:
        hardcharger.connect(
            uri=mtls_server.uri,
            username=USERNAME,
            password=mtls_server.password,
            tls_root_certs=mtls_certs["ca"],
        )
    assert error.value.status_code == adbc_driver_manager.AdbcStatusCode.UNAUTHENTICATED
    assert chargewire_code(error=error.value) == "certificate_required"


# --- sessions ------------------------------------------------------------------------------


def test_session_transaction_is_the_servers(conn):
    # The server's view of the session's transaction, which sees SQL BEGIN and ROLLBACK.
    def transaction() -> str:
        return conn.adbc_connection.get_option(
            key=hardcharger.ConnectionOptions.SESSION_TRANSACTION.value
        )

    assert transaction() == "none"
    with conn.cursor() as cur:
        cur.execute(operation="BEGIN TRANSACTION")
        assert transaction() == "open"
        cur.execute(operation="ROLLBACK")
    assert transaction() == "none"


# --- settings ------------------------------------------------------------------------------


def test_session_setting_set_get_reset(conn):
    assert conn.get_setting(name="query_timeout") == "0"
    conn.set_setting(name="query_timeout", value=30)
    assert conn.get_setting(name="hardcharger.query_timeout") == "30"
    conn.reset_setting(name="query_timeout")
    assert conn.get_setting(name="query_timeout") == "0"


def test_invalid_setting_value_is_refused(conn):
    with pytest.raises(expected_exception=adbc_driver_manager.ProgrammingError) as error:
        conn.set_setting(name="query_timeout", value="soon")
    assert "Invalid value for query_timeout: 'soon' is not a whole number" in str(error.value)
    assert chargewire_code(error=error.value) == "sql_error"


def test_valid_setting_value_twin(conn):
    conn.set_setting(name="query_timeout", value="45")
    assert conn.get_setting(name="query_timeout") == "45"


def test_unknown_setting_is_refused(conn):
    with pytest.raises(
        expected_exception=adbc_driver_manager.ProgrammingError,
        match="Unknown Hardcharger setting: hardcharger.no_such_setting",
    ):
        conn.set_setting(name="no_such_setting", value=1)
    with pytest.raises(expected_exception=KeyError, match="hardcharger.no_such_setting"):
        conn.get_setting(name="no_such_setting")


def test_global_setting_needs_global_scope(conn):
    with pytest.raises(
        expected_exception=adbc_driver_manager.ProgrammingError,
        match="hardcharger.graceful_shutdown can only be set with SET GLOBAL",
    ):
        conn.set_setting(name="graceful_shutdown", value=False)


def test_global_setting_with_global_scope_twin(conn):
    try:
        conn.set_setting(name="graceful_shutdown", value=False, scope="global")
        assert conn.get_setting(name="graceful_shutdown") == "false"
    finally:
        conn.reset_setting(name="graceful_shutdown", scope="global")
    assert conn.get_setting(name="graceful_shutdown") == "true"


def test_settings_table(conn):
    table = conn.settings()
    names = table.column("name").to_pylist()  # column() takes no keywords
    assert "hardcharger.query_tag" in names and "hardcharger.query_timeout" in names


def test_connect_applies_settings(hc_server):
    with hardcharger.connect(
        uri=hc_server.uri,
        username=USERNAME,
        password=hc_server.password,
        settings={"query_timeout": 12, "bypass_queue": True},
    ) as conn:
        assert conn.get_setting(name="query_timeout") == "12"
        assert conn.get_setting(name="bypass_queue") == "true"


def test_connect_with_a_bad_setting_fails_and_leaves_no_session(hc_server):
    def sessions() -> int:
        with hardcharger.connect(
            uri=hc_server.uri, username=USERNAME, password=hc_server.password
        ) as probe:
            with probe.cursor() as cur:
                cur.execute(operation="SELECT count(*) FROM hardcharger_sessions()")
                return cur.fetchone()[0]

    before = sessions()
    with pytest.raises(
        expected_exception=adbc_driver_manager.ProgrammingError,
        match="'soon' is not a whole number",
    ):
        hardcharger.connect(
            uri=hc_server.uri,
            username=USERNAME,
            password=hc_server.password,
            settings={"query_timeout": "soon"},
        )
    # The failed connection's session is closed, not left to time out.
    assert (
        eventually(
            conn=_admin(hc_server),
            sql="SELECT count(*) FROM hardcharger_sessions()",
            predicate=lambda n: n <= before,
            timeout=10,
        )
        <= before
    )


def _admin(server):
    return hardcharger.connect(uri=server.uri, username=USERNAME, password=server.password)


# --- query and session tags ----------------------------------------------------------------


@pytest.fixture(scope="module")
def instrumented_server(tmp_path_factory):
    server = start_server(
        workdir=tmp_path_factory.mktemp("instr"), extra_args=["--enable-instrumentation"]
    )
    yield server
    stop_server(server=server)


def test_query_and_session_tags_are_recorded(instrumented_server):
    server = instrumented_server
    with hardcharger.connect(
        uri=server.uri,
        username=USERNAME,
        password=server.password,
        session_tag={"app": "hardcharger-adbc-tests"},
        query_tag={"job": "nightly-load"},
    ) as conn:
        with conn.cursor() as cur:
            cur.execute(operation="SELECT 1 AS tagged_by_connect")
            cur.fetchall()
        session = conn.current_session()
        tag = eventually(
            conn=conn,
            predicate=lambda v: v is not None,
            sql=(
                "SELECT query_tag->>'job' FROM _hardcharger_instr.sql_statements "
                "WHERE sql_text LIKE '%tagged_by_connect%' AND sql_text NOT LIKE '%sql_statements%'"
            ),
        )
        assert tag == "nightly-load"
        app = eventually(
            conn=conn,
            predicate=lambda v: v is not None,
            sql=(
                "SELECT session_tag->>'app' FROM _hardcharger_instr.sessions "
                f"WHERE session_id = '{session}'"
            ),
        )
        assert app == "hardcharger-adbc-tests"

        conn.set_query_tag(tag={"job": "second"})
        with conn.cursor() as cur:
            cur.execute(operation="SELECT 2 AS tagged_second")
            cur.fetchall()
        assert (
            eventually(
                conn=conn,
                predicate=lambda v: v is not None,
                sql=(
                    "SELECT query_tag->>'job' FROM _hardcharger_instr.sql_statements "
                    "WHERE sql_text LIKE '%tagged_second%' AND sql_text NOT LIKE '%sql_statements%'"
                ),
            )
            == "second"
        )

        conn.set_query_tag(tag=None)
        assert conn.get_setting(name="query_tag") == ""
        with conn.cursor() as cur:
            cur.execute(operation="SELECT 3 AS untagged_third")
            cur.fetchall()
        assert (
            eventually(
                conn=conn,
                predicate=lambda v: v is not None,
                sql=(
                    "SELECT CAST(query_tag IS NULL AS VARCHAR) "
                    "FROM _hardcharger_instr.sql_statements WHERE sql_text LIKE '%untagged_third%' "
                    "AND sql_text NOT LIKE '%sql_statements%'"
                ),
            )
            == "true"
        )


def test_a_tag_that_is_not_json_is_refused(conn):
    with pytest.raises(
        expected_exception=adbc_driver_manager.ProgrammingError, match="Invalid JSON for query_tag"
    ):
        conn.set_query_tag(tag="not json")


def test_a_json_text_tag_twin(conn):
    conn.set_query_tag(tag='{"source": "text"}')
    assert conn.get_setting(name="query_tag") == '{"source": "text"}'


# --- browser sign-in -----------------------------------------------------------------------

AUDIENCE = "hardcharger-adbc-test"
CLIENT_ID = "hardcharger-adbc-client"
CLIENT_SECRET = "hardcharger-adbc-secret"


class IdP:
    """The mock identity provider, running in its own process (see mock_idp.py)."""

    email = "analyst@gizmodata.com"

    def __init__(self, issuer: str, process: subprocess.Popen):
        self.issuer = issuer
        self.process = process


@pytest.fixture(scope="module")
def idp():
    process = subprocess.Popen(
        args=[
            sys.executable,
            str(pathlib.Path(__file__).with_name("mock_idp.py")),
            AUDIENCE,
            CLIENT_ID,
            CLIENT_SECRET,
        ],
        stdout=subprocess.PIPE,
        text=True,
    )
    issuer = process.stdout.readline().strip()
    if not issuer.startswith("http://127.0.0.1:"):
        process.kill()
        pytest.fail(reason=f"the mock identity provider did not start: {issuer!r}")
    yield IdP(issuer=issuer, process=process)
    process.kill()
    process.wait()


@pytest.fixture(scope="module")
def oauth_server(tmp_path_factory, idp):
    server = start_server(
        workdir=tmp_path_factory.mktemp("oauth"),
        extra_args=[
            "--token-allowed-issuer",
            idp.issuer,
            "--token-allowed-audience",
            AUDIENCE,
            "--token-default-role",
            "admin",
            "--token-authorized-emails",
            "*@gizmodata.com",
            "--oauth-client-id",
            CLIENT_ID,
            "--oauth-client-secret",
            CLIENT_SECRET,
            # The redirect URI on the server's own port, dialled at 127.0.0.1.
            "--oauth-base-url",
            "{base_url}",
        ],
    )
    yield server
    stop_server(server=server)


FAKE_BROWSER = f"{sys.executable} {pathlib.Path(__file__).with_name('fake_browser.py')}"


def browser(code: str) -> str:
    """The fake browser command: the identity provider sends it back with ``code``."""
    return f"{FAKE_BROWSER} {code}"


def test_browser_sign_in(oauth_server, idp):
    with hardcharger.connect(
        uri=oauth_server.uri,
        auth_type="external",
        open_browser=browser(code="code-ok"),
        oauth_timeout=30,
    ) as conn:
        assert conn.current_user() == idp.email
        with conn.cursor() as cur:
            cur.execute(operation="SELECT 1 AS one")
            assert cur.fetchall() == [(1,)]


def test_browser_sign_in_with_a_rejected_code(oauth_server, idp):
    with pytest.raises(expected_exception=adbc_driver_manager.Error) as error:
        hardcharger.connect(
            uri=oauth_server.uri,
            auth_type="external",
            open_browser=browser(code="code-bad"),
            oauth_timeout=30,
        )
    assert error.value.status_code == adbc_driver_manager.AdbcStatusCode.UNAUTHENTICATED
    assert "browser sign-in" in str(error.value)
    assert "invalid_grant" in str(error.value)
    assert "identity_provider_error" in str(error.value)


def test_browser_sign_in_needs_the_servers_capability(hc_server):
    """A server without browser sign-in: refused before any flow, saying why."""
    with pytest.raises(expected_exception=adbc_driver_manager.Error) as error:
        hardcharger.connect(uri=hc_server.uri, auth_type="external", open_browser=False)
    assert error.value.status_code == adbc_driver_manager.AdbcStatusCode.UNAUTHENTICATED
    assert "does not offer browser sign-in" in str(error.value)


class SignInCounter:
    """A fake browser that counts its sign-ins."""

    def __init__(self, path: pathlib.Path, code: str = "code-ok"):
        self.path = path
        self.command = f"{FAKE_BROWSER} --count {path} {code}"

    @property
    def count(self) -> int:
        return len(self.path.read_text().splitlines()) if self.path.exists() else 0


def _shared_connect(server, counter: SignInCounter, share_sign_in):
    return hardcharger.connect(
        uri=server.uri,
        auth_type="external",
        open_browser=counter.command,
        oauth_timeout=30,
        share_sign_in=share_sign_in,
    )


def test_shared_browser_sign_in_signs_in_once(oauth_server, idp, tmp_path):
    """dbt's pattern: a connection (a database of its own) after another, each closed before
    the next opens; they share one sign-in. The key is the test's own: the driver's sign-ins
    live as long as the process, which runs the other tests too."""
    counter = SignInCounter(path=tmp_path / "count")
    for _ in range(5):
        with _shared_connect(server=oauth_server, counter=counter, share_sign_in=True) as conn:
            assert conn.current_user() == idp.email
    assert counter.count == 1


def test_shared_browser_sign_in_from_threads(oauth_server, idp, tmp_path):
    import concurrent.futures

    counter = SignInCounter(path=tmp_path / "count")

    def work(_):
        with _shared_connect(server=oauth_server, counter=counter, share_sign_in="threads") as conn:
            return conn.current_user()

    with concurrent.futures.ThreadPoolExecutor(max_workers=4) as pool:
        users = list(pool.map(work, range(6)))
    assert users == [idp.email] * 6
    assert counter.count == 1


def test_shared_browser_sign_in_is_per_key(oauth_server, idp, tmp_path):
    counter = SignInCounter(path=tmp_path / "count")
    for key in ("per-key-a", "per-key-a", "per-key-b", "per-key-b"):
        with _shared_connect(server=oauth_server, counter=counter, share_sign_in=key) as conn:
            assert conn.current_user() == idp.email
    assert counter.count == 2


def test_unshared_browser_sign_in_twin(oauth_server, idp, tmp_path):
    """Without share_sign_in, every connection signs in on its own."""
    counter = SignInCounter(path=tmp_path / "count")
    for _ in range(3):
        with _shared_connect(server=oauth_server, counter=counter, share_sign_in=False) as conn:
            assert conn.current_user() == idp.email
    assert counter.count == 3


# --- the ADBC driver manifest: other languages load the same library by name ----------------


def test_manifest_loads_the_driver_by_name_with_a_hardcharger_uri(hc_server, tmp_path, monkeypatch):
    manifest.install(directory=tmp_path)
    monkeypatch.setenv(name="ADBC_DRIVER_PATH", value=str(tmp_path))
    with adbc_driver_manager.dbapi.connect(
        driver="hardcharger",
        db_kwargs={
            "uri": f"hardcharger://127.0.0.1:{hc_server.port}?tls=false",
            "username": USERNAME,
            "password": hc_server.password,
            "adbc.hardcharger.query_tag": '{"via": "manifest"}',
        },
        autocommit=True,
    ) as conn:
        with conn.cursor() as cur:
            cur.execute("SELECT HARDCHARGER_USER()")
            assert cur.fetchone() == (USERNAME,)
        assert conn.adbc_get_info()["driver_name"] == "ADBC Hardcharger Driver - Go"
        assert conn.adbc_connection.get_option(key="adbc.hardcharger.query_tag") == (
            '{"via": "manifest"}'
        )


@pytest.mark.parametrize("form", ["name", "uri", "path", "manager"])
def test_standard_connection_profile(hc_server, tmp_path, monkeypatch, form):
    """A shared profile names the driver, not the Python wheel's absolute library path."""
    drivers = tmp_path / "drivers"
    profiles = tmp_path / "profiles"
    profiles.mkdir()
    manifest.install(directory=drivers)
    monkeypatch.setenv("ADBC_DRIVER_PATH", str(drivers))
    monkeypatch.setenv("ADBC_PROFILE_PATH", str(profiles))
    monkeypatch.setenv("HC_PROFILE_PASSWORD", hc_server.password)
    profile = profiles / "hardcharger_test.toml"
    profile.write_text(
        'profile_version = 1\ndriver = "hardcharger"\n\n[Options]\n'
        f'uri = "hardcharger://127.0.0.1:{hc_server.port}?tls=false"\n'
        f'username = "{USERNAME}"\n'
        'password = "{{ env_var(HC_PROFILE_PASSWORD) }}"\n',
        encoding="utf-8",
    )
    if form == "manager":
        connection = adbc_driver_manager.dbapi.connect(profile="hardcharger_test")
    elif form == "uri":
        connection = hardcharger.connect(uri="profile://hardcharger_test")
    elif form == "path":
        connection = hardcharger.connect(profile=str(profile))
    else:
        # Explicit arguments override the profile's options.
        monkeypatch.setenv("HC_PROFILE_PASSWORD", "incorrect-profile-password")
        connection = hardcharger.connect(profile="hardcharger_test", password=hc_server.password)
    with connection as conn, conn.cursor() as cur:
        cur.execute("SELECT 42 AS answer")
        assert cur.fetchone() == (42,)
        assert conn.adbc_get_info()["driver_name"] == "ADBC Hardcharger Driver - Go"


def test_manifest_refuses_a_bad_scheme(hc_server, tmp_path, monkeypatch):
    manifest.install(directory=tmp_path)
    monkeypatch.setenv(name="ADBC_DRIVER_PATH", value=str(tmp_path))
    with pytest.raises(
        expected_exception=adbc_driver_manager.Error, match=r"\?tls=false for plain HTTP"
    ):
        adbc_driver_manager.dbapi.connect(
            driver="hardcharger",
            db_kwargs={
                "uri": f"hardcharger+http://127.0.0.1:{hc_server.port}",
                "username": USERNAME,
                "password": hc_server.password,
            },
        )


def test_a_prepared_settings_query_re_executes_with_new_parameters(conn):
    """hardcharger-server before 3fbd36b answered no rows the second time: its table functions
    built their rows at bind time. get_setting() relies on this."""
    with conn.cursor() as cur:
        for name, want in (
            ("hardcharger.query_timeout", "0"),
            ("hardcharger.graceful_shutdown", "true"),
            ("hardcharger.bypass_queue", "false"),
        ):
            cur.execute(
                operation="SELECT CAST(value AS VARCHAR) FROM hardcharger_settings() "
                "WHERE name = ?",
                parameters=[name],
            )
            assert cur.fetchall() == [(want,)], name


# --- defaults inherited from the Chargewire driver: split results and partitioned uploads ---------

LARGE_RESULT = "SELECT i, i * 2 AS j FROM range(2000000) t(i)"


def test_a_large_result_is_split_by_default(conn):
    with conn.cursor() as cur:
        partitions, _ = cur.adbc_execute_partitions(operation=LARGE_RESULT)
    assert 2 <= len(partitions) <= 32, len(partitions)


def test_one_result_partition_when_asked(hc_server):
    with hardcharger.connect(
        uri=hc_server.uri,
        username=USERNAME,
        password=hc_server.password,
        db_kwargs={"adbc.chargewire.statement.result_partitions": "1"},
    ) as conn:
        with conn.cursor() as cur:
            partitions, _ = cur.adbc_execute_partitions(operation=LARGE_RESULT)
    assert len(partitions) == 1


@pytest.fixture
def proxied(hc_server, tmp_path):
    """Connections through a recording proxy (its own process); yields (connect, requests)."""
    log = tmp_path / "requests.log"
    log.touch()
    process = subprocess.Popen(
        args=[
            sys.executable,
            str(pathlib.Path(__file__).with_name("recording_proxy.py")),
            str(hc_server.port),
            str(log),
        ],
        stdout=subprocess.PIPE,
        text=True,
    )
    port = int(process.stdout.readline())

    def connect(**kwargs):
        return hardcharger.connect(
            uri=f"hardcharger://127.0.0.1:{port}?tls=false",
            username=USERNAME,
            password=hc_server.password,
            **kwargs,
        )

    def ingest_requests():
        lines = log.read_text().splitlines()
        single = sum(1 for r in lines if r.startswith("POST ") and r.endswith("/ingest"))
        opens = sum(1 for r in lines if r.startswith("POST ") and r.endswith("/ingests"))
        parts = sum(1 for r in lines if r.startswith("POST ") and "/parts/" in r)
        return single, opens, parts

    yield connect, ingest_requests
    process.kill()
    process.wait()


def ingest_16_batches(conn, table: str) -> None:
    import pyarrow as pa

    batches = [
        pa.record_batch(data={"k": pa.array(range(b * 5000, (b + 1) * 5000), type=pa.int64())})
        for b in range(16)
    ]
    with conn.cursor() as cur:
        rows = cur.adbc_ingest(
            table_name=table,
            data=pa.RecordBatchReader.from_batches(batches[0].schema, batches),
            mode="create",
        )
        assert rows == 80000
        cur.execute(operation=f"SELECT count(DISTINCT k) FROM {table}")
        assert cur.fetchone() == (80000,)


def test_an_ingest_goes_in_parts_by_default(proxied):
    connect, ingest_requests = proxied
    with connect() as conn:
        ingest_16_batches(conn=conn, table="py_default_parts")
    single, opens, parts = ingest_requests()
    assert single == 0 and opens == 1 and 2 <= parts <= 32, (single, opens, parts)


def test_one_ingest_part_when_asked_by_hardcharger_name(proxied):
    connect, ingest_requests = proxied
    with connect(db_kwargs={"adbc.hardcharger.ingest.parts": "1"}) as conn:
        ingest_16_batches(conn=conn, table="py_one_part")
    assert ingest_requests() == (1, 0, 0)


def test_ingest_parts_by_chargewire_name_and_on_a_connection(proxied):
    connect, ingest_requests = proxied
    with connect(db_kwargs={"adbc.chargewire.ingest.parts": "2"}) as conn:
        ingest_16_batches(conn=conn, table="py_two_parts")
    assert ingest_requests() == (0, 1, 2)
    with connect() as conn:
        conn.adbc_connection.set_options(**{"adbc.hardcharger.ingest.parts": "3"})
        ingest_16_batches(conn=conn, table="py_three_parts")
    assert ingest_requests() == (0, 2, 5)
