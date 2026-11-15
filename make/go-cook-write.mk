# make/go-cook-write.mk — the Go leg's cook-write surface (docs/SPEC-TABLES.md
# §7.2, §7.6): one gate over the committed cook-write rows, and its control.
# Included by the Makefile's wildcard include; the leg registers itself here.

COOK_WRITE_DIR := build/conformance-go-cook-write
COOK_WRITE_NEG := $(COOK_WRITE_DIR)/negative-control

# The cook-write cell passes only when it answered every case and named none
# absent: `pass N/N` with the same N twice, and no `+Ka` suffix.
COOK_WRITE_PASS_AWK := /^cook-write[ \t]/ { found=1; if ($$0 ~ /\+[0-9]+a/) { absent=1 } if ($$2=="pass") { split($$3,p,"/"); if (p[1]!="" && p[1]==p[2] && p[1] ~ /^[0-9]+$$/) { ok=1 } } } END { exit !(found && ok && !absent) }

.PHONY: conformance-go-cook-write
conformance-go-cook-write: build/conformance-harness build/conformance-go
	@mkdir -p $(COOK_WRITE_DIR)
	@./build/conformance-harness run --only go > $(COOK_WRITE_DIR)/log 2>&1 || true
	@if ! awk '$(COOK_WRITE_PASS_AWK)' $(COOK_WRITE_DIR)/log; then \
		echo "conformance-go-cook-write FAILED: the log's cook-write cell is not a full pass"; \
		grep -E '^cook-write[ \t]|^  go / cook-write:' $(COOK_WRITE_DIR)/log || true; \
		exit 1; \
	fi
	@if grep -qE '^conformance RELAXED \([^)]*\) go / cook-write:' $(COOK_WRITE_DIR)/log; then \
		echo "conformance-go-cook-write FAILED: a go / cook-write case is still relaxed"; \
		grep -E '^conformance RELAXED \([^)]*\) go / cook-write:' $(COOK_WRITE_DIR)/log; \
		exit 1; \
	fi
	@grep -E '^cook-write[ \t]' $(COOK_WRITE_DIR)/log

# THE SABOTAGE: the generated cook writer's dead-slot zeroing is removed in a
# copy of the emitted runtime (guarded by the same zero-fill the fixed writer
# gives a dead counted-array slot), the Go driver is rebuilt against that copy
# under build/ with go build -overlay, and the same verdict the target reads
# must go red. No tracked file is written to.
.PHONY: conformance-go-cook-write-negative-control
conformance-go-cook-write-negative-control: build/conformance-harness build/conformance-go
	@rm -rf $(COOK_WRITE_NEG) && mkdir -p $(COOK_WRITE_NEG)
	@src=$$(grep -l 'func (w \*tableCookWriter) value' build/tables-generated-go/examples/*.go | head -n 1); \
	[ -n "$$src" ] || { echo "NEGATIVE CONTROL FAILED: the generated Go cook runtime was not found"; exit 1; }; \
	sed 's/if !live {/if false {/' "$$src" > $(COOK_WRITE_NEG)/runtime.go; \
	cmp -s "$$src" $(COOK_WRITE_NEG)/runtime.go && { echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; }; \
	printf '{"Replace":{"%s/%s":"%s/%s"}}\n' "$(CURDIR)" "$$src" "$(CURDIR)" "$(COOK_WRITE_NEG)/runtime.go" > $(COOK_WRITE_NEG)/overlay.json
	cd test/conformance/go && go build -overlay $(CURDIR)/$(COOK_WRITE_NEG)/overlay.json -o $(CURDIR)/$(COOK_WRITE_NEG)/driver .
	@printf '#!/bin/sh\nexec %s/%s/driver "$$@"\n' "$(CURDIR)" "$(COOK_WRITE_NEG)" > $(COOK_WRITE_NEG)/driver.sh
	@chmod +x $(COOK_WRITE_NEG)/driver.sh
	@printf 'go %s/%s/driver.sh\n' "$(CURDIR)" "$(COOK_WRITE_NEG)" > $(COOK_WRITE_NEG)/drivers.txt
	@./build/conformance-harness run --drivers $(COOK_WRITE_NEG)/drivers.txt --only go > $(COOK_WRITE_NEG)/log 2>&1 || true
	@if awk '$(COOK_WRITE_PASS_AWK)' $(COOK_WRITE_NEG)/log; then \
		echo "NEGATIVE CONTROL FAILED: the target's cook-write verdict stayed green on the sabotage"; \
		grep -E '^cook-write[ \t]' $(COOK_WRITE_NEG)/log || true; \
		exit 1; \
	fi
	@grep -E '^cook-write[ \t].*FAIL' $(COOK_WRITE_NEG)/log || { \
		echo "NEGATIVE CONTROL FAILED: it went red, but not on the cook-write cell"; \
		cat $(COOK_WRITE_NEG)/log; \
		exit 1; \
	}
	@echo "cook-write negative control: the removed dead-slot zeroing turns the Go cook-write cell red"

test-go: conformance-go-cook-write conformance-go-cook-write-negative-control
