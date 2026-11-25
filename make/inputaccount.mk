# The per-leg input account of the retired ci-fast.yml input-family job
# (docs/test-input-accounting.md): each target runs tools/inputaccount
# for one reader leg of one input family exactly as that job did -- the
# plain account (every input of the family on disk opened by the leg),
# then the extra canary and the wrong canary, each of which must go red.
# The job built the tool once and removed the family build tree first;
# so does each target. The make variables a leg was handed are the job's.

INPUTACCOUNT := build/inputaccount/inputaccount

$(INPUTACCOUNT): tools/inputaccount/main.go tools/inputaccount/shard.go tools/inputaccount/watch_linux.go tools/inputaccount/watch_other.go internal/inputfamilies/account.go internal/inputfamilies/inputfamilies.go
	go build -o $@ ./tools/inputaccount

# INPUTACCOUNT_RUN(family, leg, make variables for the leg)
define INPUTACCOUNT_RUN
	rm -rf build/$(1) build/java-finite
	$(INPUTACCOUNT) -family $(1) -leg $(2) $(3)
	$(INPUTACCOUNT) -family $(1) -leg $(2) -canary extra $(3)
	$(INPUTACCOUNT) -family $(1) -leg $(2) -canary wrong $(3)
endef

.PHONY: inputaccount-packet-defaults-cpp-check
inputaccount-packet-defaults-cpp-check: $(INPUTACCOUNT)
	$(call INPUTACCOUNT_RUN,packet-defaults,cpp,)
.PHONY: inputaccount-packet-defaults-c-check
inputaccount-packet-defaults-c-check: $(INPUTACCOUNT)
	$(call INPUTACCOUNT_RUN,packet-defaults,c,)
.PHONY: inputaccount-packet-defaults-go-check
inputaccount-packet-defaults-go-check: $(INPUTACCOUNT)
	$(call INPUTACCOUNT_RUN,packet-defaults,go,)
.PHONY: inputaccount-packet-defaults-rust-check
inputaccount-packet-defaults-rust-check: $(INPUTACCOUNT)
	$(call INPUTACCOUNT_RUN,packet-defaults,rust,RUSTUP_BIN=$$(dirname "$$(command -v cargo)"))
.PHONY: inputaccount-packet-defaults-js-check
inputaccount-packet-defaults-js-check: $(INPUTACCOUNT)
	$(call INPUTACCOUNT_RUN,packet-defaults,js,NODE=node)
.PHONY: inputaccount-packet-defaults-cs-check
inputaccount-packet-defaults-cs-check: $(INPUTACCOUNT)
	$(call INPUTACCOUNT_RUN,packet-defaults,cs,)
.PHONY: inputaccount-packet-defaults-unity-check
inputaccount-packet-defaults-unity-check: $(INPUTACCOUNT)
	$(call INPUTACCOUNT_RUN,packet-defaults,unity,)
.PHONY: inputaccount-packet-defaults-java-check
inputaccount-packet-defaults-java-check: $(INPUTACCOUNT)
	$(call INPUTACCOUNT_RUN,packet-defaults,java,JAVA=java JAVAC=javac)
.PHONY: inputaccount-packet-defaults-dart-check
inputaccount-packet-defaults-dart-check: $(INPUTACCOUNT)
	$(call INPUTACCOUNT_RUN,packet-defaults,dart,DART=dart)
.PHONY: inputaccount-packet-defaults-elixir-check
inputaccount-packet-defaults-elixir-check: $(INPUTACCOUNT)
	$(call INPUTACCOUNT_RUN,packet-defaults,elixir,BEAM_PATH=$$(dirname "$$(command -v erl)"):$$(dirname "$$(command -v elixir)"))
.PHONY: inputaccount-packet-defaults-lua-check
inputaccount-packet-defaults-lua-check: $(INPUTACCOUNT)
	$(call INPUTACCOUNT_RUN,packet-defaults,lua,)
