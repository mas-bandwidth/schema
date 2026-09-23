# The Go table packages, measured once on the air bench (darwin/arm64)

What was measured: the three Go packages that carry the fixed-table lock file, wire
and pack code — `internal/lockfile`, `internal/tablewire`, `internal/tablepack` — run
once each on the **air** bench (Apple M2 MacBook Air, darwin/arm64, 8 cores, Darwin
25.6.0) inside a sandboxed card, at head `ed0c7917` of `main`. For each package this
row records how many `Test` functions the package declares, whether `go test` passes,
and the wall seconds the run took.

What it does **not** claim. This is not a parity, gap or performance investigation. It
makes no timing claim about the wire, the fixed form or any leg. The wall seconds below
are the seconds a test binary took on a fanless laptop under a sandbox wall, nothing
else, and they must not be cited as bench evidence. The round-1 companion of this row
ran on hulk (linux/x64); the two are comparable only as "did the packages run here at
all", never as a speed comparison.

Toolchain used: `go version go1.27.1 darwin/arm64`, GOROOT
`/opt/homebrew/Cellar/go/1.27.1/libexec`. `go.mod` asks for `go 1.26`, so the bench's
toolchain is newer than the module's floor and no SDK override was needed. `go mod
download` answered `no module dependencies to download`: the three packages pull
nothing from the network.

One fact about the wall belongs here, because it changes the answer. A card on this
bench reaches `go` only when the sandbox is given the toolchain's own directory to
read: with the bare wall, `go version` fails with

    go: cannot find GOROOT directory: 'go' binary is trimmed and GOROOT is not set

because Homebrew's `/opt/homebrew/bin/go` is a symlink into `../Cellar/go/1.27.1/bin/go`
and the wall cannot follow it. The numbers below were produced with
`--read /opt/homebrew/Cellar/go/1.27.1` added to the sandbox.

| package | declared Test functions | go test | wall seconds (card) | go test's own seconds |
| --- | --- | --- | --- | --- |
| `internal/lockfile` | 37 | ok | 6 | 0.683 |
| `internal/tablewire` | 54 | ok | 0 | 0.353 |
| `internal/tablepack` | 13 | ok | 0 | 0.367 |
| **total** | **104** | **3 of 3 ok** | **6** | **1.403** |

The 6 seconds against `internal/lockfile` is the compile of the whole dependency set on
a cold cache; the two packages after it reuse it and round to zero. The right-hand
column is `go test`'s own reported figure, which is the one to read.

Head measured: `ed0c7917` (`origin/main`).

## How these numbers were made

Inside `nova-sandbox run --name c9701 --size 8g --timeout 25m --read <jobs>
--read /opt/homebrew/Cellar/go/1.27.1` (backend `sandbox-exec`, one disposable APFS
volume per card), after cloning the repository into the volume and checking out
`origin/main`:

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
