# The Go table packages, measured once on the darwin/arm64 bench

What was measured: the three Go packages that carry the fixed-table lock file, wire
and pack code — `internal/lockfile`, `internal/tablewire`, `internal/tablepack` — run
once each on a darwin/arm64 card bench, inside a sandboxed card, at head `d8de03bda`
of `main`. For each package this row records how many `Test` functions the package
declares, whether `go test` passes, and the wall seconds the run took.

What it does **not** claim. This is not a parity, gap or performance investigation. It
makes no timing claim about the wire, the fixed form or any leg. The wall seconds below
are the seconds a test binary took on a shared harness under a sandbox wall, nothing
else, and they must not be cited as bench evidence. Round 1 of this row ran on hulk
(linux/x64); the two are comparable only as "did the packages run here at all", never
as a speed comparison.

Toolchain used: `go version go1.27.1 darwin/arm64`, GOROOT
`/opt/homebrew/Cellar/go/1.27.1/libexec`. `go.mod` asks for `go 1.26`, so the bench's
toolchain is newer than the module's floor and no SDK override was needed. `go mod
download` answered `no module dependencies to download`: the three packages pull
nothing from the network.

The bench this recut ran on is the studio host (Apple M3 Ultra, darwin/arm64, 32
cores), not the MacBook Air of the first cut — the row is pinned by architecture, not
by chassis, so the numbers are reported against the head they were actually produced
on and are still not bench evidence.

| package | declared Test functions | go test | wall seconds (card) | go test's own seconds |
| --- | --- | --- | --- | --- |
| `internal/lockfile` | 37 | ok | 39 | 11.368 |
| `internal/tablewire` | 54 | ok | 35 | 11.682 |
| `internal/tablepack` | 13 | ok | 20 | 11.459 |
| **total** | **104** | **3 of 3 ok** | **94** | **34.509** |

The wall figure is dominated by the cold build of the dependency set on the card's
first visit to each package; on a warm cache the same three runs round to zero. The
right-hand column is `go test`'s own reported figure, which is the one to read, and it
still folds in the first-package compile on the first visit, which is why the total is
not the sum of three test-execution times.

Head measured: `d8de03bda` (`origin/main`).

## How these numbers were made

Inside a `nova-sandbox` card with the repository cloned into the volume and checked
out at `origin/main`:

    go version
    go env GOROOT
    go env GOMODCACHE
    grep '^go ' go.mod
    go mod download

    for p in internal/lockfile internal/tablewire internal/tablepack; do
      n=$(grep -h '^func Test' $p/*_test.go | wc -l | tr -d ' ')
      s=$(date +%s); out=$(go test ./$p/ 2>&1 | tail -3); e=$(date +%s)
      echo "== $p declares $n Test functions wall=$((e-s))s"
      echo "$out"
    done

`/usr/bin/time -f` is GNU-only and does not exist on darwin, which is why the timing is
taken from `date +%s` on either side of the run.
