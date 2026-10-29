"""Names from before the protocol was renamed to Chargewire keep working (no server)."""

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
        assert kwargs == {"uri": "warp://h:1?tls=false", "adbc.chargewire.retry.max_attempts": "2"}
        assert mock_conn.call_args.kwargs["adbc.chargewire.ingest.parts"] == "1"
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
        assert kwargs["adbc.chargewire.tls.server_name"] == "new"
        assert "adbc.warp.tls.server_name" not in kwargs

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
        assert db.get_option(key="adbc.hardcharger.ingest.parts") == "2"
        assert db.get_option(key="adbc.chargewire.ingest.parts") == "2"
