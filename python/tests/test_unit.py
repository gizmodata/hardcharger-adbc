"""Unit tests (no server): how connect() maps keyword arguments to driver options, the helpers'
value handling, the manifest, and the driver library through the C API."""

from __future__ import annotations

from unittest.mock import patch

import adbc_driver_manager
import pytest

from adbc_driver_hardcharger import DEFAULT_PORT, manifest
from adbc_driver_hardcharger import dbapi as hardcharger
from adbc_driver_hardcharger._driver import _driver_path

_PATCH_DB = "adbc_driver_hardcharger.dbapi.adbc_driver_manager.AdbcDatabase"
_PATCH_CONN = "adbc_driver_hardcharger.dbapi.adbc_driver_manager.AdbcConnection"
_PATCH_CLS = "adbc_driver_hardcharger.dbapi.Connection"
PEM = "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n"


def _db_kwargs(mock_db) -> dict:
    kwargs = dict(mock_db.call_args.kwargs)
    kwargs.pop("driver", None)
    return kwargs


@patch(_PATCH_CLS)
@patch(_PATCH_CONN)
@patch(_PATCH_DB)
class TestConnectOptions:
    def test_password(self, mock_db, mock_conn, mock_cls):
        hardcharger.connect(uri="hardcharger://h?tls=false", username="u", password="p:@")
        # The URI goes to the driver as it is: the library reads hardcharger://. Nothing else
        # (partitions, compression, ...) is forced on it.
        assert _db_kwargs(mock_db=mock_db) == {
            "uri": "hardcharger://h?tls=false",
            "username": "u",
            "password": "p:@",
        }
        assert mock_db.call_args.kwargs["driver"] == _driver_path()

    def test_hardcharger_options(self, mock_db, mock_conn, mock_cls, tmp_path):
        ca = tmp_path / "ca.pem"
        ca.write_text(data=PEM)
        hardcharger.connect(
            uri="hardcharger://h",
            auth_type="external",
            oauth_timeout=2.5,
            open_browser=False,
            session_tag={"app": "etl"},
            query_tag='{"job": "x"}',
            settings={"query_timeout": 60, "hardcharger.bypass_queue": True},
            tls_root_certs=ca,
            catalog="memory",
        )
        assert _db_kwargs(mock_db=mock_db) == {
            "uri": "hardcharger://h",
            "adbc.hardcharger.auth_type": "external",
            "adbc.hardcharger.oauth.timeout_seconds": "2.5",
            "adbc.hardcharger.oauth.browser": "none",
            "adbc.hardcharger.session_tag": '{"app":"etl"}',
            "adbc.hardcharger.query_tag": '{"job": "x"}',
            "adbc.hardcharger.setting.query_timeout": "60",
            "adbc.hardcharger.setting.bypass_queue": "true",
            "adbc.chargewire.tls.root_certs": PEM,
        }
        assert mock_conn.call_args.kwargs["adbc.connection.catalog"] == "memory"

    def test_browser_command(self, mock_db, mock_conn, mock_cls):
        hardcharger.connect(uri="hardcharger://h", auth_type="external", open_browser="firefox")
        assert _db_kwargs(mock_db=mock_db)["adbc.hardcharger.oauth.browser"] == "firefox"

    def test_profile_uri(self, mock_db, mock_conn, mock_cls):
        hardcharger.connect(uri="profile://prod")
        assert _db_kwargs(mock_db=mock_db) == {"profile": "prod"}
        assert mock_db.call_args.kwargs["driver"] == "hardcharger"

    def test_named_profile(self, mock_db, mock_conn, mock_cls):
        hardcharger.connect(profile="prod")
        assert mock_db.call_args.kwargs == {"driver": "hardcharger", "profile": "prod"}

    def test_invalid_auth_type(self, mock_db, mock_conn, mock_cls):
        with pytest.raises(expected_exception=ValueError, match="invalid auth_type 'oauth'"):
            hardcharger.connect(uri="hardcharger://h", auth_type="oauth")
        mock_db.assert_not_called()

    @pytest.mark.parametrize(argnames="spelling", argvalues=["client_certificate", "mtls"])
    def test_client_certificate(self, mock_db, mock_conn, mock_cls, tmp_path, spelling):
        cert, key = tmp_path / "client.pem", tmp_path / "client-key.pem"
        cert.write_text(data=PEM)
        key.write_text(data="-----BEGIN PRIVATE KEY-----\nkey\n-----END PRIVATE KEY-----\n")
        hardcharger.connect(
            uri="hardcharger://h", auth_type=spelling, tls_client_cert=cert, tls_client_key=key
        )
        kwargs = _db_kwargs(mock_db=mock_db)
        assert kwargs["adbc.hardcharger.auth_type"] == "client_certificate"
        assert kwargs["adbc.chargewire.tls.client_cert"] == PEM
        assert kwargs["adbc.chargewire.tls.client_key"].startswith("-----BEGIN PRIVATE KEY-----")
        assert "username" not in kwargs and "password" not in kwargs

    def test_the_side_sign_in_servers_arguments_are_gone(self, mock_db, mock_conn, mock_cls):
        # Browser sign-in is on the server's own port: no sign-in server URL or port.
        for argument in ("oauth_url", "oauth_port"):
            with pytest.raises(expected_exception=TypeError, match=argument):
                hardcharger.connect(uri="hardcharger://h", auth_type="external", **{argument: "x"})
        import adbc_driver_hardcharger

        assert not hasattr(adbc_driver_hardcharger, "DEFAULT_OAUTH_PORT")
        assert not hasattr(adbc_driver_hardcharger.DatabaseOptions, "OAUTH_URL")
        assert not hasattr(adbc_driver_hardcharger.DatabaseOptions, "OAUTH_PORT")

    def test_external_auth_refuses_a_password(self, mock_db, mock_conn, mock_cls):
        with pytest.raises(expected_exception=ValueError, match="drop username, password"):
            hardcharger.connect(uri="hardcharger://h", auth_type="external", password="p")
        mock_db.assert_not_called()

    @pytest.mark.parametrize(
        argnames=("share_sign_in", "expected"),
        argvalues=[
            (True, {"adbc.hardcharger.oauth.share_sign_in": "true"}),
            (
                "alice",
                {
                    "adbc.hardcharger.oauth.share_sign_in": "true",
                    "adbc.hardcharger.oauth.sign_in_key": "alice",
                },
            ),
            (False, {}),
        ],
    )
    def test_share_sign_in(self, mock_db, mock_conn, mock_cls, share_sign_in, expected):
        hardcharger.connect(
            uri="hardcharger://h", auth_type="external", share_sign_in=share_sign_in
        )
        assert _db_kwargs(mock_db=mock_db) == {
            "uri": "hardcharger://h",
            "adbc.hardcharger.auth_type": "external",
            **expected,
        }

    def test_share_sign_in_needs_external_auth(self, mock_db, mock_conn, mock_cls):
        with pytest.raises(expected_exception=ValueError, match="needs auth_type='external'"):
            hardcharger.connect(
                uri="hardcharger://h", username="u", password="p", share_sign_in=True
            )
        with pytest.raises(expected_exception=TypeError, match="a bool or a str"):
            hardcharger.connect(uri="hardcharger://h", auth_type="external", share_sign_in=1)
        mock_db.assert_not_called()

    def test_bad_tag_type(self, mock_db, mock_conn, mock_cls):
        with pytest.raises(expected_exception=TypeError, match="a tag must be a mapping"):
            hardcharger.connect(uri="hardcharger://h", query_tag=42)
        mock_db.assert_not_called()

    def test_needs_a_uri_or_profile(self, mock_db, mock_conn, mock_cls):
        with pytest.raises(expected_exception=ValueError, match="at least one of 'uri' or"):
            hardcharger.connect()

    def test_subject_token(self, mock_db, mock_conn, mock_cls):
        hardcharger.connect(uri="hardcharger://h", subject_token="eyJ.idp.jwt")
        assert _db_kwargs(mock_db=mock_db) == {
            "uri": "hardcharger://h",
            "adbc.chargewire.auth.subject_token": "eyJ.idp.jwt",
        }

    @pytest.mark.parametrize(
        argnames=("kwargs", "text"),
        argvalues=[
            ({"username": "u", "password": "p"}, "drop username, password"),
            ({"password": "p"}, "drop password"),
            ({"access_token": "t"}, "drop adbc.chargewire.auth.access_token"),
            ({"db_kwargs": {"username": "u"}}, "drop username"),
            (
                {"db_kwargs": {"adbc.chargewire.auth.access_token": "t"}},
                "drop adbc.chargewire.auth.access",
            ),
            ({"auth_type": "external"}, "signs in through the browser: drop .*subject_token"),
            ({"auth_type": "mtls"}, "drop auth_type='client_certificate'"),
            ({"db_kwargs": {"adbc.chargewire.auth.subject_token": "other"}}, "disagree"),
        ],
    )
    def test_subject_token_refuses_other_credentials(
        self, mock_db, mock_conn, mock_cls, kwargs, text
    ):
        # The driver would pick one of them without a word (an access token wins over the
        # subject token, the subject token over a password).
        with pytest.raises(expected_exception=ValueError, match=text):
            hardcharger.connect(uri="hardcharger://h", subject_token="eyJ.idp.jwt", **kwargs)
        mock_db.assert_not_called()

    def test_subject_token_in_db_kwargs_refuses_a_password(self, mock_db, mock_conn, mock_cls):
        with pytest.raises(expected_exception=ValueError, match="drop username, password"):
            hardcharger.connect(
                uri="hardcharger://h",
                username="u",
                password="p",
                db_kwargs={"adbc.chargewire.auth.subject_token": "eyJ.idp.jwt"},
            )
        mock_db.assert_not_called()

    def test_subject_token_and_the_same_db_kwarg(self, mock_db, mock_conn, mock_cls):
        hardcharger.connect(
            uri="hardcharger://h",
            subject_token="eyJ.idp.jwt",
            db_kwargs={"adbc.chargewire.auth.subject_token": "eyJ.idp.jwt"},
        )
        assert _db_kwargs(mock_db=mock_db)["adbc.chargewire.auth.subject_token"] == "eyJ.idp.jwt"

    def test_tls_server_name(self, mock_db, mock_conn, mock_cls):
        hardcharger.connect(
            uri="hardcharger://10.0.0.7",
            username="u",
            password="p",
            tls_server_name="hc-1.gizmodata.test",
        )
        assert _db_kwargs(mock_db=mock_db) == {
            "uri": "hardcharger://10.0.0.7",
            "username": "u",
            "password": "p",
            "adbc.chargewire.tls.server_name": "hc-1.gizmodata.test",
        }

    def test_session_id_and_close_session_on_release(self, mock_db, mock_conn, mock_cls):
        hardcharger.connect(
            uri="hardcharger://h",
            access_token="tok",
            session_id="s_1",
            close_session_on_release=True,
        )
        assert _db_kwargs(mock_db=mock_db) == {
            "uri": "hardcharger://h",
            "adbc.chargewire.auth.access_token": "tok",
            "adbc.chargewire.session.id": "s_1",
            "adbc.chargewire.session.close_on_release": "true",
        }
        hardcharger.connect(uri="hardcharger://h", close_session_on_release=False)
        assert _db_kwargs(mock_db=mock_db)["adbc.chargewire.session.close_on_release"] == "false"
        hardcharger.connect(uri="hardcharger://h")
        assert "adbc.chargewire.session.close_on_release" not in _db_kwargs(mock_db=mock_db)

    @pytest.mark.parametrize(
        argnames=("kwargs", "error", "text"),
        argvalues=[
            ({"session_id": ""}, ValueError, "session_id can't be empty"),
            ({"close_session_on_release": "false"}, TypeError, "must be a bool, not str"),
            (
                {"session_id": "s_1", "db_kwargs": {"adbc.chargewire.session.id": "s_2"}},
                ValueError,
                "session_id= and db_kwargs",
            ),
            (
                {
                    "close_session_on_release": True,
                    "db_kwargs": {"adbc.chargewire.session.close_on_release": "false"},
                },
                ValueError,
                "disagree",
            ),
        ],
    )
    def test_session_arguments_are_checked(self, mock_db, mock_conn, mock_cls, kwargs, error, text):
        with pytest.raises(expected_exception=error, match=text):
            hardcharger.connect(uri="hardcharger://h", **kwargs)
        mock_db.assert_not_called()

    def test_tls_server_name_conflicts(self, mock_db, mock_conn, mock_cls):
        with pytest.raises(expected_exception=ValueError, match="disagree: give one of them"):
            hardcharger.connect(
                uri="hardcharger://10.0.0.7",
                tls_server_name="hc-1.gizmodata.test",
                db_kwargs={"adbc.chargewire.tls.server_name": "hc-2.gizmodata.test"},
            )
        for kwargs in ({"subject_token": ""}, {"tls_server_name": ""}):
            with pytest.raises(expected_exception=ValueError, match="can't be empty"):
                hardcharger.connect(uri="hardcharger://h", **kwargs)
        mock_db.assert_not_called()


