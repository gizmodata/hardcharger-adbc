"""Option keys: Hardcharger's (``adbc.hardcharger.*``) and the Chargewire driver's
(``adbc.chargewire.*``), which the Hardcharger driver is built on."""

import enum


class DatabaseOptions(enum.Enum):
    """Database options. The standard ``uri``, ``username`` and ``password``
    options and the ``adbc.telemetry.*`` options apply too."""

    #: "password" (the default); "external": sign in through a browser with the server's
    #: identity provider when the first connection opens (on the server's own port);
    #: or "client_certificate" ("mtls"): sign in with the TLS client certificate
    #: (TLS_CLIENT_CERT and TLS_CLIENT_KEY) and no password (``adbc.chargewire.auth.type``).
    AUTH_TYPE = "adbc.hardcharger.auth_type"
    #: Seconds to wait for the browser sign-in (default 300).
    OAUTH_TIMEOUT_SECONDS = "adbc.hardcharger.oauth.timeout_seconds"
    #: Command that opens the sign-in page (URL appended); "none" prints the URL instead.
    OAUTH_BROWSER = "adbc.hardcharger.oauth.browser"
    #: "true": share one browser sign-in among this process's databases that sign in to the
    #: same server (same TLS trust and SIGN_IN_KEY); "false" (the default):
    #: each database signs in on its own.
    OAUTH_SHARE_SIGN_IN = "adbc.hardcharger.oauth.share_sign_in"
    #: Names a shared sign-in (default ""): databases share one only when their keys match.
    OAUTH_SIGN_IN_KEY = "adbc.hardcharger.oauth.sign_in_key"
    #: A JSON object tagging every session (hardcharger.session_tag).
    SESSION_TAG = "adbc.hardcharger.session_tag"
    #: A JSON object recorded with every statement (hardcharger.query_tag).
    QUERY_TAG = "adbc.hardcharger.query_tag"
    #: Prefix: ``SETTING_PREFIX + "query_timeout"`` sets hardcharger.query_timeout on every session.
    SETTING_PREFIX = "adbc.hardcharger.setting."
    #: The most connections one bulk ingest is sent over at once (default 8; 1: one ordered
    #: stream): ``adbc.chargewire.ingest.parts`` under Hardcharger's name. Also a connection option.
    INGEST_PARTS = "adbc.hardcharger.ingest.parts"

    #: Do not verify the server's TLS certificate (development only).
    TLS_SKIP_VERIFY = "adbc.chargewire.tls.skip_verify"
    #: PEM-encoded CA certificates to trust instead of the system's.
    TLS_ROOT_CERTS = "adbc.chargewire.tls.root_certs"
    #: PEM certificate chain presented to servers that require mutual TLS.
    TLS_CLIENT_CERT = "adbc.chargewire.tls.client_cert"
    #: PEM private key of the client certificate.
    TLS_CLIENT_KEY = "adbc.chargewire.tls.client_key"
    #: Host name the server certificate is checked against, instead of the URI's.
    TLS_SERVER_NAME = "adbc.chargewire.tls.server_name"
    #: A Chargewire access token obtained elsewhere (used as is, never renewed by the driver).
    #: Readable too: the token in use (a bearer secret; see ``Connection.access_token``).
    ACCESS_TOKEN = "adbc.chargewire.auth.access_token"
    #: Read-only: when the token in use expires, RFC 3339 UTC ("" when unknown).
    ACCESS_TOKEN_EXPIRES_AT = "adbc.chargewire.auth.access_token_expires_at"
    #: An identity provider's token, exchanged for an access token.
    SUBJECT_TOKEN = "adbc.chargewire.auth.subject_token"
    #: How often a request that is safe to repeat is sent, first try included.
    RETRY_MAX_ATTEMPTS = "adbc.chargewire.retry.max_attempts"
    #: Replace a session the server lost with a new one ("true", the default).
    SESSION_RECONNECT = "adbc.chargewire.session.reconnect"
    #: Attach every connection to this existing session instead of creating one (it must be
    #: the signed-in user's): its open transaction, temporary tables and settings carry on.
    SESSION_ID = "adbc.chargewire.session.id"
    #: "true"/"false": whether closing a connection ends its session on the server. Default:
    #: "true" for a session the connection created, "false" for one it attached to.
    SESSION_CLOSE_ON_RELEASE = "adbc.chargewire.session.close_on_release"


