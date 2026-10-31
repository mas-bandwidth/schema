# ROADMAP.md is generated, end to end, from docs/roadmap.sexp and the evidence
# overlay docs/roadmap-evidence/*.sexp by nova-roadmap, the one roadmap
# generator every repository uses (a nova-tools release binary; it began as this
# repository's render.lisp, ported to Go with the same data form and page).
# Edit the data, then `make roadmap`; never edit ROADMAP.md by hand.
# `make roadmap-check` is the drift gate: it exits 1 when the committed page is
# not what the data renders, and the fast CI lane runs it on every push.
# `nova-roadmap help form` documents the form. The pinned binary is downloaded
# once into bin/ and checked against the release's SHA256SUMS; NOVA_ROADMAP=<path>
# runs another one. This file is included by path (not from make/*.mk) so the
# port registry wildcard stays live compiler backends only.
NOVA_ROADMAP_VERSION ?= v1.6.0
NOVA_ROADMAP ?= bin/nova-roadmap-$(NOVA_ROADMAP_VERSION)

.PHONY: roadmap roadmap-check roadmap-test
roadmap: $(NOVA_ROADMAP)
	$(NOVA_ROADMAP) render

roadmap-check: $(NOVA_ROADMAP)
	$(NOVA_ROADMAP) check

roadmap-test:
	go test -count=1 ./tools/roadmap/

bin/nova-roadmap-%:
	@mkdir -p bin
	@os=$$(uname -s | tr A-Z a-z); arch=$$(uname -m); \
	case $$arch in x86_64) arch=amd64;; aarch64) arch=arm64;; esac; \
	asset=nova-roadmap_$*_$${os}_$${arch}; \
	base=https://github.com/mas-bandwidth/nova-tools/releases/download/$*; \
	curl -fsSL -o $@.tmp "$$base/$$asset" && \
	sum=$$(curl -fsSL "$$base/SHA256SUMS" | awk -v a="$$asset" '$$2 == a {print $$1}') && \
	test -n "$$sum" && { echo "$$sum  $@.tmp" | sha256sum -c - >/dev/null 2>&1 || echo "$$sum  $@.tmp" | shasum -a 256 -c - >/dev/null; } && \
	chmod +x $@.tmp && mv $@.tmp $@ || { rm -f $@.tmp; echo "nova-roadmap $* for $$os/$$arch: download or checksum failed" >&2; exit 2; }
