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
LUA_FUZZ_HARNESS_REV := 8
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
LUA_FUZZ_HARNESS_B64 := LS0gdGVzdC9sdWEtZnV6ei9mdXp6Lmx1YSDigJQgdGhlIEx1YSBwb3J0J3Mgc2VlZGVkIG11dGF0aW9uIGZ1enplciBmb3IgdGhlCi0tIGdlbmVyYXRlZCByZWFkZXJzIChzY2hlbWEtbHVhLW5pZ2h0bHktZnV6eikuCi0tCi0tIFRIRSBIQVJORVNTIHJlYWRzIHRlc3QvbHVhLWZ1enovY29ycHVzLnR4dCAodGhlIHVuaXQga2V5IC0+IHNjaGVtYSBkaXJlY3RvcnkKLS0gcGFpcnMgdGhlIE1ha2VmaWxlJ3MgdGFibGVzX2dlbmVyYXRlIHJlYWRzKSwgbG9hZHMgZXZlcnkgZ2VuZXJhdGVkIHVuaXQncwotLSBUYWJsZXMgbW9kdWxlLCBhbmQgZHJpdmVzIG9uZSBlbnRyeSBwb2ludCBwZXIgcmVhZGVyIGtpbmQgb3ZlciBldmVyeSB0YWJsZQotLSBuYW1lIHRoYXQgbW9kdWxlIHJlZ2lzdGVyczoKLS0KLS0gICBsb2FkICAgIHRoZSB0b2xlcmFudCBmb3JtLTEgbG9hZCAocnVudGltZS9sb2FkLmx1YSBMLkxvYWQpCi0tICAgc3RyaWN0ICB0aGUgc3RyaWN0IHJlYWQgKHJ1bnRpbWUvcmVmdXNhbC5sdWEgUi5SZWFkU3RyaWN0KQotLSAgIHRleHQgICAgdGhlIHRleHQgZm9ybSdzIGJhc2U2NCBib2R5IChydW50aW1lL3RleHQubHVhIFRleHQuTG9hZFRleHQpCi0tICAganNvbiAgICB0aGUgdGV4dCBmb3JtJ3MgSlNPTiBib2R5IChydW50aW1lL2pzb24ubHVhIEouRnJvbUpzb24pCi0tCi0tIFRIRSBPUkFDTEU6IG5vIGNyYXNoLCBubyBoYW5nLCBubyBhc3NlcnQ7IGEgcmVmdXNhbCBpcyBhIGNsZWFuIHR5cGVkIGVycm9yCi0tIChyZWZ1c2FsLmx1YSdzIG5hbWVzKSwgbmV2ZXIgYSByYXcgTHVhIGVycm9yIGFuZCBuZXZlciBhIHNpbGVudCBzdWNjZXNzLgotLSBUaGUgc2VlZCBpcyByZWNvcmRlZCB3aXRoIGV2ZXJ5IGZpbmRpbmcgKHRoZSBmaW5kaW5nIGZpbGUncyBwYXlsb2FkIGlzIHRoZQotLSBleGFjdCBieXRlcywgc28gcmVwbGF5IG5lZWRzIG5vIGNsb2NrIGFuZCBubyBQUk5HKS4KLS0KLS0gdXNhZ2U6Ci0tICAgbHVhIHRlc3QvbHVhLWZ1enovZnV6ei5sdWEgLS1nZW5lcmF0ZWQgRElSIC0tY29ycHVzIEZJTEUKLS0gICAgICAgWy0tc2Vjb25kcyBOIHwgLS1pdGVyYXRpb25zIE5dIFstLXNlZWQgTl0gWy0tZmluZGluZ3MgRElSXQotLSAgICAgICBbLS13aXJlLXNlZWRzIERJUl0gWy0tanNvbi1zZWVkcyBESVJdIFstLXRleHQtc2VlZHMgRElSXQotLSAgIGx1YSB0ZXN0L2x1YS1mdXp6L2Z1enoubHVhIC0tZ2VuZXJhdGVkIERJUiAtLWNvcnB1cyBGSUxFCi0tICAgICAgIC0tcmVwbGF5IERJUiBbLS1maW5kaW5ncyBESVJdCgpsb2NhbCBvcHRzID0gewogIHNlY29uZHMgPSAxMCwgaXRlcmF0aW9ucyA9IDAsIHNlZWQgPSAxLAogIGZpbmRpbmdzID0gIiIsIHJlcGxheSA9ICIiLAp9CmxvY2FsIGkgPSAxCndoaWxlIGkgPD0gI2FyZyBkbwogIGxvY2FsIGEgPSBhcmdbaV0KICBsb2NhbCBrLCB2ID0gYTptYXRjaCgiXiUtJS0oWyV3LV0rKT0oLiopJCIpCiAgaWYgbm90IGsgdGhlbgogICAgayA9IGE6bWF0Y2goIl4lLSUtKFsldy1dKykkIikKICAgIGlmIGsgdGhlbiBpID0gaSArIDE7IHYgPSBhcmdbaV0gZW5kCiAgZW5kCiAgaWYgbm90IGsgdGhlbgogICAgaW8uc3RkZXJyOndyaXRlKCJmdXp6OiB1bmtub3duIGFyZ3VtZW50ICIgLi4gYSAuLiAiXG4iKQogICAgb3MuZXhpdCgyKQogIGVuZAogIGsgPSBrOmdzdWIoIiUtIiwgIl8iKQogIG9wdHNba10gPSB2CiAgaSA9IGkgKyAxCmVuZApmb3IgXywgayBpbiBpcGFpcnMoeyAiZ2VuZXJhdGVkIiwgImNvcnB1cyIgfSkgZG8KICBpZiBub3Qgb3B0c1trXSB0aGVuCiAgICBpby5zdGRlcnI6d3JpdGUoImZ1eno6IC0tIiAuLiBrOmdzdWIoIl8iLCAiLSIpIC4uICIgaXMgcmVxdWlyZWRcbiIpCiAgICBvcy5leGl0KDIpCiAgZW5kCmVuZAoKbG9jYWwgR0VORVJBVEVEID0gb3B0cy5nZW5lcmF0ZWQKCmxvY2FsIGZ1bmN0aW9uIHJlYWRGaWxlKHBhdGgpCiAgbG9jYWwgZiwgZXJyID0gaW8ub3BlbihwYXRoLCAicmIiKQogIGlmIG5vdCBmIHRoZW4gcmV0dXJuIG5pbCwgZXJyIGVuZAogIGxvY2FsIGRhdGEgPSBmOnJlYWQoIiphIikKICBmOmNsb3NlKCkKICByZXR1cm4gZGF0YQplbmQKCmxvY2FsIGZ1bmN0aW9uIHdyaXRlRmlsZShwYXRoLCBkYXRhKQogIGxvY2FsIGYgPSBhc3NlcnQoaW8ub3BlbihwYXRoLCAid2IiKSkKICBmOndyaXRlKGRhdGEpCiAgZjpjbG9zZSgpCmVuZAoKbG9jYWwgZnVuY3Rpb24gbGluZXMocGF0aCkKICBsb2NhbCBmID0gYXNzZXJ0KGlvLm9wZW4ocGF0aCwgInIiKSkKICBsb2NhbCBvdXQgPSB7fQogIGZvciByYXcgaW4gZjpsaW5lcygpIGRvCiAgICBsb2NhbCB0ZXh0ID0gcmF3OmdzdWIoIl4lcysiLCAiIik6Z3N1YigiJXMrJCIsICIiKQogICAgaWYgI3RleHQgPiAwIGFuZCB0ZXh0OnN1YigxLCAxKSB+PSAiIyIgdGhlbiBvdXRbI291dCArIDFdID0gdGV4dCBlbmQKICBlbmQKICBmOmNsb3NlKCkKICByZXR1cm4gb3V0CmVuZAoKLS0gQkFTRTY0LCB0aGUgdGV4dCBmb3JtJ3MgYm9keSBlbmNvZGluZyAodGV4dC5sdWEpLgpsb2NhbCBCNjQgPSAiQUJDREVGR0hJSktMTU5PUFFSU1RVVldYWVphYmNkZWZnaGlqa2xtbm9wcXJzdHV2d3h5ejAxMjM0NTY3ODkrLyIKbG9jYWwgZnVuY3Rpb24gYjY0KGJ5dGVzKQogIGxvY2FsIG91dCA9IHt9CiAgZm9yIHAgPSAxLCAjYnl0ZXMsIDMgZG8KICAgIGxvY2FsIGEsIGIsIGMgPSBieXRlczpieXRlKHAsIHAgKyAyKQogICAgbG9jYWwgbiA9IChhIDw8IDE2KSB8ICgoYiBvciAwKSA8PCA4KSB8IChjIG9yIDApCiAgICBvdXRbI291dCArIDFdID0gQjY0OnN1YigoKG4gPj4gMTgpICYgNjMpICsgMSwgKChuID4+IDE4KSAmIDYzKSArIDEpCiAgICBvdXRbI291dCArIDFdID0gQjY0OnN1YigoKG4gPj4gMTIpICYgNjMpICsgMSwgKChuID4+IDEyKSAmIDYzKSArIDEpCiAgICBvdXRbI291dCArIDFdID0gYiBhbmQgQjY0OnN1YigoKG4gPj4gNikgJiA2MykgKyAxLCAoKG4gPj4gNikgJiA2MykgKyAxKSBvciAiPSIKICAgIG91dFsjb3V0ICsgMV0gPSBjIGFuZCBCNjQ6c3ViKChuICYgNjMpICsgMSwgKG4gJiA2MykgKyAxKSBvciAiPSIKICBlbmQKICByZXR1cm4gdGFibGUuY29uY2F0KG91dCkKZW5kCmxvY2FsIGZ1bmN0aW9uIHVuYjY0KHRleHQpCiAgbG9jYWwgbWFwID0ge30KICBmb3IgaiA9IDEsICNCNjQgZG8gbWFwW0I2NDpzdWIoaiwgaildID0gaiAtIDEgZW5kCiAgbG9jYWwgb3V0ID0ge30KICBsb2NhbCBuLCBiaXRzID0gMCwgMAogIGZvciBqID0gMSwgI3RleHQgZG8KICAgIGxvY2FsIGMgPSB0ZXh0OnN1YihqLCBqKQogICAgaWYgYyA9PSAiPSIgdGhlbiBicmVhayBlbmQKICAgIGxvY2FsIHYgPSBtYXBbY10KICAgIGlmIG5vdCB2IHRoZW4gcmV0dXJuIG5pbCBlbmQKICAgIG4gPSAobiA8PCA2KSB8IHYKICAgIGJpdHMgPSBiaXRzICsgNgogICAgaWYgYml0cyA+PSA4IHRoZW4KICAgICAgYml0cyA9IGJpdHMgLSA4CiAgICAgIG91dFsjb3V0ICsgMV0gPSBzdHJpbmcuY2hhcigobiA+PiBiaXRzKSAmIDI1NSkKICAgIGVuZAogIGVuZAogIHJldHVybiB0YWJsZS5jb25jYXQob3V0KQplbmQKCi0tIEEgQ09SUFVTIExJTkUgPGtpbmQ+IDx1bml0PiA8dGFibGU+IDxiYXNlNjQgcGF5bG9hZD4gaXMgYm90aCB0aGUgZmluZGluZwotLSBmaWxlIGFuZCB0aGUgcmVwbGF5IGlucHV0OiB0aGUgcGF5bG9hZCBpcyB0aGUgZXhhY3QgYnl0ZXMsIHNvIGEgcmVwbGF5Ci0tIHJlcHJvZHVjZXMgdGhlIGlucHV0IHdpdGhvdXQgdGhlIFBSTkcuCmxvY2FsIGZ1bmN0aW9uIGRlY29kZUNhc2UodGV4dCkKICB0ZXh0ID0gdGV4dDpnc3ViKCIlcyskIiwgIiIpCiAgbG9jYWwga2luZCwgdW5pdCwgdGFibGVOYW1lLCBwYXlsb2FkID0gdGV4dDptYXRjaCgiXiglUyspJXMrKCVTKyklcysoJVMrKSVzKyglUyspJCIpCiAgaWYgbm90IGtpbmQgdGhlbiByZXR1cm4gbmlsIGVuZAogIGxvY2FsIGJ5dGVzID0gdW5iNjQocGF5bG9hZCkKICBpZiBub3QgYnl0ZXMgdGhlbiByZXR1cm4gbmlsIGVuZAogIHJldHVybiB7IGtpbmQgPSBraW5kLCB1bml0ID0gdW5pdCwgdGFibGUgPSB0YWJsZU5hbWUsIGJ5dGVzID0gYnl0ZXMgfQplbmQKCmxvY2FsIGZ1bmN0aW9uIGVuY29kZUNhc2UoYykKICByZXR1cm4gYy5raW5kIC4uICIgIiAuLiBjLnVuaXQgLi4gIiAiIC4uIGMudGFibGUgLi4gIiAiIC4uIGI2NChjLmJ5dGVzKSAuLiAiXG4iCmVuZAoKLS0gVEhFIFRZUEVEIFJFRlVTQUxTOiByZWZ1c2FsLmx1YSdzIG5hbWVzIChpdHMgUmVhZFN0cmljdCBhbmQgY291bnRlcnMsIGluCi0tIFRhYmxlUmVwb3J0LkVycigpJ3Mgb3JkZXIsIGRvY3MvU1BFQy1UQUJMRVMubWQgwqc2LjUpLCBsb2FkLmx1YSdzIHN0cmljdAotLSByZWFzb25zIChjb3VudF9yZWZ1c2FsLCDCpzIuOSksIGZpeGVkLmx1YSdzIGZvcm0tMyByZWZ1c2FscyAoUmVhZFN0cmljdCBoYW5kcwotLSBhIGZpeGVkIHJvb3QncyBmb3JtLTMgYnl0ZXMgdG8gVC5SZWFkRml4ZWQpLCBwbHVzIHRleHQubHVhJ3MgYmFzZTY0X2RhbWFnZWQKLS0gYW5kIHRoZSBydW50aW1lJ3MgaW52YWxpZF92YWx1ZS4gQSBMdWEgZXJyb3Igd2hvc2UgbWVzc2FnZSBpcyBvbmUgb2YgdGhlc2UKLS0gaXMgYSBjbGVhbiByZWZ1c2FsLCBhbmQgdGhlIG9yYWNsZSBhY2NlcHRzIGl0LiBpbnRlcm5hbC9jaSdzCi0tIFRlc3RMdWFOaWdodGx5RnV6ekxhbmUgZmFpbHMgd2hlbiB0aGUgcnVudGltZSBuYW1lcyBhIHJlZnVzYWwgdGhpcyB0YWJsZQotLSBkb2VzIG5vdCBjYXJyeSwgc28gdGhlIG9yYWNsZSBjYW5ub3QgZHJpZnQgYmVoaW5kIHRoZSBwb3J0IGFnYWluLgpsb2NhbCBUWVBFRCA9IHsKICBwcmV2aW91c19mb3JtID0gdHJ1ZSwgdW5rbm93bl9mb3JtID0gdHJ1ZSwgbWVzc2FnZV9mb3JtX2FzX2ZpbGUgPSB0cnVlLAogIGludGVncml0eV9taXNtYXRjaCA9IHRydWUsIHdpcmVfZGFtYWdlZCA9IHRydWUsIG1pc3NpbmdfcmVxdWlyZWQgPSB0cnVlLAogIG1hbGZvcm1lZCA9IHRydWUsIGJsb2Jfc2hhcmVkID0gdHJ1ZSwgb3V0X29mX3JhbmdlID0gdHJ1ZSwKICBraW5kX21pc21hdGNoID0gdHJ1ZSwgbm9uX2Zpbml0ZV9mbG9hdCA9IHRydWUsIGNvdW50X3JlZnVzYWwgPSB0cnVlLAogIHJlcGVhdGVkX2tleSA9IHRydWUsIHVua25vd24gPSB0cnVlLAogIGJhc2U2NF9kYW1hZ2VkID0gdHJ1ZSwgaW52YWxpZF92YWx1ZSA9IHRydWUsCiAgY291bnRfb3Zlcl9leHRlbnRfY2FwID0gdHJ1ZSwgY291bnRfb3Zlcl9ib3VuZCA9IHRydWUsIGxlbmd0aF9vdmVyX2JvdW5kID0gdHJ1ZSwgbGF5b3V0X21hbGZvcm1lZCA9IHRydWUsCiAgbGF5b3V0X3JlY29yZF90b29fbGFyZ2UgPSB0cnVlLCBsYXlvdXRfdW5zdXBwb3J0ZWQgPSB0cnVlLAogIG5vX2xheW91dCA9IHRydWUsIHBsYW5fdG9vX2xhcmdlID0gdHJ1ZSwKICBiYXRjaF90b29fbGFyZ2UgPSB0cnVlLCBsYXlvdXRfdW5rbm93biA9IHRydWUsIG5ld2VyX2Zvcm0gPSB0cnVlLAogIHJlc2VydmVkX25vdF96ZXJvID0gdHJ1ZSwgdGV4dF9pbGxfZm9ybWVkID0gdHJ1ZSwgdHJ1bmNhdGVkID0gdHJ1ZSwKICBibG9iX292ZXJfc2l6ZV9jYXAgPSB0cnVlLAp9CmxvY2FsIGZ1bmN0aW9uIHR5cGVkKGVycikKICBsb2NhbCBzID0gdG9zdHJpbmcoZXJyKQogIGlmIFRZUEVEW3NdIHRoZW4gcmV0dXJuIHRydWUgZW5kCiAgbG9jYWwgbmFtZSA9IHM6bWF0Y2goIl4oWyVhX10rKToiKSBvciBzOm1hdGNoKCI6JWQrOiAoWyVhX10rKSIpCiAgcmV0dXJuIG5hbWUgfj0gbmlsIGFuZCBUWVBFRFtuYW1lXSA9PSB0cnVlCmVuZAoKLS0gVEhFIFVOSVRTLiBwYWNrYWdlLmxvYWRlZCBpcyBjbGVhcmVkIGJlZm9yZSBlYWNoIHVuaXQgYmVjYXVzZSBldmVyeSB1bml0Ci0tIHNoaXBzIGl0cyBvd24gZGVzY3JpcHRvci9sb2FkL3JlZnVzYWwvanNvbi90ZXh0IG1vZHVsZXMgdW5kZXIgdGhlIHNhbWUKLS0gbmFtZXM7IHJlcXVpcmUoKSB3b3VsZCBoYW5kIGEgdW5pdCB0aGUgcHJldmlvdXMgdW5pdCdzIHJ1bnRpbWUgb3RoZXJ3aXNlLgpsb2NhbCB1bml0cyA9IHt9CmZvciBfLCBsaW5lIGluIGlwYWlycyhsaW5lcyhvcHRzLmNvcnB1cykpIGRvCiAgbG9jYWwga2V5LCBkaXIgPSBsaW5lOm1hdGNoKCJeKCVTKyklcysoJVMrKSQiKQogIGlmIGtleSB0aGVuIHVuaXRzWyN1bml0cyArIDFdID0geyBrZXkgPSBrZXksIGRpciA9IGRpciB9IGVuZAplbmQKCmxvY2FsIGZ1bmN0aW9uIHRhYmxlc01vZHVsZShrZXkpCiAgbG9jYWwgcCA9IGlvLnBvcGVuKCJscyAiIC4uIEdFTkVSQVRFRCAuLiAiLyIgLi4ga2V5IC4uICIvKlRhYmxlcy5sdWEgMj4vZGV2L251bGwgfCBoZWFkIC0xIikKICBsb2NhbCBwYXRoID0gcDpyZWFkKCIqbCIpCiAgcDpjbG9zZSgpCiAgcmV0dXJuIHBhdGgKZW5kCgpsb2NhbCBmdW5jdGlvbiBsb2FkVW5pdChrZXkpCiAgbG9jYWwgbW9kcGF0aCA9IHRhYmxlc01vZHVsZShrZXkpCiAgaWYgbm90IG1vZHBhdGggdGhlbiByZXR1cm4gbmlsIGVuZAogIGxvY2FsIGRpciA9IG1vZHBhdGg6bWF0Y2goIl4oLiopL1teL10rJCIpCiAgZm9yIG5hbWUgaW4gcGFpcnMocGFja2FnZS5sb2FkZWQpIGRvIHBhY2thZ2UubG9hZGVkW25hbWVdID0gbmlsIGVuZAogIHBhY2thZ2UucGF0aCA9IGRpciAuLiAiLz8ubHVhOyIgLi4gcGFja2FnZS5wYXRoCiAgbG9jYWwgVCA9IGRvZmlsZShtb2RwYXRoKQogIGxvY2FsIEwgPSBkb2ZpbGUoZGlyIC4uICIvbG9hZC5sdWEiKQogIGxvY2FsIFIgPSBkb2ZpbGUoZGlyIC4uICIvcmVmdXNhbC5sdWEiKQogIGxvY2FsIFggPSBkb2ZpbGUoZGlyIC4uICIvdGV4dC5sdWEiKQogIGxvY2FsIEogPSBkb2ZpbGUoZGlyIC4uICIvanNvbi5sdWEiKQogIGxvY2FsIG5hbWVzLCB0YWJsZXMgPSB7fSwge30KICBmb3IgbmFtZSwgZGVzYyBpbiBwYWlycyhULlRhYmxlcyBvciB7fSkgZG8KICAgIGlmIGRlc2MudGFibGUgdGhlbiBuYW1lc1sjbmFtZXMgKyAxXSA9IG5hbWU7IHRhYmxlc1tuYW1lXSA9IHRydWUgZW5kCiAgZW5kCiAgdGFibGUuc29ydChuYW1lcykKICByZXR1cm4geyBrZXkgPSBrZXksIFQgPSBULCBMID0gTCwgUiA9IFIsIFggPSBYLCBKID0gSiwgbmFtZXMgPSBuYW1lcywgdGFibGVzID0gdGFibGVzIH0KZW5kCgpsb2NhbCBsb2FkZWQgPSB7fQpsb2NhbCB0YXJnZXRzID0ge30KZm9yIF8sIHUgaW4gaXBhaXJzKHVuaXRzKSBkbwogIGxvY2FsIG0gPSBsb2FkVW5pdCh1LmtleSkKICBpZiBtIHRoZW4KICAgIGxvYWRlZFt1LmtleV0gPSBtCiAgICBmb3IgXywgbmFtZSBpbiBpcGFpcnMobS5uYW1lcykgZG8KICAgICAgZm9yIF8sIGtpbmQgaW4gaXBhaXJzKHsgImxvYWQiLCAic3RyaWN0IiwgInRleHQiLCAianNvbiIgfSkgZG8KICAgICAgICB0YXJnZXRzWyN0YXJnZXRzICsgMV0gPSB7IHVuaXQgPSB1LmtleSwgdGFibGUgPSBuYW1lLCBraW5kID0ga2luZCB9CiAgICAgIGVuZAogICAgZW5kCiAgZW5kCmVuZAppZiAjdGFyZ2V0cyA9PSAwIHRoZW4KICBpby5zdGRlcnI6d3JpdGUoImZ1eno6IG5vIGdlbmVyYXRlZCB1bml0IHVuZGVyICIgLi4gR0VORVJBVEVEIC4uICIgKHJ1biB0aGUgbWFrZSB0YXJnZXQpXG4iKQogIG9zLmV4aXQoMikKZW5kCgotLSBUSEUgU0VFRFMuIFdpcmUgdmVjdG9ycyBkcml2ZSBsb2FkL3N0cmljdC90ZXh0ICh0ZXh0IGlzIHRoZWlyIGJhc2U2NCBib2R5KTsKLS0gSlNPTiB2ZWN0b3JzIGRyaXZlIGpzb24uIFRoZSBHbyBmdXp6IGNvcnB1cyAoaW50ZXJuYWwvZnV6ei90ZXN0ZGF0YSkgaXMKLS0gaW5jbHVkZWQgYXMgcmF3IG11dGF0aW9uIHJvb3RzLCBhcyB0aGUgY2FyZCBhc2tzLgpsb2NhbCBzZWVkcyA9IHsgbG9hZCA9IHt9LCBzdHJpY3QgPSB7fSwgdGV4dCA9IHt9LCBqc29uID0ge30gfQpsb2NhbCBmdW5jdGlvbiBhZGRTZWVkcyhraW5kLCBkaXIsIG1heCkKICBsb2NhbCBwID0gaW8ucG9wZW4oImZpbmQgIiAuLiBkaXIgLi4gIiAtdHlwZSBmIDI+L2Rldi9udWxsIHwgc29ydCIpCiAgZm9yIHBhdGggaW4gcDpsaW5lcygpIGRvCiAgICBsb2NhbCBkYXRhID0gcmVhZEZpbGUocGF0aCkKICAgIGlmIGRhdGEgYW5kICNkYXRhID4gMCBhbmQgI2RhdGEgPD0gKG1heCBvciA0MDk2KSB0aGVuCiAgICAgIHNlZWRzW2tpbmRdWyNzZWVkc1traW5kXSArIDFdID0gZGF0YQogICAgZW5kCiAgZW5kCiAgcDpjbG9zZSgpCmVuZAppZiBvcHRzLndpcmVfc2VlZHMgdGhlbgogIGZvciBkaXIgaW4gb3B0cy53aXJlX3NlZWRzOmdtYXRjaCgiW146XSsiKSBkbwogICAgYWRkU2VlZHMoImxvYWQiLCBkaXIpOyBhZGRTZWVkcygic3RyaWN0IiwgZGlyKTsgYWRkU2VlZHMoInRleHQiLCBkaXIpCiAgZW5kCmVuZAppZiBvcHRzLmpzb25fc2VlZHMgdGhlbgogIGZvciBkaXIgaW4gb3B0cy5qc29uX3NlZWRzOmdtYXRjaCgiW146XSsiKSBkbyBhZGRTZWVkcygianNvbiIsIGRpcikgZW5kCmVuZAppZiBvcHRzLnRleHRfc2VlZHMgdGhlbgogIGZvciBkaXIgaW4gb3B0cy50ZXh0X3NlZWRzOmdtYXRjaCgiW146XSsiKSBkbwogICAgZm9yIF8sIGRhdGEgaW4gaXBhaXJzKChmdW5jdGlvbigpCiAgICAgIGxvY2FsIG91dCA9IHt9CiAgICAgIGxvY2FsIHAgPSBpby5wb3BlbigiZmluZCAiIC4uIGRpciAuLiAiIC10eXBlIGYgMj4vZGV2L251bGwgfCBzb3J0IikKICAgICAgZm9yIHBhdGggaW4gcDpsaW5lcygpIGRvCiAgICAgICAgbG9jYWwgZCA9IHJlYWRGaWxlKHBhdGgpCiAgICAgICAgaWYgZCB0aGVuIG91dFsjb3V0ICsgMV0gPSBkIGVuZAogICAgICBlbmQKICAgICAgcDpjbG9zZSgpCiAgICAgIHJldHVybiBvdXQKICAgIGVuZCkoKSkgZG8gc2VlZHMudGV4dFsjc2VlZHMudGV4dCArIDFdID0gZGF0YSBlbmQKICBlbmQKZW5kCmZvciBfLCBraW5kIGluIGlwYWlycyh7ICJsb2FkIiwgInN0cmljdCIsICJ0ZXh0IiB9KSBkbwogIGlmICNzZWVkc1traW5kXSA9PSAwIHRoZW4gc2VlZHNba2luZF1bMV0gPSBzdHJpbmcuY2hhcigxKSAuLiAiXDBcMFwwXDAiIGVuZAplbmQKaWYgI3NlZWRzLmpzb24gPT0gMCB0aGVuIHNlZWRzLmpzb25bMV0gPSAie30iIGVuZAoKLS0gTVVUQVRJT04sIHNlZWRlZCBhbmQgZGV0ZXJtaW5pc3RpYzogeG9yc2hpZnQ2NCogb3ZlciBMdWEgNS40IGludGVnZXJzLgpsb2NhbCBzdGF0ZSA9IHRvbnVtYmVyKG9wdHMuc2VlZCkgb3IgMQppZiBzdGF0ZSA9PSAwIHRoZW4gc3RhdGUgPSAxIGVuZApsb2NhbCBmdW5jdGlvbiBybmQoKQogIHN0YXRlID0gc3RhdGUgfiAoc3RhdGUgPDwgMTMpCiAgc3RhdGUgPSBzdGF0ZSB+IChzdGF0ZSA+PiA3KQogIHN0YXRlID0gc3RhdGUgfiAoc3RhdGUgPDwgMTcpCiAgcmV0dXJuIHN0YXRlCmVuZApsb2NhbCBmdW5jdGlvbiBwaWNrbihuKQogIGxvY2FsIHYgPSBybmQoKSAmIDB4N2ZmZmZmZmZmZmZmZmZmZgogIHJldHVybiAodiAlIG4pICsgMQplbmQKCmxvY2FsIElOVEVSRVNUSU5HID0geyAwLCAxLCAyLCAzLCAweDdmLCAweDgwLCAweDgxLCAweGZlLCAweGZmLCAweDNkLCAweDdiLCAweDIyIH0KCmxvY2FsIGZ1bmN0aW9uIG11dGF0ZShiYXNlKQogIGxvY2FsIGJ5dGVzID0geyBiYXNlOmJ5dGUoMSwgbWF0aC5taW4oI2Jhc2UsIDQwOTYpKSB9CiAgbG9jYWwgZWRpdHMgPSBwaWNrbig2KQogIGZvciBfID0gMSwgZWRpdHMgZG8KICAgIGxvY2FsIG9wID0gcGlja24oNikKICAgIGxvY2FsIG4gPSAjYnl0ZXMKICAgIGlmIG9wID09IDEgYW5kIG4gPiAwIHRoZW4KICAgICAgbG9jYWwgYXQgPSBwaWNrbihuKQogICAgICBsb2NhbCBiaXQgPSAxIDw8IChwaWNrbig4KSAtIDEpCiAgICAgIGJ5dGVzW2F0XSA9IChieXRlc1thdF0gfiBiaXQpICYgMjU1CiAgICBlbHNlaWYgb3AgPT0gMiBhbmQgbiA+IDAgdGhlbgogICAgICBieXRlc1twaWNrbihuKV0gPSBJTlRFUkVTVElOR1twaWNrbigjSU5URVJFU1RJTkcpXQogICAgZWxzZWlmIG9wID09IDMgYW5kIG4gPCA0MDk2IHRoZW4KICAgICAgbG9jYWwgYXQgPSBwaWNrbihuICsgMSkKICAgICAgdGFibGUuaW5zZXJ0KGJ5dGVzLCBhdCwgSU5URVJFU1RJTkdbcGlja24oI0lOVEVSRVNUSU5HKV0pCiAgICBlbHNlaWYgb3AgPT0gNCBhbmQgbiA+IDEgdGhlbgogICAgICB0YWJsZS5yZW1vdmUoYnl0ZXMsIHBpY2tuKG4pKQogICAgZWxzZWlmIG9wID09IDUgYW5kIG4gPiAwIGFuZCBuIDwgNDA5NiB0aGVuCiAgICAgIGxvY2FsIGZyb20gPSBwaWNrbihuKQogICAgICBsb2NhbCBsZW4gPSBwaWNrbihtYXRoLm1pbigxNiwgbiAtIGZyb20gKyAxKSkKICAgICAgbG9jYWwgYXQgPSBwaWNrbihuICsgMSkKICAgICAgZm9yIGogPSBsZW4sIDEsIC0xIGRvIHRhYmxlLmluc2VydChieXRlcywgYXQsIGJ5dGVzW2Zyb20gKyBqIC0gMV0pIGVuZAogICAgZWxzZWlmIG9wID09IDYgYW5kIG4gPiAwIHRoZW4KICAgICAgbG9jYWwgYXQgPSBwaWNrbihuKQogICAgICBieXRlc1thdF0gPSBybmQoKSAmIDI1NQogICAgZW5kCiAgZW5kCiAgcmV0dXJuIHN0cmluZy5jaGFyKHRhYmxlLnVucGFjayhieXRlcykpCmVuZAoKLS0gVEhFIFJFQURFUlMuIEVhY2ggcmV0dXJucyB0cnVlIHdoZW4gdGhlIGlucHV0IHdhcyBoYW5kbGVkIChhIHZhbHVlIG9yIGEKLS0gY2xlYW4gdHlwZWQgcmVmdXNhbCkgYW5kIHJhaXNlcyBvbmx5IHRoZSByYXcgTHVhIGVycm9yIHRoZSBvcmFjbGUgcmVwb3J0cy4KLS0gQSBjYXNlIG5vIHJlYWRlciBjYW4gdGFrZSAoYSB1bml0IHRoYXQgd2FzIG5vdCBnZW5lcmF0ZWQsIGEgdGFibGUgaXRzIHVuaXQKLS0gZG9lcyBub3QgcmVnaXN0ZXIsIGEga2luZCB3aXRoIG5vIGVudHJ5IHBvaW50KSByYWlzZXMgYSBoYXJuZXNzIGVycm9yLCB3aGljaAotLSBpcyBub3QgYSB0eXBlZCByZWZ1c2FsOiBpdCB1c2VkIHRvIHJldHVybiBhcyBpZiBhIHJlYWRlciBoYWQgcGFzc2VkLCBzbyBhCi0tIHNhdmVkIGNhc2UgZm9yIGEgdW5pdCB0aGF0IHN0b3BwZWQgZ2VuZXJhdGluZyByZXBsYXllZCBncmVlbiBhbmQgcmVhZAotLSBub3RoaW5nLgpsb2NhbCBLSU5EUyA9IHsgbG9hZCA9IHRydWUsIHN0cmljdCA9IHRydWUsIHRleHQgPSB0cnVlLCBqc29uID0gdHJ1ZSB9CmxvY2FsIGZ1bmN0aW9uIGRyaXZlKGMpCiAgbG9jYWwgdSA9IGxvYWRlZFtjLnVuaXRdCiAgaWYgbm90IHUgdGhlbiBlcnJvcigiaGFybmVzczogdW5pdCAiIC4uIHRvc3RyaW5nKGMudW5pdCkgLi4gIiB3YXMgbm90IGdlbmVyYXRlZCwgc28gbm8gcmVhZGVyIHRha2VzIHRoaXMgY2FzZSIsIDApIGVuZAogIGlmIG5vdCBLSU5EU1tjLmtpbmRdIHRoZW4gZXJyb3IoImhhcm5lc3M6IG5vIHJlYWRlciBmb3Iga2luZCAiIC4uIHRvc3RyaW5nKGMua2luZCksIDApIGVuZAogIGlmIG5vdCB1LnRhYmxlc1tjLnRhYmxlXSB0aGVuIGVycm9yKCJoYXJuZXNzOiB1bml0ICIgLi4gYy51bml0IC4uICIgcmVnaXN0ZXJzIG5vIHRhYmxlICIgLi4gdG9zdHJpbmcoYy50YWJsZSksIDApIGVuZAogIGxvY2FsIFQsIG5hbWUsIGJ5dGVzID0gdS5ULCBjLnRhYmxlLCBjLmJ5dGVzCiAgaWYgYy5raW5kID09ICJsb2FkIiB0aGVuCiAgICBsb2NhbCByZXBvcnQgPSB7CiAgICAgIGRhbWFnZWQgPSBmYWxzZSwgcmVmdXNlZCA9IDAsIHVua25vd24gPSAwLCBraW5kQ2hhbmdlZCA9IDAsCiAgICAgIG91dE9mUmFuZ2UgPSAwLCB3aWRlbmVkID0gMCwgcmVwZWF0ZWRLZXkgPSAwLAogICAgfQogICAgdS5MLkxvYWQoVCwgbmFtZSwgYnl0ZXMsIHJlcG9ydCkKICBlbHNlaWYgYy5raW5kID09ICJzdHJpY3QiIHRoZW4KICAgIHUuUi5SZWFkU3RyaWN0KFQsIG5hbWUsIGJ5dGVzKQogIGVsc2VpZiBjLmtpbmQgPT0gInRleHQiIHRoZW4KICAgIHUuWC5Mb2FkVGV4dChULCBuYW1lLCBiNjQoYnl0ZXMpKQogIGVsc2VpZiBjLmtpbmQgPT0gImpzb24iIHRoZW4KICAgIGxvY2FsIHJlcG9ydCA9IHsKICAgICAgZGFtYWdlZCA9IGZhbHNlLCByZWZ1c2VkID0gMCwgdW5rbm93biA9IDAsIGtpbmRDaGFuZ2VkID0gMCwKICAgICAgb3V0T2ZSYW5nZSA9IDAsIHdpZGVuZWQgPSAwLCByZXBlYXRlZEtleSA9IDAsCiAgICB9CiAgICB1LkouRnJvbUpzb24oVCwgbmFtZSwgYnl0ZXMsIHJlcG9ydCkKICBlbmQKZW5kCgpsb2NhbCBmaW5kaW5nc0RpciA9IG9wdHMuZmluZGluZ3MKbG9jYWwgZnVuY3Rpb24gc2F2ZUZpbmRpbmcoYywgZXJyKQogIGlmIG5vdCBmaW5kaW5nc0RpciBvciBmaW5kaW5nc0RpciA9PSAiIiB0aGVuIHJldHVybiBlbmQKICBvcy5leGVjdXRlKCJta2RpciAtcCAiIC4uIGZpbmRpbmdzRGlyKQogIGxvY2FsIHNsdWcgPSBjLmtpbmQgLi4gIl8iIC4uIGMudW5pdCAuLiAiXyIgLi4gYy50YWJsZTpnc3ViKCJbXiV3X10iLCAiXyIpCiAgbG9jYWwgbiA9IDEKICBsb2NhbCBwYXRoCiAgcmVwZWF0CiAgICBwYXRoID0gZmluZGluZ3NEaXIgLi4gIi8iIC4uIHNsdWcgLi4gIl8iIC4uIG4gLi4gIi5jYXNlIgogICAgbG9jYWwgZiA9IGlvLm9wZW4ocGF0aCwgInIiKQogICAgaWYgZiB0aGVuIGY6Y2xvc2UoKTsgcGF0aCA9IG5pbDsgbiA9IG4gKyAxIGVuZAogIHVudGlsIHBhdGgKICB3cml0ZUZpbGUocGF0aCwgZW5jb2RlQ2FzZShjKSkKICB3cml0ZUZpbGUocGF0aCAuLiAiLmVyciIsIHRvc3RyaW5nKGVycikgLi4gIlxuIikKICByZXR1cm4gcGF0aAplbmQKCi0tIFJFUExBWTogZXZlcnkgc2F2ZWQgY2FzZSwgb25jZSwgd2l0aCBubyBjbG9jay4gQSBzYXZlZCBmaW5kaW5nIHRoYXQgbm8KLS0gbG9uZ2VyIGZpcmVzIGlzIHN0aWxsIHJlcGxheWVkOyBhIGNhc2UgdGhhdCBmaXJlcyBpcyBhIHJlZ3Jlc3Npb24uCi0tIEV2ZXJ5IHNhdmVkIGZpbGUgaXMgY291bnRlZCwgYW5kIGV2ZXJ5IG9uZSBtdXN0IGJlIHJlcGxheWVkOiBhIGZpbGUgdGhhdAotLSBkb2VzIG5vdCBkZWNvZGUgaXMgYSBmYWlsdXJlLCBuZXZlciBhIGZpbGUgbGVmdCBvdXQgb2YgdGhlIGNvdW50LCBhbmQgYQotLSBkaXJlY3RvcnkgdGhhdCBpcyBtaXNzaW5nIG9yIGhvbGRzIG5vIGNhc2UgaXMgYSByZXBsYXkgb2Ygbm90aGluZywgd2hpY2ggaXMKLS0gcmVkLiBFYWNoIG9mIHRoZXNlIHVzZWQgdG8gcmVwb3J0ICIwIGZhaWxlZCIgYW5kIGV4aXQgMC4KaWYgb3B0cy5yZXBsYXkgYW5kIG9wdHMucmVwbGF5IH49ICIiIHRoZW4KICBsb2NhbCBmaWxlcywgY2FzZXMsIGJhZCA9IDAsIDAsIDAKICBsb2NhbCBmdW5jdGlvbiBmYWlsKHBhdGgsIHdoeSkKICAgIGJhZCA9IGJhZCArIDEKICAgIGlvLnN0ZGVycjp3cml0ZSgiUkVQTEFZIEZBSUwgIiAuLiBwYXRoIC4uICI6ICIgLi4gd2h5IC4uICJcbiIpCiAgZW5kCiAgbG9jYWwgZCA9IGlvLm9wZW4ob3B0cy5yZXBsYXkgLi4gIi8uIiwgInIiKQogIGlmIG5vdCBkIHRoZW4KICAgIGlvLnN0ZGVycjp3cml0ZSgibHVhIGZ1enogcmVwbGF5OiAiIC4uIG9wdHMucmVwbGF5IC4uICIgaXMgbm90IGEgZGlyZWN0b3J5OyBub3RoaW5nIHdhcyByZXBsYXllZFxuIikKICAgIG9zLmV4aXQoMSkKICBlbmQKICBkOmNsb3NlKCkKICBsb2NhbCBwID0gaW8ucG9wZW4oImZpbmQgIiAuLiBvcHRzLnJlcGxheSAuLiAiIC10eXBlIGYgLW5hbWUgJyouY2FzZScgMj4vZGV2L251bGwgfCBzb3J0IikKICBmb3IgcGF0aCBpbiBwOmxpbmVzKCkgZG8KICAgIGZpbGVzID0gZmlsZXMgKyAxCiAgICBsb2NhbCB0ZXh0ID0gcmVhZEZpbGUocGF0aCkKICAgIGxvY2FsIGMgPSB0ZXh0IGFuZCBkZWNvZGVDYXNlKHRleHQpCiAgICBpZiBub3QgYyB0aGVuCiAgICAgIGZhaWwocGF0aCwgInRoZSBzYXZlZCBjYXNlIGRvZXMgbm90IGRlY29kZSBhcyA8a2luZD4gPHVuaXQ+IDx0YWJsZT4gPGJhc2U2ND4sIHNvIG5vIHJlYWRlciB0b29rIGl0IikKICAgIGVsc2UKICAgICAgY2FzZXMgPSBjYXNlcyArIDEKICAgICAgbG9jYWwgb2ssIGVyciA9IHBjYWxsKGRyaXZlLCBjKQogICAgICBpZiBub3Qgb2sgYW5kIG5vdCB0eXBlZChlcnIpIHRoZW4gZmFpbChwYXRoLCB0b3N0cmluZyhlcnIpKSBlbmQKICAgIGVuZAogIGVuZAogIHA6Y2xvc2UoKQogIGlvLndyaXRlKHN0cmluZy5mb3JtYXQoImx1YSBmdXp6IHJlcGxheTogJWQgY2FzZXMsICVkIGZhaWxlZFxuIiwgY2FzZXMsIGJhZCkpCiAgaWYgZmlsZXMgPT0gMCB0aGVuCiAgICBpby5zdGRlcnI6d3JpdGUoImx1YSBmdXp6IHJlcGxheTogIiAuLiBvcHRzLnJlcGxheSAuLiAiIGhvbGRzIG5vIHNhdmVkIGNhc2U7IG5vdGhpbmcgd2FzIHJlcGxheWVkXG4iKQogICAgb3MuZXhpdCgxKQogIGVuZAogIG9zLmV4aXQoYmFkID09IDAgYW5kIDAgb3IgMSkKZW5kCgotLSBGVVpaOiB0aGUgdGltZSBidWRnZXQgaXMgYW4gYXJndW1lbnQ7IC0taXRlcmF0aW9ucyBib3VuZHMgYSBjb250cm9sIHJ1bi4KbG9jYWwgc2Vjb25kcyA9IHRvbnVtYmVyKG9wdHMuc2Vjb25kcykgb3IgMTAKbG9jYWwgaXRlcmF0aW9ucyA9IHRvbnVtYmVyKG9wdHMuaXRlcmF0aW9ucykgb3IgMApsb2NhbCBkZWFkbGluZSA9IG9zLnRpbWUoKSArIHNlY29uZHMKbG9jYWwgY291bnQsIGZhaWxzID0gMCwgMAp3aGlsZSB0cnVlIGRvCiAgaWYgaXRlcmF0aW9ucyA+IDAgdGhlbgogICAgaWYgY291bnQgPj0gaXRlcmF0aW9ucyB0aGVuIGJyZWFrIGVuZAogIGVsc2VpZiBvcy50aW1lKCkgPj0gZGVhZGxpbmUgdGhlbgogICAgYnJlYWsKICBlbmQKICBsb2NhbCB0ID0gdGFyZ2V0c1twaWNrbigjdGFyZ2V0cyldCiAgbG9jYWwga2luZCA9IHQua2luZAogIGxvY2FsIGJhc2UgPSBzZWVkc1traW5kXVtwaWNrbigjc2VlZHNba2luZF0pXQogIGxvY2FsIGMgPSB7IGtpbmQgPSBraW5kLCB1bml0ID0gdC51bml0LCB0YWJsZSA9IHQudGFibGUsIGJ5dGVzID0gbXV0YXRlKGJhc2UpIH0KICBsb2NhbCBvaywgZXJyID0gcGNhbGwoZHJpdmUsIGMpCiAgY291bnQgPSBjb3VudCArIDEKICBpZiBub3Qgb2sgYW5kIG5vdCB0eXBlZChlcnIpIHRoZW4KICAgIGZhaWxzID0gZmFpbHMgKyAxCiAgICBsb2NhbCBwYXRoID0gc2F2ZUZpbmRpbmcoYywgZXJyKQogICAgaW8uc3RkZXJyOndyaXRlKCJGVVpaIEZBSUwgIiAuLiAocGF0aCBvciAiKG5vIGZpbmRpbmcgZGlyKSIpIC4uICI6ICIgLi4gdG9zdHJpbmcoZXJyKSAuLiAiXG4iKQogICAgaWYgZmFpbHMgPj0gMTAgdGhlbiBicmVhayBlbmQKICBlbmQKZW5kCmlvLndyaXRlKHN0cmluZy5mb3JtYXQoImx1YSBmdXp6OiB0YXJnZXRzPSVkIGl0ZXJhdGlvbnM9JWQgZmFpbHVyZXM9JWQgc2VlZD0lZFxuIiwKICAjdGFyZ2V0cywgY291bnQsIGZhaWxzLCB0b251bWJlcihvcHRzLnNlZWQpIG9yIDEpKQpvcy5leGl0KGZhaWxzID09IDAgYW5kIDAgb3IgMSkK

