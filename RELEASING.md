# Releasing hardcharger-adbc

A release is two tags on one commit of `main`: `vX.Y.Z`, which `release.yml` acts on (the full
test gate, the shared library for every platform, the wheels, the GitHub Release and PyPI
`adbc-driver-hardcharger`), and `go/vX.Y.Z`, which versions the Go module
`github.com/gizmodata/hardcharger-adbc/go/v2` (v2 requires the `/v2` module/import suffix; it lives in `go/`, so Go's subdirectory-module
convention applies; `go-module.yml` checks the pair and warms the module proxy). The Python
package and the Go module share an independent driver version line. Do not derive their
version from the Hardcharger server release.

The Go module depends on `github.com/gizmodata/chargewire-adbc/go/v2`, so the generic driver is
released first.

## Order

1. **chargewire-adbc first**: its `vX.Y.Z` and `go/vX.Y.Z` tags are pushed and its workflows are
   green (its `RELEASING.md`), and the module proxy serves the version:

   ```bash
   GOFLAGS=-mod=mod GOPROXY=https://proxy.golang.org go list -m github.com/gizmodata/chargewire-adbc/go/v2@vX.Y.Z
   ```

2. **Move `go/go.mod` to that release** (from a pseudo-version, or from a `replace` of a local
   checkout), on a release branch:

   ```bash
   cd go
   go mod edit -dropreplace github.com/gizmodata/chargewire-adbc/go/v2
   go get github.com/gizmodata/chargewire-adbc/go/v2@vX.Y.Z
   go mod tidy
   cd ..
   scripts/third_party_notices.sh        # its chargewire-adbc entry names the version; CI checks the file
   (cd go && make test)
   ```

   Commit it ("Build on chargewire-adbc/go/v2 vX.Y.Z"), with a CHANGELOG line, push the branch,
   and open a pull request. Wait for CI (the Go unit and live suites, the Python bindings,
   the notices check), then merge through the repository's protected pull-request process.
3. The version in `python/src/adbc_driver_hardcharger/_version.py` and `CHANGELOG.md`'s
   `## [X.Y.Z] - YYYY-MM-DD` section with today's date, on `main`, green. Before merging, run
   `release.yml` on the release branch (`gh workflow run release.yml --ref <branch>`) and wait for
   every job to pass: a dispatch runs everything a tag runs except the GitHub Release and PyPI, so
   a failure costs no tag.
4. Fetch the exact landed main commit and verify its tree matches the tested candidate
   (`git rev-parse origin/main^{tree}` equals the dispatched commit's).
   Tag the release on that commit and push the release tag first:

   ```bash
   git fetch origin main
   sha=$(git rev-parse origin/main)
   git tag -a vX.Y.Z -m "hardcharger-adbc X.Y.Z" "$sha"
   git push origin vX.Y.Z
   # After release.yml succeeds and the release is verified:
   git tag -a go/vX.Y.Z -m "github.com/gizmodata/hardcharger-adbc/go/v2 vX.Y.Z" "$sha"
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

   The release has 12 assets: six library archives, five wheels and `SHA256SUMS` (the
   installer at [gizmodata/adbc-install](https://github.com/gizmodata/adbc-install) checks an
   archive against it before installing). Download them all and verify the sums:

   ```bash
   d=$(mktemp -d); gh release download vX.Y.Z -R gizmodata/hardcharger-adbc -D "$d"
   (cd "$d" && sha256sum -c SHA256SUMS && test "$(wc -l < SHA256SUMS)" -eq 11)
   ```
