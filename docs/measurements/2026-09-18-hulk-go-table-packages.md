# Go table packages measured once on hulk at main

On 2026-09-18 the Go leg's table unit packages (`internal/lockfile`,
`internal/tablewire`, `internal/tablepack`) were measured once on the **hulk**
bench at head `ed0c79178a695737f02b254e30219c1befcbbb94` (`origin/main`). This is
a bounded repeat measurement only: it makes no parity, gap or performance claim,
makes no timing claim about the wire, and is not bench evidence. The bench
offered a sandboxed card no usable Go toolchain, which is the measurement: the
default `go` on `PATH` and the card-named toolchain both refused to execute, and
the only Go that could run was older than the version `go.mod` requires, so no
`go test` executed for any of the three packages.

The Go toolchain that was **used** is none: `go version` resolved to
`/home/gaffer/go/bin/go` and printed `Permission denied`, and
`/home/gaffer/sdk/go1.26.5/bin/go version` also printed `Permission denied`; no
version string was obtainable from either. The toolchain that was **rejected** is
`go version go1.22.2 linux/amd64` (the only accessible Go, at `/usr/bin/go`),
which `go.mod` rejects with
`go: go.mod requires go >= 1.26 (running go 1.22.2; GOTOOLCHAIN=local)`.

The declared Test-function counts are static and need no toolchain, so they are
reported as measured by `grep`; every number that would depend on a Go toolchain
is marked `REFUSED BY THE WALL` because no `go test` ran.

| package | declared Test functions | ok/FAIL | wall seconds |
| --- | --- | --- | --- |
| internal/lockfile | 37 | REFUSED BY THE WALL | REFUSED BY THE WALL |
| internal/tablewire | 54 | REFUSED BY THE WALL | REFUSED BY THE WALL |
| internal/tablepack | 13 | REFUSED BY THE WALL | REFUSED BY THE WALL |

## How these numbers were made

STEP 3 commands, verbatim:

```
go version
/home/gaffer/sdk/go1.26.5/bin/go version
```

and their output, verbatim:

```
$ go version
/usr/bin/bash: line 1: /home/gaffer/go/bin/go: Permission denied

$ /home/gaffer/sdk/go1.26.5/bin/go version
/usr/bin/bash: line 1: /home/gaffer/sdk/go1.26.5/bin/go: Permission denied
```

`GO` was therefore left empty, and the accessible fallback was checked, verbatim:

```
$ /usr/bin/go version
go version go1.22.2 linux/amd64

$ /usr/bin/go env GOROOT GOPATH GOTOOLCHAIN
/usr/lib/go-1.22
/home/gaffer/rowan-working/tmp/d747c8bf-0401-4a59-b4c3-2cc93a63270c-card-9602/data/go
local
```

STEP 4 command, verbatim:

```
for p in internal/lockfile internal/tablewire internal/tablepack; do echo "== $p declares $(grep -h '^func Test' $p/*_test.go | wc -l) Test functions"; /usr/bin/time -f "wall=%es" $GO test ./$p/ 2>&1 | tail -3; done 2>&1 | tee ../scratch/gotest.txt
```

With `GO` empty the loop's `go test` cannot run; the declaration counts it
prints before that point were captured, verbatim:

```
== internal/lockfile declares 37 Test functions
== internal/tablewire declares 54 Test functions
== internal/tablepack declares 13 Test functions
```

The card-named toolchain was then attempted directly for each package, verbatim:

```
-- internal/lockfile --
/usr/bin/bash: line 1: /home/gaffer/sdk/go1.26.5/bin/go: Permission denied
-- internal/tablewire --
/usr/bin/bash: line 1: /home/gaffer/sdk/go1.26.5/bin/go: Permission denied
-- internal/tablepack --
/usr/bin/bash: line 1: /home/gaffer/sdk/go1.26.5/bin/go: Permission denied
```

and the only accessible Go was attempted, verbatim:

```
-- internal/lockfile --
go: go.mod requires go >= 1.26 (running go 1.22.2; GOTOOLCHAIN=local)
Command exited with non-zero status 1
wall=0.00s
-- internal/tablewire --
go: go.mod requires go >= 1.26 (running go 1.22.2; GOTOOLCHAIN=local)
Command exited with non-zero status 1
wall=0.00s
-- internal/tablepack --
go: go.mod requires go >= 1.26 (running go 1.22.2; GOTOOLCHAIN=local)
Command exited with non-zero status 1
wall=0.00s
```
