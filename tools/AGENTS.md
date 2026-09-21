# AGENTS.md — generated map of tools/

Do not edit. `make map` regenerates this file. Root: [AGENTS.md](../AGENTS.md). Rules: [CONTRIBUTING.md](../docs/CONTRIBUTING.md).

| dir | purpose | guard | command |
| --- | --- | --- | --- |
| `agentsmap/` | AGENTS.md generator and drift guard | `go test ./tools/agentsmap` | `make map` |
| `fixedtwin/` | fixed-form twin | `go test ./tools/fixedtwin` | `go test ./tools/fixedtwin` |
| `negativecontrols/` | negative-control enumerator | `go test ./tools/negativecontrols` | `go run ./tools/negativecontrols check` |
| `roadmap/` | ROADMAP.md table honesty | `go test ./tools/roadmap` | `go test ./tools/roadmap` |
| `sabotage/` | negative-control sabotages | `go test ./tools/negativecontrols` | `go run ./tools/sabotage` |
| `slowgatescan/` | SCHEMA_SLOW recipe scan | `make slow-gate-scan` | `make slow-gate-scan` |
| `treelock/` | generated-tree lock | `go test ./tools/treelock` | `go test ./tools/treelock` |
| `vuln/` | govulncheck wrapper | `make vuln-selftest` | `make vuln-selftest` |
