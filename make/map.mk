# AGENTS.md is a generated map (nova-tools#2498 S11). The catalog lives in
# tools/agentsmap; this target rewrites the root page and the per-directory
# pages. go test ./tools/agentsmap fails when they drift. Discovered by the
# Makefile's wildcard include, so a new map target does not edit Makefile.
.PHONY: map
map:
	go run ./tools/agentsmap
