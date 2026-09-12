# Building `challenge/run-checks`

Exact command, from the repository root, GHC 9.14.1 on PATH (`/opt/homebrew/bin`):

```
cd challenge && ghc -O1 -o run-checks RunChecks.hs
```

Boot libraries only (base, bytestring, containers, directory, filepath). No Hackage, no cabal file.
The committed `run-checks` binary is the output of exactly that command (darwin/arm64).

Invocation (shell-free, argv only):

```
challenge/run-checks <absolute manifest path> <fixture path or row id>
```
