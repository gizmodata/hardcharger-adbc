"""The PEP 249 exception class of each kind of failure, against a real hardcharger-server.

A user's SQL that fails, before it runs (sql_error) or while it runs (execution_error, HTTP
422), is a ProgrammingError, never an InternalError (which says the driver or the server is
broken); a violated constraint is an IntegrityError; a cancel or a timeout is an
OperationalError; a wrong password is the ProgrammingError of status UNAUTHENTICATED (the
driver manager's class for it).
"""

from __future__ import annotations

import threading
import time

import adbc_driver_manager
import pytest

from adbc_driver_hardcharger import StatementOptions
from adbc_driver_hardcharger import dbapi as hardcharger
from conftest import USERNAME

pytestmark = pytest.mark.integration

Status = adbc_driver_manager.AdbcStatusCode

# CPU-bound for far longer than any test timeout: only a server-side stop ends it early.
LONG_QUERY = "SELECT sum(a.range * b.range) FROM range(100000000) a, range(100000) b"


def chargewire_code(error: BaseException) -> str | None:
    for key, value in getattr(error, "details", None) or []:
        if (key.decode() if isinstance(key, bytes) else key) == "chargewire.code":
            return value.decode() if isinstance(value, bytes) else value
    return None


def run(conn, operation: str) -> BaseException:
    """Run a statement to the end of its result and return what it raised."""
    with conn.cursor() as cur:
        try:
            cur.execute(operation=operation)
            while cur.fetchmany(size=100_000):
                pass
        except BaseException as error:  # noqa: BLE001 - the test inspects whatever it was
            return error
    pytest.fail(reason=f"{operation} succeeded; want an error")


def assert_raised(
    error: BaseException,
    klass: type,
    status: adbc_driver_manager.AdbcStatusCode,
    code: str | None,
    sqlstate: str | None = None,
    message: str | None = None,
) -> None:
    assert type(error) is klass, f"{type(error).__name__}, want {klass.__name__}: {error}"
    assert not isinstance(error, hardcharger.InternalError), error
    assert error.status_code == status, error
    assert chargewire_code(error=error) == code, error
    if sqlstate is not None:
        assert error.sqlstate == sqlstate, error
    if message is not None:
        assert message in str(error), error


@pytest.mark.parametrize(
    argnames=("operation", "message"),
    argvalues=[
        # DuckDB's / divides floating point (1/0 is no error); // divides integers.
        ("SELECT 1 // 0", "ivision by zero"),
        ("SELECT 10 // (i - 3) FROM range(5) t(i)", "ivision by zero"),
        ("SELECT CAST(s AS INTEGER) FROM (VALUES ('1'), ('x')) t(s)", "Conversion Error"),
        # Fails after 3,000,000 rows, usually while the result streams.
        (
            "SELECT CASE WHEN i < 3000000 THEN i ELSE error('boom') END FROM range(6000000) t(i)",
            "boom",
        ),
    ],
    ids=["division-by-zero", "division-by-zero-over-rows", "conversion", "mid-stream"],
)
def test_execution_error_is_a_programming_error(conn, operation, message):
    assert_raised(
        error=run(conn=conn, operation=operation),
        klass=hardcharger.ProgrammingError,
        status=Status.INVALID_ARGUMENT,
        code="execution_error",
        message=message,
    )


def test_syntax_error_is_a_programming_error(conn):
    assert_raised(
        error=run(conn=conn, operation="SELEC 1"),
        klass=hardcharger.ProgrammingError,
        status=Status.INVALID_ARGUMENT,
        code="sql_error",
        sqlstate="42601",
        message="Parser Error",
    )


def test_unknown_table_is_a_programming_error(conn):
    assert_raised(
        error=run(conn=conn, operation="SELECT * FROM errclass_no_such_table"),
        klass=hardcharger.ProgrammingError,
        status=Status.NOT_FOUND,
        code="sql_error",
        sqlstate="42P01",
        message="errclass_no_such_table",
    )


def test_duplicate_key_is_an_integrity_error(conn):
    with conn.cursor() as cur:
        cur.execute(operation="CREATE OR REPLACE TABLE errclass_keys (id INTEGER PRIMARY KEY)")
        cur.execute(operation="INSERT INTO errclass_keys VALUES (1)")
    assert_raised(
        error=run(conn=conn, operation="INSERT INTO errclass_keys VALUES (1)"),
        klass=hardcharger.IntegrityError,
        status=Status.INTEGRITY,
        code="execution_error",
        sqlstate="23000",
    )


def test_cancel_is_an_operational_error(conn):
    cur = conn.cursor()
    errors: list[BaseException] = []

    def execute() -> None:
        try:
            cur.execute(operation=LONG_QUERY)
            cur.fetchall()
        except BaseException as error:  # noqa: BLE001
            errors.append(error)

    worker = threading.Thread(target=execute, daemon=True)
    worker.start()
    time.sleep(1.0)
    cur.adbc_cancel()
    worker.join(timeout=30)
    assert not worker.is_alive(), "the query was not interrupted within 30s"
    cur.close()
    assert errors, "the cancelled query raised nothing"
    assert type(errors[0]) is hardcharger.OperationalError, repr(errors[0])
    assert errors[0].status_code == Status.CANCELLED, errors[0]
    with conn.cursor() as probe:
        probe.execute(operation="SELECT 42")
        assert probe.fetchone() == (42,)


def test_timeout_is_an_operational_error(conn):
    with conn.cursor(adbc_stmt_kwargs={StatementOptions.TIMEOUT_SECONDS.value: "0.3"}) as cur:
        with pytest.raises(expected_exception=hardcharger.Error) as caught:
            cur.execute(operation=LONG_QUERY)
            cur.fetchall()
    assert_raised(
        error=caught.value,
        klass=hardcharger.OperationalError,
        status=Status.TIMEOUT,
        code="statement_timeout",
    )


def test_bad_password_is_unauthenticated(hc_server):
    with pytest.raises(expected_exception=hardcharger.Error) as caught:
        with hardcharger.connect(
            uri=hc_server.uri, username=USERNAME, password="not-" + hc_server.password
        ) as conn:
            with conn.cursor() as cur:
                cur.execute(operation="SELECT 1")
    assert_raised(
        error=caught.value,
        klass=hardcharger.ProgrammingError,
        status=Status.UNAUTHENTICATED,
        code="invalid_credentials",
    )
    assert hc_server.password not in str(caught.value)
