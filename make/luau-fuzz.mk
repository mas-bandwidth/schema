# make/luau-fuzz.mk — the LUAU leg's wire-fuzz gate (docs/PORT-LUAU.md, THE
# CARD MAP: schema-luau-fuzz-differential; test/conformance/README.md, "The
# wire-fuzz driver"; docs/SPEC-TABLES.md §4.2). Included by the Makefile's
# wildcard include.
#
# The pinned Luau has no io library, so the harness's little-endian pipe is
# spoken by test/conformance/luau/wirefuzz.luau under the native host
# tools/luaufuzzhost (its __luaufuzz.read and __luaufuzz.write are stdin and
# stdout), started by the executable driver test/conformance/luau/
# wirefuzz-driver. The in-package differential and its negative control run
# beside it, over the same corpus and the same pinned interpreter.

LUAU_FUZZ_SEED ?= 1
LUAU_FUZZ_N    ?= 2000

# build/luaufuzzhost/luaufuzzhost is built by make/schema-luau-nightly-fuzz.mk;
# its path is spelled out because that file is included after this one.
.PHONY: tables-luau-wire-fuzz
tables-luau-wire-fuzz: build/conformance-harness build/conformance-luau build/luaufuzzhost/luaufuzzhost
	@mkdir -p build/wire-fuzz
	./build/conformance-harness wire-fuzz --driver test/conformance/luau/wirefuzz-driver \
		--seed $(LUAU_FUZZ_SEED) --n $(LUAU_FUZZ_N) --failed build/wire-fuzz/failed-luau.bin
	sh test/slowgate/proof tables-luau-wire-fuzz \
		'TestLuauWireFuzzAgreesWithTheGoEngine TestLuauWireFuzzNegativeControl' \
		-count=1 -timeout 600s ./internal/codegen/luautable/ \
		-run '^TestLuauWireFuzz(AgreesWithTheGoEngine|NegativeControl)$$' -v