$(LUA_FUZZ_SCRIPT): make/schema-lua-nightly-fuzz.mk
	@mkdir -p $(dir $@)
	@printf '%s' '$(LUA_FUZZ_HARNESS_B64)' | base64 -d > $@ 2>/dev/null || \
		printf '%s' '$(LUA_FUZZ_HARNESS_B64)' | base64 -D > $@
	@chmod +x $@
	@echo "schema-lua-fuzz: harness rev $(LUA_FUZZ_HARNESS_REV) written to $@"

# A SHORT WIRE ROOT: the truncated-body input the planted control needs,
# generated (this card may not commit it to the port's test tree). It is
# shorter than the nine-byte form-1 framing, so it is the vector that reaches
# load.lua's `blen < 9` bound: a nine-byte seed sits exactly on the bound and
# never trips it.
$(LUA_FUZZ_SEEDDIR)/truncated.bin: make/schema-lua-nightly-fuzz.mk
	@mkdir -p $(dir $@)
	@printf '\001\000\000' > $@
	@echo "schema-lua-fuzz: 3-byte truncated wire seed written to $@"

# one generated unit per corpus line; bin/schema (its lua target) is the port's own
# emitter, so the fuzzer and the gate cannot disagree about what was generated.
$(LUA_FUZZ_GENERATED)/.stamp: bin/schema $(LUA_FUZZ_CORPUS) $(LUA_FUZZ_SCRIPT) $(LUA_FUZZ_SEEDDIR)/truncated.bin make/schema-lua-nightly-fuzz.mk
	@rm -rf $(LUA_FUZZ_GENERATED) && mkdir -p $(LUA_FUZZ_GENERATED)
	@while read -r key dir; do \
		[ -n "$$key" ] || continue; \
		if ! ./bin/schema generate --lang lua --out $(LUA_FUZZ_GENERATED)/$$key $$dir > $(LUA_FUZZ_GENERATED)/$$key.log 2>&1; then \
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
	@set +e; $(LUA_FUZZ_LUA) $(LUA_FUZZ_SCRIPT) --generated $(LUA_FUZZ_GENERATED) \
		--corpus $(LUA_FUZZ_CORPUS) --seconds $(LUA_FUZZ_SECONDS) \
		--seed $(LUA_FUZZ_SEED) --findings $(LUA_FUZZ_FINDINGS) $(LUA_FUZZ_SEEDS) > build/lua-fuzz/nightly.log 2>&1; \
	status=$$?; cat build/lua-fuzz/nightly.log; \
	if [ -n "$$(ls $(LUA_FUZZ_FINDINGS)/*.case 2>/dev/null)" ]; then \
		echo "schema-lua-nightly-fuzz: findings under $(LUA_FUZZ_FINDINGS):"; \
		ls $(LUA_FUZZ_FINDINGS)/*.case; exit 1; \
	fi; \
	[ $$status -eq 0 ] || { echo "schema-lua-nightly-fuzz: the harness exited $$status"; exit 1; }; \
	grep -qE '^lua fuzz: targets=[1-9][0-9]* iterations=[1-9][0-9]* failures=0 seed=$(LUA_FUZZ_SEED)$$' build/lua-fuzz/nightly.log || \
		{ echo "schema-lua-nightly-fuzz: no line reports targets and iterations above zero at seed $(LUA_FUZZ_SEED); the run fuzzed nothing"; exit 1; }

# THE PLANTED-BAD-PATTERN CONTROL. A scratch copy of the generated tree has the
# short-file guard in load.lua's open removed (`if blen < 9` becomes
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
	sed -i.bak 's/^  if blen < 9 then$$/  if false then -- lua-fuzz-plant/' $$nc/generated/examples/load.lua && rm -f $$nc/generated/examples/load.lua.bak; \
	grep -q '^  if false then -- lua-fuzz-plant$$' $$nc/generated/examples/load.lua || \
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
schema-lua-fuzz-replay: schema-lua-fuzz-toolchain schema-lua-fuzz-negative-control schema-lua-fuzz-replay-controls
	$(LUA_FUZZ_LUA) $(LUA_FUZZ_SCRIPT) --generated $(LUA_FUZZ_GENERATED) \
		--corpus $(LUA_FUZZ_CORPUS) --replay $(LUA_FUZZ_REGRESS)

# THE REPLAY'S OWN CONTROLS, run before every replay: the committed saved cases
# under testdata/lua-nightly-fuzz/replay-controls. The healthy one must replay
# as one case; each broken one must be refused. A case for a unit, kind or
# table no reader takes, a saved file that does not decode, an empty directory
# and a missing one each used to report "0 failed" and exit 0 (F-077, F-078).
LUA_FUZZ_REPLAY_CONTROLS := testdata/lua-nightly-fuzz/replay-controls
LUA_FUZZ_REPLAY_REFUSED  := unbound-unit unsupported-kind unregistered-table unreadable-beside-good
.PHONY: schema-lua-fuzz-replay-controls
schema-lua-fuzz-replay-controls: schema-lua-fuzz-toolchain $(LUA_FUZZ_GENERATED)/.stamp
	@c=$(LUA_FUZZ_REPLAY_CONTROLS); w=build/lua-fuzz-replay-controls; \
	rm -rf $$w && mkdir -p $$w/empty || exit 1; \
	replay() { $(LUA_FUZZ_LUA) $(LUA_FUZZ_SCRIPT) --generated $(LUA_FUZZ_GENERATED) --corpus $(LUA_FUZZ_CORPUS) --replay "$$1"; }; \
	if ! replay $$c/good > $$w/good.log 2>&1; then \
		echo "lua replay control failed: the healthy saved case was refused"; cat $$w/good.log; exit 1; fi; \
	grep -qx 'lua fuzz replay: 1 cases, 0 failed' $$w/good.log || \
		{ echo "lua replay control failed: the healthy saved case did not replay as one case"; cat $$w/good.log; exit 1; }; \
	echo "lua replay control: the healthy saved case replays (1 cases, 0 failed)"; \
	dirs=0; for d in $$c/*/; do dirs=$$((dirs + 1)); done; \
	[ $$dirs -eq 5 ] || { echo "lua replay controls: $$c holds $$dirs directories, the recipe runs good and 4 refused"; exit 1; }; \
	refused=0; \
	for case in $(addprefix $$c/,$(LUA_FUZZ_REPLAY_REFUSED)) $$w/empty $$w/missing; do \
		name=$$(basename $$case); \
		if replay $$case > $$w/$$name.log 2>&1; then \
			echo "lua replay control failed: $$name replayed green"; cat $$w/$$name.log; exit 1; fi; \
		echo "lua replay control: $$name is refused ($$(grep -v '^lua fuzz replay:' $$w/$$name.log | head -1))"; \
		refused=$$((refused + 1)); \
	done; \
	[ $$refused -eq 6 ] || { echo "lua replay controls: $$refused of 6 ran"; exit 1; }; \
	echo "lua replay controls: 6 of 6 broken replays refused"
