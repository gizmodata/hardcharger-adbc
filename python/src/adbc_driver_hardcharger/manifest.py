"""The ADBC driver manifest ``hardcharger.toml``: ``driver = "hardcharger"`` in any ADBC driver
manager (C/C++, Go, R, Rust, Python), loading the driver library this wheel installed.

    python -m adbc_driver_hardcharger.manifest            # install for this user
    python -m adbc_driver_hardcharger.manifest --print    # print it
    python -m adbc_driver_hardcharger.manifest --dir DIR  # install into DIR

The library reads ``hardcharger://`` URIs and the ``hardcharger.*`` options itself, so they
work through the manifest exactly as from Python.
"""

from __future__ import annotations

import argparse
import os
import pathlib
import sys
from typing import Optional

from ._version import __version__

MANIFEST_NAME = "hardcharger.toml"
ENTRYPOINT = "AdbcDriverHardchargerInit"


def _platform_key() -> str:
    import platform

    machine = platform.machine().lower()
    arch = {"x86_64": "amd64", "amd64": "amd64", "arm64": "arm64", "aarch64": "arm64"}.get(machine)
    if arch is None:
        raise RuntimeError(f"no Hardcharger driver library is built for {machine}")
    system = {"linux": "linux", "darwin": "macos", "win32": "windows"}.get(sys.platform)
    if system is None:
        raise RuntimeError(f"no Hardcharger driver library is built for {sys.platform}")
    return f"{system}_{arch}"


def library_path() -> str:
    """The driver library this Python environment uses."""
    from ._driver import _driver_path

    return _driver_path()


def render(library: Optional[str] = None) -> str:
    """The manifest's text, pointing at ``library`` (default: :func:`library_path`)."""
    library = library or library_path()
    escaped = library.replace("\\", "\\\\").replace('"', '\\"')
    return (
        "manifest_version = 1\n\n"
        "name = 'Hardcharger'\n"
        f"version = '{__version__}'\n"
        "publisher = 'GizmoData'\n"
        "license = 'Apache-2.0'\n"
        "url = 'https://github.com/gizmodata/hardcharger-adbc'\n\n"
        "[ADBC]\n"
        "version = '1.1.0'\n\n"
        "[Driver]\n"
        f"entrypoint = '{ENTRYPOINT}'\n\n"
        "[Driver.shared]\n"
        f'{_platform_key()} = "{escaped}"\n'
    )


def user_directory() -> pathlib.Path:
    """The per-user directory ADBC driver managers search for manifests."""
    if sys.platform == "darwin":
        return pathlib.Path.home() / "Library" / "Application Support" / "ADBC" / "Drivers"
    if os.name == "nt":
        base = os.environ.get("LOCALAPPDATA") or str(pathlib.Path.home() / "AppData" / "Local")
        return pathlib.Path(base) / "ADBC" / "Drivers"
    base = os.environ.get("XDG_CONFIG_HOME") or str(pathlib.Path.home() / ".config")
    return pathlib.Path(base) / "adbc" / "drivers"


def install(directory: Optional[os.PathLike] = None) -> pathlib.Path:
    """Write ``hardcharger.toml`` into ``directory`` (default: :func:`user_directory`)."""
    target = pathlib.Path(directory) if directory is not None else user_directory()
    target.mkdir(parents=True, exist_ok=True)
    path = target / MANIFEST_NAME
    path.write_text(data=render(), encoding="utf-8")
    return path


def main(argv: Optional[list[str]] = None) -> int:
    parser = argparse.ArgumentParser(
        prog="python -m adbc_driver_hardcharger.manifest",
        description='Install the ADBC driver manifest for driver = "hardcharger".',
    )
    parser.add_argument("--print", action="store_true", help="print the manifest instead")
    parser.add_argument("--dir", help="directory to install it into (default: the user's)")
    args = parser.parse_args(args=argv)
    if args.print:
        sys.stdout.write(render())
        return 0
    print(install(directory=args.dir))
    return 0


if __name__ == "__main__":
    sys.exit(main())