.PHONY: inputaccount-packet-defaults-luau-check
inputaccount-packet-defaults-luau-check: $(INPUTACCOUNT)
	$(call INPUTACCOUNT_RUN,packet-defaults,luau,)
.PHONY: inputaccount-packet-defaults-moo-check
inputaccount-packet-defaults-moo-check: $(INPUTACCOUNT)
	$(call INPUTACCOUNT_RUN,packet-defaults,moo,)
.PHONY: inputaccount-packet-defaults-squirrel-check
inputaccount-packet-defaults-squirrel-check: $(INPUTACCOUNT)
	$(call INPUTACCOUNT_RUN,packet-defaults,squirrel,)
.PHONY: inputaccount-packet-defaults-goose-check
inputaccount-packet-defaults-goose-check: $(INPUTACCOUNT)
	$(call INPUTACCOUNT_RUN,packet-defaults,goose,)
.PHONY: inputaccount-packet-defaults-python-check
inputaccount-packet-defaults-python-check: $(INPUTACCOUNT)
	$(call INPUTACCOUNT_RUN,packet-defaults,python,)
.PHONY: inputaccount-packet-defaults-ruby-check
inputaccount-packet-defaults-ruby-check: $(INPUTACCOUNT)
	$(call INPUTACCOUNT_RUN,packet-defaults,ruby,)

.PHONY: inputaccount-packet-end-cpp-check
inputaccount-packet-end-cpp-check: $(INPUTACCOUNT)
	$(call INPUTACCOUNT_RUN,packet-end,cpp,)
.PHONY: inputaccount-packet-end-c-check
inputaccount-packet-end-c-check: $(INPUTACCOUNT)
	$(call INPUTACCOUNT_RUN,packet-end,c,)
.PHONY: inputaccount-packet-end-rust-check
inputaccount-packet-end-rust-check: $(INPUTACCOUNT)
	$(call INPUTACCOUNT_RUN,packet-end,rust,RUSTUP_BIN=$$(dirname "$$(command -v cargo)"))
.PHONY: inputaccount-packet-end-js-check
inputaccount-packet-end-js-check: $(INPUTACCOUNT)
	$(call INPUTACCOUNT_RUN,packet-end,js,NODE=node)
.PHONY: inputaccount-packet-end-cs-check
inputaccount-packet-end-cs-check: $(INPUTACCOUNT)
	$(call INPUTACCOUNT_RUN,packet-end,cs,)
.PHONY: inputaccount-packet-end-java-check
inputaccount-packet-end-java-check: $(INPUTACCOUNT)
	$(call INPUTACCOUNT_RUN,packet-end,java,JAVA=java JAVAC=javac)
.PHONY: inputaccount-packet-end-dart-check
inputaccount-packet-end-dart-check: $(INPUTACCOUNT)
	$(call INPUTACCOUNT_RUN,packet-end,dart,DART=dart)
.PHONY: inputaccount-packet-end-elixir-check
inputaccount-packet-end-elixir-check: $(INPUTACCOUNT)
	$(call INPUTACCOUNT_RUN,packet-end,elixir,BEAM_PATH=$$(dirname "$$(command -v erl)"):$$(dirname "$$(command -v elixir)"))
.PHONY: inputaccount-packet-end-lua-check
inputaccount-packet-end-lua-check: $(INPUTACCOUNT)
	$(call INPUTACCOUNT_RUN,packet-end,lua,)
.PHONY: inputaccount-packet-end-luau-check
inputaccount-packet-end-luau-check: $(INPUTACCOUNT)
	$(call INPUTACCOUNT_RUN,packet-end,luau,)
.PHONY: inputaccount-packet-end-moo-check
inputaccount-packet-end-moo-check: $(INPUTACCOUNT)
	$(call INPUTACCOUNT_RUN,packet-end,moo,)
