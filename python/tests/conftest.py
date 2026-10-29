"""Shared fixtures.

Integration tests (marked ``integration``) run against real ``hardcharger-server`` processes
started from ``HARDCHARGER_ADBC_TEST_SERVER_BINARY`` on free loopback ports, each with a fresh
admin password containing ``:`` and ``@``. Without the variable they fail; they never skip.
The servers get a clean environment: no ``HARDCHARGER_*`` variable from the shell running the
tests reaches them, so a developer's real settings or credentials can't leak in, and each
has a ``HOME`` of its own (its work directory), never the user's.
"""

from __future__ import annotations

import dataclasses
import datetime
import os
import secrets
import signal
import socket
import ssl
import subprocess
import time
import urllib.error
import urllib.request
from pathlib import Path

import pytest

SERVER_BINARY_ENV = "HARDCHARGER_ADBC_TEST_SERVER_BINARY"
USERNAME = "hc_adbc_test"


@dataclasses.dataclass
class Server:
    host: str
    port: int
    password: str
    process: subprocess.Popen
    log: Path
    tls: bool = False

    @property
    def base_url(self) -> str:
        return f"{'https' if self.tls else 'http'}://{self.host}:{self.port}"

    @property
    def uri(self) -> str:
        """The server's hardcharger:// URI (plain HTTP unless it serves TLS)."""
        if self.tls:
            return f"hardcharger://{self.host}:{self.port}"
        return f"hardcharger://{self.host}:{self.port}?tls=false"

    def log_tail(self, lines: int = 40) -> str:
        try:
            return "\n".join(self.log.read_text(errors="replace").splitlines()[-lines:])
        except OSError:
            return ""


def free_port() -> int:
    with socket.socket(family=socket.AF_INET, type=socket.SOCK_STREAM) as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


def server_binary() -> str:
    binary = os.environ.get(SERVER_BINARY_ENV)
    if not binary:
        pytest.fail(
            reason=f"set {SERVER_BINARY_ENV} to a hardcharger-server binary to run the "
            "integration tests"
        )
    if not os.access(path=binary, mode=os.X_OK):
        pytest.fail(reason=f"{SERVER_BINARY_ENV}={binary} is not an executable")
    return binary


def clean_environment() -> dict[str, str]:
    return {key: value for key, value in os.environ.items() if not key.startswith("HARDCHARGER_")}


def _wait_up(server: Server, timeout: float = 30) -> None:
    context = ssl.create_default_context()
    context.check_hostname = False
    context.verify_mode = ssl.CERT_NONE
    deadline = time.monotonic() + timeout
    last_error: Exception | None = None
    while time.monotonic() < deadline:
        if server.process.poll() is not None:
            break
        try:
            with urllib.request.urlopen(
                url=f"{server.base_url}/chargewire", timeout=2, context=context
            ):
                return
        except (urllib.error.URLError, OSError) as error:
            if "CERTIFICATE_REQUIRED" in str(error):
                return  # it requires client certificates and answered the handshake
            last_error = error
        time.sleep(0.1)
    stop_server(server=server)
    pytest.fail(
        reason=f"hardcharger-server did not answer at {server.base_url}/chargewire "
        f"({last_error}); its log:\n{server.log_tail()}"
    )


def start_server(workdir: Path, extra_args: list[str] | None = None) -> Server:
    """Start hardcharger-server on a free port, in ``workdir``, with an in-memory database
    and DuckDB's persistent secrets in ``workdir`` (never ~/.duckdb's). ``{base_url}`` in an
    argument is the server's own URL."""
    binary = server_binary()
    workdir.mkdir(parents=True, exist_ok=True)
    port = free_port()
    extra_args = [
        arg.replace("{base_url}", f"http://127.0.0.1:{port}") for arg in (extra_args or [])
    ]
    password = secrets.token_urlsafe(nbytes=12) + ":p@ss:word"
    log = workdir / "hardcharger-server.log"
    with log.open(mode="ab") as out:
        process = subprocess.Popen(
            args=[
                binary,
                "--host",
                "127.0.0.1",
                "--port",
                str(port),
                "--health-port",
                "0",
                "--database",
                ":memory:",
                "--admin-username",
                USERNAME,
                "--init-sql-commands",
                f"SET secret_directory = '{(workdir / 'secrets').as_posix()}'",
                *extra_args,
            ],
            env={
                **clean_environment(),
                "HARDCHARGER_ADMIN_PASSWORD": password,
                "HOME": str(workdir),
            },
            cwd=workdir,
            stdout=out,
            stderr=subprocess.STDOUT,
        )
    server = Server(
        host="127.0.0.1",
        port=port,
        password=password,
        process=process,
        log=log,
        tls="--tls-cert" in extra_args,
    )
    _wait_up(server=server)
    return server


def stop_server(server: Server) -> None:
    if server.process.poll() is not None:
        return
    server.process.send_signal(signal.SIGTERM)
    server.process.send_signal(signal.SIGTERM)  # the second signal stops without draining
    try:
        server.process.wait(timeout=20)
    except subprocess.TimeoutExpired:
        server.process.kill()


@pytest.fixture(scope="session")
def hc_server(tmp_path_factory):
    """A plain-HTTP server shared by the tests that need nothing special."""
    server = start_server(workdir=tmp_path_factory.mktemp("plain"))
    yield server
    stop_server(server=server)


