# make/schema-lua-runs-hostile-corpus.mk — the LUA port runs every input of
# the shared hostile corpus in its unit tier (docs/HOSTILE-CORPUS.md, built by
# schema-shared-hostile-corpus). Each case's verdict, named refusal and decoded
# value digest is the Go engine's, committed in the corpus manifest; the port's
# generated reader is what this runner asks. The runner reads files only: no
# network, no clock, no service.
#
# The tests live in internal/ci/schema_lua_runs_hostile_corpus_test.go. When
# internal/ci/hostilecorpus/corpus is present on disk it is read directly; when
# sprint/schema-shared (1c72760b3) has not yet merged into this port stream, the
# runner materializes the byte-identical 157-case corpus into t.TempDir() from
# the checkout's conformance vectors and the embedded non-conformance inputs.
.PHONY: schema-lua-runs-hostile-corpus
schema-lua-runs-hostile-corpus: toolchain-lua
	@if [ ! -x "$(LUA)" ]; then \
		echo "tool missing: lua (pinned $(LUA_VERSION)) at $(LUA); run make dist-lua"; \
		exit 1; \
	fi
	@echo "lua hostile corpus: every verdict, named reason and decoded-value digest (Lua $(LUA_VERSION))"
	GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 SCHEMA_REQUIRE_CORPUS=1 nice -n 19 go test ./internal/ci \
		-run '^(TestSchemaLuaRunsHostileCorpus|TestSchemaLuaHostileCorpusPlantedReader|TestSchemaLuaHostileCorpusLane)$$' -count=1 -timeout 600s

# THE PLANTED-BAD-PATTERN CONTROL: a wire reader bug goes into a scratch copy of
# the emitted load.lua (the form gate admits the byte past the carried forms),
# and the corpus comparison must go red. Red with the plant in, green with it
# out; the heavy corpus replay stays nightly, this control rides every push.
.PHONY: schema-lua-runs-hostile-corpus-planted
schema-lua-runs-hostile-corpus-planted: toolchain-lua
	@if [ ! -x "$(LUA)" ]; then \
		echo "tool missing: lua (pinned $(LUA_VERSION)) at $(LUA); run make dist-lua"; \
		exit 1; \
	fi
	@echo "lua hostile corpus: planted-reader control (Lua $(LUA_VERSION))"
	GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 SCHEMA_REQUIRE_CORPUS=1 nice -n 19 go test ./internal/ci \
		-run '^TestSchemaLuaHostileCorpusPlantedReader$$' -count=1 -timeout 600s

# The corpus runner is part of the Lua port's normal unit target.
test-lua: schema-lua-runs-hostile-corpus
