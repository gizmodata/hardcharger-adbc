"""Names and servers from before the protocol was renamed to Chargewire.

The old URI scheme and option keys run against the suite's own server. A server from before
the rename (one already running, e.g. the ``ghcr.io/gizmodata/hardcharger:2.2.0`` image) is
named by ``HARDCHARGER_ADBC_TEST_LEGACY_URI``, with the admin's password in
``HARDCHARGER_ADBC_TEST_LEGACY_PASSWORD`` (and user name in
``HARDCHARGER_ADBC_TEST_LEGACY_USERNAME``, default ``hardcharger``). Without the URI those
tests skip; with it, they fail when the driver can't use it.
"""

from __future__ import annotations

import os
import time

import adbc_driver_manager
import pytest

from adbc_driver_hardcharger import dbapi as hardcharger
from conftest import USERNAME

pytestmark = pytest.mark.integration

LEGACY_URI_ENV = "HARDCHARGER_ADBC_TEST_LEGACY_URI"
LEGACY_PASSWORD_ENV = "HARDCHARGER_ADBC_TEST_LEGACY_PASSWORD"
LEGACY_USERNAME_ENV = "HARDCHARGER_ADBC_TEST_LEGACY_USERNAME"
SLOW_QUERY = "SELECT sum(a.range * b.range) FROM range(100000000) a, range(100000) b"


def error_code(error: BaseException) -> str | None:
    for key, value in getattr(error, "details", None) or []:
        if (key.decode() if isinstance(key, bytes) else key) == "chargewire.code":
            return value.decode() if isinstance(value, bytes) else value
    return None


def test_old_scheme_and_option_keys(hc_server):
    with hardcharger.connect(
        uri=f"warp://{hc_server.host}:{hc_server.port}?tls=false",
        username=USERNAME,
        password=hc_server.password,
        db_kwargs={"adbc.warp.statement.timeout_seconds": "0.3"},
        conn_kwargs={"adbc.warp.ingest.parts": "1"},
    ) as conn:
        assert conn.adbc_connection.get_option(key="adbc.hardcharger.ingest.parts") == "1"
        with conn.cursor() as cur:
            cur.execute(operation="SELECT 42 AS answer")
            assert cur.fetchone() == (42,)
        # The old key's value is in force: the server stops a long statement after 0.3 s.
        with conn.cursor() as cur:
            with pytest.raises(expected_exception=adbc_driver_manager.Error) as caught:
                cur.execute(operation=SLOW_QUERY)
                cur.fetchall()
            assert caught.value.status_code == adbc_driver_manager.AdbcStatusCode.TIMEOUT
            assert error_code(error=caught.value) == "statement_timeout", caught.value


@pytest.fixture
def legacy_credentials() -> dict[str, str]:
    uri = os.environ.get(LEGACY_URI_ENV)
    if not uri:
        pytest.skip(reason=f"set {LEGACY_URI_ENV} to a server from before the rename")
    password = os.environ.get(LEGACY_PASSWORD_ENV)
    if not password:
        pytest.fail(reason=f"{LEGACY_URI_ENV} is set: set {LEGACY_PASSWORD_ENV} too")
    username = os.environ.get(LEGACY_USERNAME_ENV) or "hardcharger"
    return {"uri": uri, "username": username, "password": password}


def test_a_server_from_before_the_rename(legacy_credentials):
    import pyarrow as pa

    with hardcharger.connect(**legacy_credentials, query_tag={"job": "legacy"}) as conn:
        assert conn.adbc_get_info()["vendor_name"] == "Hardcharger"
        with conn.cursor() as cur:
            cur.execute(operation="SELECT 42 AS answer")
            assert cur.fetchone() == (42,)
            cur.execute(operation="SELECT sum(i) AS s FROM range(10) t(i)")
            assert cur.fetchone() == (45,)
            # A prepared statement with a parameter.
            cur.execute(operation="SELECT ? + 1 AS n", parameters=[41])
            assert cur.fetchone() == (42,)
            # A bulk ingest.
            table = f"legacy_ingest_{time.time_ns()}"
            rows = cur.adbc_ingest(
                table_name=table,
                data=pa.table({"id": [1, 2, 3], "city": ["Boston", "Denver", None]}),
                mode="create",
            )
            assert rows == 3
            cur.execute(operation=f"SELECT city FROM {table} ORDER BY id")
            assert cur.fetchall() == [("Boston",), ("Denver",), (None,)]
            cur.execute(operation=f"DROP TABLE {table}")
        # Hardcharger's settings.
        conn.set_setting(name="query_timeout", value=30)
        assert conn.get_setting(name="query_timeout") == "30"
        # A SQL error keeps its status and code.
        with conn.cursor() as cur:
            with pytest.raises(expected_exception=adbc_driver_manager.Error) as caught:
                cur.execute(operation="SELECT * FROM legacy_no_such_table")
            assert error_code(error=caught.value) == "sql_error", caught.value


def test_a_server_from_before_the_rename_with_old_names(legacy_credentials):
    # The old scheme has no default port of Hardcharger's: the URI names one.
    uri = legacy_credentials["uri"].replace("hardcharger://", "warp://", 1)
    with hardcharger.connect(
        **{**legacy_credentials, "uri": uri},
        db_kwargs={"adbc.warp.retry.max_attempts": "2"},
    ) as conn:
        with conn.cursor() as cur:
            cur.execute(operation="SELECT 42 AS answer")
            assert cur.fetchone() == (42,)
