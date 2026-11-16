# THE PACKET TEXT WRITE CONTRACT (docs/SPEC.md §5.1 row 8): both tiers refuse
# ill-formed text before any byte rides, and write every well-formed case.
build/packet-write-text/js/.stamp: bin/schema test/packet-write-text/js/Text.schema
	@mkdir -p build/packet-write-text/js
	./bin/schema generate --lang js --out build/packet-write-text/js test/packet-write-text/js/Text.schema
	@printf '{"type":"module"}\n' > build/packet-write-text/js/package.json
	@touch $@

.PHONY: packet-write-text-js packet-write-text-js-negative-control
packet-write-text-js: build/packet-write-text/js/.stamp
	$(NODE) test/packet-write-text/js/main.mjs
	NODE_ENV=production $(NODE) test/packet-write-text/js/main.mjs

packet-write-text-js-negative-control: packet-write-text-js
	@mkdir -p build/packet-write-text/js-negative
	go run ./tools/sabotage -name packet-write-text-js-write \
		-out build/packet-write-text/js-negative/utf8.gotext internal/codegen/js/utf8.go
	@printf '{"Replace":{"%s/internal/codegen/js/utf8.go":"%s/build/packet-write-text/js-negative/utf8.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/packet-write-text/js-negative/overlay.json
	go run -overlay=build/packet-write-text/js-negative/overlay.json ./cmd/schema generate --lang js --out build/packet-write-text/js-negative test/packet-write-text/js/Text.schema
	@printf '{"type":"module"}\n' > build/packet-write-text/js-negative/package.json
	$(NODE) --check build/packet-write-text/js-negative/Text.js
	$(NODE) --check build/packet-write-text/js-negative/TextFlat.js
	@if $(NODE) test/packet-write-text/js/main.mjs "$(CURDIR)/build/packet-write-text/js-negative" > build/packet-write-text/js-negative/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: missing packet text write checks passed in JavaScript'; exit 1; fi
	@grep -Fq 'FAILED: packet write text interior-null runtime writer accepted' build/packet-write-text/js-negative/log || \
		{ echo 'NEGATIVE CONTROL FAILED: JavaScript failed for another reason'; cat build/packet-write-text/js-negative/log; exit 1; }
	@echo 'packet write text JavaScript negative control: removed write checks accept ill-formed text'

test-js: packet-write-text-js packet-write-text-js-negative-control
