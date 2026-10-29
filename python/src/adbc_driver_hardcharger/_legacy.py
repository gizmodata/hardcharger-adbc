"""Option keys from before the protocol was renamed to Chargewire keep working.

:func:`current_options` reads them as their current spellings before ``connect()`` looks at
them, so its own checks (a credential given twice, or two ways) see them too. The driver
library reads them as well, and the old URI scheme. This file is the only place that knows
the old names.
"""

from __future__ import annotations

from collections.abc import Mapping

_LEGACY_PREFIX = "adbc.warp."
_CURRENT_PREFIX = "adbc.chargewire."


def current_options(options: Mapping[str, str]) -> dict[str, str]:
    """``options`` with every old option key replaced by the current one. When both
    spellings of a key are given, the current one wins."""
    out: dict[str, str] = {}
    for key, value in options.items():
        if isinstance(key, str) and key.startswith(_LEGACY_PREFIX):
            current = _CURRENT_PREFIX + key[len(_LEGACY_PREFIX) :]
            if current not in options:
                out[current] = value
            continue
        out[key] = value
    return out
