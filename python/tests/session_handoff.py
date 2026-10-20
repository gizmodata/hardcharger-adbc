"""Process 1 of the session hand-off test, run as its own process:
``python session_handoff.py URI TOKEN`` (the access token in the environment variable named by
TOKEN) begins a transaction, inserts a row into ``handoff`` without committing, prints the
session id, and exits with the session still open on the server (``close_session_on_release``
false)."""

import os
import sys

from adbc_driver_hardcharger import dbapi as hardcharger

uri, token_variable = sys.argv[1], sys.argv[2]
with hardcharger.connect(
    uri=uri, access_token=os.environ[token_variable], close_session_on_release=False
) as conn:
    with conn.cursor() as cur:
        cur.execute(operation="BEGIN TRANSACTION")
        cur.execute(operation="INSERT INTO handoff VALUES (1)")
    print(conn.session_id, flush=True)
