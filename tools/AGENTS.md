# AGENTS.md — generated map of tools/

Do not edit. `make map` regenerates this file. Root: [AGENTS.md](../AGENTS.md). Rules: [CONTRIBUTING.md](../docs/CONTRIBUTING.md).

| dir | purpose | guard | command |
| --- | --- | --- | --- |
| `agentsmap/` | AGENTS.md generator and drift guard | `go test ./tools/agentsmap` | `make map` |
| `cellmerge/` | three-way PORTING.md row merge for diff3 hunks | `go test ./tools/cellmerge` | `go run ./tools/cellmerge docs/PORTING.md` |
| `fixedtwin/` | fixed-form twin | `go test ./tools/fixedtwin` | `go test ./tools/fixedtwin` |
| `hostilecorpus/` | shared hostile-input corpus generator | `make schema-shared-hostile-corpus` | `make schema-shared-hostile-corpus-generate` |
| `luaufuzzhost/` | Luau nightly fuzz host (C++) | `make luau-nightly-fuzz-replay` | `make luau-nightly-fuzz` |
| `matrix/` | language matrix page | `make matrix-check` | `make matrix` |
| `moofuzzhost/` | moo wire-fuzz native host (C++) | `make tables-moo-wire-fuzz` | `make tables-moo-wire-fuzz` |
| `negativecontrols/` | negative-control enumerator | `go test ./tools/negativecontrols` | `go run ./tools/negativecontrols check` |
| `releasediff/` | previous-release differential (#463) | `make release-differential-negative-control` | `make release-differential` |
| `releasetag/` | promotion and release tags | `go test ./tools/releasetag` | `make release` |
| `roadmap/` | ROADMAP.md table honesty | `go test ./tools/roadmap` | `go test ./tools/roadmap` |
| `sabotage/` | negative-control sabotages | `go test ./tools/negativecontrols` | `go run ./tools/sabotage` |
| `slowgatescan/` | SCHEMA_SLOW recipe scan | `make slow-gate-scan` | `make slow-gate-scan` |
| `treelock/` | generated-tree lock | `go test ./tools/treelock` | `go test ./tools/treelock` |
| `vuln/` | govulncheck wrapper | `make vuln-selftest` | `make vuln-selftest` |
