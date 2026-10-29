"""ADBC driver for Hardcharger, GizmoData's DuckDB 2.0 server.

Python bindings over the native Go driver (``libadbc_driver_hardcharger``), which is built on
the Chargewire ADBC driver and adds ``hardcharger://`` URIs with Hardcharger's port 31337, query
and session tags, ``hardcharger.*`` settings and browser sign-in, for every ADBC language.

Quick start::

    from adbc_driver_hardcharger import dbapi as hardcharger

    with hardcharger.connect(uri="hardcharger://localhost?tls=false", username="hardcharger",
                             password="change-me") as conn:
        with conn.cursor() as cur:
            cur.execute(operation="SELECT 42 AS answer")
            print(cur.fetch_arrow_table())
"""

from __future__ import annotations

from ._options import ConnectionOptions, DatabaseOptions, StatementOptions
from ._version import __version__

#: Hardcharger's Chargewire port, used when a hardcharger:// URI names none.
DEFAULT_PORT = 31337

__all__ = [
    "DEFAULT_PORT",
    "ConnectionOptions",
    "DatabaseOptions",
    "StatementOptions",
    "__version__",
]
