# make/name-buildability.mk — the name-buildability nightly (SPEC §4.6,
# File-name safety and Platform-name safety; tools/namebuild).
#
# A unit `schema check` accepts generates code that builds and loads in every
# target. testdata/name-buildability is the name-shadowing survey's probe
# corpus; the checker holds every row's verdict in the fast lane
# (internal/check, TestNameBuildabilityCorpus), and these targets generate
# every row the checker accepts in a target and build and load it with that
# target's own toolchain. They run nightly only: it is minutes of every
# toolchain, and the fast lane's two minutes are law.
#
# Each target is one Go test, TestNameBuildability_<target>, with a subtest
# per row; each subtest is a registry instance,
# <lang>.nightly.codegen.name-buildability.<case>, which schema-test runs
# through name-buildability-run (tools/schema-test/namebuild.go).
#
# The toolchains are this Makefile's own variables, handed to the tests under
# the same names; Elixir's interpreter is BEAM_PATH on PATH.
NAME_BUILDABILITY_DIR     ?= build/name-buildability
NAME_BUILDABILITY_SUMMARY ?= $(NAME_BUILDABILITY_DIR)/summary.txt
NAME_BUILDABILITY_PARALLEL ?= 4

NAME_BUILDABILITY_ENV = PATH="$(BEAM_PATH):$$PATH" \
	CC="$(CC)" CXX="$(CXX)" DOTNET="$(DOTNET)" DART="$(DART)" JAVAC="$(JAVAC)" JAVA="$(JAVA)" NODE="$(NODE)" \
	PYTHON="$(PYTHON)" RUBY="$(RUBY)" LUA="$(LUA)" LUAU="$(LUAU)" LUAU_ANALYZE="$(LUAU_ANALYZE)" \
	MOO="$(MOO)" MOO_ANALYZE="$(MOO_ANALYZE)" SQ="$(SQ)" LOBSTER="$(LOBSTER)" GOOSE="$(GOOSE)" \
	SERIALIZE="$(abspath $(SERIALIZE))" SERIALIZE_C="$(abspath $(SERIALIZE_C))" SERIALIZE_CS="$(abspath $(SERIALIZE_CS))" \
	SERIALIZE_GO="$(abspath $(SERIALIZE_GO))" SERIALIZE_RS="$(abspath $(SERIALIZE_RS))"

NAME_BUILDABILITY_DEPS = dist-lua dist-luau dist-moo dist-squirrel dist-goose dist-lobster

# name-buildability: every target's test, and the per-target summary, printed
# whether the run passes or fails.
.PHONY: name-buildability
name-buildability: $(NAME_BUILDABILITY_DEPS)
	@mkdir -p $(NAME_BUILDABILITY_DIR)
	@rm -f $(NAME_BUILDABILITY_SUMMARY)
	@status=0; \
	$(NAME_BUILDABILITY_ENV) NAMEBUILD_SUMMARY="$(abspath $(NAME_BUILDABILITY_SUMMARY))" \
	go test -count=1 -timeout=0 -tags namebuild_targets -parallel $(NAME_BUILDABILITY_PARALLEL) -run '^TestNameBuildability_' ./tools/namebuild || status=$$?; \
	if [ -f $(NAME_BUILDABILITY_SUMMARY) ]; then cat $(NAME_BUILDABILITY_SUMMARY); \
	else echo "NAME-BUILDABILITY FAILED: no summary at $(NAME_BUILDABILITY_SUMMARY) (the run stopped before its last line)"; status=1; fi; \
	exit $$status

# name-buildability-deps: the toolchains the tests run, installed. schema-test
# runs it once before any language's name-buildability-run.
.PHONY: name-buildability-deps
name-buildability-deps: $(NAME_BUILDABILITY_DEPS)

# name-buildability-run: the subtests the go test -run pattern in
# NAME_BUILDABILITY_RUN names, as go test -json events written to the file
# NAME_BUILDABILITY_JSON (both from the environment). It exits as go test
# does: non-zero when a subtest fails, and the events say which. It installs
# no toolchain: the dist targets rebuild on every call, so languages run side
# by side would install the same one at once; name-buildability-deps comes
# first, once (schema-test's builder runs it).
.PHONY: name-buildability-run
name-buildability-run:
	@if [ -z "$$NAME_BUILDABILITY_RUN" ] || [ -z "$$NAME_BUILDABILITY_JSON" ]; then \
		echo "name-buildability-run: NAME_BUILDABILITY_RUN (a go test -run pattern) and NAME_BUILDABILITY_JSON (the events file) are both required" >&2; exit 2; \
	fi
	@$(NAME_BUILDABILITY_ENV) \
	go test -count=1 -timeout=0 -tags namebuild_targets -json -parallel $(NAME_BUILDABILITY_PARALLEL) -run "$$NAME_BUILDABILITY_RUN" ./tools/namebuild >"$$NAME_BUILDABILITY_JSON"
