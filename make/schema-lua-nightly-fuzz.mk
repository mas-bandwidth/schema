# make/schema-lua-nightly-fuzz.mk — the LUA port's nightly fuzzing leg
# (schema-lua-nightly-fuzz). Included by the Makefile's wildcard include; the
# Makefile needs no edit.
#
# THE SUBJECT: the generated readers of every table in the corpus set the
# Makefile's own tables_generate function reads — the repository's test-schema
# corpus, not a second one. The corpus file the harness reads, and the harness
# itself, are BUILD ARTIFACTS under build/ (this card may not add files to the
# port's test tree); both derive from files this card owns.
#
# THE FOUR READER KINDS, one entry point each in the generated harness:
# load (runtime/load.lua L.Load, the tolerant form-1 load), strict
# (runtime/refusal.lua R.ReadStrict), text (runtime/text.lua Text.LoadText,
# the base64 text body) and json (runtime/json.lua J.FromJson, the JSON text
# form). The oracle is: no crash, no hang, no assert; a refusal is a clean
# typed error (refusal.lua's names), never a raw Lua error and never a silent
# success.
#
# THE FUZZER is a seeded mutation fuzzer (xorshift64*, Lua 5.4 integers), so a
# finding records its exact bytes and replays without a clock or a PRNG. It is
# pinned with the interpreter: PUC-Rio Lua 5.4.8, the pin make/lua.mk carries
# (LUA_VERSION / LUA_SHA256); LUA_FUZZ_HARNESS_REV is the harness revision,
# bumped by hand when the fuzzer's mutation or oracle changes.
LUA_FUZZ_HARNESS_REV := 1
LUA_FUZZ_SECONDS ?= 600
LUA_FUZZ_SEED    ?= 1
LUA_FUZZ_FINDINGS ?= build/lua-fuzz/findings
LUA_FUZZ_CORPUS  := build/lua-fuzz/corpus.txt
LUA_FUZZ_REGRESS := build/lua-fuzz/corpus
LUA_FUZZ_GENERATED := build/lua-fuzz/generated
LUA_FUZZ_SCRIPT  := build/lua-fuzz/fuzz.lua
LUA_FUZZ_SEEDDIR := build/lua-fuzz/seeds

# the pinned build when `make dist-lua` has populated it; otherwise the fleet
# Lua 5.4. The version is still 5.4, and the probe refuses by name when neither
# resolves.
LUA_FUZZ_LUA ?= $(if $(wildcard $(LUA)),$(LUA),$(shell command -v lua5.4 2>/dev/null || command -v lua 2>/dev/null))
LUA_FUZZ_SEEDS := --wire-seeds $(LUA_FUZZ_SEEDDIR):testdata/wire/tables:testdata/conformance/tables:internal/fuzz/testdata \
                  --json-seeds testdata/conformance/tables/json \
                  --text-seeds testdata/conformance/text

.PHONY: schema-lua-fuzz-toolchain
schema-lua-fuzz-toolchain:
	@if [ -z "$(LUA_FUZZ_LUA)" ] || [ ! -x "$(LUA_FUZZ_LUA)" ]; then \
		echo "tool missing: lua $(LUA_VERSION) (pin LUA=$(LUA), no lua5.4 on PATH)"; \
		exit 1; \
	fi
	@echo "schema-lua-fuzz: Lua $(LUA_VERSION) $(LUA_FUZZ_LUA) (harness rev $(LUA_FUZZ_HARNESS_REV))"

# ONE CORPUS, THE MAKEFILE'S: the tables_generate block is the corpus the
# repository's own generate targets read. Only its directory inputs are taken
# (the single-file test/tables versioning schemas are a separate gate); each
# directory becomes one unit keyed by its basename.
$(LUA_FUZZ_CORPUS): Makefile make/schema-lua-nightly-fuzz.mk
	@mkdir -p $(dir $@)
	@awk '/^define tables_generate/{f=1;next} /^endef/{f=0} f' Makefile | \
		sed -n 's|.*[[:space:]]\(tables/[A-Za-z0-9_]*\)[[:space:]]*$$|\1|p' | \
		while read -r dir; do printf '%s\t%s\n' "$$(basename $$dir)" "$$dir"; done > $@
	@test -s $@
	@echo "schema-lua-fuzz: $$(wc -l < $@ | tr -d ' ') corpus units from the Makefile's tables_generate block"