.PHONY: inputaccount-packet-end-squirrel-check
inputaccount-packet-end-squirrel-check: $(INPUTACCOUNT)
	$(call INPUTACCOUNT_RUN,packet-end,squirrel,)
.PHONY: inputaccount-packet-end-goose-check
inputaccount-packet-end-goose-check: $(INPUTACCOUNT)
	$(call INPUTACCOUNT_RUN,packet-end,goose,)
.PHONY: inputaccount-packet-end-lobster-check
inputaccount-packet-end-lobster-check: $(INPUTACCOUNT)
	$(call INPUTACCOUNT_RUN,packet-end,lobster,)

.PHONY: inputaccount-packet-finite-go-check
inputaccount-packet-finite-go-check: $(INPUTACCOUNT)
	$(call INPUTACCOUNT_RUN,packet-finite,go,)
.PHONY: inputaccount-packet-finite-java-check
inputaccount-packet-finite-java-check: $(INPUTACCOUNT)
	$(call INPUTACCOUNT_RUN,packet-finite,java,JAVA=java JAVAC=javac)
.PHONY: inputaccount-packet-finite-dart-check
inputaccount-packet-finite-dart-check: $(INPUTACCOUNT)
	$(call INPUTACCOUNT_RUN,packet-finite,dart,DART=dart)
.PHONY: inputaccount-packet-finite-elixir-check
inputaccount-packet-finite-elixir-check: $(INPUTACCOUNT)
	$(call INPUTACCOUNT_RUN,packet-finite,elixir,BEAM_PATH=$$(dirname "$$(command -v erl)"):$$(dirname "$$(command -v elixir)"))
.PHONY: inputaccount-packet-finite-lua-check
inputaccount-packet-finite-lua-check: $(INPUTACCOUNT)
	$(call INPUTACCOUNT_RUN,packet-finite,lua,)
.PHONY: inputaccount-packet-finite-luau-check
inputaccount-packet-finite-luau-check: $(INPUTACCOUNT)
	$(call INPUTACCOUNT_RUN,packet-finite,luau,)
.PHONY: inputaccount-packet-finite-moo-check
inputaccount-packet-finite-moo-check: $(INPUTACCOUNT)
	$(call INPUTACCOUNT_RUN,packet-finite,moo,)
.PHONY: inputaccount-packet-finite-squirrel-check
inputaccount-packet-finite-squirrel-check: $(INPUTACCOUNT)
	$(call INPUTACCOUNT_RUN,packet-finite,squirrel,)
.PHONY: inputaccount-packet-finite-goose-check
inputaccount-packet-finite-goose-check: $(INPUTACCOUNT)
	$(call INPUTACCOUNT_RUN,packet-finite,goose,)
.PHONY: inputaccount-packet-finite-python-check
inputaccount-packet-finite-python-check: $(INPUTACCOUNT)
	$(call INPUTACCOUNT_RUN,packet-finite,python,)
.PHONY: inputaccount-packet-finite-ruby-check
inputaccount-packet-finite-ruby-check: $(INPUTACCOUNT)
	$(call INPUTACCOUNT_RUN,packet-finite,ruby,)

.PHONY: inputaccount-packet-void-cpp-check
inputaccount-packet-void-cpp-check: $(INPUTACCOUNT)
	$(call INPUTACCOUNT_RUN,packet-void,cpp,)
.PHONY: inputaccount-packet-void-c-check
inputaccount-packet-void-c-check: $(INPUTACCOUNT)
	$(call INPUTACCOUNT_RUN,packet-void,c,)
.PHONY: inputaccount-packet-void-python-check
inputaccount-packet-void-python-check: $(INPUTACCOUNT)
	$(call INPUTACCOUNT_RUN,packet-void,python,)
.PHONY: inputaccount-packet-void-ruby-check
inputaccount-packet-void-ruby-check: $(INPUTACCOUNT)
	$(call INPUTACCOUNT_RUN,packet-void,ruby,)
