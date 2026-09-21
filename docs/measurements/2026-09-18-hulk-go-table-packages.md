# Go table packages measured once, recut at the moved tip

This is the recut of the card-9602 measurement: the Go leg's table unit packages
(`internal/lockfile`, `internal/tablewire`, `internal/tablepack`) measured once.
The original run on the hulk bench (linux/x64) measured head `ed0c7917` and found
the wall had refused the Go toolchain outright — `go version` resolved to a
`Permission denied` path, and the only reachable Go was `go1.22.2`, which
`go.mod` rejected for being older than its `go 1.26` floor — so no `go test`
executed for any of the three packages. That is not the finding here.

The tip moved under the card: this recut re-measures at head
`d8de03bdab59538384685835c7a354bdd4b5b078` (`origin/main`), and it landed on a
darwin/arm64 host rather than the linux hulk bench, so the Go toolchain **is**
present and `go.mod`'s floor is met. The three packages ran and passed. The
declared `Test`-function counts are unchanged from the prior pass; every number
that would depend on a Go toolchain is now a measured `ok` instead of the prior
`REFUSED BY THE WALL`.

This is a bounded repeat measurement only: it makes no parity, gap or performance
claim, makes no timing claim about the wire, and is not bench evidence. The wall
seconds below are the seconds a `go test` binary took on a shared build host,
nothing else, and must not be cited as bench evidence.

| package | declared Test functions | go test | go test's own seconds |
| --- | --- | --- | --- |
| internal/lockfile | 37 | ok | 32.977 |
| internal/tablewire | 54 | ok | 9.078 |
| internal/tablepack | 13 | ok | 10.983 |
| **total** | **104** | **3 of 3 ok** | **53.038** |

The `internal/lockfile` seconds are the compile of the whole dependency set on a
cold cache; the two packages after it reuse that work. The column to read is
`go test`'s own reported figure.

Toolchain used: `go version go1.27.1 darwin/arm64`, GOROOT
`/opt/homebrew/Cellar/go/1.27.1/libexec`, GOMODCACHE
`/Users/glenn/rowan-working/tmp/cache/go-mod`. `grep '^go ' go.mod` reports
`go 1.26`, so the bench's toolchain is newer than the module's floor and no SDK
override was needed. `go mod download` answered `no module dependencies to
download`: the three packages pull nothing from the network.

Head measured: `d8de03bdab59538384685835c7a354bdd4b5b078` (`origin/main`).

## How these numbers were made

On a darwin/arm64 host (Darwin 25.6.0), inside the sandboxed card, after the
repository is at `origin/main`:

```
go version
go env GOROOT GOPATH GOMODCACHE GOTOOLCHAIN
grep '^go ' go.mod
go mod download

for p in internal/lockfile internal/tablewire internal/tablepack; do
  n=$(grep -h '^func Test' $p/*_test.go | wc -l | tr -d ' ')
  echo "== $p declares $n Test functions"
  go test ./$p/ 2>&1 | tail -1
done
```

and the output, verbatim:

```
$ go version
go version go1.27.1 darwin/arm64

$ go env GOROOT GOPATH GOMODCACHE GOTOOLCHAIN
/opt/homebrew/Cellar/go/1.27.1/libexec
/Users/glenn/rowan-working/tmp/73f60ff2-5d8a-68bd-d2ce-45b6e75141e2-card-00-recut-schema-1111-r1-1/data/go
/Users/glenn/rowan-working/tmp/cache/go-mod
local

$ grep '^go ' go.mod
go 1.26

$ go mod download
go: no module dependencies to download

== internal/lockfile declares 37 Test functions
ok  	github.com/mas-bandwidth/schema/v2/internal/lockfile	32.977s
== internal/tablewire declares 54 Test functions
ok  	github.com/mas-bandwidth/schema/v2/internal/tablewire	9.078s
== internal/tablepack declares 13 Test functions
ok  	github.com/mas-bandwidth/schema/v2/internal/tablepack	10.983s
```

`/usr/bin/time -f` is GNU-only and does not exist on darwin, which is why the
seconds above are `go test`'s own reported figure rather than a wall-clock
wrapper.
