# Releasing hardcharger-adbc

A release is two tags on one commit of `main`: `vX.Y.Z`, which `release.yml` acts on (the full
test gate, the shared library for every platform, the wheels, the GitHub Release and PyPI
`adbc-driver-hardcharger`), and `go/vX.Y.Z`, which versions the Go module
`github.com/gizmodata/hardcharger-adbc/go/v2` (v2 requires the `/v2` module/import suffix; it lives in `go/`, so Go's subdirectory-module
convention applies; `go-module.yml` checks the pair and warms the module proxy). The Python
package and the Go module share one version line, the server's (2.0.0 with Hardcharger 2.0.0).

The Go module depends on `github.com/gizmodata/chargewire-adbc/go/v2`, so the generic driver is
released first.

## Order

1. **chargewire-adbc first**: its `vX.Y.Z` and `go/vX.Y.Z` tags are pushed and its workflows are
   green (its `RELEASING.md`), and the module proxy serves the version:

   ```bash
   GOFLAGS=-mod=mod GOPROXY=https://proxy.golang.org go list -m github.com/gizmodata/chargewire-adbc/go/v2@vX.Y.Z
   ```

2. **Move `go/go.mod` from the pseudo-version to that release**, on `main`:

   ```bash
   cd go
   go get github.com/gizmodata/chargewire-adbc/go/v2@vX.Y.Z
   go mod tidy
   cd ..
   scripts/third_party_notices.sh        # its chargewire-adbc entry names the version; CI checks the file
   (cd go && make test)
   ```

   Commit it ("Build on chargewire-adbc/go/v2 vX.Y.Z"), with a CHANGELOG line, push `main`, and wait
   for CI (the Go unit and live suites, the Python bindings, the notices check).
3. The version in `python/src/adbc_driver_hardcharger/_version.py` and `CHANGELOG.md`'s
   `## [X.Y.Z] - YYYY-MM-DD` section with today's date, on `main`, green.
4. Tag the release, then the Go module, on that commit, and push the release tag first:

   ```bash
   sha=$(git rev-parse main)
   git tag -a vX.Y.Z -m "hardcharger-adbc X.Y.Z" "$sha"
   git tag -a go/vX.Y.Z -m "github.com/gizmodata/hardcharger-adbc/go/v2 vX.Y.Z" "$sha"
   git push origin vX.Y.Z
   git push origin go/vX.Y.Z
   ```

   `release.yml` runs on `vX.Y.Z` (its `v*` trigger does not match `go/v*`); `go-module.yml` runs
   on `go/vX.Y.Z`, and refuses a commit that `vX.Y.Z` is not on or whose `go.mod` still names a
   pseudo-version of chargewire-adbc.
5. Wait for both workflows, then check what was published:

   ```bash
   curl -sS https://pypi.org/pypi/adbc-driver-hardcharger/json | python3 -c 'import json,sys; print(json.load(sys.stdin)["info"]["version"])'
   gh release view vX.Y.Z -R gizmodata/hardcharger-adbc --json assets --jq '.assets[].name'
   GOFLAGS=-mod=mod GOPROXY=https://proxy.golang.org go list -m github.com/gizmodata/hardcharger-adbc/go/v2@vX.Y.Z
   ```

## While the repositories are internal

`go/go.mod` keeps the pseudo-version of chargewire-adbc (`v0.0.0-<date>-<sha>`, fetched over SSH
with the `CHARGEWIRE_ADBC_DEPLOY_KEY` deploy key and `GOPRIVATE=github.com/gizmodata/*`,
`.github/actions/go-private`) until chargewire-adbc's `go/vX.Y.Z` tag exists and is public:
`proxy.golang.org` cannot read an internal repository, and the released version does not exist
before the tag. `go-module.yml` refuses a pseudo-version, so a `go/vX.Y.Z` tag of this module
cannot be pushed before step 2. Once both repositories are public, remove
`.github/actions/go-private`, its uses in `ci.yml`, `release.yml` and `go-module.yml`, the
`CHARGEWIRE_ADBC_DEPLOY_KEY` secret and the `GOPRIVATE` notes in the README.
