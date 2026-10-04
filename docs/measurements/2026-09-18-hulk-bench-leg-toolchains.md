# Hulk bench leg toolchains — measured 2026-09-18

This document records which of the nine bench legs that `bench/run.sh` knows
(`c`, `cpp`, `go`, `rust`, `cs`, `js`, `java`, `dart`, `elixir`) the hulk bench
host can actually build, by asking for the compiler or runtime each leg needs
inside a sandboxed card. It was measured at head
`ed0c79178a695737f02b254e30219c1befcbbb94` on branch
`rowan/measure-hulk-bench-leg-toolchains` (repo `mas-bandwidth/schema`, `main`).
It carries **no timing** and is **not bench evidence**: no benchmark was run and
no clock was started. A leg whose toolchain is absent or whose program the
sandbox refuses is skipped and recorded here rather than failed, so any round
read on hulk must first be scoped to the legs this document shows it can build.

| leg | program the leg needs | present | version string |
|---|---|---|---|
| c | gcc | yes | `gcc (Ubuntu 13.3.0-6ubuntu2~24.04.1) 13.3.0` |
| cpp | g++ | yes | `g++ (Ubuntu 13.3.0-6ubuntu2~24.04.1) 13.3.0` |
| go | go | refused | `/usr/bin/bash: line 1: /home/gaffer/go/bin/go: Permission denied` (and `/home/gaffer/sdk/go1.26.5/bin/go: Permission denied`) |
| rust | rustc, cargo | no | `rustc: ABSENT`; `cargo: ABSENT` |
| cs | dotnet | no | `dotnet: ABSENT` |
| js | node | no | `node: ABSENT` |
| java | java, javac | no | `java: ABSENT`; `javac: ABSENT` |
| dart | dart | no | `dart: ABSENT` |
| elixir | elixir, mix | no | `elixir: ABSENT`; `mix: ABSENT` |

**Hulk can build 2 of the 9 legs** (`c`, `cpp`). It cannot build `go`, `rust`,
`cs`, `js`, `java`, `dart`, `elixir`.

## How these numbers were made

STEP 3 — every leg's compiler or runtime asked for by name:

```
for t in g++ gcc go rustc cargo dotnet node java javac dart elixir mix; do printf "%s: " "$t"; command -v "$t" >/dev/null 2>&1 && ("$t" --version 2>&1 | head -1) || echo ABSENT; done 2>&1 | tee ../scratch/tools.txt
```

STEP 4 — the repository's newer pinned Go, whose refusal is the go leg's answer
here:

```
/home/gaffer/sdk/go1.26.5/bin/go version 2>&1 | tee -a ../scratch/tools.txt
```
