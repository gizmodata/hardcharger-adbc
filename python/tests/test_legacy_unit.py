"""Names from before the ADBC driver style guide, and from before the protocol was renamed to
Chargewire, keep working, with a deprecation warning (no server)."""

from __future__ import annotations

from unittest.mock import patch

import adbc_driver_manager
import pytest

from adbc_driver_hardcharger import dbapi as hardcharger
from adbc_driver_hardcharger._driver import _driver_path

_PATCH_DB = "adbc_driver_hardcharger.dbapi.adbc_driver_manager.AdbcDatabase"
_PATCH_CONN = "adbc_driver_hardcharger.dbapi.adbc_driver_manager.AdbcConnection"
_PATCH_CLS = "adbc_driver_hardcharger.dbapi.Connection"


@patch(_PATCH_CLS)
@patch(_PATCH_CONN)
@patch(_PATCH_DB)
class TestOldOptionKeys:
    def test_old_keys_reach_the_driver_as_current_ones(self, mock_db, mock_conn, mock_cls):
        hardcharger.connect(
            uri="warp://h:1?tls=false",
            db_kwargs={"adbc.warp.retry.max_attempts": "2"},
            conn_kwargs={"adbc.warp.ingest.parts": "1"},
        )
        kwargs = dict(mock_db.call_args.kwargs)
        kwargs.pop("driver")
        assert kwargs == {"uri": "warp://h:1?tls=false", "hardcharger.retry.max_attempts": "2"}
        assert mock_conn.call_args.kwargs["hardcharger.ingest.parts"] == "1"
        assert "adbc.warp.ingest.parts" not in mock_conn.call_args.kwargs

    def test_both_spellings_the_current_one_wins(self, mock_db, mock_conn, mock_cls):
        hardcharger.connect(
            uri="hardcharger://h",
            db_kwargs={
                "adbc.warp.tls.server_name": "old",
                "adbc.chargewire.tls.server_name": "new",
            },
        )
        kwargs = mock_db.call_args.kwargs
        assert kwargs["hardcharger.tls.server_name"] == "new"
        assert "adbc.warp.tls.server_name" not in kwargs
        assert "adbc.chargewire.tls.server_name" not in kwargs

    def test_connects_checks_see_old_keys(self, mock_db, mock_conn, mock_cls):
        # A keyword argument and an old key for the same option, with different values.
        with pytest.raises(expected_exception=ValueError, match="disagree"):
            hardcharger.connect(
                uri="hardcharger://h",
                tls_server_name="a",
                db_kwargs={"adbc.warp.tls.server_name": "b"},
            )
        # A token exchange beside a password given under the old key.
        with pytest.raises(expected_exception=ValueError, match="token exchange"):
            hardcharger.connect(
                uri="hardcharger://h",
                username="u",
                password="p",
                db_kwargs={"adbc.warp.auth.subject_token": "eyJ.idp.jwt"},
            )


def test_the_library_reads_old_keys_and_the_old_scheme():
    with adbc_driver_manager.AdbcDatabase(
        driver=_driver_path(),
        uri="warp://127.0.0.1:1?tls=false",
        **{"adbc.warp.ingest.parts": "2"},
    ) as db:
        assert db.get_option(key="hardcharger.ingest.parts") == "2"
        assert db.get_option(key="adbc.hardcharger.ingest.parts") == "2"
        assert db.get_option(key="adbc.chargewire.ingest.parts") == "2"


@pytest.fixture
def fresh_warnings(monkeypatch):
    from adbc_driver_hardcharger import _legacy

    monkeypatch.setattr(_legacy, "_warned", set())


