# Which bench legs the air can build, measured inside a card (darwin/arm64)

What was measured: for each of the nine bench legs — cpp, c, go, rust, cs, js, java,
dart, elixir — the compiler or runtime that leg needs, whether a **sandboxed card** on
the **air** bench (Apple M2 MacBook Air, darwin/arm64, 8 cores, Darwin 25.6.0) can run
that program, and its exact version string. Head measured: `ed0c7917` of `main`.

This row carries **no timing**, runs **no benchmark**, and is **not bench evidence**.
It is an inventory, and its only purpose is that any round later run on the air is read
against the legs the air can actually build. Round 1 wrote the same list for hulk
(linux/x64); this is its darwin counterpart.

The inventory is taken twice on purpose — once inside the wall and once on the bare
machine — because a program that is installed and on `PATH` can still be **absent for a
card**. On this bench three of them are, and that gap is the finding.

| leg | program the leg needs | present inside a card | version inside a card | on the bare machine |
| --- | --- | --- | --- | --- |
| cpp | `c++` | yes | Apple clang version 21.0.0 (clang-2100.1.1.101) | same |
| c | `cc` | yes | Apple clang version 21.0.0 (clang-2100.1.1.101) | same |
| go | `go` | **refused** | `go: cannot find GOROOT directory: 'go' binary is trimmed and GOROOT is not set` | go1.27.1 darwin/arm64 |
| rust | `rustc` | yes | rustc 1.98.1 (48a229cea 2026-09-01) (Homebrew) | same |
| cs | `dotnet` | **refused** | `Failed to resolve full path of the current executable []` | 10.0.401 |
| js | `node` | yes | v26.9.0 | same |
| java | `java` | **refused** | `The operation couldn't be completed. Unable to locate a Java Runtime.` | openjdk version "27" 2026-09-15 |
| dart | `dart` | no | ABSENT | ABSENT |
| elixir | `elixir` | no | ABSENT | ABSENT |

**Four of the nine legs build in a card on the air today: cpp, c, rust, js.** The five
it cannot are go, cs, java (all three installed on the machine but unreachable through
the bare wall) and dart, elixir (not installed at all).

The three refusals are one shape with three faces: each of those toolchains finds its
own runtime by walking out of the directory its launcher lives in, and the bare wall
does not grant that directory.

* `go` — `/opt/homebrew/bin/go` is a symlink into `../Cellar/go/1.27.1/bin/go`, and the
  Homebrew build is trimmed, so a `go` that cannot read its Cellar directory has no
  GOROOT. Granting the sandbox `--read /opt/homebrew/Cellar/go/1.27.1` makes `go version`
  answer `go version go1.27.1 darwin/arm64` immediately; the go leg is therefore
  reachable with one extra read root, not truly absent.
* `dotnet` — resolves its own executable path at startup and gets an empty answer
  inside the wall.
* `java` — `/usr/bin/java` is the macOS stub, which locates a real JDK through a
  system service the wall does not reach.

`PATH` inside the card was
`/Users/glenn/.local/bin:/opt/homebrew/bin:/opt/homebrew/sbin:/usr/bin:/bin:/usr/sbin:/sbin`,
the same as outside, so nothing here is a `PATH` accident.

## How these numbers were made

Inside `nova-sandbox run --name c9702 --size 4g --timeout 15m --read <jobs>` (backend
`sandbox-exec`, one disposable APFS volume per card, `net=nopromise`), after cloning the
repository into the volume and checking out `origin/main`:

    for t in c++ clang++ cc gcc go rustc cargo dotnet node java javac dart elixir mix; do
      printf "%s: " "$t"
      if command -v "$t" >/dev/null 2>&1; then "$t" --version 2>&1 | head -1; else echo ABSENT; fi
    done
    echo "PATH=$PATH"

`go`, `dotnet` and `java` do not answer `--version`, so they were asked again in their
own dialect, inside the wall and then outside it:

    go version
    go env GOROOT
    dotnet --list-sdks
    java -version

and the GOROOT remedy was confirmed with

    nova-sandbox run --name g1 --size 1g --timeout 5m \
      --read /opt/homebrew/Cellar/go/1.27.1 -- /bin/sh -c 'go version'
