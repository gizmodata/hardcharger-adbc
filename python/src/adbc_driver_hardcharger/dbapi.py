"""DB-API 2.0 interface for Hardcharger, backed by the native Go driver.

Example::

    from adbc_driver_hardcharger import dbapi as hardcharger

    with hardcharger.connect(uri="hardcharger://db.example.com", username="joe",
                             password="...") as conn:
        with conn.cursor() as cur:
            cur.execute(operation="SELECT 42 AS answer")
            print(cur.fetch_arrow_table())

``hardcharger://host[:port]`` is HTTPS (TLS on), with the port defaulting to 31337; add
``?tls=false`` for plain HTTP to a development server. The URI, the browser sign-in, tags and
settings are handled by the driver library (``libadbc_driver_hardcharger``), so every ADBC
language gets the same behaviour; this module only maps keyword arguments to its options.
"""

from __future__ import annotations

import json
import os
from collections.abc import Mapping
from typing import Any, Literal, Optional, Union

import adbc_driver_manager
from adbc_driver_manager.dbapi import Connection as _BaseConnection
from adbc_driver_manager.dbapi import Cursor as _BaseCursor

# Re-export the DB-API 2.0 module globals, type constructors and exceptions (PEP 249: a
# driver's module exposes Error, Warning and the rest). They are the driver manager's own
# classes, so `except dbapi.ProgrammingError` and `except adbc_driver_manager.ProgrammingError`
# catch the same errors.
from adbc_driver_manager.dbapi import (  # noqa: F401
    DatabaseError,
    DataError,
    Date,
    DateFromTicks,
    Error,
    IntegrityError,
    InterfaceError,
    InternalError,
    NotSupportedError,
    OperationalError,
    ProgrammingError,
    Time,
    TimeFromTicks,
    Timestamp,
    TimestampFromTicks,
    Warning,
    apilevel,
    paramstyle,
    threadsafety,
)

from ._driver import _driver_path
from ._options import ConnectionOptions, DatabaseOptions

PemSource = Union[str, bytes, "os.PathLike[str]"]
#: A query or session tag: a JSON object, as a mapping or as JSON text.
Tag = Union[Mapping[str, Any], str]
SettingValue = Union[str, int, bool]


def _tag_text(tag: Optional[Tag]) -> str:
    if tag is None:
        return ""
    if isinstance(tag, str):
        return tag
    if isinstance(tag, Mapping):
        return json.dumps(obj=dict(tag), separators=(",", ":"), sort_keys=True)
    raise TypeError(f"a tag must be a mapping or JSON text, not {type(tag).__name__}")


def _setting_text(value: SettingValue) -> str:
    if isinstance(value, bool):
        return "true" if value else "false"
    if isinstance(value, (int, str)):
        return str(value)
    raise TypeError(f"a setting's value must be a str, int or bool, not {type(value).__name__}")


def _bare(name: str) -> str:
    return name[len("hardcharger.") :] if name.startswith("hardcharger.") else name


def _pem(value: PemSource) -> str:
    """PEM text given directly, or read from a file path."""
    if isinstance(value, bytes):
        return value.decode(encoding="ascii")
    text = os.fspath(value)
    if "-----BEGIN" in text:
        return text
    with open(file=text, encoding="ascii") as f:
        return f.read()


