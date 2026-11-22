# make/name-buildability.mk — the name-buildability nightly (SPEC §4.6,
# File-name safety and Platform-name safety; tools/namebuild).
#
# A unit `schema check` accepts generates code that builds and loads in all
# 18 targets. testdata/name-buildability is the name-shadowing survey's probe
# corpus; the checker holds every row's verdict in the fast lane
# (internal/check, TestNameBuildabilityCorpus), and this target generates
# every row the checker accepts in every target and builds and loads it with
# that target's own toolchain. It runs nightly only
# (.github/workflows/schema-name-buildability.yml): it is minutes of every
# toolchain, and the fast lane's two minutes are law.
#
# The toolchains are this Makefile's own variables, handed to the test under
# the same names; Elixir's interpreter is BEAM_PATH on PATH. The per-target
# summary is written to NAME_BUILDABILITY_SUMMARY and printed whether the run
# passes or fails.
NAME_BUILDABILITY_DIR     ?= build/name-buildability
NAME_BUILDABILITY_SUMMARY ?= $(NAME_BUILDABILITY_DIR)/summary.txt
NAME_BUILDABILITY_PARALLEL ?= 4

.PHONY: name-buildability
name-buildability: dist-lua dist-luau dist-moo dist-squirrel dist-goose dist-lobster
	@mkdir -p $(NAME_BUILDABILITY_DIR)
	@rm -f $(NAME_BUILDABILITY_SUMMARY)
	@status=0; \
	SCHEMA_SLOW=1 PATH="$(BEAM_PATH):$$PATH" NAMEBUILD_SUMMARY="$(abspath $(NAME_BUILDABILITY_SUMMARY))" \
	CC="$(CC)" CXX="$(CXX)" DOTNET="$(DOTNET)" DART="$(DART)" JAVAC="$(JAVAC)" JAVA="$(JAVA)" NODE="$(NODE)" \
	PYTHON="$(PYTHON)" RUBY="$(RUBY)" LUA="$(LUA)" LUAU="$(LUAU)" LUAU_ANALYZE="$(LUAU_ANALYZE)" \
	MOO="$(MOO)" MOO_ANALYZE="$(MOO_ANALYZE)" SQ="$(SQ)" LOBSTER="$(LOBSTER)" GOOSE="$(GOOSE)" \
	SERIALIZE="$(abspath $(SERIALIZE))" SERIALIZE_C="$(abspath $(SERIALIZE_C))" SERIALIZE_CS="$(abspath $(SERIALIZE_CS))" \
	SERIALIZE_GO="$(abspath $(SERIALIZE_GO))" SERIALIZE_RS="$(abspath $(SERIALIZE_RS))" \
	go test -count=1 -timeout=0 -parallel $(NAME_BUILDABILITY_PARALLEL) -run '^TestNameBuildability$$' ./tools/namebuild || status=$$?; \
	if [ -f $(NAME_BUILDABILITY_SUMMARY) ]; then cat $(NAME_BUILDABILITY_SUMMARY); \
	else echo "NAME-BUILDABILITY FAILED: no summary at $(NAME_BUILDABILITY_SUMMARY) (the run stopped before its last line)"; status=1; fi; \
	exit $$status
