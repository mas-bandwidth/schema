# AGENTS.md is a generated map (nova-tools#2498 S11). The catalog lives in
# this directory; this target rewrites the root page and the per-directory
# pages. go test ./tools/agentsmap fails when they drift. The Makefile
# includes this file by path; it is not under make/, so $(wildcard make/*.mk)
# stays live compiler backends only.
.PHONY: map
map:
	go run ./tools/agentsmap