class Cursor(_BaseCursor):
    """Hardcharger cursor.

    Two DB-API niceties on top of the driver manager's cursor: after a statement without a
    result set (DDL, DML) ``description`` is ``None``, and fetching returns an empty result
    (``None``/``[]``) instead of raising, as ``sqlite3`` and ``duckdb`` do. Fetching before any
    ``execute()`` still raises. ``execute_update()`` returns the rows affected directly.
    """

    _executed_without_result_set = False

    def execute(self, operation, parameters=None):
        """Execute a statement. Returns this cursor."""
        self._executed_without_result_set = False
        super().execute(operation=operation, parameters=parameters)
        if self._results is not None and len(self._results.description) == 0:
            self._results.close()
            self._results = None
            self._executed_without_result_set = True
        return self

    def execute_update(self, query: str) -> int:
        """Execute a statement without a result set; return the rows affected (-1 when the
        server does not report a count, e.g. for DDL)."""
        self.adbc_statement.set_sql_query(query)
        rowcount = self.adbc_statement.execute_update()
        self._rowcount = rowcount
        self._results = None
        self._last_query = query
        self._executed_without_result_set = True
        return rowcount

    def fetchone(self):
        if self._results is None and self._executed_without_result_set:
            return None
        return super().fetchone()

    def fetchmany(self, size=None):
        if self._results is None and self._executed_without_result_set:
            return []
        if size is None:
            return super().fetchmany()
        return super().fetchmany(size=size)

    def fetchall(self):
        if self._results is None and self._executed_without_result_set:
            return []
        return super().fetchall()


class Connection(_BaseConnection):
    """A Hardcharger connection: helpers for session settings and tags, through the driver's
    ``adbc.hardcharger.*`` connection options."""

    def cursor(self, *, adbc_stmt_kwargs: Optional[dict[str, Any]] = None) -> Cursor:
        """Create a new :class:`Cursor`."""
        cursor = Cursor(conn=self, adbc_stmt_kwargs=adbc_stmt_kwargs, dbapi_backend=self._backend)
        self._cursors.add(cursor)
        return cursor

    def _scalar(self, sql: str) -> Any:
        with self.cursor() as cur:
            cur.execute(operation=sql)
            row = cur.fetchone()
            return None if row is None else row[0]

    def set_setting(
        self, name: str, value: SettingValue, *, scope: Literal["session", "global"] = "session"
    ) -> None:
        """``SET [SESSION | GLOBAL] hardcharger.<name> = <value>``.

        Session settings (``query_timeout``, ...) apply to this connection's session; global
        ones (``max_concurrent_statements``, ...) to the whole server, and only an admin may
        set them.
        """
        if scope == "session":
            prefix = ConnectionOptions.SETTING_PREFIX.value
        elif scope == "global":
            prefix = ConnectionOptions.GLOBAL_SETTING_PREFIX.value
        else:
            raise ValueError(f"scope must be 'session' or 'global', not {scope!r}")
        text = _setting_text(value=value)
        if text == "":
            raise ValueError("a setting's value can't be empty: use reset_setting()")
        self.adbc_connection.set_options(**{prefix + _bare(name=name): text})

    def reset_setting(self, name: str, *, scope: Literal["session", "global"] = "session") -> None:
        """``RESET [SESSION | GLOBAL] hardcharger.<name>``: back to the server's value."""
        prefix = (
            ConnectionOptions.GLOBAL_SETTING_PREFIX.value
            if scope == "global"
            else ConnectionOptions.SETTING_PREFIX.value
        )
        self.adbc_connection.set_options(**{prefix + _bare(name=name): ""})

    def get_setting(self, name: str) -> str:
        """The value of ``hardcharger.<name>`` that applies to this session, as text.

        Raises:
            KeyError: when the server has no such setting, or it is admin-only and this
                session's user is not an admin.
        """
        key = ConnectionOptions.SETTING_PREFIX.value + _bare(name=name)
        try:
            return self.adbc_connection.get_option(key=key)
        except adbc_driver_manager.Error as error:
            if error.status_code == adbc_driver_manager.AdbcStatusCode.NOT_FOUND:
                raise KeyError(str(error)) from None
            raise

    def settings(self):
        """Every setting visible to this session (``hardcharger_settings()``), as an Arrow table."""
        with self.cursor() as cur:
            cur.execute(operation="SELECT * FROM hardcharger_settings() ORDER BY name")
            return cur.fetch_arrow_table()

    def set_query_tag(self, tag: Optional[Tag]) -> None:
        """Tag this session's following statements (``hardcharger.query_tag``), a JSON object
        Hardcharger records with each statement in its instrumentation. ``None`` removes it."""
        self.adbc_connection.set_options(**{ConnectionOptions.QUERY_TAG.value: _tag_text(tag=tag)})

    def set_session_tag(self, tag: Optional[Tag]) -> None:
        """Tag this session (``hardcharger.session_tag``), a JSON object; ``None`` removes it."""
        self.adbc_connection.set_options(
            **{ConnectionOptions.SESSION_TAG.value: _tag_text(tag=tag)}
        )

    def current_user(self) -> str:
        """The signed-in user (``HARDCHARGER_USER()``)."""
        return self._scalar(sql="SELECT HARDCHARGER_USER()")

    @property
    def session_id(self) -> str:
        """This connection's server session id (``adbc.warp.session.id``), for another
        process to attach to with ``connect(session_id=...)``."""
        return self.adbc_connection.get_option(key=ConnectionOptions.SESSION_ID.value)

    def current_session(self) -> str:
        """This connection's server session id (``HARDCHARGER_CURRENT_SESSION()``)."""
        return self._scalar(sql="SELECT CAST(HARDCHARGER_CURRENT_SESSION() AS VARCHAR)")

    def server_version(self) -> str:
        """The server's version, e.g. ``0.2.0`` (``HARDCHARGER_VERSION()``)."""
        return self._scalar(sql="SELECT HARDCHARGER_VERSION()")