@pytest.mark.usefixtures("fresh_warnings")
@patch(_PATCH_CLS)
@patch(_PATCH_CONN)
@patch(_PATCH_DB)
class TestStyleGuideNames:
    @pytest.mark.parametrize(
        ("old", "current"),
        [
            ("adbc.hardcharger.auth_type", "hardcharger.auth.type"),
            ("adbc.hardcharger.oauth.browser", "hardcharger.oauth.browser"),
            ("adbc.hardcharger.session_tag", "hardcharger.session.tag"),
            ("adbc.hardcharger.query_tag", "hardcharger.session.query_tag"),
            ("adbc.hardcharger.setting.query_timeout", "hardcharger.setting.query_timeout"),
            ("adbc.hardcharger.ingest.parts", "hardcharger.ingest.parts"),
            ("adbc.hardcharger.resume_timeout", "hardcharger.retry.resume_timeout_seconds"),
            ("adbc.chargewire.statement.timeout_seconds", "hardcharger.query.timeout_seconds"),
            ("adbc.chargewire.statement.query_tag", "hardcharger.query.tag"),
            ("adbc.chargewire.resume_timeout", "hardcharger.retry.resume_timeout_seconds"),
            ("adbc.chargewire.ipc_compression", "hardcharger.ipc.compression"),
            ("adbc.chargewire.tls.root_certs", "hardcharger.tls.root_certs"),
            ("adbc.warp.statement.result_order", "hardcharger.query.result_order"),
            # Hardcharger has its own auth.type and oauth.* (other values): the Chargewire
            # driver's keep its name.
            ("adbc.chargewire.auth.type", "chargewire.auth.type"),
            ("adbc.chargewire.oauth.browser", "chargewire.oauth.browser"),
            ("adbc.chargewire.oauth.timeout_seconds", "chargewire.oauth.timeout_seconds"),
        ],
    )
    def test_old_name_reaches_the_driver_as_current_and_warns_once(
        self, mock_db, mock_conn, mock_cls, capsys, old, current
    ):
        for _ in range(3):
            hardcharger.connect(uri="hardcharger://h", db_kwargs={old: "v-secret-value"})
            assert mock_db.call_args.kwargs[current] == "v-secret-value"
            assert old not in mock_db.call_args.kwargs
        err = capsys.readouterr().err
        assert err == f'[hardcharger] option "{old}" is deprecated; use "{current}"\n'
        assert "v-secret-value" not in err

    def test_chargewire_names_are_hardchargers_silently(self, mock_db, mock_conn, mock_cls, capsys):
        hardcharger.connect(
            uri="hardcharger://h", db_kwargs={"chargewire.query.timeout_seconds": "5"}
        )
        assert mock_db.call_args.kwargs["hardcharger.query.timeout_seconds"] == "5"
        assert capsys.readouterr().err == ""

    def test_the_current_name_wins_in_any_order(self, mock_db, mock_conn, mock_cls):
        for options in (
            {
                "adbc.chargewire.statement.timeout_seconds": "1",
                "chargewire.query.timeout_seconds": "2",
                "hardcharger.query.timeout_seconds": "3",
            },
            {
                "hardcharger.query.timeout_seconds": "3",
                "chargewire.query.timeout_seconds": "2",
                "adbc.chargewire.statement.timeout_seconds": "1",
            },
        ):
            hardcharger.connect(uri="hardcharger://h", db_kwargs=options)
            kwargs = mock_db.call_args.kwargs
            assert kwargs["hardcharger.query.timeout_seconds"] == "3"
            assert "chargewire.query.timeout_seconds" not in kwargs

    def test_unknown_keys_keep_their_spelling(self, mock_db, mock_conn, mock_cls, capsys):
        hardcharger.connect(
            uri="hardcharger://h",
            db_kwargs={"adbc.hardcharger.no_such_option": "1", "adbc.chargewire.nope": "1"},
        )
        kwargs = mock_db.call_args.kwargs
        assert kwargs["adbc.hardcharger.no_such_option"] == "1"
        assert kwargs["adbc.chargewire.nope"] == "1"
        assert capsys.readouterr().err == ""


def test_the_library_warns_on_stderr_once_and_never_prints_values():
    import subprocess
    import sys
    import textwrap

    secret = "s3cr3t-never-printed"
    script = textwrap.dedent(
        f"""
        import adbc_driver_manager
        from adbc_driver_hardcharger._driver import _driver_path
        for _ in range(3):
            with adbc_driver_manager.AdbcDatabase(
                driver=_driver_path(),
                uri="hardcharger://127.0.0.1:1?tls=false",
                **{{
                    "adbc.hardcharger.query_tag": '{{"a": 1}}',
                    "adbc.chargewire.statement.timeout_seconds": "2.5",
                    "adbc.chargewire.auth.access_token": "{secret}",
                }},
            ) as db:
                assert db.get_option(key="hardcharger.query.timeout_seconds") == "2.5"
                assert db.get_option(key="hardcharger.session.query_tag") == '{{"a": 1}}'
        """
    )
    done = subprocess.run(
        args=[sys.executable, "-c", script], capture_output=True, text=True, timeout=60
    )
    assert done.returncode == 0, done.stderr
    assert secret not in done.stderr
    assert sorted(done.stderr.splitlines()) == [
        '[hardcharger] option "adbc.chargewire.auth.access_token" is deprecated; '
        'use "hardcharger.auth.access_token"',
        '[hardcharger] option "adbc.chargewire.statement.timeout_seconds" is deprecated; '
        'use "hardcharger.query.timeout_seconds"',
        '[hardcharger] option "adbc.hardcharger.query_tag" is deprecated; '
        'use "hardcharger.session.query_tag"',
    ]
