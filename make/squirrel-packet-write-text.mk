# THE PACKET TEXT WRITE CONTRACT (docs/SPEC.md §5.1 row 8): the packet writer
# refuses (false) a string(N) whose used bytes hold a zero byte or ill-formed
# UTF-8 in every build, and writes every well-formed case of the shared list.
build/packet-write-text/squirrel/.stamp: bin/schema test/packet-write-text/Text.schema make/squirrel-packet-write-text.mk
	@rm -rf build/packet-write-text/squirrel
	./bin/schema generate --lang squirrel --out build/packet-write-text/squirrel test/packet-write-text/Text.schema
	@touch $@

.PHONY: packet-write-text-squirrel packet-write-text-squirrel-negative-control
packet-write-text-squirrel: build/packet-write-text/squirrel/.stamp toolchain-squirrel
	"$(SQ)" test/packet-write-text/squirrel/main.nut build/packet-write-text/squirrel > build/packet-write-text/squirrel/stdout 2> build/packet-write-text/squirrel/stderr
	@cat build/packet-write-text/squirrel/stdout
	@if [ -s build/packet-write-text/squirrel/stderr ] || grep -q '^FAILED:' build/packet-write-text/squirrel/stdout; then \
		cat build/packet-write-text/squirrel/stderr >&2; exit 1; fi

# THE WRITE-TEXT CONTROL: drop only the emitted write-side text check and
# require the driver to red the interior zero byte by name.
packet-write-text-squirrel-negative-control: packet-write-text-squirrel
	@rm -rf build/packet-write-text/squirrel-negative
	@mkdir -p build/packet-write-text/squirrel-negative
	go run ./tools/sabotage -name packet-write-text-squirrel-write -out build/packet-write-text/squirrel-negative/write.gotext internal/codegen/squirrel/write.go
	@printf '{"Replace":{"%s/internal/codegen/squirrel/write.go":"%s/build/packet-write-text/squirrel-negative/write.gotext"}}\n' "$(CURDIR)" "$(CURDIR)" > build/packet-write-text/squirrel-negative/overlay.json
	go run -overlay=build/packet-write-text/squirrel-negative/overlay.json ./cmd/schema generate --lang squirrel --out build/packet-write-text/squirrel-negative/gen test/packet-write-text/Text.schema
	"$(SQ)" test/packet-write-text/squirrel/main.nut build/packet-write-text/squirrel-negative/gen > build/packet-write-text/squirrel-negative/log 2>&1 || true
	@grep -Fq 'FAILED: packet write text interior-null writer accepted ill-formed text' build/packet-write-text/squirrel-negative/log || \
		{ echo 'NEGATIVE CONTROL FAILED: Squirrel write text check removal did not red interior-null'; cat build/packet-write-text/squirrel-negative/log; exit 1; }
	@grep -Fm1 'FAILED: packet write text interior-null writer accepted ill-formed text' build/packet-write-text/squirrel-negative/log
	@echo 'packet write text Squirrel negative control: a missing write text check accepts ill-formed text'

test-squirrel: packet-write-text-squirrel packet-write-text-squirrel-negative-control