def connect(
    uri: Optional[str] = None,
    *,
    profile: Optional[str] = None,
    username: Optional[str] = None,
    password: Optional[str] = None,
    access_token: Optional[str] = None,
    subject_token: Optional[str] = None,
    auth_type: Literal["password", "external", "client_certificate", "mtls"] = "password",
    oauth_timeout: Optional[float] = None,
    open_browser: Union[bool, str] = True,
    share_sign_in: Union[bool, str] = False,
    session_tag: Optional[Tag] = None,
    query_tag: Optional[Tag] = None,
    settings: Optional[Mapping[str, SettingValue]] = None,
    tls_skip_verify: bool = False,
    tls_root_certs: Optional[PemSource] = None,
    tls_client_cert: Optional[PemSource] = None,
    tls_client_key: Optional[PemSource] = None,
    tls_server_name: Optional[str] = None,
    session_id: Optional[str] = None,
    close_session_on_release: Optional[bool] = None,
    catalog: Optional[str] = None,
    db_schema: Optional[str] = None,
    db_kwargs: Optional[dict[str, str]] = None,
    conn_kwargs: Optional[dict[str, str]] = None,
    autocommit: bool = True,
) -> Connection:
    """Connect to a Hardcharger server (DB-API 2.0).

    Args:
        uri: ``hardcharger://host[:port][?tls=false]``: HTTPS unless ``?tls=false``, port 31337
            unless given. ``warp://``, ``https://`` and ``http://`` URIs work too, as does
            ``profile://<name>``.
        profile: Name (or absolute path) of an ADBC connection profile.
        username: User name for password sign-in.
        password: Password for password sign-in.
        access_token: A Hardcharger access token obtained elsewhere.
        subject_token: An identity provider's token (an OIDC ID token or other JWT the server
            trusts: ``--token-jwks-uri`` or ``--token-signature-verify-cert-path``, with its
            issuer and audience) that the driver exchanges for a Hardcharger access token
            (the ``token_exchange`` grant). Not with ``username``, ``password`` or
            ``access_token``, nor with ``auth_type`` ``"external"`` or
            ``"client_certificate"``.
        auth_type: ``"password"`` (default); ``"external"``: sign in through a browser with
            the server's identity provider (``hardcharger-server --oauth-client-id``; the
            server lists the Warp SQL capability ``oauth_browser``); or
            ``"client_certificate"`` (``"mtls"``): sign in with the TLS client certificate
            (``tls_client_cert`` and ``tls_client_key``) and no password, on a server with
            ``--mtls-ca-cert`` and ``--mtls-certificate-sign-in``. The token is bound to the
            certificate.
        oauth_timeout: Seconds to wait for the browser sign-in (default 300).
        open_browser: ``True`` opens the system browser (or ``$HARDCHARGER_BROWSER``),
            ``False`` prints the URL to stderr, and a string is the command to open it with
            (the URL is appended).
        share_sign_in: For ``auth_type="external"``: ``True`` shares one browser sign-in among
            every connection this process opens to the same server (same TLS trust) with
            ``share_sign_in=True``, instead of one browser window per connection. The first
            connection signs in (connections opened meanwhile wait for it); the others reuse
            the server's access token it got, until shortly before it expires, when the next
            connection signs in again. A
            string shares under that name, for a process that signs in as more than one person
            or role: only connections with the same name share. The token stays in this
            process's memory and never goes to another server.
        session_tag: A JSON object tagging the session (``hardcharger.session_tag``).
        query_tag: A JSON object tagging the session's statements (``hardcharger.query_tag``).
        settings: ``hardcharger.<name>`` session settings to apply, e.g.
            ``{"query_timeout": 60}``.
        tls_skip_verify: Do not verify the server's certificate (development only).
        tls_root_certs: CA certificates to trust, as PEM text or a file path.
        tls_client_cert: Client certificate chain for mutual TLS (PEM text or path).
        tls_client_key: The client certificate's private key (PEM text or path).
        tls_server_name: The server's host name for TLS, when it isn't the URI's (connecting
            by IP address, or through a tunnel or load balancer): the driver sends it as the
            TLS server name (SNI) and checks the server's certificate against it.
        session_id: Attach to this existing session (``Connection.session_id`` of another
            connection, of this or another process) instead of creating one: its open
            transaction, temporary tables and settings carry on here. Sign in as the
            session's user (typically with the same ``access_token``); a session the server
            doesn't have, or another user's, is refused with ``session_not_found``. The
            session lives on the server instance that created it: behind a load balancer,
            this works only when routing reaches that instance.
        close_session_on_release: Whether closing the connection ends its session on the
            server. Default: ``True`` for a session the connection created, ``False`` for
            one it attached to (``session_id``), which then stays until a connection closes it
            with ``True`` or it idles out. ``False`` on the first connection hands its
            session, open transaction included, to the next.
        catalog: Catalog to make current for the session.
        db_schema: Schema to make current for the session.
        db_kwargs: Extra ADBC database options (``adbc.hardcharger.*``, ``adbc.warp.*``).
        conn_kwargs: Extra ADBC connection options.
        autocommit: Enable autocommit (default True).

    Returns:
        A DB-API 2.0 :class:`Connection`.
    """
    if uri is None and profile is None:
        raise ValueError("Must provide at least one of 'uri' or 'profile'.")
    if auth_type == "mtls":
        auth_type = "client_certificate"
    if auth_type not in ("password", "external", "client_certificate"):
        raise ValueError(
            f"invalid auth_type {auth_type!r}: expected 'password', 'external' or "
            "'client_certificate' ('mtls')"
        )
    if auth_type == "external" and (username or password or access_token or subject_token):
        raise ValueError(
            "auth_type='external' signs in through the browser: drop username, password, "
            "access_token and subject_token"
        )
    if subject_token is not None:
        if not subject_token:
            raise ValueError("subject_token can't be empty")
        if auth_type != "password":
            raise ValueError(
                f"subject_token signs in by token exchange: drop auth_type={auth_type!r}"
            )
    if tls_server_name is not None and not tls_server_name:
        raise ValueError("tls_server_name can't be empty")
    if session_id is not None and not session_id:
        raise ValueError("session_id can't be empty")
    if close_session_on_release is not None and not isinstance(close_session_on_release, bool):
        raise TypeError(
            "close_session_on_release must be a bool, not "
            f"{type(close_session_on_release).__name__}"
        )
    if not isinstance(share_sign_in, (bool, str)):
        raise TypeError(
            f"share_sign_in must be a bool or a str, not {type(share_sign_in).__name__}"
        )
    if share_sign_in is not False and auth_type != "external":
        raise ValueError("share_sign_in shares a browser sign-in: it needs auth_type='external'")

    db_kwargs = dict(db_kwargs or {})
    conn_kwargs = dict(conn_kwargs or {})
    options = DatabaseOptions

    def put(key: str, value: Optional[str]) -> None:
        if value is not None:
            db_kwargs.setdefault(key, value)

    def put_only(key: str, value: Optional[str], name: str) -> None:
        """put(), refusing a different value for the same option in db_kwargs."""
        if value is not None and db_kwargs.get(key, value) != value:
            raise ValueError(f"{name}= and db_kwargs[{key!r}] disagree: give one of them")
        put(key=key, value=value)

    if uri is not None and uri.startswith("profile://"):
        put(key="profile", value=uri[len("profile://") :])
    else:
        put(key="uri", value=uri)
    put(key="profile", value=profile)
    put(key="username", value=username)
    put(key="password", value=password)
    put(key=options.ACCESS_TOKEN.value, value=access_token)
    put_only(key=options.SUBJECT_TOKEN.value, value=subject_token, name="subject_token")
    if db_kwargs.get(options.SUBJECT_TOKEN.value):
        # The driver would pick one credential silently (an access token over the subject
        # token, the subject token over a password): refuse the mix instead.
        given = [
            key
            for key in ("username", "password", options.ACCESS_TOKEN.value)
            if db_kwargs.get(key)
        ]
        if given:
            raise ValueError(
                f"subject_token ({options.SUBJECT_TOKEN.value}) signs in by token exchange: "
                f"drop {', '.join(given)}"
            )
    if auth_type != "password":
        put(key=options.AUTH_TYPE.value, value=auth_type)
    put(
        key=options.OAUTH_TIMEOUT_SECONDS.value,
        value=None if oauth_timeout is None else str(oauth_timeout),
    )
    if open_browser is False:
        put(key=options.OAUTH_BROWSER.value, value="none")
    elif isinstance(open_browser, str):
        put(key=options.OAUTH_BROWSER.value, value=open_browser)
    if share_sign_in is not False:
        put(key=options.OAUTH_SHARE_SIGN_IN.value, value="true")
        if isinstance(share_sign_in, str):
            put(key=options.OAUTH_SIGN_IN_KEY.value, value=share_sign_in)
    if session_tag is not None:
        put(key=options.SESSION_TAG.value, value=_tag_text(tag=session_tag))
    if query_tag is not None:
        put(key=options.QUERY_TAG.value, value=_tag_text(tag=query_tag))
    for name, value in (settings or {}).items():
        put(key=options.SETTING_PREFIX.value + _bare(name=name), value=_setting_text(value=value))
    if tls_skip_verify:
        put(key=options.TLS_SKIP_VERIFY.value, value="true")
    for key, value in (
        (options.TLS_ROOT_CERTS.value, tls_root_certs),
        (options.TLS_CLIENT_CERT.value, tls_client_cert),
        (options.TLS_CLIENT_KEY.value, tls_client_key),
    ):
        if value is not None:
            put(key=key, value=_pem(value=value))
    put_only(key=options.TLS_SERVER_NAME.value, value=tls_server_name, name="tls_server_name")
    put_only(key=options.SESSION_ID.value, value=session_id, name="session_id")
    put_only(
        key=options.SESSION_CLOSE_ON_RELEASE.value,
        value=None
        if close_session_on_release is None
        else _setting_text(value=close_session_on_release),
        name="close_session_on_release",
    )

    standard = adbc_driver_manager.ConnectionOptions
    if catalog is not None:
        conn_kwargs.setdefault(standard.CURRENT_CATALOG.value, catalog)
    if db_schema is not None:
        conn_kwargs.setdefault(standard.CURRENT_DB_SCHEMA.value, db_schema)

    db = None
    conn = None
    try:
        db = adbc_driver_manager.AdbcDatabase(driver=_driver_path(), **db_kwargs)
        conn = adbc_driver_manager.AdbcConnection(database=db, **conn_kwargs)
        return Connection(db=db, conn=conn, autocommit=autocommit)
    except Exception:
        if conn:
            conn.close()
        if db:
            db.close()
        raise
