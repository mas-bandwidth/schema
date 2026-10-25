# make/schema-lua-runs-hostile-corpus.mk — the LUA port runs every input of
# the shared hostile corpus in its unit tier (docs/HOSTILE-CORPUS.md, built by
# schema-shared-hostile-corpus). Each case's verdict, named refusal and decoded
# value digest is the Go engine's, committed in the corpus manifest; the port's
# generated reader is what this runner asks. The runner reads files only: no
# network, no clock, no service.
#
# The tests live in internal/ci/schema_lua_runs_hostile_corpus_test.go. The
# corpus is not on this BASE yet (it arrives through
# lua-base-forward-merge-schema-go-r), so SCHEMA_REQUIRE_CORPUS=1 makes an
# absent corpus a named failure rather than a silent pass.
.PHONY: schema-lua-runs-hostile-corpus
schema-lua-runs-hostile-corpus: toolchain-lua
	@if [ ! -x "$(LUA)" ]; then \
		echo "tool missing: lua (pinned $(LUA_VERSION)) at $(LUA); run make dist-lua"; \
		exit 1; \
	fi
	@echo "lua hostile corpus: every verdict, named reason and decoded-value digest (Lua $(LUA_VERSION))"
	GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 SCHEMA_REQUIRE_CORPUS=1 nice -n 19 sh test/slowgate/proof schema-lua-runs-hostile-corpus \
		'TestSchemaLuaRunsHostileCorpus TestSchemaLuaHostileCorpusPlantedReader TestSchemaLuaHostileCorpusLane' \
		./internal/ci -run '^(TestSchemaLuaRunsHostileCorpus|TestSchemaLuaHostileCorpusPlantedReader|TestSchemaLuaHostileCorpusLane)$$' -count=1 -timeout 600s

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
	GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 SCHEMA_REQUIRE_CORPUS=1 nice -n 19 sh test/slowgate/proof schema-lua-runs-hostile-corpus-planted \
		'TestSchemaLuaHostileCorpusPlantedReader' \
		./internal/ci -run '^TestSchemaLuaHostileCorpusPlantedReader$$' -count=1 -timeout 600s

# The corpus runner is part of the Lua port's normal unit target.
test-lua: schema-lua-runs-hostile-corpus