class ConnectionOptions(enum.Enum):
    """Connection options."""

    #: The session's tag (read and write; "" removes it).
    SESSION_TAG = "adbc.hardcharger.session_tag"
    #: The tag of the session's following statements (read and write; "" removes it).
    QUERY_TAG = "adbc.hardcharger.query_tag"
    #: Prefix: read or SET SESSION hardcharger.NAME ("" resets it).
    SETTING_PREFIX = "adbc.hardcharger.setting."
    #: Prefix: SET GLOBAL hardcharger.NAME, admins only ("" resets it).
    GLOBAL_SETTING_PREFIX = "adbc.hardcharger.global_setting."
    #: The most connections one bulk ingest is sent over at once (``adbc.chargewire.ingest.parts``).
    INGEST_PARTS = "adbc.hardcharger.ingest.parts"

    #: The connection's current server session id (read-only; to attach to a session, use
    #: the database option).
    SESSION_ID = "adbc.chargewire.session.id"
    #: "true"/"false": whether closing this connection ends its session on the server (read
    #: and write).
    SESSION_CLOSE_ON_RELEASE = "adbc.chargewire.session.close_on_release"
    #: The access token the connection sends (read-only here): renewed first when due, so it
    #: is the current one. A bearer secret: store it encrypted, never log it.
    ACCESS_TOKEN = "adbc.chargewire.auth.access_token"
    #: When that token expires, RFC 3339 UTC ("" when unknown; read-only).
    ACCESS_TOKEN_EXPIRES_AT = "adbc.chargewire.auth.access_token_expires_at"
    #: How often the connection's session was lost and replaced.
    SESSION_REPLACEMENTS = "adbc.chargewire.session.replacements"
    #: The session's transaction as the server reports it (read-only): "none", "open",
    #: "aborted", or "unknown" (a value the driver doesn't know). It sees SQL ``BEGIN`` and
    #: ``COMMIT`` too. Reading it costs a request.
    SESSION_TRANSACTION = "adbc.chargewire.session.transaction"


class StatementOptions(enum.Enum):
    """Statement options. They can also be set on the database or the
    connection, as defaults for the statements created from it."""

    #: Run queries asynchronously, with a retained, resumable result.
    ASYNC = "adbc.chargewire.statement.async"
    #: Ask the server to keep results for re-reading ("true"/"false").
    RETAIN_RESULT = "adbc.chargewire.statement.retain_result"
    #: Seconds after which the server cancels a statement.
    TIMEOUT_SECONDS = "adbc.chargewire.statement.timeout_seconds"
    #: A JSON object recorded with every statement and ingest on the server
    #: (``Chargewire-Query-Tag``): sent compact and in ASCII, at most 4,096 bytes so.
    QUERY_TAG = "adbc.chargewire.statement.query_tag"
    #: Arrow buffer compression for results and uploads: "zstd" or "none".
    IPC_COMPRESSION = "adbc.chargewire.ipc_compression"
    #: The most partitions a result may be split into ("8", the default; "1": never). A
    #: server that offers it splits a large result, and the driver reads every partition at
    #: once, each over its own connection. A result of fewer than two batches comes as one
    #: stream either way.
    RESULT_PARTITIONS = "adbc.chargewire.statement.result_partitions"
    #: "preserve" (the default): a split result comes back in order; "any": in whatever
    #: order its batches arrive.
    RESULT_ORDER = "adbc.chargewire.statement.result_order"
    #: The most connections one bulk ingest is sent over at once ("8", the default; "1":
    #: one stream). An upload of two batches or more is then a partitioned ingest, committed
    #: as one; its rows land in no defined order, so set "1" to keep the upload's order.
    INGEST_PARTS = "adbc.chargewire.ingest.parts"
    #: The id of the statement's last execution (read-only).
    STATEMENT_ID = "adbc.chargewire.statement.id"
