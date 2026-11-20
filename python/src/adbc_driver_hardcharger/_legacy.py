"""Option names from before the ADBC driver style guide keep working.

The style guide (https://adbc-drivers.org/building-drivers/style-guide.html) reserves the
``adbc.`` prefix for the ADBC specification's own options, so Hardcharger's options are
``hardcharger.*``, and the Chargewire driver's, which the Hardcharger driver is built on, are
``hardcharger.*`` here too. :func:`current_options` reads the old names (``adbc.hardcharger.*``,
``adbc.chargewire.*`` and, from before the protocol's rename, ``adbc.warp.*``) and the Chargewire
driver's own ``chargewire.*`` names as the current ones before ``connect()`` looks at them, so its
own checks (a credential given twice, or two ways) see them too. An old name prints a
deprecation warning to stderr the first time a process uses it, as the driver library does for
options set on it directly. This file is the only place in the package that knows the old names.
"""

from __future__ import annotations

import sys
import threading
from collections.abc import Mapping

_PREFIX = "hardcharger."
_OLD_OWN_PREFIX = "adbc.hardcharger."
_OLD_CHARGEWIRE_PREFIXES = ("adbc.chargewire.", "adbc.warp.")
_CHARGEWIRE_PREFIX = "chargewire."

# Hardcharger's own options from before the style guide: the part after adbc.hardcharger.
# mapped to the part after hardcharger. (setting.NAME and global_setting.NAME keep theirs).
_OWN_RENAMED = {
    "auth_type": "auth.type",
    "oauth.timeout_seconds": "oauth.timeout_seconds",
    "oauth.browser": "oauth.browser",
    "oauth.share_sign_in": "oauth.share_sign_in",
    "oauth.sign_in_key": "oauth.sign_in_key",
    "session_tag": "session.tag",
    "query_tag": "session.query_tag",
    "ingest.parts": "ingest.parts",
    "resume_timeout": "retry.resume_timeout_seconds",
}

# The Chargewire driver's options: the part after its old prefixes mapped to the part after
# its current one (and after hardcharger. here).
_CHARGEWIRE_RENAMED = {
    "tls.skip_verify": "tls.skip_verify",
    "tls.root_certs": "tls.root_certs",
    "tls.client_cert": "tls.client_cert",
    "tls.client_key": "tls.client_key",
    "tls.server_name": "tls.server_name",
    "auth.type": "auth.type",
    "oauth.browser": "oauth.browser",
    "oauth.timeout_seconds": "oauth.timeout_seconds",
    "auth.access_token": "auth.access_token",
    "auth.access_token_expires_at": "auth.access_token_expires_at",
    "auth.subject_token": "auth.subject_token",
    "retry.max_attempts": "retry.max_attempts",
    "resume_timeout": "retry.resume_timeout_seconds",
    "session.reconnect": "session.reconnect",
    "session.close_on_release": "session.close_on_release",
    "session.id": "session.id",
    "session.replacements": "session.replacements",
    "session.transaction": "session.transaction",
    "statement.async": "query.async",
    "statement.retain_result": "query.retain_result",
    "statement.timeout_seconds": "query.timeout_seconds",
    "statement.query_tag": "query.tag",
    "ipc_compression": "ipc.compression",
    "statement.result_partitions": "query.result_partitions",
    "statement.result_order": "query.result_order",
    "ingest.parts": "ingest.parts",
    "statement.id": "query.id",
}
_CHARGEWIRE_CURRENT = frozenset(_CHARGEWIRE_RENAMED.values())
# The Chargewire driver's options whose name, under hardcharger., is one of Hardcharger's own
# with another meaning (its auth types are password, external, client_certificate): they stay
# chargewire.X.
_CHARGEWIRE_ONLY = frozenset({"auth.type", "oauth.browser", "oauth.timeout_seconds"})

_warned: set[str] = set()
_warned_lock = threading.Lock()


def _warn(old: str, current: str) -> None:
    """Once per process for each old name: say which name to use. Keys only, never values."""
    with _warned_lock:
        if old in _warned:
            return
        _warned.add(old)
    sys.stderr.write(f'[hardcharger] option "{old}" is deprecated; use "{current}"\n')


def current_key(key: str) -> tuple[str, int]:
    """The current name of ``key`` and how it was spelled: 0 Hardcharger's current name (or
    not an option of ours), 1 the Chargewire driver's ``chargewire.*``, 2 an old name (which
    warns). A key under an old prefix that names no option is returned as given, so the
    driver's error names what the caller wrote."""
    if not isinstance(key, str):
        return key, 0
    if key.startswith(_OLD_OWN_PREFIX):
        rest = key[len(_OLD_OWN_PREFIX) :]
        current = None
        if rest in _OWN_RENAMED:
            current = _PREFIX + _OWN_RENAMED[rest]
        else:
            for prefix in ("setting.", "global_setting."):
                if rest.startswith(prefix) and len(rest) > len(prefix):
                    current = _PREFIX + rest
        if current is not None:
            _warn(old=key, current=current)
            return current, 2
        return key, 0
    for prefix in _OLD_CHARGEWIRE_PREFIXES:
        if key.startswith(prefix):
            rest = key[len(prefix) :]
            # The old prefix with either the old part after it or the current one
            # (adbc.chargewire.retry.resume_timeout_seconds), as the driver reads them.
            renamed = _CHARGEWIRE_RENAMED.get(rest, rest if rest in _CHARGEWIRE_CURRENT else None)
            if renamed is not None:
                if renamed in _CHARGEWIRE_ONLY:
                    current = _CHARGEWIRE_PREFIX + renamed
                    _warn(old=key, current=current)
                    return current, 2
                current = _PREFIX + renamed
                _warn(old=key, current=current)
                return current, 2
            return key, 0
    rest = key[len(_CHARGEWIRE_PREFIX) :]
    if (
        key.startswith(_CHARGEWIRE_PREFIX)
        and rest in _CHARGEWIRE_CURRENT
        and rest not in _CHARGEWIRE_ONLY
    ):
        return _PREFIX + key[len(_CHARGEWIRE_PREFIX) :], 1
    return key, 0


def current_options(options: Mapping[str, str]) -> dict[str, str]:
    """``options`` under their current names. When several keys name one option,
    Hardcharger's current name wins over the Chargewire driver's, and either over an old
    name (which still warns), whatever the order they were given in."""
    out: dict[str, str] = {}
    rank: dict[str, int] = {}
    for key in sorted(options, key=str):
        current, spelled = current_key(key=key)
        if current in rank and rank[current] <= spelled:
            continue
        out[current] = options[key]
        rank[current] = spelled
    return out
