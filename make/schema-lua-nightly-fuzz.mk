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
LUA_FUZZ_HARNESS_REV := 9
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
LUA_FUZZ_HARNESS_B64 := LS0gdGVzdC9sdWEtZnV6ei9mdXp6Lmx1YSDigJQgdGhlIEx1YSBwb3J0J3Mgc2VlZGVkIG11dGF0aW9uIGZ1enplciBmb3IgdGhlCi0tIGdlbmVyYXRlZCByZWFkZXJzIChzY2hlbWEtbHVhLW5pZ2h0bHktZnV6eikuCi0tCi0tIFRIRSBIQVJORVNTIHJlYWRzIHRlc3QvbHVhLWZ1enovY29ycHVzLnR4dCAodGhlIHVuaXQga2V5IC0+IHNjaGVtYSBkaXJlY3RvcnkKLS0gcGFpcnMgdGhlIE1ha2VmaWxlJ3MgdGFibGVzX2dlbmVyYXRlIHJlYWRzKSwgbG9hZHMgZXZlcnkgZ2VuZXJhdGVkIHVuaXQncwotLSBUYWJsZXMgbW9kdWxlLCBhbmQgZHJpdmVzIG9uZSBlbnRyeSBwb2ludCBwZXIgcmVhZGVyIGtpbmQgb3ZlciBldmVyeSB0YWJsZQotLSBuYW1lIHRoYXQgbW9kdWxlIHJlZ2lzdGVyczoKLS0KLS0gICBsb2FkICAgIHRoZSB0b2xlcmFudCBmb3JtLTEgbG9hZCAocnVudGltZS9sb2FkLmx1YSBMLkxvYWQpCi0tICAgc3RyaWN0ICB0aGUgc3RyaWN0IHJlYWQgKHJ1bnRpbWUvcmVmdXNhbC5sdWEgUi5SZWFkU3RyaWN0KQotLSAgIHRleHQgICAgdGhlIHRleHQgZm9ybSdzIGJhc2U2NCBib2R5IChydW50aW1lL3RleHQubHVhIFRleHQuTG9hZFRleHQpCi0tICAganNvbiAgICB0aGUgdGV4dCBmb3JtJ3MgSlNPTiBib2R5IChydW50aW1lL2pzb24ubHVhIEouRnJvbUpzb24pCi0tCi0tIFRIRSBPUkFDTEU6IG5vIGNyYXNoLCBubyBoYW5nLCBubyBhc3NlcnQ7IGEgcmVmdXNhbCBpcyBhIGNsZWFuIHR5cGVkIGVycm9yCi0tIChyZWZ1c2FsLmx1YSdzIG5hbWVzKSwgbmV2ZXIgYSByYXcgTHVhIGVycm9yIGFuZCBuZXZlciBhIHNpbGVudCBzdWNjZXNzLgotLSBUaGUgc2VlZCBpcyByZWNvcmRlZCB3aXRoIGV2ZXJ5IGZpbmRpbmcgKHRoZSBmaW5kaW5nIGZpbGUncyBwYXlsb2FkIGlzIHRoZQotLSBleGFjdCBieXRlcywgc28gcmVwbGF5IG5lZWRzIG5vIGNsb2NrIGFuZCBubyBQUk5HKS4KLS0KLS0gdXNhZ2U6Ci0tICAgbHVhIHRlc3QvbHVhLWZ1enovZnV6ei5sdWEgLS1nZW5lcmF0ZWQgRElSIC0tY29ycHVzIEZJTEUKLS0gICAgICAgWy0tc2Vjb25kcyBOIHwgLS1pdGVyYXRpb25zIE5dIFstLXNlZWQgTl0gWy0tZmluZGluZ3MgRElSXQotLSAgICAgICBbLS13aXJlLXNlZWRzIERJUl0gWy0tanNvbi1zZWVkcyBESVJdIFstLXRleHQtc2VlZHMgRElSXQotLSAgIGx1YSB0ZXN0L2x1YS1mdXp6L2Z1enoubHVhIC0tZ2VuZXJhdGVkIERJUiAtLWNvcnB1cyBGSUxFCi0tICAgICAgIC0tcmVwbGF5IERJUiBbLS1maW5kaW5ncyBESVJdCgpsb2NhbCBvcHRzID0gewogIHNlY29uZHMgPSAxMCwgaXRlcmF0aW9ucyA9IDAsIHNlZWQgPSAxLAogIGZpbmRpbmdzID0gIiIsIHJlcGxheSA9ICIiLAp9CmxvY2FsIGkgPSAxCndoaWxlIGkgPD0gI2FyZyBkbwogIGxvY2FsIGEgPSBhcmdbaV0KICBsb2NhbCBrLCB2ID0gYTptYXRjaCgiXiUtJS0oWyV3LV0rKT0oLiopJCIpCiAgaWYgbm90IGsgdGhlbgogICAgayA9IGE6bWF0Y2goIl4lLSUtKFsldy1dKykkIikKICAgIGlmIGsgdGhlbiBpID0gaSArIDE7IHYgPSBhcmdbaV0gZW5kCiAgZW5kCiAgaWYgbm90IGsgdGhlbgogICAgaW8uc3RkZXJyOndyaXRlKCJmdXp6OiB1bmtub3duIGFyZ3VtZW50ICIgLi4gYSAuLiAiXG4iKQogICAgb3MuZXhpdCgyKQogIGVuZAogIGsgPSBrOmdzdWIoIiUtIiwgIl8iKQogIG9wdHNba10gPSB2CiAgaSA9IGkgKyAxCmVuZApmb3IgXywgayBpbiBpcGFpcnMoeyAiZ2VuZXJhdGVkIiwgImNvcnB1cyIgfSkgZG8KICBpZiBub3Qgb3B0c1trXSB0aGVuCiAgICBpby5zdGRlcnI6d3JpdGUoImZ1eno6IC0tIiAuLiBrOmdzdWIoIl8iLCAiLSIpIC4uICIgaXMgcmVxdWlyZWRcbiIpCiAgICBvcy5leGl0KDIpCiAgZW5kCmVuZAoKbG9jYWwgR0VORVJBVEVEID0gb3B0cy5nZW5lcmF0ZWQKCmxvY2FsIGZ1bmN0aW9uIHJlYWRGaWxlKHBhdGgpCiAgbG9jYWwgZiwgZXJyID0gaW8ub3BlbihwYXRoLCAicmIiKQogIGlmIG5vdCBmIHRoZW4gcmV0dXJuIG5pbCwgZXJyIGVuZAogIGxvY2FsIGRhdGEgPSBmOnJlYWQoIiphIikKICBmOmNsb3NlKCkKICByZXR1cm4gZGF0YQplbmQKCmxvY2FsIGZ1bmN0aW9uIHdyaXRlRmlsZShwYXRoLCBkYXRhKQogIGxvY2FsIGYgPSBhc3NlcnQoaW8ub3BlbihwYXRoLCAid2IiKSkKICBmOndyaXRlKGRhdGEpCiAgZjpjbG9zZSgpCmVuZAoKbG9jYWwgZnVuY3Rpb24gbGluZXMocGF0aCkKICBsb2NhbCBmID0gYXNzZXJ0KGlvLm9wZW4ocGF0aCwgInIiKSkKICBsb2NhbCBvdXQgPSB7fQogIGZvciByYXcgaW4gZjpsaW5lcygpIGRvCiAgICBsb2NhbCB0ZXh0ID0gcmF3OmdzdWIoIl4lcysiLCAiIik6Z3N1YigiJXMrJCIsICIiKQogICAgaWYgI3RleHQgPiAwIGFuZCB0ZXh0OnN1YigxLCAxKSB+PSAiIyIgdGhlbiBvdXRbI291dCArIDFdID0gdGV4dCBlbmQKICBlbmQKICBmOmNsb3NlKCkKICByZXR1cm4gb3V0CmVuZAoKLS0gQkFTRTY0LCB0aGUgdGV4dCBmb3JtJ3MgYm9keSBlbmNvZGluZyAodGV4dC5sdWEpLgpsb2NhbCBCNjQgPSAiQUJDREVGR0hJSktMTU5PUFFSU1RVVldYWVphYmNkZWZnaGlqa2xtbm9wcXJzdHV2d3h5ejAxMjM0NTY3ODkrLyIKbG9jYWwgZnVuY3Rpb24gYjY0KGJ5dGVzKQogIGxvY2FsIG91dCA9IHt9CiAgZm9yIHAgPSAxLCAjYnl0ZXMsIDMgZG8KICAgIGxvY2FsIGEsIGIsIGMgPSBieXRlczpieXRlKHAsIHAgKyAyKQogICAgbG9jYWwgbiA9IChhIDw8IDE2KSB8ICgoYiBvciAwKSA8PCA4KSB8IChjIG9yIDApCiAgICBvdXRbI291dCArIDFdID0gQjY0OnN1YigoKG4gPj4gMTgpICYgNjMpICsgMSwgKChuID4+IDE4KSAmIDYzKSArIDEpCiAgICBvdXRbI291dCArIDFdID0gQjY0OnN1YigoKG4gPj4gMTIpICYgNjMpICsgMSwgKChuID4+IDEyKSAmIDYzKSArIDEpCiAgICBvdXRbI291dCArIDFdID0gYiBhbmQgQjY0OnN1YigoKG4gPj4gNikgJiA2MykgKyAxLCAoKG4gPj4gNikgJiA2MykgKyAxKSBvciAiPSIKICAgIG91dFsjb3V0ICsgMV0gPSBjIGFuZCBCNjQ6c3ViKChuICYgNjMpICsgMSwgKG4gJiA2MykgKyAxKSBvciAiPSIKICBlbmQKICByZXR1cm4gdGFibGUuY29uY2F0KG91dCkKZW5kCmxvY2FsIGZ1bmN0aW9uIHVuYjY0KHRleHQpCiAgbG9jYWwgbWFwID0ge30KICBmb3IgaiA9IDEsICNCNjQgZG8gbWFwW0I2NDpzdWIoaiwgaildID0gaiAtIDEgZW5kCiAgbG9jYWwgb3V0ID0ge30KICBsb2NhbCBuLCBiaXRzID0gMCwgMAogIGZvciBqID0gMSwgI3RleHQgZG8KICAgIGxvY2FsIGMgPSB0ZXh0OnN1YihqLCBqKQogICAgaWYgYyA9PSAiPSIgdGhlbiBicmVhayBlbmQKICAgIGxvY2FsIHYgPSBtYXBbY10KICAgIGlmIG5vdCB2IHRoZW4gcmV0dXJuIG5pbCBlbmQKICAgIG4gPSAobiA8PCA2KSB8IHYKICAgIGJpdHMgPSBiaXRzICsgNgogICAgaWYgYml0cyA+PSA4IHRoZW4KICAgICAgYml0cyA9IGJpdHMgLSA4CiAgICAgIG91dFsjb3V0ICsgMV0gPSBzdHJpbmcuY2hhcigobiA+PiBiaXRzKSAmIDI1NSkKICAgIGVuZAogIGVuZAogIHJldHVybiB0YWJsZS5jb25jYXQob3V0KQplbmQKCi0tIEEgQ09SUFVTIExJTkUgPGtpbmQ+IDx1bml0PiA8dGFibGU+IDxiYXNlNjQgcGF5bG9hZD4gaXMgYm90aCB0aGUgZmluZGluZwotLSBmaWxlIGFuZCB0aGUgcmVwbGF5IGlucHV0OiB0aGUgcGF5bG9hZCBpcyB0aGUgZXhhY3QgYnl0ZXMsIHNvIGEgcmVwbGF5Ci0tIHJlcHJvZHVjZXMgdGhlIGlucHV0IHdpdGhvdXQgdGhlIFBSTkcuCmxvY2FsIGZ1bmN0aW9uIGRlY29kZUNhc2UodGV4dCkKICB0ZXh0ID0gdGV4dDpnc3ViKCIlcyskIiwgIiIpCiAgbG9jYWwga2luZCwgdW5pdCwgdGFibGVOYW1lLCBwYXlsb2FkID0gdGV4dDptYXRjaCgiXiglUyspJXMrKCVTKyklcysoJVMrKSVzKyglUyspJCIpCiAgaWYgbm90IGtpbmQgdGhlbiByZXR1cm4gbmlsIGVuZAogIGxvY2FsIGJ5dGVzID0gdW5iNjQocGF5bG9hZCkKICAtLSB0aGUgcGF5bG9hZCBpcyB0aGUgY2Fub25pY2FsIGVuY29kaW5nIG9mIGl0cyBieXRlcywgZXhhY3RseTogdW5iNjQgc3RvcHMKICAtLSBhdCB0aGUgZmlyc3QgJz0nLCBzbyAiZTMwPSEiIHVzZWQgdG8gZGVjb2RlIGFzICJlMzA9IiBhbmQgcmVwbGF5IGEgY2FzZQogIC0tIHRoYXQgaXMgbm90IHRoZSBzYXZlZCB0ZXh0CiAgaWYgbm90IGJ5dGVzIG9yIGI2NChieXRlcykgfj0gcGF5bG9hZCB0aGVuIHJldHVybiBuaWwgZW5kCiAgcmV0dXJuIHsga2luZCA9IGtpbmQsIHVuaXQgPSB1bml0LCB0YWJsZSA9IHRhYmxlTmFtZSwgYnl0ZXMgPSBieXRlcyB9CmVuZAoKbG9jYWwgZnVuY3Rpb24gZW5jb2RlQ2FzZShjKQogIHJldHVybiBjLmtpbmQgLi4gIiAiIC4uIGMudW5pdCAuLiAiICIgLi4gYy50YWJsZSAuLiAiICIgLi4gYjY0KGMuYnl0ZXMpIC4uICJcbiIKZW5kCgotLSBUSEUgVFlQRUQgUkVGVVNBTFM6IHJlZnVzYWwubHVhJ3MgbmFtZXMgKGl0cyBSZWFkU3RyaWN0IGFuZCBjb3VudGVycywgaW4KLS0gVGFibGVSZXBvcnQuRXJyKCkncyBvcmRlciwgZG9jcy9TUEVDLVRBQkxFUy5tZCDCpzYuNSksIGxvYWQubHVhJ3Mgc3RyaWN0Ci0tIHJlYXNvbnMgKGNvdW50X3JlZnVzYWwsIMKnMi45KSwgZml4ZWQubHVhJ3MgZm9ybS0zIHJlZnVzYWxzIChSZWFkU3RyaWN0IGhhbmRzCi0tIGEgZml4ZWQgcm9vdCdzIGZvcm0tMyBieXRlcyB0byBULlJlYWRGaXhlZCksIHBsdXMgdGV4dC5sdWEncyBiYXNlNjRfZGFtYWdlZAotLSBhbmQgdGhlIHJ1bnRpbWUncyBpbnZhbGlkX3ZhbHVlLiBBIEx1YSBlcnJvciB3aG9zZSBtZXNzYWdlIGlzIG9uZSBvZiB0aGVzZQotLSBpcyBhIGNsZWFuIHJlZnVzYWwsIGFuZCB0aGUgb3JhY2xlIGFjY2VwdHMgaXQuIGludGVybmFsL2NpJ3MKLS0gVGVzdEx1YU5pZ2h0bHlGdXp6TGFuZSBmYWlscyB3aGVuIHRoZSBydW50aW1lIG5hbWVzIGEgcmVmdXNhbCB0aGlzIHRhYmxlCi0tIGRvZXMgbm90IGNhcnJ5LCBzbyB0aGUgb3JhY2xlIGNhbm5vdCBkcmlmdCBiZWhpbmQgdGhlIHBvcnQgYWdhaW4uCmxvY2FsIFRZUEVEID0gewogIHByZXZpb3VzX2Zvcm0gPSB0cnVlLCB1bmtub3duX2Zvcm0gPSB0cnVlLCBtZXNzYWdlX2Zvcm1fYXNfZmlsZSA9IHRydWUsCiAgaW50ZWdyaXR5X21pc21hdGNoID0gdHJ1ZSwgd2lyZV9kYW1hZ2VkID0gdHJ1ZSwgbWlzc2luZ19yZXF1aXJlZCA9IHRydWUsCiAgbWFsZm9ybWVkID0gdHJ1ZSwgYmxvYl9zaGFyZWQgPSB0cnVlLCBvdXRfb2ZfcmFuZ2UgPSB0cnVlLAogIGtpbmRfbWlzbWF0Y2ggPSB0cnVlLCBub25fZmluaXRlX2Zsb2F0ID0gdHJ1ZSwgY291bnRfcmVmdXNhbCA9IHRydWUsCiAgcmVwZWF0ZWRfa2V5ID0gdHJ1ZSwgdW5rbm93biA9IHRydWUsCiAgYmFzZTY0X2RhbWFnZWQgPSB0cnVlLCBpbnZhbGlkX3ZhbHVlID0gdHJ1ZSwKICBjb3VudF9vdmVyX2V4dGVudF9jYXAgPSB0cnVlLCBjb3VudF9vdmVyX2JvdW5kID0gdHJ1ZSwgbGVuZ3RoX292ZXJfYm91bmQgPSB0cnVlLCBsYXlvdXRfbWFsZm9ybWVkID0gdHJ1ZSwKICBsYXlvdXRfcmVjb3JkX3Rvb19sYXJnZSA9IHRydWUsIGxheW91dF91bnN1cHBvcnRlZCA9IHRydWUsCiAgbm9fbGF5b3V0ID0gdHJ1ZSwgcGxhbl90b29fbGFyZ2UgPSB0cnVlLAogIGJhdGNoX3Rvb19sYXJnZSA9IHRydWUsIGxheW91dF91bmtub3duID0gdHJ1ZSwgbmV3ZXJfZm9ybSA9IHRydWUsCiAgcmVzZXJ2ZWRfbm90X3plcm8gPSB0cnVlLCB0ZXh0X2lsbF9mb3JtZWQgPSB0cnVlLCB0cnVuY2F0ZWQgPSB0cnVlLAogIGJsb2Jfb3Zlcl9zaXplX2NhcCA9IHRydWUsCn0KbG9jYWwgZnVuY3Rpb24gdHlwZWQoZXJyKQogIGxvY2FsIHMgPSB0b3N0cmluZyhlcnIpCiAgaWYgVFlQRURbc10gdGhlbiByZXR1cm4gdHJ1ZSBlbmQKICBsb2NhbCBuYW1lID0gczptYXRjaCgiXihbJWFfXSspOiIpIG9yIHM6bWF0Y2goIjolZCs6IChbJWFfXSspIikKICByZXR1cm4gbmFtZSB+PSBuaWwgYW5kIFRZUEVEW25hbWVdID09IHRydWUKZW5kCgotLSBUSEUgVU5JVFMuIHBhY2thZ2UubG9hZGVkIGlzIGNsZWFyZWQgYmVmb3JlIGVhY2ggdW5pdCBiZWNhdXNlIGV2ZXJ5IHVuaXQKLS0gc2hpcHMgaXRzIG93biBkZXNjcmlwdG9yL2xvYWQvcmVmdXNhbC9qc29uL3RleHQgbW9kdWxlcyB1bmRlciB0aGUgc2FtZQotLSBuYW1lczsgcmVxdWlyZSgpIHdvdWxkIGhhbmQgYSB1bml0IHRoZSBwcmV2aW91cyB1bml0J3MgcnVudGltZSBvdGhlcndpc2UuCmxvY2FsIHVuaXRzID0ge30KZm9yIF8sIGxpbmUgaW4gaXBhaXJzKGxpbmVzKG9wdHMuY29ycHVzKSkgZG8KICBsb2NhbCBrZXksIGRpciA9IGxpbmU6bWF0Y2goIl4oJVMrKSVzKyglUyspJCIpCiAgaWYga2V5IHRoZW4gdW5pdHNbI3VuaXRzICsgMV0gPSB7IGtleSA9IGtleSwgZGlyID0gZGlyIH0gZW5kCmVuZAoKLS0gRklMRSBESVNDT1ZFUlkgdGhhdCBjYW5ub3QgbG9zZSBhIGZpbGUgcXVpZXRseS4gZmluZCdzIG93biBleGl0IHN0YXR1cyBpcwotLSByZWFkIGJhY2sgYXMgYSBsYXN0IGxpbmUgKGBmaW5kIHwgc29ydGAgcmVwb3J0ZWQgc29ydCdzIHN0YXR1cywgYW5kIHRoZQotLSBjbG9zZSBzdGF0dXMgd2FzIGlnbm9yZWQsIHNvIGEgZmluZCB0aGF0IGxpc3RlZCBvbmUgZmlsZSBvZiB0d28gYW5kIGZhaWxlZAotLSByZXBsYXllZCBvbmUgY2FzZSBncmVlbik7IHRoZSBuYW1lcyBhcmUgc29ydGVkIGhlcmU7IGEgZmFpbHVyZSByZXR1cm5zIG5pbAotLSBhbmQgdGhlIHJlYXNvbiwgYW5kIHRoZSBjYWxsZXIgc3RvcHMuCmxvY2FsIGZ1bmN0aW9uIGxpc3RGaWxlcyhkaXIsIGdsb2IpCiAgbG9jYWwgcXVvdGVkID0gIiciIC4uIGRpcjpnc3ViKCInIiwgIidcXCcnIikgLi4gIiciCiAgbG9jYWwgY21kID0gImZpbmQgIiAuLiBxdW90ZWQgLi4gIiAtdHlwZSBmIiAuLiAoZ2xvYiBhbmQgKCIgLW5hbWUgJyIgLi4gZ2xvYiAuLiAiJyIpIG9yICIiKSAuLiAiOyBlY2hvIFwiZmluZC1leGl0PSQ/XCIiCiAgbG9jYWwgcCA9IGlvLnBvcGVuKGNtZCkKICBsb2NhbCBvdXQsIHN0YXR1cyA9IHt9LCBuaWwKICBmb3IgbGluZSBpbiBwOmxpbmVzKCkgZG8KICAgIGxvY2FsIGNvZGUgPSBsaW5lOm1hdGNoKCJeZmluZCUtZXhpdD0oJWQrKSQiKQogICAgaWYgY29kZSB0aGVuIHN0YXR1cyA9IHRvbnVtYmVyKGNvZGUpIGVsc2Ugb3V0WyNvdXQgKyAxXSA9IGxpbmUgZW5kCiAgZW5kCiAgbG9jYWwgY2xvc2VkID0gcDpjbG9zZSgpCiAgaWYgc3RhdHVzIH49IDAgb3Igbm90IGNsb3NlZCB0aGVuCiAgICByZXR1cm4gbmlsLCAiZmluZCBleGl0ZWQgIiAuLiB0b3N0cmluZyhzdGF0dXMpIC4uICIsIHNvIHRoZSBsaXN0aW5nIG1heSBiZSBwYXJ0aWFsIgogIGVuZAogIHRhYmxlLnNvcnQob3V0KQogIHJldHVybiBvdXQKZW5kCgpsb2NhbCBmdW5jdGlvbiB0YWJsZXNNb2R1bGUoa2V5KQogIGxvY2FsIHAgPSBpby5wb3BlbigibHMgIiAuLiBHRU5FUkFURUQgLi4gIi8iIC4uIGtleSAuLiAiLypUYWJsZXMubHVhIDI+L2Rldi9udWxsIHwgaGVhZCAtMSIpCiAgbG9jYWwgcGF0aCA9IHA6cmVhZCgiKmwiKQogIHA6Y2xvc2UoKQogIHJldHVybiBwYXRoCmVuZAoKbG9jYWwgZnVuY3Rpb24gbG9hZFVuaXQoa2V5KQogIGxvY2FsIG1vZHBhdGggPSB0YWJsZXNNb2R1bGUoa2V5KQogIGlmIG5vdCBtb2RwYXRoIHRoZW4gcmV0dXJuIG5pbCBlbmQKICBsb2NhbCBkaXIgPSBtb2RwYXRoOm1hdGNoKCJeKC4qKS9bXi9dKyQiKQogIGZvciBuYW1lIGluIHBhaXJzKHBhY2thZ2UubG9hZGVkKSBkbyBwYWNrYWdlLmxvYWRlZFtuYW1lXSA9IG5pbCBlbmQKICBwYWNrYWdlLnBhdGggPSBkaXIgLi4gIi8/Lmx1YTsiIC4uIHBhY2thZ2UucGF0aAogIGxvY2FsIFQgPSBkb2ZpbGUobW9kcGF0aCkKICBsb2NhbCBMID0gZG9maWxlKGRpciAuLiAiL2xvYWQubHVhIikKICBsb2NhbCBSID0gZG9maWxlKGRpciAuLiAiL3JlZnVzYWwubHVhIikKICBsb2NhbCBYID0gZG9maWxlKGRpciAuLiAiL3RleHQubHVhIikKICBsb2NhbCBKID0gZG9maWxlKGRpciAuLiAiL2pzb24ubHVhIikKICBsb2NhbCBuYW1lcywgdGFibGVzID0ge30sIHt9CiAgZm9yIG5hbWUsIGRlc2MgaW4gcGFpcnMoVC5UYWJsZXMgb3Ige30pIGRvCiAgICBpZiBkZXNjLnRhYmxlIHRoZW4gbmFtZXNbI25hbWVzICsgMV0gPSBuYW1lOyB0YWJsZXNbbmFtZV0gPSB0cnVlIGVuZAogIGVuZAogIHRhYmxlLnNvcnQobmFtZXMpCiAgcmV0dXJuIHsga2V5ID0ga2V5LCBUID0gVCwgTCA9IEwsIFIgPSBSLCBYID0gWCwgSiA9IEosIG5hbWVzID0gbmFtZXMsIHRhYmxlcyA9IHRhYmxlcyB9CmVuZAoKbG9jYWwgbG9hZGVkID0ge30KbG9jYWwgdGFyZ2V0cyA9IHt9CmZvciBfLCB1IGluIGlwYWlycyh1bml0cykgZG8KICBsb2NhbCBtID0gbG9hZFVuaXQodS5rZXkpCiAgaWYgbSB0aGVuCiAgICBsb2FkZWRbdS5rZXldID0gbQogICAgZm9yIF8sIG5hbWUgaW4gaXBhaXJzKG0ubmFtZXMpIGRvCiAgICAgIGZvciBfLCBraW5kIGluIGlwYWlycyh7ICJsb2FkIiwgInN0cmljdCIsICJ0ZXh0IiwgImpzb24iIH0pIGRvCiAgICAgICAgdGFyZ2V0c1sjdGFyZ2V0cyArIDFdID0geyB1bml0ID0gdS5rZXksIHRhYmxlID0gbmFtZSwga2luZCA9IGtpbmQgfQogICAgICBlbmQKICAgIGVuZAogIGVuZAplbmQKaWYgI3RhcmdldHMgPT0gMCB0aGVuCiAgaW8uc3RkZXJyOndyaXRlKCJmdXp6OiBubyBnZW5lcmF0ZWQgdW5pdCB1bmRlciAiIC4uIEdFTkVSQVRFRCAuLiAiIChydW4gdGhlIG1ha2UgdGFyZ2V0KVxuIikKICBvcy5leGl0KDIpCmVuZAoKLS0gVEhFIFNFRURTLiBXaXJlIHZlY3RvcnMgZHJpdmUgbG9hZC9zdHJpY3QvdGV4dCAodGV4dCBpcyB0aGVpciBiYXNlNjQgYm9keSk7Ci0tIEpTT04gdmVjdG9ycyBkcml2ZSBqc29uLiBUaGUgR28gZnV6eiBjb3JwdXMgKGludGVybmFsL2Z1enovdGVzdGRhdGEpIGlzCi0tIGluY2x1ZGVkIGFzIHJhdyBtdXRhdGlvbiByb290cywgYXMgdGhlIGNhcmQgYXNrcy4KbG9jYWwgc2VlZHMgPSB7IGxvYWQgPSB7fSwgc3RyaWN0ID0ge30sIHRleHQgPSB7fSwganNvbiA9IHt9IH0KbG9jYWwgZnVuY3Rpb24gc2VlZEZpbGVzKGRpcikKICBsb2NhbCBwYXRocywgZXJyID0gbGlzdEZpbGVzKGRpcikKICBpZiBub3QgcGF0aHMgdGhlbgogICAgaW8uc3RkZXJyOndyaXRlKCJmdXp6OiBzZWVkIGRpcmVjdG9yeSAiIC4uIGRpciAuLiAiOiAiIC4uIGVyciAuLiAiXG4iKQogICAgb3MuZXhpdCgyKQogIGVuZAogIHJldHVybiBwYXRocwplbmQKbG9jYWwgZnVuY3Rpb24gYWRkU2VlZHMoa2luZCwgZGlyLCBtYXgpCiAgZm9yIF8sIHBhdGggaW4gaXBhaXJzKHNlZWRGaWxlcyhkaXIpKSBkbwogICAgbG9jYWwgZGF0YSA9IHJlYWRGaWxlKHBhdGgpCiAgICBpZiBkYXRhIGFuZCAjZGF0YSA+IDAgYW5kICNkYXRhIDw9IChtYXggb3IgNDA5NikgdGhlbgogICAgICBzZWVkc1traW5kXVsjc2VlZHNba2luZF0gKyAxXSA9IGRhdGEKICAgIGVuZAogIGVuZAplbmQKaWYgb3B0cy53aXJlX3NlZWRzIHRoZW4KICBmb3IgZGlyIGluIG9wdHMud2lyZV9zZWVkczpnbWF0Y2goIlteOl0rIikgZG8KICAgIGFkZFNlZWRzKCJsb2FkIiwgZGlyKTsgYWRkU2VlZHMoInN0cmljdCIsIGRpcik7IGFkZFNlZWRzKCJ0ZXh0IiwgZGlyKQogIGVuZAplbmQKaWYgb3B0cy5qc29uX3NlZWRzIHRoZW4KICBmb3IgZGlyIGluIG9wdHMuanNvbl9zZWVkczpnbWF0Y2goIlteOl0rIikgZG8gYWRkU2VlZHMoImpzb24iLCBkaXIpIGVuZAplbmQKaWYgb3B0cy50ZXh0X3NlZWRzIHRoZW4KICBmb3IgZGlyIGluIG9wdHMudGV4dF9zZWVkczpnbWF0Y2goIlteOl0rIikgZG8KICAgIGZvciBfLCBwYXRoIGluIGlwYWlycyhzZWVkRmlsZXMoZGlyKSkgZG8KICAgICAgbG9jYWwgZCA9IHJlYWRGaWxlKHBhdGgpCiAgICAgIGlmIGQgdGhlbiBzZWVkcy50ZXh0WyNzZWVkcy50ZXh0ICsgMV0gPSBkIGVuZAogICAgZW5kCiAgZW5kCmVuZApmb3IgXywga2luZCBpbiBpcGFpcnMoeyAibG9hZCIsICJzdHJpY3QiLCAidGV4dCIgfSkgZG8KICBpZiAjc2VlZHNba2luZF0gPT0gMCB0aGVuIHNlZWRzW2tpbmRdWzFdID0gc3RyaW5nLmNoYXIoMSkgLi4gIlwwXDBcMFwwIiBlbmQKZW5kCmlmICNzZWVkcy5qc29uID09IDAgdGhlbiBzZWVkcy5qc29uWzFdID0gInt9IiBlbmQKCi0tIE1VVEFUSU9OLCBzZWVkZWQgYW5kIGRldGVybWluaXN0aWM6IHhvcnNoaWZ0NjQqIG92ZXIgTHVhIDUuNCBpbnRlZ2Vycy4KbG9jYWwgc3RhdGUgPSB0b251bWJlcihvcHRzLnNlZWQpIG9yIDEKaWYgc3RhdGUgPT0gMCB0aGVuIHN0YXRlID0gMSBlbmQKbG9jYWwgZnVuY3Rpb24gcm5kKCkKICBzdGF0ZSA9IHN0YXRlIH4gKHN0YXRlIDw8IDEzKQogIHN0YXRlID0gc3RhdGUgfiAoc3RhdGUgPj4gNykKICBzdGF0ZSA9IHN0YXRlIH4gKHN0YXRlIDw8IDE3KQogIHJldHVybiBzdGF0ZQplbmQKbG9jYWwgZnVuY3Rpb24gcGlja24obikKICBsb2NhbCB2ID0gcm5kKCkgJiAweDdmZmZmZmZmZmZmZmZmZmYKICByZXR1cm4gKHYgJSBuKSArIDEKZW5kCgpsb2NhbCBJTlRFUkVTVElORyA9IHsgMCwgMSwgMiwgMywgMHg3ZiwgMHg4MCwgMHg4MSwgMHhmZSwgMHhmZiwgMHgzZCwgMHg3YiwgMHgyMiB9Cgpsb2NhbCBmdW5jdGlvbiBtdXRhdGUoYmFzZSkKICBsb2NhbCBieXRlcyA9IHsgYmFzZTpieXRlKDEsIG1hdGgubWluKCNiYXNlLCA0MDk2KSkgfQogIGxvY2FsIGVkaXRzID0gcGlja24oNikKICBmb3IgXyA9IDEsIGVkaXRzIGRvCiAgICBsb2NhbCBvcCA9IHBpY2tuKDYpCiAgICBsb2NhbCBuID0gI2J5dGVzCiAgICBpZiBvcCA9PSAxIGFuZCBuID4gMCB0aGVuCiAgICAgIGxvY2FsIGF0ID0gcGlja24obikKICAgICAgbG9jYWwgYml0ID0gMSA8PCAocGlja24oOCkgLSAxKQogICAgICBieXRlc1thdF0gPSAoYnl0ZXNbYXRdIH4gYml0KSAmIDI1NQogICAgZWxzZWlmIG9wID09IDIgYW5kIG4gPiAwIHRoZW4KICAgICAgYnl0ZXNbcGlja24obildID0gSU5URVJFU1RJTkdbcGlja24oI0lOVEVSRVNUSU5HKV0KICAgIGVsc2VpZiBvcCA9PSAzIGFuZCBuIDwgNDA5NiB0aGVuCiAgICAgIGxvY2FsIGF0ID0gcGlja24obiArIDEpCiAgICAgIHRhYmxlLmluc2VydChieXRlcywgYXQsIElOVEVSRVNUSU5HW3BpY2tuKCNJTlRFUkVTVElORyldKQogICAgZWxzZWlmIG9wID09IDQgYW5kIG4gPiAxIHRoZW4KICAgICAgdGFibGUucmVtb3ZlKGJ5dGVzLCBwaWNrbihuKSkKICAgIGVsc2VpZiBvcCA9PSA1IGFuZCBuID4gMCBhbmQgbiA8IDQwOTYgdGhlbgogICAgICBsb2NhbCBmcm9tID0gcGlja24obikKICAgICAgbG9jYWwgbGVuID0gcGlja24obWF0aC5taW4oMTYsIG4gLSBmcm9tICsgMSkpCiAgICAgIGxvY2FsIGF0ID0gcGlja24obiArIDEpCiAgICAgIGZvciBqID0gbGVuLCAxLCAtMSBkbyB0YWJsZS5pbnNlcnQoYnl0ZXMsIGF0LCBieXRlc1tmcm9tICsgaiAtIDFdKSBlbmQKICAgIGVsc2VpZiBvcCA9PSA2IGFuZCBuID4gMCB0aGVuCiAgICAgIGxvY2FsIGF0ID0gcGlja24obikKICAgICAgYnl0ZXNbYXRdID0gcm5kKCkgJiAyNTUKICAgIGVuZAogIGVuZAogIHJldHVybiBzdHJpbmcuY2hhcih0YWJsZS51bnBhY2soYnl0ZXMpKQplbmQKCi0tIFRIRSBSRUFERVJTLiBFYWNoIHJldHVybnMgdHJ1ZSB3aGVuIHRoZSBpbnB1dCB3YXMgaGFuZGxlZCAoYSB2YWx1ZSBvciBhCi0tIGNsZWFuIHR5cGVkIHJlZnVzYWwpIGFuZCByYWlzZXMgb25seSB0aGUgcmF3IEx1YSBlcnJvciB0aGUgb3JhY2xlIHJlcG9ydHMuCi0tIEEgY2FzZSBubyByZWFkZXIgY2FuIHRha2UgKGEgdW5pdCB0aGF0IHdhcyBub3QgZ2VuZXJhdGVkLCBhIHRhYmxlIGl0cyB1bml0Ci0tIGRvZXMgbm90IHJlZ2lzdGVyLCBhIGtpbmQgd2l0aCBubyBlbnRyeSBwb2ludCkgcmFpc2VzIGEgaGFybmVzcyBlcnJvciwgd2hpY2gKLS0gaXMgbm90IGEgdHlwZWQgcmVmdXNhbDogaXQgdXNlZCB0byByZXR1cm4gYXMgaWYgYSByZWFkZXIgaGFkIHBhc3NlZCwgc28gYQotLSBzYXZlZCBjYXNlIGZvciBhIHVuaXQgdGhhdCBzdG9wcGVkIGdlbmVyYXRpbmcgcmVwbGF5ZWQgZ3JlZW4gYW5kIHJlYWQKLS0gbm90aGluZy4KbG9jYWwgS0lORFMgPSB7IGxvYWQgPSB0cnVlLCBzdHJpY3QgPSB0cnVlLCB0ZXh0ID0gdHJ1ZSwganNvbiA9IHRydWUgfQpsb2NhbCBmdW5jdGlvbiBkcml2ZShjKQogIGxvY2FsIHUgPSBsb2FkZWRbYy51bml0XQogIGlmIG5vdCB1IHRoZW4gZXJyb3IoImhhcm5lc3M6IHVuaXQgIiAuLiB0b3N0cmluZyhjLnVuaXQpIC4uICIgd2FzIG5vdCBnZW5lcmF0ZWQsIHNvIG5vIHJlYWRlciB0YWtlcyB0aGlzIGNhc2UiLCAwKSBlbmQKICBpZiBub3QgS0lORFNbYy5raW5kXSB0aGVuIGVycm9yKCJoYXJuZXNzOiBubyByZWFkZXIgZm9yIGtpbmQgIiAuLiB0b3N0cmluZyhjLmtpbmQpLCAwKSBlbmQKICBpZiBub3QgdS50YWJsZXNbYy50YWJsZV0gdGhlbiBlcnJvcigiaGFybmVzczogdW5pdCAiIC4uIGMudW5pdCAuLiAiIHJlZ2lzdGVycyBubyB0YWJsZSAiIC4uIHRvc3RyaW5nKGMudGFibGUpLCAwKSBlbmQKICBsb2NhbCBULCBuYW1lLCBieXRlcyA9IHUuVCwgYy50YWJsZSwgYy5ieXRlcwogIGlmIGMua2luZCA9PSAibG9hZCIgdGhlbgogICAgbG9jYWwgcmVwb3J0ID0gewogICAgICBkYW1hZ2VkID0gZmFsc2UsIHJlZnVzZWQgPSAwLCB1bmtub3duID0gMCwga2luZENoYW5nZWQgPSAwLAogICAgICBvdXRPZlJhbmdlID0gMCwgd2lkZW5lZCA9IDAsIHJlcGVhdGVkS2V5ID0gMCwKICAgIH0KICAgIHUuTC5Mb2FkKFQsIG5hbWUsIGJ5dGVzLCByZXBvcnQpCiAgZWxzZWlmIGMua2luZCA9PSAic3RyaWN0IiB0aGVuCiAgICB1LlIuUmVhZFN0cmljdChULCBuYW1lLCBieXRlcykKICBlbHNlaWYgYy5raW5kID09ICJ0ZXh0IiB0aGVuCiAgICB1LlguTG9hZFRleHQoVCwgbmFtZSwgYjY0KGJ5dGVzKSkKICBlbHNlaWYgYy5raW5kID09ICJqc29uIiB0aGVuCiAgICBsb2NhbCByZXBvcnQgPSB7CiAgICAgIGRhbWFnZWQgPSBmYWxzZSwgcmVmdXNlZCA9IDAsIHVua25vd24gPSAwLCBraW5kQ2hhbmdlZCA9IDAsCiAgICAgIG91dE9mUmFuZ2UgPSAwLCB3aWRlbmVkID0gMCwgcmVwZWF0ZWRLZXkgPSAwLAogICAgfQogICAgdS5KLkZyb21Kc29uKFQsIG5hbWUsIGJ5dGVzLCByZXBvcnQpCiAgZW5kCmVuZAoKbG9jYWwgZmluZGluZ3NEaXIgPSBvcHRzLmZpbmRpbmdzCmxvY2FsIGZ1bmN0aW9uIHNhdmVGaW5kaW5nKGMsIGVycikKICBpZiBub3QgZmluZGluZ3NEaXIgb3IgZmluZGluZ3NEaXIgPT0gIiIgdGhlbiByZXR1cm4gZW5kCiAgb3MuZXhlY3V0ZSgibWtkaXIgLXAgIiAuLiBmaW5kaW5nc0RpcikKICBsb2NhbCBzbHVnID0gYy5raW5kIC4uICJfIiAuLiBjLnVuaXQgLi4gIl8iIC4uIGMudGFibGU6Z3N1YigiW14ld19dIiwgIl8iKQogIGxvY2FsIG4gPSAxCiAgbG9jYWwgcGF0aAogIHJlcGVhdAogICAgcGF0aCA9IGZpbmRpbmdzRGlyIC4uICIvIiAuLiBzbHVnIC4uICJfIiAuLiBuIC4uICIuY2FzZSIKICAgIGxvY2FsIGYgPSBpby5vcGVuKHBhdGgsICJyIikKICAgIGlmIGYgdGhlbiBmOmNsb3NlKCk7IHBhdGggPSBuaWw7IG4gPSBuICsgMSBlbmQKICB1bnRpbCBwYXRoCiAgd3JpdGVGaWxlKHBhdGgsIGVuY29kZUNhc2UoYykpCiAgd3JpdGVGaWxlKHBhdGggLi4gIi5lcnIiLCB0b3N0cmluZyhlcnIpIC4uICJcbiIpCiAgcmV0dXJuIHBhdGgKZW5kCgotLSBSRVBMQVk6IGV2ZXJ5IHNhdmVkIGNhc2UsIG9uY2UsIHdpdGggbm8gY2xvY2suIEEgc2F2ZWQgZmluZGluZyB0aGF0IG5vCi0tIGxvbmdlciBmaXJlcyBpcyBzdGlsbCByZXBsYXllZDsgYSBjYXNlIHRoYXQgZmlyZXMgaXMgYSByZWdyZXNzaW9uLgotLSBFdmVyeSBzYXZlZCBmaWxlIGlzIGNvdW50ZWQsIGFuZCBldmVyeSBvbmUgbXVzdCBiZSByZXBsYXllZDogYSBmaWxlIHRoYXQKLS0gZG9lcyBub3QgZGVjb2RlIGlzIGEgZmFpbHVyZSwgbmV2ZXIgYSBmaWxlIGxlZnQgb3V0IG9mIHRoZSBjb3VudCwgYW5kIGEKLS0gZGlyZWN0b3J5IHRoYXQgaXMgbWlzc2luZyBvciBob2xkcyBubyBjYXNlIGlzIGEgcmVwbGF5IG9mIG5vdGhpbmcsIHdoaWNoIGlzCi0tIHJlZC4gRWFjaCBvZiB0aGVzZSB1c2VkIHRvIHJlcG9ydCAiMCBmYWlsZWQiIGFuZCBleGl0IDAuCmlmIG9wdHMucmVwbGF5IGFuZCBvcHRzLnJlcGxheSB+PSAiIiB0aGVuCiAgbG9jYWwgZmlsZXMsIGNhc2VzLCBiYWQgPSAwLCAwLCAwCiAgbG9jYWwgZnVuY3Rpb24gZmFpbChwYXRoLCB3aHkpCiAgICBiYWQgPSBiYWQgKyAxCiAgICBpby5zdGRlcnI6d3JpdGUoIlJFUExBWSBGQUlMICIgLi4gcGF0aCAuLiAiOiAiIC4uIHdoeSAuLiAiXG4iKQogIGVuZAogIGxvY2FsIGQgPSBpby5vcGVuKG9wdHMucmVwbGF5IC4uICIvLiIsICJyIikKICBpZiBub3QgZCB0aGVuCiAgICBpby5zdGRlcnI6d3JpdGUoImx1YSBmdXp6IHJlcGxheTogIiAuLiBvcHRzLnJlcGxheSAuLiAiIGlzIG5vdCBhIGRpcmVjdG9yeTsgbm90aGluZyB3YXMgcmVwbGF5ZWRcbiIpCiAgICBvcy5leGl0KDEpCiAgZW5kCiAgZDpjbG9zZSgpCiAgbG9jYWwgcGF0aHMsIGVyciA9IGxpc3RGaWxlcyhvcHRzLnJlcGxheSwgIiouY2FzZSIpCiAgaWYgbm90IHBhdGhzIHRoZW4KICAgIGlvLnN0ZGVycjp3cml0ZSgibHVhIGZ1enogcmVwbGF5OiAiIC4uIG9wdHMucmVwbGF5IC4uICI6ICIgLi4gZXJyIC4uICI7IG5vdGhpbmcgd2FzIHJlcGxheWVkXG4iKQogICAgb3MuZXhpdCgxKQogIGVuZAogIGZvciBfLCBwYXRoIGluIGlwYWlycyhwYXRocykgZG8KICAgIGZpbGVzID0gZmlsZXMgKyAxCiAgICBsb2NhbCB0ZXh0ID0gcmVhZEZpbGUocGF0aCkKICAgIGxvY2FsIGMgPSB0ZXh0IGFuZCBkZWNvZGVDYXNlKHRleHQpCiAgICBpZiBub3QgYyB0aGVuCiAgICAgIGZhaWwocGF0aCwgInRoZSBzYXZlZCBjYXNlIGRvZXMgbm90IGRlY29kZSBhcyA8a2luZD4gPHVuaXQ+IDx0YWJsZT4gPGNhbm9uaWNhbCBiYXNlNjQ+LCBzbyBubyByZWFkZXIgdG9vayBpdCIpCiAgICBlbHNlCiAgICAgIGNhc2VzID0gY2FzZXMgKyAxCiAgICAgIGxvY2FsIG9rLCBlcnIgPSBwY2FsbChkcml2ZSwgYykKICAgICAgaWYgbm90IG9rIGFuZCBub3QgdHlwZWQoZXJyKSB0aGVuIGZhaWwocGF0aCwgdG9zdHJpbmcoZXJyKSkgZW5kCiAgICBlbmQKICBlbmQKICBpby53cml0ZShzdHJpbmcuZm9ybWF0KCJsdWEgZnV6eiByZXBsYXk6ICVkIGNhc2VzLCAlZCBmYWlsZWRcbiIsIGNhc2VzLCBiYWQpKQogIGlmIGZpbGVzID09IDAgdGhlbgogICAgaW8uc3RkZXJyOndyaXRlKCJsdWEgZnV6eiByZXBsYXk6ICIgLi4gb3B0cy5yZXBsYXkgLi4gIiBob2xkcyBubyBzYXZlZCBjYXNlOyBub3RoaW5nIHdhcyByZXBsYXllZFxuIikKICAgIG9zLmV4aXQoMSkKICBlbmQKICBvcy5leGl0KGJhZCA9PSAwIGFuZCAwIG9yIDEpCmVuZAoKLS0gRlVaWjogdGhlIHRpbWUgYnVkZ2V0IGlzIGFuIGFyZ3VtZW50OyAtLWl0ZXJhdGlvbnMgYm91bmRzIGEgY29udHJvbCBydW4uCmxvY2FsIHNlY29uZHMgPSB0b251bWJlcihvcHRzLnNlY29uZHMpIG9yIDEwCmxvY2FsIGl0ZXJhdGlvbnMgPSB0b251bWJlcihvcHRzLml0ZXJhdGlvbnMpIG9yIDAKbG9jYWwgZGVhZGxpbmUgPSBvcy50aW1lKCkgKyBzZWNvbmRzCmxvY2FsIGNvdW50LCBmYWlscyA9IDAsIDAKd2hpbGUgdHJ1ZSBkbwogIGlmIGl0ZXJhdGlvbnMgPiAwIHRoZW4KICAgIGlmIGNvdW50ID49IGl0ZXJhdGlvbnMgdGhlbiBicmVhayBlbmQKICBlbHNlaWYgb3MudGltZSgpID49IGRlYWRsaW5lIHRoZW4KICAgIGJyZWFrCiAgZW5kCiAgbG9jYWwgdCA9IHRhcmdldHNbcGlja24oI3RhcmdldHMpXQogIGxvY2FsIGtpbmQgPSB0LmtpbmQKICBsb2NhbCBiYXNlID0gc2VlZHNba2luZF1bcGlja24oI3NlZWRzW2tpbmRdKV0KICBsb2NhbCBjID0geyBraW5kID0ga2luZCwgdW5pdCA9IHQudW5pdCwgdGFibGUgPSB0LnRhYmxlLCBieXRlcyA9IG11dGF0ZShiYXNlKSB9CiAgbG9jYWwgb2ssIGVyciA9IHBjYWxsKGRyaXZlLCBjKQogIGNvdW50ID0gY291bnQgKyAxCiAgaWYgbm90IG9rIGFuZCBub3QgdHlwZWQoZXJyKSB0aGVuCiAgICBmYWlscyA9IGZhaWxzICsgMQogICAgbG9jYWwgcGF0aCA9IHNhdmVGaW5kaW5nKGMsIGVycikKICAgIGlvLnN0ZGVycjp3cml0ZSgiRlVaWiBGQUlMICIgLi4gKHBhdGggb3IgIihubyBmaW5kaW5nIGRpcikiKSAuLiAiOiAiIC4uIHRvc3RyaW5nKGVycikgLi4gIlxuIikKICAgIGlmIGZhaWxzID49IDEwIHRoZW4gYnJlYWsgZW5kCiAgZW5kCmVuZAppby53cml0ZShzdHJpbmcuZm9ybWF0KCJsdWEgZnV6ejogdGFyZ2V0cz0lZCBpdGVyYXRpb25zPSVkIGZhaWx1cmVzPSVkIHNlZWQ9JWRcbiIsCiAgI3RhcmdldHMsIGNvdW50LCBmYWlscywgdG9udW1iZXIob3B0cy5zZWVkKSBvciAxKSkKb3MuZXhpdChmYWlscyA9PSAwIGFuZCAwIG9yIDEpCg==

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
	case_file=$$(ls $$nc/findings/*.case 2>/dev/null | sed -n 1p); \
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
# under testdata/lua-nightly-fuzz/replay-controls. The healthy directory must
# replay as its two cases; each broken one must be refused. A case for a unit,
# kind or table no reader takes, a saved file that does not decode (or whose
# base64 has a tail past its padding), an empty directory, a missing one, and a
# find that lists one file of two and fails each used to report "0 failed" and
# exit 0 (F-077, F-078). partial-find replays good/ with
# testdata/lua-nightly-fuzz/partial-find.sh installed as `find`.
LUA_FUZZ_REPLAY_CONTROLS := testdata/lua-nightly-fuzz/replay-controls
LUA_FUZZ_REPLAY_REFUSED  := unbound-unit unsupported-kind unregistered-table unreadable-beside-good malformed-base64
.PHONY: schema-lua-fuzz-replay-controls
schema-lua-fuzz-replay-controls: schema-lua-fuzz-toolchain $(LUA_FUZZ_GENERATED)/.stamp
	@c=$(LUA_FUZZ_REPLAY_CONTROLS); w=build/lua-fuzz-replay-controls; \
	rm -rf $$w && mkdir -p $$w/empty $$w/shim || exit 1; \
	cp testdata/lua-nightly-fuzz/partial-find.sh $$w/shim/find && chmod +x $$w/shim/find || exit 1; \
	replay() { $(LUA_FUZZ_LUA) $(LUA_FUZZ_SCRIPT) --generated $(LUA_FUZZ_GENERATED) --corpus $(LUA_FUZZ_CORPUS) --replay "$$1"; }; \
	if ! replay $$c/good > $$w/good.log 2>&1; then \
		echo "lua replay control failed: the healthy saved cases were refused"; cat $$w/good.log; exit 1; fi; \
	grep -qx 'lua fuzz replay: 2 cases, 0 failed' $$w/good.log || \
		{ echo "lua replay control failed: the healthy directory did not replay as its two cases"; cat $$w/good.log; exit 1; }; \
	echo "lua replay control: the healthy directory replays (2 cases, 0 failed)"; \
	dirs=0; for d in $$c/*/; do dirs=$$((dirs + 1)); done; \
	[ $$dirs -eq 6 ] || { echo "lua replay controls: $$c holds $$dirs directories, the recipe runs good and 5 refused"; exit 1; }; \
	refused=0; \
	for case in $(addprefix $$c/,$(LUA_FUZZ_REPLAY_REFUSED)) $$w/empty $$w/missing partial-find; do \
		name=$$(basename $$case); \
		if [ "$$name" = partial-find ]; then \
			( PATH=$$PWD/$$w/shim:$$PATH; export PATH; replay $$c/good ) > $$w/$$name.log 2>&1; \
		else \
			replay $$case > $$w/$$name.log 2>&1; \
		fi; \
		if [ $$? -eq 0 ]; then \
			echo "lua replay control failed: $$name replayed green"; cat $$w/$$name.log; exit 1; fi; \
		reason=$$(grep -v ' cases, [0-9]* failed$$' $$w/$$name.log | sed -n 1p); \
		[ -n "$$reason" ] || { echo "lua replay control failed: $$name was refused without a reason"; exit 1; }; \
		[ "$$name" != partial-find ] || case "$$reason" in *"find exited 7"*) ;; *) \
			echo "lua replay control failed: partial-find was refused, but not for the failed find: $$reason"; exit 1 ;; esac; \
		echo "lua replay control: $$name is refused ($$reason)"; \
		refused=$$((refused + 1)); \
	done; \
	[ $$refused -eq 8 ] || { echo "lua replay controls: $$refused of 8 ran"; exit 1; }; \
	echo "lua replay controls: 8 of 8 broken replays refused"