@pytest.fixture
def conn(hc_server):
    from adbc_driver_hardcharger import dbapi as hardcharger

    with hardcharger.connect(
        uri=hc_server.uri, username=USERNAME, password=hc_server.password
    ) as connection:
        yield connection


def make_certificates(
    out_dir: Path,
    clients: dict[str, str] | None = None,
    server_dns_names: list[str] | None = None,
) -> dict[str, Path]:
    """A throwaway CA, and a server certificate for 127.0.0.1/localhost it signed (or, with
    ``server_dns_names``, for those DNS names alone: no IP address).

    ``clients`` maps a name to an e-mail address: for each, a client certificate whose identity
    is that e-mail SAN, at ``paths[name]`` with its key at ``paths[name + "-key"]``.
    """
    import ipaddress

    from cryptography import x509
    from cryptography.hazmat.primitives import hashes, serialization
    from cryptography.hazmat.primitives.asymmetric import ec
    from cryptography.x509.oid import NameOID

    now = datetime.datetime.now(tz=datetime.timezone.utc)

    def name(common_name: str) -> x509.Name:
        return x509.Name(
            attributes=[x509.NameAttribute(oid=NameOID.COMMON_NAME, value=common_name)]
        )

    ca_key = ec.generate_private_key(curve=ec.SECP256R1())
    ca_cert = (
        x509.CertificateBuilder()
        .subject_name(name=name(common_name="hardcharger-adbc test CA"))
        .issuer_name(name=name(common_name="hardcharger-adbc test CA"))
        .public_key(key=ca_key.public_key())
        .serial_number(number=x509.random_serial_number())
        .not_valid_before(time=now - datetime.timedelta(minutes=5))
        .not_valid_after(time=now + datetime.timedelta(days=1))
        .add_extension(extval=x509.BasicConstraints(ca=True, path_length=None), critical=True)
        .add_extension(
            extval=x509.KeyUsage(
                digital_signature=True,
                content_commitment=False,
                key_encipherment=False,
                data_encipherment=False,
                key_agreement=False,
                key_cert_sign=True,
                crl_sign=True,
                encipher_only=False,
                decipher_only=False,
            ),
            critical=True,
        )
        .add_extension(
            extval=x509.SubjectKeyIdentifier.from_public_key(public_key=ca_key.public_key()),
            critical=False,
        )
        .sign(private_key=ca_key, algorithm=hashes.SHA256())
    )
    server_key = ec.generate_private_key(curve=ec.SECP256R1())
    server_cert = (
        x509.CertificateBuilder()
        .subject_name(name=name(common_name=(server_dns_names or ["localhost"])[0]))
        .issuer_name(name=ca_cert.subject)
        .public_key(key=server_key.public_key())
        .serial_number(number=x509.random_serial_number())
        .not_valid_before(time=now - datetime.timedelta(minutes=5))
        .not_valid_after(time=now + datetime.timedelta(days=1))
        .add_extension(
            extval=x509.SubjectAlternativeName(
                general_names=(
                    [x509.DNSName(value=dns_name) for dns_name in server_dns_names]
                    if server_dns_names
                    else [
                        x509.DNSName(value="localhost"),
                        x509.IPAddress(value=ipaddress.ip_address(address="127.0.0.1")),
                    ]
                )
            ),
            critical=False,
        )
        .sign(private_key=ca_key, algorithm=hashes.SHA256())
    )
    paths = {
        "ca": out_dir / "ca.pem",
        "cert": out_dir / "server.pem",
        "key": out_dir / "server-key.pem",
    }
    paths["ca"].write_bytes(data=ca_cert.public_bytes(encoding=serialization.Encoding.PEM))
    paths["cert"].write_bytes(data=server_cert.public_bytes(encoding=serialization.Encoding.PEM))
    paths["key"].write_bytes(
        data=server_key.private_bytes(
            encoding=serialization.Encoding.PEM,
            format=serialization.PrivateFormat.PKCS8,
            encryption_algorithm=serialization.NoEncryption(),
        )
    )
    for client, email in (clients or {}).items():
        client_key = ec.generate_private_key(curve=ec.SECP256R1())
        client_cert = (
            x509.CertificateBuilder()
            .subject_name(name=name(common_name=client))
            .issuer_name(name=ca_cert.subject)
            .public_key(key=client_key.public_key())
            .serial_number(number=x509.random_serial_number())
            .not_valid_before(time=now - datetime.timedelta(minutes=5))
            .not_valid_after(time=now + datetime.timedelta(days=1))
            .add_extension(
                extval=x509.SubjectAlternativeName(general_names=[x509.RFC822Name(value=email)]),
                critical=False,
            )
            .add_extension(
                extval=x509.ExtendedKeyUsage(usages=[x509.oid.ExtendedKeyUsageOID.CLIENT_AUTH]),
                critical=False,
            )
            .add_extension(
                extval=x509.AuthorityKeyIdentifier.from_issuer_public_key(
                    public_key=ca_key.public_key()
                ),
                critical=False,
            )
            .sign(private_key=ca_key, algorithm=hashes.SHA256())
        )
        paths[client] = out_dir / f"{client}.pem"
        paths[client + "-key"] = out_dir / f"{client}-key.pem"
        paths[client].write_bytes(
            data=client_cert.public_bytes(encoding=serialization.Encoding.PEM)
        )
        paths[client + "-key"].write_bytes(
            data=client_key.private_bytes(
                encoding=serialization.Encoding.PEM,
                format=serialization.PrivateFormat.PKCS8,
                encryption_algorithm=serialization.NoEncryption(),
            )
        )
    return paths