class _FakeAdbcConnection:
    def __init__(self, options: dict):
        self.options = options

    def get_option(self, key):
        return self.options[key]


class _FakeConnection:
    def __init__(self, options: dict):
        self.adbc_connection = _FakeAdbcConnection(options=options)


def test_access_token_properties():
    import datetime

    conn = _FakeConnection(
        options={
            "adbc.chargewire.auth.access_token": "eyJ.tok",
            "adbc.chargewire.auth.access_token_expires_at": "2026-10-20T15:14:13Z",
        }
    )
    assert hardcharger.Connection.access_token.fget(conn) == "eyJ.tok"
    assert hardcharger.Connection.access_token_expires_at.fget(conn) == datetime.datetime(
        year=2026, month=10, day=20, hour=15, minute=14, second=13, tzinfo=datetime.timezone.utc
    )
    conn.adbc_connection.options["adbc.chargewire.auth.access_token_expires_at"] = ""
    assert hardcharger.Connection.access_token_expires_at.fget(conn) is None


def test_value_helpers():
    assert hardcharger._tag_text(tag={"b": 2, "a": "x"}) == '{"a":"x","b":2}'
    assert hardcharger._tag_text(tag=None) == ""
    assert hardcharger._setting_text(value=True) == "true"
    assert hardcharger._setting_text(value=30) == "30"
    with pytest.raises(expected_exception=TypeError, match="str, int or bool, not float"):
        hardcharger._setting_text(value=1.5)
    assert DEFAULT_PORT == 31337