# THE HARNESS, as a build artifact. It is base64 in this file (so make
# does not expand the Lua's `$` anchors) and decoded into build/; this card
# may not add files to the port's test tree (PATHS-PROPOSED names
# test/lua-fuzz/fuzz.lua in its report).
LUA_FUZZ_HARNESS_B64 := LS0gdGVzdC9sdWEtZnV6ei9mdXp6Lmx1YSDigJQgdGhlIEx1YSBwb3J0J3Mgc2VlZGVkIG11dGF0aW9uIGZ1enplciBmb3IgdGhlCi0tIGdlbmVyYXRlZCByZWFkZXJzIChzY2hlbWEtbHVhLW5pZ2h0bHktZnV6eikuCi0tCi0tIFRIRSBIQVJORVNTIHJlYWRzIHRlc3QvbHVhLWZ1enovY29ycHVzLnR4dCAodGhlIHVuaXQga2V5IC0+IHNjaGVtYSBkaXJlY3RvcnkKLS0gcGFpcnMgdGhlIE1ha2VmaWxlJ3MgdGFibGVzX2dlbmVyYXRlIHJlYWRzKSwgbG9hZHMgZXZlcnkgZ2VuZXJhdGVkIHVuaXQncwotLSBUYWJsZXMgbW9kdWxlLCBhbmQgZHJpdmVzIG9uZSBlbnRyeSBwb2ludCBwZXIgcmVhZGVyIGtpbmQgb3ZlciBldmVyeSB0YWJsZQotLSBuYW1lIHRoYXQgbW9kdWxlIHJlZ2lzdGVyczoKLS0KLS0gICBsb2FkICAgIHRoZSB0b2xlcmFudCBmb3JtLTEgbG9hZCAocnVudGltZS9sb2FkLmx1YSBMLkxvYWQpCi0tICAgc3RyaWN0ICB0aGUgc3RyaWN0IHJlYWQgKHJ1bnRpbWUvcmVmdXNhbC5sdWEgUi5SZWFkU3RyaWN0KQotLSAgIHRleHQgICAgdGhlIHRleHQgZm9ybSdzIGJhc2U2NCBib2R5IChydW50aW1lL3RleHQubHVhIFRleHQuTG9hZFRleHQpCi0tICAganNvbiAgICB0aGUgdGV4dCBmb3JtJ3MgSlNPTiBib2R5IChydW50aW1lL2pzb24ubHVhIEouRnJvbUpzb24pCi0tCi0tIFRIRSBPUkFDTEU6IG5vIGNyYXNoLCBubyBoYW5nLCBubyBhc3NlcnQ7IGEgcmVmdXNhbCBpcyBhIGNsZWFuIHR5cGVkIGVycm9yCi0tIChyZWZ1c2FsLmx1YSdzIG5hbWVzKSwgbmV2ZXIgYSByYXcgTHVhIGVycm9yIGFuZCBuZXZlciBhIHNpbGVudCBzdWNjZXNzLgotLSBUaGUgc2VlZCBpcyByZWNvcmRlZCB3aXRoIGV2ZXJ5IGZpbmRpbmcgKHRoZSBmaW5kaW5nIGZpbGUncyBwYXlsb2FkIGlzIHRoZQotLSBleGFjdCBieXRlcywgc28gcmVwbGF5IG5lZWRzIG5vIGNsb2NrIGFuZCBubyBQUk5HKS4KLS0KLS0gdXNhZ2U6Ci0tICAgbHVhIHRlc3QvbHVhLWZ1enovZnV6ei5sdWEgLS1nZW5lcmF0ZWQgRElSIC0tY29ycHVzIEZJTEUKLS0gICAgICAgWy0tc2Vjb25kcyBOIHwgLS1pdGVyYXRpb25zIE5dIFstLXNlZWQgTl0gWy0tZmluZGluZ3MgRElSXQotLSAgICAgICBbLS13aXJlLXNlZWRzIERJUl0gWy0tanNvbi1zZWVkcyBESVJdIFstLXRleHQtc2VlZHMgRElSXQotLSAgIGx1YSB0ZXN0L2x1YS1mdXp6L2Z1enoubHVhIC0tZ2VuZXJhdGVkIERJUiAtLWNvcnB1cyBGSUxFCi0tICAgICAgIC0tcmVwbGF5IERJUiBbLS1maW5kaW5ncyBESVJdCgpsb2NhbCBvcHRzID0gewogIHNlY29uZHMgPSAxMCwgaXRlcmF0aW9ucyA9IDAsIHNlZWQgPSAxLAogIGZpbmRpbmdzID0gIiIsIHJlcGxheSA9ICIiLAp9CmxvY2FsIGkgPSAxCndoaWxlIGkgPD0gI2FyZyBkbwogIGxvY2FsIGEgPSBhcmdbaV0KICBsb2NhbCBrLCB2ID0gYTptYXRjaCgiXiUtJS0oWyV3LV0rKT0oLiopJCIpCiAgaWYgbm90IGsgdGhlbgogICAgayA9IGE6bWF0Y2goIl4lLSUtKFsldy1dKykkIikKICAgIGlmIGsgdGhlbiBpID0gaSArIDE7IHYgPSBhcmdbaV0gZW5kCiAgZW5kCiAgaWYgbm90IGsgdGhlbgogICAgaW8uc3RkZXJyOndyaXRlKCJmdXp6OiB1bmtub3duIGFyZ3VtZW50ICIgLi4gYSAuLiAiXG4iKQogICAgb3MuZXhpdCgyKQogIGVuZAogIGsgPSBrOmdzdWIoIiUtIiwgIl8iKQogIG9wdHNba10gPSB2CiAgaSA9IGkgKyAxCmVuZApmb3IgXywgayBpbiBpcGFpcnMoeyAiZ2VuZXJhdGVkIiwgImNvcnB1cyIgfSkgZG8KICBpZiBub3Qgb3B0c1trXSB0aGVuCiAgICBpby5zdGRlcnI6d3JpdGUoImZ1eno6IC0tIiAuLiBrOmdzdWIoIl8iLCAiLSIpIC4uICIgaXMgcmVxdWlyZWRcbiIpCiAgICBvcy5leGl0KDIpCiAgZW5kCmVuZAoKbG9jYWwgR0VORVJBVEVEID0gb3B0cy5nZW5lcmF0ZWQKCmxvY2FsIGZ1bmN0aW9uIHJlYWRGaWxlKHBhdGgpCiAgbG9jYWwgZiwgZXJyID0gaW8ub3BlbihwYXRoLCAicmIiKQogIGlmIG5vdCBmIHRoZW4gcmV0dXJuIG5pbCwgZXJyIGVuZAogIGxvY2FsIGRhdGEgPSBmOnJlYWQoIiphIikKICBmOmNsb3NlKCkKICByZXR1cm4gZGF0YQplbmQKCmxvY2FsIGZ1bmN0aW9uIHdyaXRlRmlsZShwYXRoLCBkYXRhKQogIGxvY2FsIGYgPSBhc3NlcnQoaW8ub3BlbihwYXRoLCAid2IiKSkKICBmOndyaXRlKGRhdGEpCiAgZjpjbG9zZSgpCmVuZAoKbG9jYWwgZnVuY3Rpb24gbGluZXMocGF0aCkKICBsb2NhbCBmID0gYXNzZXJ0KGlvLm9wZW4ocGF0aCwgInIiKSkKICBsb2NhbCBvdXQgPSB7fQogIGZvciByYXcgaW4gZjpsaW5lcygpIGRvCiAgICBsb2NhbCB0ZXh0ID0gcmF3OmdzdWIoIl4lcysiLCAiIik6Z3N1YigiJXMrJCIsICIiKQogICAgaWYgI3RleHQgPiAwIGFuZCB0ZXh0OnN1YigxLCAxKSB+PSAiIyIgdGhlbiBvdXRbI291dCArIDFdID0gdGV4dCBlbmQKICBlbmQKICBmOmNsb3NlKCkKICByZXR1cm4gb3V0CmVuZAoKLS0gQkFTRTY0LCB0aGUgdGV4dCBmb3JtJ3MgYm9keSBlbmNvZGluZyAodGV4dC5sdWEpLgpsb2NhbCBCNjQgPSAiQUJDREVGR0hJSktMTU5PUFFSU1RVVldYWVphYmNkZWZnaGlqa2xtbm9wcXJzdHV2d3h5ejAxMjM0NTY3ODkrLyIKbG9jYWwgZnVuY3Rpb24gYjY0KGJ5dGVzKQogIGxvY2FsIG91dCA9IHt9CiAgZm9yIHAgPSAxLCAjYnl0ZXMsIDMgZG8KICAgIGxvY2FsIGEsIGIsIGMgPSBieXRlczpieXRlKHAsIHAgKyAyKQogICAgbG9jYWwgbiA9IChhIDw8IDE2KSB8ICgoYiBvciAwKSA8PCA4KSB8IChjIG9yIDApCiAgICBvdXRbI291dCArIDFdID0gQjY0OnN1YigoKG4gPj4gMTgpICYgNjMpICsgMSwgKChuID4+IDE4KSAmIDYzKSArIDEpCiAgICBvdXRbI291dCArIDFdID0gQjY0OnN1YigoKG4gPj4gMTIpICYgNjMpICsgMSwgKChuID4+IDEyKSAmIDYzKSArIDEpCiAgICBvdXRbI291dCArIDFdID0gYiBhbmQgQjY0OnN1YigoKG4gPj4gNikgJiA2MykgKyAxLCAoKG4gPj4gNikgJiA2MykgKyAxKSBvciAiPSIKICAgIG91dFsjb3V0ICsgMV0gPSBjIGFuZCBCNjQ6c3ViKChuICYgNjMpICsgMSwgKG4gJiA2MykgKyAxKSBvciAiPSIKICBlbmQKICByZXR1cm4gdGFibGUuY29uY2F0KG91dCkKZW5kCmxvY2FsIGZ1bmN0aW9uIHVuYjY0KHRleHQpCiAgbG9jYWwgbWFwID0ge30KICBmb3IgaiA9IDEsICNCNjQgZG8gbWFwW0I2NDpzdWIoaiwgaildID0gaiAtIDEgZW5kCiAgbG9jYWwgb3V0ID0ge30KICBsb2NhbCBuLCBiaXRzID0gMCwgMAogIGZvciBqID0gMSwgI3RleHQgZG8KICAgIGxvY2FsIGMgPSB0ZXh0OnN1YihqLCBqKQogICAgaWYgYyA9PSAiPSIgdGhlbiBicmVhayBlbmQKICAgIGxvY2FsIHYgPSBtYXBbY10KICAgIGlmIG5vdCB2IHRoZW4gcmV0dXJuIG5pbCBlbmQKICAgIG4gPSAobiA8PCA2KSB8IHYKICAgIGJpdHMgPSBiaXRzICsgNgogICAgaWYgYml0cyA+PSA4IHRoZW4KICAgICAgYml0cyA9IGJpdHMgLSA4CiAgICAgIG91dFsjb3V0ICsgMV0gPSBzdHJpbmcuY2hhcigobiA+PiBiaXRzKSAmIDI1NSkKICAgIGVuZAogIGVuZAogIHJldHVybiB0YWJsZS5jb25jYXQob3V0KQplbmQKCi0tIEEgQ09SUFVTIExJTkUgPGtpbmQ+IDx1bml0PiA8dGFibGU+IDxiYXNlNjQgcGF5bG9hZD4gaXMgYm90aCB0aGUgZmluZGluZwotLSBmaWxlIGFuZCB0aGUgcmVwbGF5IGlucHV0OiB0aGUgcGF5bG9hZCBpcyB0aGUgZXhhY3QgYnl0ZXMsIHNvIGEgcmVwbGF5Ci0tIHJlcHJvZHVjZXMgdGhlIGlucHV0IHdpdGhvdXQgdGhlIFBSTkcuCmxvY2FsIGZ1bmN0aW9uIGRlY29kZUNhc2UodGV4dCkKICB0ZXh0ID0gdGV4dDpnc3ViKCIlcyskIiwgIiIpCiAgbG9jYWwga2luZCwgdW5pdCwgdGFibGVOYW1lLCBwYXlsb2FkID0gdGV4dDptYXRjaCgiXiglUyspJXMrKCVTKyklcysoJVMrKSVzKyglUyspJCIpCiAgaWYgbm90IGtpbmQgdGhlbiByZXR1cm4gbmlsIGVuZAogIGxvY2FsIGJ5dGVzID0gdW5iNjQocGF5bG9hZCkKICBpZiBub3QgYnl0ZXMgdGhlbiByZXR1cm4gbmlsIGVuZAogIHJldHVybiB7IGtpbmQgPSBraW5kLCB1bml0ID0gdW5pdCwgdGFibGUgPSB0YWJsZU5hbWUsIGJ5dGVzID0gYnl0ZXMgfQplbmQKCmxvY2FsIGZ1bmN0aW9uIGVuY29kZUNhc2UoYykKICByZXR1cm4gYy5raW5kIC4uICIgIiAuLiBjLnVuaXQgLi4gIiAiIC4uIGMudGFibGUgLi4gIiAiIC4uIGI2NChjLmJ5dGVzKSAuLiAiXG4iCmVuZAoKLS0gVEhFIFRZUEVEIFJFRlVTQUxTOiByZWZ1c2FsLmx1YSdzIG5hbWVzLCBwbHVzIHRleHQubHVhJ3MgYmFzZTY0X2RhbWFnZWQgYW5kCi0tIHRoZSBydW50aW1lJ3MgaW52YWxpZF92YWx1ZS4gQSBMdWEgZXJyb3Igd2hvc2UgbWVzc2FnZSBpcyBvbmUgb2YgdGhlc2UgaXMgYQotLSBjbGVhbiByZWZ1c2FsLCBhbmQgdGhlIG9yYWNsZSBhY2NlcHRzIGl0Lgpsb2NhbCBUWVBFRCA9IHsKICBwcmV2aW91c19mb3JtID0gdHJ1ZSwgdW5rbm93bl9mb3JtID0gdHJ1ZSwgbWVzc2FnZV9mb3JtX2FzX2ZpbGUgPSB0cnVlLAogIGludGVncml0eV9taXNtYXRjaCA9IHRydWUsIG1hbGZvcm1lZCA9IHRydWUsIGNsYW1wZWQgPSB0cnVlLAogIGtpbmRfbWlzbWF0Y2ggPSB0cnVlLCBkdXBsaWNhdGUgPSB0cnVlLCB1bmtub3duID0gdHJ1ZSwKICBiYXNlNjRfZGFtYWdlZCA9IHRydWUsIGludmFsaWRfdmFsdWUgPSB0cnVlLAp9CmxvY2FsIGZ1bmN0aW9uIHR5cGVkKGVycikKICBsb2NhbCBzID0gdG9zdHJpbmcoZXJyKQogIGlmIFRZUEVEW3NdIHRoZW4gcmV0dXJuIHRydWUgZW5kCiAgbG9jYWwgbmFtZSA9IHM6bWF0Y2goIl4oWyVhX10rKToiKSBvciBzOm1hdGNoKCI6JWQrOiAoWyVhX10rKSIpCiAgcmV0dXJuIG5hbWUgfj0gbmlsIGFuZCBUWVBFRFtuYW1lXSA9PSB0cnVlCmVuZAoKLS0gVEhFIFVOSVRTLiBwYWNrYWdlLmxvYWRlZCBpcyBjbGVhcmVkIGJlZm9yZSBlYWNoIHVuaXQgYmVjYXVzZSBldmVyeSB1bml0Ci0tIHNoaXBzIGl0cyBvd24gZGVzY3JpcHRvci9sb2FkL3JlZnVzYWwvanNvbi90ZXh0IG1vZHVsZXMgdW5kZXIgdGhlIHNhbWUKLS0gbmFtZXM7IHJlcXVpcmUoKSB3b3VsZCBoYW5kIGEgdW5pdCB0aGUgcHJldmlvdXMgdW5pdCdzIHJ1bnRpbWUgb3RoZXJ3aXNlLgpsb2NhbCB1bml0cyA9IHt9CmZvciBfLCBsaW5lIGluIGlwYWlycyhsaW5lcyhvcHRzLmNvcnB1cykpIGRvCiAgbG9jYWwga2V5LCBkaXIgPSBsaW5lOm1hdGNoKCJeKCVTKyklcysoJVMrKSQiKQogIGlmIGtleSB0aGVuIHVuaXRzWyN1bml0cyArIDFdID0geyBrZXkgPSBrZXksIGRpciA9IGRpciB9IGVuZAplbmQKCmxvY2FsIGZ1bmN0aW9uIHRhYmxlc01vZHVsZShrZXkpCiAgbG9jYWwgcCA9IGlvLnBvcGVuKCJscyAiIC4uIEdFTkVSQVRFRCAuLiAiLyIgLi4ga2V5IC4uICIvKlRhYmxlcy5sdWEgMj4vZGV2L251bGwgfCBoZWFkIC0xIikKICBsb2NhbCBwYXRoID0gcDpyZWFkKCIqbCIpCiAgcDpjbG9zZSgpCiAgcmV0dXJuIHBhdGgKZW5kCgpsb2NhbCBmdW5jdGlvbiBsb2FkVW5pdChrZXkpCiAgbG9jYWwgbW9kcGF0aCA9IHRhYmxlc01vZHVsZShrZXkpCiAgaWYgbm90IG1vZHBhdGggdGhlbiByZXR1cm4gbmlsIGVuZAogIGxvY2FsIGRpciA9IG1vZHBhdGg6bWF0Y2goIl4oLiopL1teL10rJCIpCiAgZm9yIG5hbWUgaW4gcGFpcnMocGFja2FnZS5sb2FkZWQpIGRvIHBhY2thZ2UubG9hZGVkW25hbWVdID0gbmlsIGVuZAogIHBhY2thZ2UucGF0aCA9IGRpciAuLiAiLz8ubHVhOyIgLi4gcGFja2FnZS5wYXRoCiAgbG9jYWwgVCA9IGRvZmlsZShtb2RwYXRoKQogIGxvY2FsIEwgPSBkb2ZpbGUoZGlyIC4uICIvbG9hZC5sdWEiKQogIGxvY2FsIFIgPSBkb2ZpbGUoZGlyIC4uICIvcmVmdXNhbC5sdWEiKQogIGxvY2FsIFggPSBkb2ZpbGUoZGlyIC4uICIvdGV4dC5sdWEiKQogIGxvY2FsIEogPSBkb2ZpbGUoZGlyIC4uICIvanNvbi5sdWEiKQogIGxvY2FsIG5hbWVzID0ge30KICBmb3IgbmFtZSwgZGVzYyBpbiBwYWlycyhULlRhYmxlcyBvciB7fSkgZG8KICAgIGlmIGRlc2MudGFibGUgdGhlbiBuYW1lc1sjbmFtZXMgKyAxXSA9IG5hbWUgZW5kCiAgZW5kCiAgdGFibGUuc29ydChuYW1lcykKICByZXR1cm4geyBrZXkgPSBrZXksIFQgPSBULCBMID0gTCwgUiA9IFIsIFggPSBYLCBKID0gSiwgbmFtZXMgPSBuYW1lcyB9CmVuZAoKbG9jYWwgbG9hZGVkID0ge30KbG9jYWwgdGFyZ2V0cyA9IHt9CmZvciBfLCB1IGluIGlwYWlycyh1bml0cykgZG8KICBsb2NhbCBtID0gbG9hZFVuaXQodS5rZXkpCiAgaWYgbSB0aGVuCiAgICBsb2FkZWRbdS5rZXldID0gbQogICAgZm9yIF8sIG5hbWUgaW4gaXBhaXJzKG0ubmFtZXMpIGRvCiAgICAgIGZvciBfLCBraW5kIGluIGlwYWlycyh7ICJsb2FkIiwgInN0cmljdCIsICJ0ZXh0IiwgImpzb24iIH0pIGRvCiAgICAgICAgdGFyZ2V0c1sjdGFyZ2V0cyArIDFdID0geyB1bml0ID0gdS5rZXksIHRhYmxlID0gbmFtZSwga2luZCA9IGtpbmQgfQogICAgICBlbmQKICAgIGVuZAogIGVuZAplbmQKaWYgI3RhcmdldHMgPT0gMCB0aGVuCiAgaW8uc3RkZXJyOndyaXRlKCJmdXp6OiBubyBnZW5lcmF0ZWQgdW5pdCB1bmRlciAiIC4uIEdFTkVSQVRFRCAuLiAiIChydW4gdGhlIG1ha2UgdGFyZ2V0KVxuIikKICBvcy5leGl0KDIpCmVuZAoKLS0gVEhFIFNFRURTLiBXaXJlIHZlY3RvcnMgZHJpdmUgbG9hZC9zdHJpY3QvdGV4dCAodGV4dCBpcyB0aGVpciBiYXNlNjQgYm9keSk7Ci0tIEpTT04gdmVjdG9ycyBkcml2ZSBqc29uLiBUaGUgR28gZnV6eiBjb3JwdXMgKGludGVybmFsL2Z1enovdGVzdGRhdGEpIGlzCi0tIGluY2x1ZGVkIGFzIHJhdyBtdXRhdGlvbiByb290cywgYXMgdGhlIGNhcmQgYXNrcy4KbG9jYWwgc2VlZHMgPSB7IGxvYWQgPSB7fSwgc3RyaWN0ID0ge30sIHRleHQgPSB7fSwganNvbiA9IHt9IH0KbG9jYWwgZnVuY3Rpb24gYWRkU2VlZHMoa2luZCwgZGlyLCBtYXgpCiAgbG9jYWwgcCA9IGlvLnBvcGVuKCJmaW5kICIgLi4gZGlyIC4uICIgLXR5cGUgZiAyPi9kZXYvbnVsbCB8IHNvcnQiKQogIGZvciBwYXRoIGluIHA6bGluZXMoKSBkbwogICAgbG9jYWwgZGF0YSA9IHJlYWRGaWxlKHBhdGgpCiAgICBpZiBkYXRhIGFuZCAjZGF0YSA+IDAgYW5kICNkYXRhIDw9IChtYXggb3IgNDA5NikgdGhlbgogICAgICBzZWVkc1traW5kXVsjc2VlZHNba2luZF0gKyAxXSA9IGRhdGEKICAgIGVuZAogIGVuZAogIHA6Y2xvc2UoKQplbmQKaWYgb3B0cy53aXJlX3NlZWRzIHRoZW4KICBmb3IgZGlyIGluIG9wdHMud2lyZV9zZWVkczpnbWF0Y2goIlteOl0rIikgZG8KICAgIGFkZFNlZWRzKCJsb2FkIiwgZGlyKTsgYWRkU2VlZHMoInN0cmljdCIsIGRpcik7IGFkZFNlZWRzKCJ0ZXh0IiwgZGlyKQogIGVuZAplbmQKaWYgb3B0cy5qc29uX3NlZWRzIHRoZW4KICBmb3IgZGlyIGluIG9wdHMuanNvbl9zZWVkczpnbWF0Y2goIlteOl0rIikgZG8gYWRkU2VlZHMoImpzb24iLCBkaXIpIGVuZAplbmQKaWYgb3B0cy50ZXh0X3NlZWRzIHRoZW4KICBmb3IgZGlyIGluIG9wdHMudGV4dF9zZWVkczpnbWF0Y2goIlteOl0rIikgZG8KICAgIGZvciBfLCBkYXRhIGluIGlwYWlycygoZnVuY3Rpb24oKQogICAgICBsb2NhbCBvdXQgPSB7fQogICAgICBsb2NhbCBwID0gaW8ucG9wZW4oImZpbmQgIiAuLiBkaXIgLi4gIiAtdHlwZSBmIDI+L2Rldi9udWxsIHwgc29ydCIpCiAgICAgIGZvciBwYXRoIGluIHA6bGluZXMoKSBkbwogICAgICAgIGxvY2FsIGQgPSByZWFkRmlsZShwYXRoKQogICAgICAgIGlmIGQgdGhlbiBvdXRbI291dCArIDFdID0gZCBlbmQKICAgICAgZW5kCiAgICAgIHA6Y2xvc2UoKQogICAgICByZXR1cm4gb3V0CiAgICBlbmQpKCkpIGRvIHNlZWRzLnRleHRbI3NlZWRzLnRleHQgKyAxXSA9IGRhdGEgZW5kCiAgZW5kCmVuZApmb3IgXywga2luZCBpbiBpcGFpcnMoeyAibG9hZCIsICJzdHJpY3QiLCAidGV4dCIgfSkgZG8KICBpZiAjc2VlZHNba2luZF0gPT0gMCB0aGVuIHNlZWRzW2tpbmRdWzFdID0gc3RyaW5nLmNoYXIoMSkgLi4gIlwwXDBcMFwwIiBlbmQKZW5kCmlmICNzZWVkcy5qc29uID09IDAgdGhlbiBzZWVkcy5qc29uWzFdID0gInt9IiBlbmQKCi0tIE1VVEFUSU9OLCBzZWVkZWQgYW5kIGRldGVybWluaXN0aWM6IHhvcnNoaWZ0NjQqIG92ZXIgTHVhIDUuNCBpbnRlZ2Vycy4KbG9jYWwgc3RhdGUgPSB0b251bWJlcihvcHRzLnNlZWQpIG9yIDEKaWYgc3RhdGUgPT0gMCB0aGVuIHN0YXRlID0gMSBlbmQKbG9jYWwgZnVuY3Rpb24gcm5kKCkKICBzdGF0ZSA9IHN0YXRlIH4gKHN0YXRlIDw8IDEzKQogIHN0YXRlID0gc3RhdGUgfiAoc3RhdGUgPj4gNykKICBzdGF0ZSA9IHN0YXRlIH4gKHN0YXRlIDw8IDE3KQogIHJldHVybiBzdGF0ZQplbmQKbG9jYWwgZnVuY3Rpb24gcGlja24obikKICBsb2NhbCB2ID0gcm5kKCkgJiAweDdmZmZmZmZmZmZmZmZmZmYKICByZXR1cm4gKHYgJSBuKSArIDEKZW5kCgpsb2NhbCBJTlRFUkVTVElORyA9IHsgMCwgMSwgMiwgMywgMHg3ZiwgMHg4MCwgMHg4MSwgMHhmZSwgMHhmZiwgMHgzZCwgMHg3YiwgMHgyMiB9Cgpsb2NhbCBmdW5jdGlvbiBtdXRhdGUoYmFzZSkKICBsb2NhbCBieXRlcyA9IHsgYmFzZTpieXRlKDEsIG1hdGgubWluKCNiYXNlLCA0MDk2KSkgfQogIGxvY2FsIGVkaXRzID0gcGlja24oNikKICBmb3IgXyA9IDEsIGVkaXRzIGRvCiAgICBsb2NhbCBvcCA9IHBpY2tuKDYpCiAgICBsb2NhbCBuID0gI2J5dGVzCiAgICBpZiBvcCA9PSAxIGFuZCBuID4gMCB0aGVuCiAgICAgIGxvY2FsIGF0ID0gcGlja24obikKICAgICAgbG9jYWwgYml0ID0gMSA8PCAocGlja24oOCkgLSAxKQogICAgICBieXRlc1thdF0gPSAoYnl0ZXNbYXRdIH4gYml0KSAmIDI1NQogICAgZWxzZWlmIG9wID09IDIgYW5kIG4gPiAwIHRoZW4KICAgICAgYnl0ZXNbcGlja24obildID0gSU5URVJFU1RJTkdbcGlja24oI0lOVEVSRVNUSU5HKV0KICAgIGVsc2VpZiBvcCA9PSAzIGFuZCBuIDwgNDA5NiB0aGVuCiAgICAgIGxvY2FsIGF0ID0gcGlja24obiArIDEpCiAgICAgIHRhYmxlLmluc2VydChieXRlcywgYXQsIElOVEVSRVNUSU5HW3BpY2tuKCNJTlRFUkVTVElORyldKQogICAgZWxzZWlmIG9wID09IDQgYW5kIG4gPiAxIHRoZW4KICAgICAgdGFibGUucmVtb3ZlKGJ5dGVzLCBwaWNrbihuKSkKICAgIGVsc2VpZiBvcCA9PSA1IGFuZCBuID4gMCBhbmQgbiA8IDQwOTYgdGhlbgogICAgICBsb2NhbCBmcm9tID0gcGlja24obikKICAgICAgbG9jYWwgbGVuID0gcGlja24obWF0aC5taW4oMTYsIG4gLSBmcm9tICsgMSkpCiAgICAgIGxvY2FsIGF0ID0gcGlja24obiArIDEpCiAgICAgIGZvciBqID0gbGVuLCAxLCAtMSBkbyB0YWJsZS5pbnNlcnQoYnl0ZXMsIGF0LCBieXRlc1tmcm9tICsgaiAtIDFdKSBlbmQKICAgIGVsc2VpZiBvcCA9PSA2IGFuZCBuID4gMCB0aGVuCiAgICAgIGxvY2FsIGF0ID0gcGlja24obikKICAgICAgYnl0ZXNbYXRdID0gcm5kKCkgJiAyNTUKICAgIGVuZAogIGVuZAogIHJldHVybiBzdHJpbmcuY2hhcih0YWJsZS51bnBhY2soYnl0ZXMpKQplbmQKCi0tIFRIRSBSRUFERVJTLiBFYWNoIHJldHVybnMgdHJ1ZSB3aGVuIHRoZSBpbnB1dCB3YXMgaGFuZGxlZCAoYSB2YWx1ZSBvciBhCi0tIGNsZWFuIHR5cGVkIHJlZnVzYWwpIGFuZCByYWlzZXMgb25seSB0aGUgcmF3IEx1YSBlcnJvciB0aGUgb3JhY2xlIHJlcG9ydHMuCmxvY2FsIGZ1bmN0aW9uIGRyaXZlKGMpCiAgbG9jYWwgdSA9IGxvYWRlZFtjLnVuaXRdCiAgaWYgbm90IHUgdGhlbiByZXR1cm4gZW5kCiAgbG9jYWwgVCwgbmFtZSwgYnl0ZXMgPSB1LlQsIGMudGFibGUsIGMuYnl0ZXMKICBpZiBjLmtpbmQgPT0gImxvYWQiIHRoZW4KICAgIGxvY2FsIHJlcG9ydCA9IHsKICAgICAgbWFsZm9ybWVkID0gZmFsc2UsIHJlZnVzZWQgPSAwLCB1bmtub3duID0gMCwga2luZE1pc21hdGNoID0gMCwKICAgICAgY2xhbXBlZCA9IDAsIHdpZGVuZWQgPSAwLCBkdXBsaWNhdGUgPSAwLAogICAgfQogICAgdS5MLkxvYWQoVCwgbmFtZSwgYnl0ZXMsIHJlcG9ydCkKICBlbHNlaWYgYy5raW5kID09ICJzdHJpY3QiIHRoZW4KICAgIHUuUi5SZWFkU3RyaWN0KFQsIG5hbWUsIGJ5dGVzKQogIGVsc2VpZiBjLmtpbmQgPT0gInRleHQiIHRoZW4KICAgIHUuWC5Mb2FkVGV4dChULCBuYW1lLCBiNjQoYnl0ZXMpKQogIGVsc2VpZiBjLmtpbmQgPT0gImpzb24iIHRoZW4KICAgIGxvY2FsIHJlcG9ydCA9IHsKICAgICAgbWFsZm9ybWVkID0gZmFsc2UsIHJlZnVzZWQgPSAwLCB1bmtub3duID0gMCwga2luZE1pc21hdGNoID0gMCwKICAgICAgY2xhbXBlZCA9IDAsIHdpZGVuZWQgPSAwLCBkdXBsaWNhdGUgPSAwLAogICAgfQogICAgdS5KLkZyb21Kc29uKFQsIG5hbWUsIGJ5dGVzLCByZXBvcnQpCiAgZW5kCmVuZAoKbG9jYWwgZmluZGluZ3NEaXIgPSBvcHRzLmZpbmRpbmdzCmxvY2FsIGZ1bmN0aW9uIHNhdmVGaW5kaW5nKGMsIGVycikKICBpZiBub3QgZmluZGluZ3NEaXIgb3IgZmluZGluZ3NEaXIgPT0gIiIgdGhlbiByZXR1cm4gZW5kCiAgb3MuZXhlY3V0ZSgibWtkaXIgLXAgIiAuLiBmaW5kaW5nc0RpcikKICBsb2NhbCBzbHVnID0gYy5raW5kIC4uICJfIiAuLiBjLnVuaXQgLi4gIl8iIC4uIGMudGFibGU6Z3N1YigiW14ld19dIiwgIl8iKQogIGxvY2FsIG4gPSAxCiAgbG9jYWwgcGF0aAogIHJlcGVhdAogICAgcGF0aCA9IGZpbmRpbmdzRGlyIC4uICIvIiAuLiBzbHVnIC4uICJfIiAuLiBuIC4uICIuY2FzZSIKICAgIGxvY2FsIGYgPSBpby5vcGVuKHBhdGgsICJyIikKICAgIGlmIGYgdGhlbiBmOmNsb3NlKCk7IHBhdGggPSBuaWw7IG4gPSBuICsgMSBlbmQKICB1bnRpbCBwYXRoCiAgd3JpdGVGaWxlKHBhdGgsIGVuY29kZUNhc2UoYykpCiAgd3JpdGVGaWxlKHBhdGggLi4gIi5lcnIiLCB0b3N0cmluZyhlcnIpIC4uICJcbiIpCiAgcmV0dXJuIHBhdGgKZW5kCgotLSBSRVBMQVk6IGV2ZXJ5IHNhdmVkIGNhc2UsIG9uY2UsIHdpdGggbm8gY2xvY2suIEEgc2F2ZWQgZmluZGluZyB0aGF0IG5vCi0tIGxvbmdlciBmaXJlcyBpcyBzdGlsbCByZXBsYXllZDsgYSBjYXNlIHRoYXQgZmlyZXMgaXMgYSByZWdyZXNzaW9uLgppZiBvcHRzLnJlcGxheSBhbmQgb3B0cy5yZXBsYXkgfj0gIiIgdGhlbgogIGxvY2FsIGNhc2VzID0gMAogIGxvY2FsIGJhZCA9IDAKICBsb2NhbCBwID0gaW8ucG9wZW4oImZpbmQgIiAuLiBvcHRzLnJlcGxheSAuLiAiIC10eXBlIGYgLW5hbWUgJyouY2FzZScgMj4vZGV2L251bGwgfCBzb3J0IikKICBmb3IgcGF0aCBpbiBwOmxpbmVzKCkgZG8KICAgIGxvY2FsIHRleHQgPSByZWFkRmlsZShwYXRoKQogICAgbG9jYWwgYyA9IHRleHQgYW5kIGRlY29kZUNhc2UodGV4dCkKICAgIGlmIGMgdGhlbgogICAgICBjYXNlcyA9IGNhc2VzICsgMQogICAgICBsb2NhbCBvaywgZXJyID0gcGNhbGwoZHJpdmUsIGMpCiAgICAgIGlmIG5vdCBvayBhbmQgbm90IHR5cGVkKGVycikgdGhlbgogICAgICAgIGJhZCA9IGJhZCArIDEKICAgICAgICBpby5zdGRlcnI6d3JpdGUoIlJFUExBWSBGQUlMICIgLi4gcGF0aCAuLiAiOiAiIC4uIHRvc3RyaW5nKGVycikgLi4gIlxuIikKICAgICAgZW5kCiAgICBlbmQKICBlbmQKICBwOmNsb3NlKCkKICBpby53cml0ZShzdHJpbmcuZm9ybWF0KCJsdWEgZnV6eiByZXBsYXk6ICVkIGNhc2VzLCAlZCBmYWlsZWRcbiIsIGNhc2VzLCBiYWQpKQogIG9zLmV4aXQoYmFkID09IDAgYW5kIDAgb3IgMSkKZW5kCgotLSBGVVpaOiB0aGUgdGltZSBidWRnZXQgaXMgYW4gYXJndW1lbnQ7IC0taXRlcmF0aW9ucyBib3VuZHMgYSBjb250cm9sIHJ1bi4KbG9jYWwgc2Vjb25kcyA9IHRvbnVtYmVyKG9wdHMuc2Vjb25kcykgb3IgMTAKbG9jYWwgaXRlcmF0aW9ucyA9IHRvbnVtYmVyKG9wdHMuaXRlcmF0aW9ucykgb3IgMApsb2NhbCBkZWFkbGluZSA9IG9zLnRpbWUoKSArIHNlY29uZHMKbG9jYWwgY291bnQsIGZhaWxzID0gMCwgMAp3aGlsZSB0cnVlIGRvCiAgaWYgaXRlcmF0aW9ucyA+IDAgdGhlbgogICAgaWYgY291bnQgPj0gaXRlcmF0aW9ucyB0aGVuIGJyZWFrIGVuZAogIGVsc2VpZiBvcy50aW1lKCkgPj0gZGVhZGxpbmUgdGhlbgogICAgYnJlYWsKICBlbmQKICBsb2NhbCB0ID0gdGFyZ2V0c1twaWNrbigjdGFyZ2V0cyldCiAgbG9jYWwga2luZCA9IHQua2luZAogIGxvY2FsIGJhc2UgPSBzZWVkc1traW5kXVtwaWNrbigjc2VlZHNba2luZF0pXQogIGxvY2FsIGMgPSB7IGtpbmQgPSBraW5kLCB1bml0ID0gdC51bml0LCB0YWJsZSA9IHQudGFibGUsIGJ5dGVzID0gbXV0YXRlKGJhc2UpIH0KICBsb2NhbCBvaywgZXJyID0gcGNhbGwoZHJpdmUsIGMpCiAgY291bnQgPSBjb3VudCArIDEKICBpZiBub3Qgb2sgYW5kIG5vdCB0eXBlZChlcnIpIHRoZW4KICAgIGZhaWxzID0gZmFpbHMgKyAxCiAgICBsb2NhbCBwYXRoID0gc2F2ZUZpbmRpbmcoYywgZXJyKQogICAgaW8uc3RkZXJyOndyaXRlKCJGVVpaIEZBSUwgIiAuLiAocGF0aCBvciAiKG5vIGZpbmRpbmcgZGlyKSIpIC4uICI6ICIgLi4gdG9zdHJpbmcoZXJyKSAuLiAiXG4iKQogICAgaWYgZmFpbHMgPj0gMTAgdGhlbiBicmVhayBlbmQKICBlbmQKZW5kCmlvLndyaXRlKHN0cmluZy5mb3JtYXQoImx1YSBmdXp6OiB0YXJnZXRzPSVkIGl0ZXJhdGlvbnM9JWQgZmFpbHVyZXM9JWQgc2VlZD0lZFxuIiwKICAjdGFyZ2V0cywgY291bnQsIGZhaWxzLCB0b251bWJlcihvcHRzLnNlZWQpIG9yIDEpKQpvcy5leGl0KGZhaWxzID09IDAgYW5kIDAgb3IgMSkK

