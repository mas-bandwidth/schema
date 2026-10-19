# make/schema-c-generated-cbmc-readers.mk — the CBMC lens on the generated C
# readers (ideas roadmap group schema-per-language-lenses; Glenn, 2026-10-18).
# CBMC is heavy, so this gate is nightly-only: the workflow that rides it runs
# on a schedule, never per push. Included by the Makefile's wildcard include;
# no Makefile edit is needed.
#
# Toolchain: CBMC, installed fleet-wide from its official source. The version
# is pinned here and probed; a missing or differently-versioned tool is a
# failure, never a skipped pass.

CBMC ?= cbmc
CBMC_VERSION ?= 6.6.0
CBMC_UNWIND ?= 8
CBMC_READERS ?= $(wildcard generated/c/*.c)

.PHONY: schema-c-generated-cbmc-readers schema-c-generated-cbmc-readers-negative-control \
	schema-c-generated-cbmc-readers-probe schema-c-generated-cbmc-readers-driver

# The probe pins the tool. It surfaces a clean 'tool missing: cbmc' when the
# checker is not on the machine, so the gate can never pass having run nothing.
schema-c-generated-cbmc-readers-probe:
	@command -v $(CBMC) >/dev/null 2>&1 || { echo 'tool missing: cbmc'; exit 1; }
	@v=$$($(CBMC) --version); \
	 case "$$v" in *$(CBMC_VERSION)*) ;; *) \
	   echo "cbmc version mismatch (want $(CBMC_VERSION), got $$v)"; exit 1;; esac

# A driver that gives CBMC an entry point per generated reader. It includes
# the generated headers (so the reader bodies and their message types are in
# one translation unit) and hands each reader a zeroed, correctly-typed value
# rather than NULL: the readers write through the value pointer, so passing
# NULL would make CBMC's pointer check fail every reader. Only stream readers
# of the form `read_X(serialize_read_stream_t *s, T *v)` are harnessed;
# non-stream helpers (e.g. schema_interior_null_) are not readers and are
# left out. The port's serialize C runtime supplies the stream type and the
# serialize_read_* primitives; without the sibling checkout this target fails
# by name.
schema-c-generated-cbmc-readers-driver: schema-c-generated-cbmc-readers-probe generated/c/.stamp
	@mkdir -p build/cbmc-readers
	@printf '#include <stdint.h>\n#include <string.h>\n' > build/cbmc-readers/driver.c
	@grep -rlE 'SCHEMA_C_READ_INLINE' generated/c/*.h | sort | \
	  sed 's|.*/||; s|^|#include "|; s|$$|"|' >> build/cbmc-readers/driver.c
	@printf '\n#include "serialize.h"\n' >> build/cbmc-readers/driver.c
	@grep -rhoE 'READ_INLINE int read_[a-z_][a-zA-Z0-9_]*\( serialize_read_stream_t \* \w+, [A-Za-z_][A-Za-z0-9_]* \* \w+ \)' generated/c/*.h | sort -u | \
	  sed -E 's/.* (read_[a-z_][a-zA-Z0-9_]*)\( serialize_read_stream_t \* \w+, ([A-Za-z_][A-Za-z0-9_]*) \* \w+ \)/\1|\2/' | \
	  while IFS='|' read -r fn ty; do \
	    printf '%s\n' "static void cbmc_harness_$$fn(void) { serialize_read_stream_t s; memset(&s, 0, sizeof s); $$ty v; memset(&v, 0, sizeof v); $$fn(&s, &v); }" >> build/cbmc-readers/driver.c; \
	  done
	@printf '%s\n' 'int main(void) {' >> build/cbmc-readers/driver.c
	@grep -hoE 'cbmc_harness_(read_[a-z_][a-zA-Z0-9_]*)' build/cbmc-readers/driver.c | sort -u | \
	  sed 's/.*/    &();/' >> build/cbmc-readers/driver.c
	@printf '%s\n' '    return 0;' '}' >> build/cbmc-readers/driver.c

schema-c-generated-cbmc-readers: schema-c-generated-cbmc-readers-probe schema-c-generated-cbmc-readers-driver
	@if [ ! -s build/cbmc-readers/driver.c ]; then echo 'no generated C readers found'; exit 1; fi
	@$(CBMC) build/cbmc-readers/driver.c -Igenerated/c -I$(SERIALIZE_C) \
	  --bounds-check --pointer-check --unsigned-overflow-check \
	  --unwind $(CBMC_UNWIND) > build/cbmc-readers/out.log 2>&1; st=$$?; \
	 if [ $$st -ne 0 ] || grep -q 'VERIFICATION FAILED' build/cbmc-readers/out.log; then \
	   echo 'CBMC: the generated C readers FAILED (exit '$$st'; see build/cbmc-readers/out.log)'; \
	   tail -20 build/cbmc-readers/out.log; \
	   exit 1; \
	 fi
	@echo 'schema generated C readers: CBMC verification passed (unwind $(CBMC_UNWIND))'

# The planted-bad-pattern control: a scratch copy of a generated reader with a
# read sabotaged to index past its buffer, which the checker must catch so the
# gate fires. Red when the plant is in, green when it is out.
schema-c-generated-cbmc-readers-negative-control: schema-c-generated-cbmc-readers-probe
	@rm -rf build/cbmc-readers/negative && mkdir -p build/cbmc-readers/negative
	@printf '%s\n' \
	  'int planted_oob_reader(const unsigned char *p) { return p[64] + p[65]; }' \
	  'int main(void) { unsigned char b[8]; return planted_oob_reader(b); }' \
	  > build/cbmc-readers/negative/planted.c
	@$(CBMC) build/cbmc-readers/negative/planted.c --pointer-check --unwind $(CBMC_UNWIND) \
	  2>&1 | tee build/cbmc-readers/negative/planted.log
	@if grep -q 'VERIFICATION FAILED' build/cbmc-readers/negative/planted.log; then \
	  echo 'CBMC negative control: planted OOB read caught'; \
	else \
	  echo 'NEGATIVE CONTROL FAILED: planted OOB read not caught'; exit 1; fi