class TestManifest:
    def test_render(self):
        text = manifest.render(library="/opt/lib/libadbc_driver_hardcharger.so")
        assert "name = 'Hardcharger'" in text
        assert "entrypoint = 'AdbcDriverHardchargerInit'" in text
        assert f'{manifest._platform_key()} = "/opt/lib/libadbc_driver_hardcharger.so"' in text

    def test_install_points_at_the_bundled_library(self, tmp_path):
        path = manifest.install(directory=tmp_path)
        assert path == tmp_path / "hardcharger.toml"
        assert _driver_path().replace("\\", "\\\\") in path.read_text()


class TestLibrary:
    """The driver library through the ADBC driver manager's C API, with no server."""

    def test_keeps_the_hardcharger_uri_and_options(self):
        with adbc_driver_manager.AdbcDatabase(
            driver=_driver_path(),
            uri="hardcharger://127.0.0.1?tls=false",
            username="u",
            password="p",
            **{
                "adbc.hardcharger.query_tag": '{"a":1}',
                "adbc.hardcharger.oauth.timeout_seconds": "40",
            },
        ) as db:
            assert db.get_option(key="uri") == "hardcharger://127.0.0.1?tls=false"
            assert db.get_option(key="adbc.hardcharger.query_tag") == '{"a":1}'
            assert db.get_option(key="adbc.hardcharger.oauth.timeout_seconds") == "40"

    def test_the_side_sign_in_servers_options_are_refused(self):
        # The library refuses them by name: browser sign-in is on the server's own port.
        for key in ("adbc.hardcharger.oauth.port", "adbc.hardcharger.oauth.url"):
            with pytest.raises(expected_exception=adbc_driver_manager.NotSupportedError) as error:
                adbc_driver_manager.AdbcDatabase(
                    driver=_driver_path(), uri="hardcharger://127.0.0.1", **{key: "x"}
                )
            assert f"unknown option {key}" in str(error.value)

    def test_client_certificate_reaches_the_chargewire_driver(self):
        # Its checks run on auth_type=client_certificate: a password beside the certificate is
        # refused before anything is sent.
        with pytest.raises(expected_exception=adbc_driver_manager.Error) as error:
            adbc_driver_manager.AdbcDatabase(
                driver=_driver_path(),
                uri="hardcharger://127.0.0.1",
                username="u",
                password="p",
                **{"adbc.hardcharger.auth_type": "client_certificate"},
            )
        assert error.value.status_code == adbc_driver_manager.AdbcStatusCode.INVALID_ARGUMENT
        assert "signs in with the client certificate alone; do not also set username" in str(
            error.value
        )
        with pytest.raises(expected_exception=adbc_driver_manager.Error) as error:
            adbc_driver_manager.AdbcDatabase(
                driver=_driver_path(),
                uri="hardcharger://127.0.0.1",
                **{"adbc.hardcharger.auth_type": "mtls"},
            )
        assert "needs a client certificate: set adbc.chargewire.tls.client_cert" in str(error.value)

    def test_subject_token_reaches_the_chargewire_driver(self):
        # Its checks see it: a token exchange beside a client certificate sign-in is refused.
        with pytest.raises(expected_exception=adbc_driver_manager.Error) as error:
            adbc_driver_manager.AdbcDatabase(
                driver=_driver_path(),
                uri="hardcharger://127.0.0.1",
                **{
                    "adbc.hardcharger.auth_type": "client_certificate",
                    "adbc.chargewire.auth.subject_token": "eyJ.idp.jwt",
                },
            )
        assert "do not also set adbc.chargewire.auth.subject_token" in str(error.value)

    def test_tls_server_name_reaches_the_chargewire_driver(self):
        with adbc_driver_manager.AdbcDatabase(
            driver=_driver_path(),
            uri="hardcharger://10.0.0.7",
            **{"adbc.chargewire.tls.server_name": "hc-1.gizmodata.test"},
        ) as db:
            assert db.get_option(key="adbc.chargewire.tls.server_name") == "hc-1.gizmodata.test"

    def test_session_options_reach_the_chargewire_driver(self):
        with adbc_driver_manager.AdbcDatabase(
            driver=_driver_path(),
            uri="hardcharger://127.0.0.1",
            **{
                "adbc.chargewire.session.id": "s_1",
                "adbc.chargewire.session.close_on_release": "false",
            },
        ) as db:
            assert db.get_option(key="adbc.chargewire.session.id") == "s_1"
            assert db.get_option(key="adbc.chargewire.session.close_on_release") == "false"

    def test_plus_http_is_refused(self):
        with pytest.raises(expected_exception=adbc_driver_manager.Error) as error:
            adbc_driver_manager.AdbcDatabase(
                driver=_driver_path(), uri="hardcharger+http://127.0.0.1:31337"
            )
        assert error.value.status_code == adbc_driver_manager.AdbcStatusCode.INVALID_ARGUMENT
        assert "hardcharger://host:port?tls=false for plain HTTP" in str(error.value)

    @pytest.mark.parametrize(
        argnames=("key", "value", "text"),
        argvalues=[
            ("adbc.hardcharger.auth_type", "oauth", "expected password, external or client_cert"),
            ("adbc.hardcharger.setting.bad name", "1", "invalid Hardcharger setting name"),
            ("adbc.hardcharger.nope", "1", "unknown option"),
        ],
    )
    def test_bad_options_are_refused(self, key, value, text):
        with pytest.raises(expected_exception=adbc_driver_manager.Error, match=text):
            adbc_driver_manager.AdbcDatabase(
                driver=_driver_path(), uri="hardcharger://h", **{key: value}
            )