$(LUA_FUZZ_SCRIPT): make/schema-lua-nightly-fuzz.mk
	@mkdir -p $(dir $@)
	@printf '%s' '$(LUA_FUZZ_HARNESS_B64)' | base64 -d > $@ 2>/dev/null || \
		printf '%s' '$(LUA_FUZZ_HARNESS_B64)' | base64 -D > $@
	@chmod +x $@
	@echo "schema-lua-fuzz: harness rev $(LUA_FUZZ_HARNESS_REV) written to $@"

# A SHORT WIRE ROOT: the truncated-body input the planted control needs,
# generated (this card may not commit it to the port's test tree).
$(LUA_FUZZ_SEEDDIR)/truncated.bin: make/schema-lua-nightly-fuzz.mk
	@mkdir -p $(dir $@)
	@printf '\001\000\000\000\000\000\000\000\000' > $@
	@echo "schema-lua-fuzz: 9-byte truncated wire seed written to $@"

# one generated unit per corpus line; the tag-built compiler is the port's own
# emitter, so the fuzzer and the gate cannot disagree about what was generated.
$(LUA_FUZZ_GENERATED)/.stamp: build/schema-lua $(LUA_FUZZ_CORPUS) $(LUA_FUZZ_SCRIPT) $(LUA_FUZZ_SEEDDIR)/truncated.bin make/schema-lua-nightly-fuzz.mk
	@rm -rf $(LUA_FUZZ_GENERATED) && mkdir -p $(LUA_FUZZ_GENERATED)
	@while read -r key dir; do \
		[ -n "$$key" ] || continue; \
		if ! ./build/schema-lua generate --lang lua --out $(LUA_FUZZ_GENERATED)/$$key $$dir > $(LUA_FUZZ_GENERATED)/$$key.log 2>&1; then \
			rm -rf $(LUA_FUZZ_GENERATED)/$$key; \
			mv $(LUA_FUZZ_GENERATED)/$$key.log $(LUA_FUZZ_GENERATED)/$$key.refused; \
			echo "schema-lua-fuzz: $$key refused: $$(head -1 $(LUA_FUZZ_GENERATED)/$$key.refused)"; \
			continue; \
		fi; \
		rm -f $(LUA_FUZZ_GENERATED)/$$key.log; \
	done < $(LUA_FUZZ_CORPUS)
	@echo "schema-lua-fuzz: $$(ls -d $(LUA_FUZZ_GENERATED)/*/ | wc -l | tr -d ' ') units generated from $(LUA_FUZZ_CORPUS)"
	@touch $@

# THE NIGHTLY TARGET: the time budget is the argument (default ten minutes).
.PHONY: schema-lua-nightly-fuzz
schema-lua-nightly-fuzz: schema-lua-fuzz-toolchain $(LUA_FUZZ_GENERATED)/.stamp
	@rm -rf $(LUA_FUZZ_FINDINGS) && mkdir -p $(LUA_FUZZ_FINDINGS)
	$(LUA_FUZZ_LUA) $(LUA_FUZZ_SCRIPT) --generated $(LUA_FUZZ_GENERATED) \
		--corpus $(LUA_FUZZ_CORPUS) --seconds $(LUA_FUZZ_SECONDS) \
		--seed $(LUA_FUZZ_SEED) --findings $(LUA_FUZZ_FINDINGS) $(LUA_FUZZ_SEEDS)
	@if [ -n "$$(ls $(LUA_FUZZ_FINDINGS)/*.case 2>/dev/null)" ]; then \
		echo "schema-lua-nightly-fuzz: findings under $(LUA_FUZZ_FINDINGS):"; \
		ls $(LUA_FUZZ_FINDINGS)/*.case; exit 1; \
	fi

# THE PLANTED-BAD-PATTERN CONTROL. A scratch copy of the generated tree has the
# short-file guard in load.lua's open removed (`if #bytes < 9` becomes
# `if false`), which turns a truncated body into a raw string.unpack error.
# The fuzzer must find it inside a bounded run, the saved case must then FAIL
# replay against the sabotage, and the same case must PASS replay against the
# clean tree — so the control shows both the red line and the absence of a
# false red on the port.
.PHONY: schema-lua-fuzz-negative-control
schema-lua-fuzz-negative-control: schema-lua-fuzz-toolchain $(LUA_FUZZ_GENERATED)/.stamp
	@set -eu; \
	nc=build/lua-fuzz-negative-control; \
	rm -rf $$nc && mkdir -p $$nc/generated $$nc/findings $(LUA_FUZZ_REGRESS); \
	cp -r $(LUA_FUZZ_GENERATED)/. $$nc/generated/; \
	sed -i 's/if #bytes < 9 then report.malformed = true; return nil end/if false then report.malformed = true; return nil end/' $$nc/generated/examples/load.lua; \
	grep -q 'if false then report.malformed = true; return nil end' $$nc/generated/examples/load.lua || \
		{ echo "negative control: the plant did not land"; exit 1; }; \
	if ! $(LUA_FUZZ_LUA) $(LUA_FUZZ_SCRIPT) --generated $$nc/generated \
		--corpus $(LUA_FUZZ_CORPUS) --iterations 4000 --seed 7 \
		--findings $$nc/findings --wire-seeds $(LUA_FUZZ_SEEDDIR) \
		--json-seeds testdata/conformance/tables/json > $$nc/fuzz.log 2>&1; then \
		: ; \
	else \
		echo "NEGATIVE CONTROL FAILED: the planted missing bound survived 4000 iterations"; \
		cat $$nc/fuzz.log; exit 1; \
	fi; \
	case_file=$$(ls $$nc/findings/*.case 2>/dev/null | head -1); \
	[ -n "$$case_file" ] || { echo "NEGATIVE CONTROL FAILED: no saved input"; cat $$nc/fuzz.log; exit 1; }; \
	cp "$$case_file" $(LUA_FUZZ_REGRESS)/planted_missing_bound.case; \
	if $(LUA_FUZZ_LUA) $(LUA_FUZZ_SCRIPT) --generated $$nc/generated \
		--corpus $(LUA_FUZZ_CORPUS) --replay $$nc/findings > $$nc/replay-sabotage.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the saved input did not fail replay on the sabotage"; \
		cat $$nc/replay-sabotage.log; exit 1; \
	fi; \
	if ! $(LUA_FUZZ_LUA) $(LUA_FUZZ_SCRIPT) --generated $(LUA_FUZZ_GENERATED) \
		--corpus $(LUA_FUZZ_CORPUS) --replay $(LUA_FUZZ_REGRESS) > $$nc/replay-clean.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a clean tree failed the same saved input"; \
		cat $$nc/replay-clean.log; exit 1; \
	fi; \
	echo "negative control: removing the reader's length bound turns the Lua fuzz oracle RED and the saved input fails replay; the clean tree stays green"

# THE FAST PARTS: replay every saved input, no clock, no PRNG. This is what the
# pull-request lane runs, after the control has written the saved input. Real
# findings would be committed to a regression corpus (PATHS-PROPOSED); this
# card may not add files to the port's test tree.
.PHONY: schema-lua-fuzz-replay
schema-lua-fuzz-replay: schema-lua-fuzz-toolchain schema-lua-fuzz-negative-control
	$(LUA_FUZZ_LUA) $(LUA_FUZZ_SCRIPT) --generated $(LUA_FUZZ_GENERATED) \
		--corpus $(LUA_FUZZ_CORPUS) --replay $(LUA_FUZZ_REGRESS)
