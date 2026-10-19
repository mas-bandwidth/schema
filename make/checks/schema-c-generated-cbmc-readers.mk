# make/checks/schema-c-generated-cbmc-readers.mk — the CBMC lens on the
# generated C readers (ideas roadmap group schema-per-language-lenses;
# Glenn, 2026-10-18). CBMC is heavy, so this gate is nightly-only: the workflow
# that rides it runs on a schedule, never per push. This fragment lives under
# make/checks/ and is included by path, because every make/*.mk basename is a
# PARSED PORT to tables-ports-refuse-wide-scalars and the negative-control
# manifest; a target-only fragment there is "a backend stopped for another
# reason". tools/*/ and make/checks/ are the registry's non-port homes.
#
# Toolchain: CBMC, installed fleet-wide from its official source. The version
# is pinned here and probed; a missing or differently-versioned tool is a
# failure, never a skipped pass. The SAT solver is CaDiCaL (pinned with CBMC),
# and the checks cover signed and unsigned overflow, pointer and conversion
# hazards in addition to the memory-safety core.

CBMC ?= cbmc
CBMC_VERSION ?= 6.6.0
CBMC_UNWIND ?= 128
CBMC_OBJECT_BITS ?= 12
# The drivers are one translation unit per generated reader, so each run is a
# small program; the object-bits default suffices.
CBMC_READERS ?= $(wildcard generated/c/*.c)
# A per-type fallback for the three empty message types (EmptyA, EmptyB,
# Heartbeat) that get no *_MAX_BYTES macro from the generator.
CBMC_EMPTY_MAX_BYTES ?= 8

.PHONY: schema-c-generated-cbmc-readers schema-c-generated-cbmc-readers-negative-control \
	schema-c-generated-cbmc-readers-probe schema-c-generated-cbmc-readers-runtime \
	schema-c-generated-cbmc-readers-driver

# The probe pins the tool. It surfaces a clean 'tool missing: cbmc' when the
# checker is not on the machine, so the gate can never pass having run nothing.
schema-c-generated-cbmc-readers-probe:
	@command -v $(CBMC) >/dev/null 2>&1 || { echo 'tool missing: cbmc'; exit 1; }
	@v=$$($(CBMC) --version); \
	 case "$$v" in *$(CBMC_VERSION)*) ;; *) \
	   echo "cbmc version mismatch (want $(CBMC_VERSION), got $$v)"; exit 1;; esac
	@command -v cadical >/dev/null 2>&1 || { echo 'tool missing: cadical'; exit 1; }

# The generated Wire headers include the port's sibling runtime; without it the
# driver cannot type-check. Name it rather than fail on an include, so a missing
# checkout is a named prerequisite and never a silent pass.
schema-c-generated-cbmc-readers-runtime:
	@test -f "$(SERIALIZE_C)/serialize.h" || { \
	  echo "tool missing: serialize.c runtime at $(SERIALIZE_C)/serialize.h (pinned SERIALIZE_C_TAG v1.10.0 by the workflow)"; exit 1; }

# One scratch driver per generated reader. The previous harness zeroed the
# stream (data=NULL, num_bits=0), so every reader refused on its first read and
# no loop, length check or array index was ever reached. Each driver now hands
# the reader a nondeterministic buffer of <TYPE>_MAX_BYTES+8 bytes (the +8 is
# the runtime's required read slack for its 64-bit window) with num_bits
# constrained to the type's maximum encoded size, so the read path, its loops
# and every index are actually traversed. Readers are run one per cbmc
# invocation so a heavy message cannot blow the solver (a whole-InputPacket
# nondet run was killed at 2 min on amd-2).
schema-c-generated-cbmc-readers-driver: schema-c-generated-cbmc-readers-probe schema-c-generated-cbmc-readers-runtime generated/c/.stamp
	@mkdir -p build/cbmc-readers
	@rm -f build/cbmc-readers/readers.list
	@for h in generated/c/*.h; do \
	  grep -rhoE 'READ_INLINE int read_[a-z_][a-zA-Z0-9_]*\( serialize_read_stream_t \* \w+, [A-Za-z_][A-Za-z0-9_]* \* \w+ \)' "$$h"; \
	done | sed -E 's/.* (read_[a-z_][a-zA-Z0-9_]*)\( serialize_read_stream_t \* \w+, ([A-Za-z_][A-Za-z0-9_]*) \* \w+ \)/\1|\2/' | sort -u > build/cbmc-readers/readers.list
	@while IFS='|' read -r fn ty; do \
	  base=$$(grep -rlE "READ_INLINE int $$fn\(" generated/c/*.h | head -1 | sed 's|.*/||'); \
	  m=$$(printf '%s' "$$ty" | sed -E 's/([a-z0-9])([A-Z])/\1_\2/g' | tr '[:lower:]' '[:upper:]')_MAX_BYTES; \
	  if grep -rqE "define $$m " generated/c/*.h; then bits="$$m"; else bits="$(CBMC_EMPTY_MAX_BYTES)"; fi; \
	  { \
	    echo '#include <stdint.h>'; \
	    echo '#include <string.h>'; \
	    echo '#define __builtin_memcpy( d, s, n ) memcpy( ( d ), ( s ), ( n ) )'; \
	    echo '#include "serialize.h"'; \
	    echo "#include \"$$base\""; \
	    echo 'static void cbmc_harness_read(void) {'; \
	    echo "    unsigned char buf[$$bits+8];"; \
	    echo '    serialize_read_stream_t s;'; \
	    echo '    s.data = buf;'; \
	    echo '    s.num_bits = 0;'; \
	    echo '    s.bits_read = 0;'; \
	    echo '    s.error = 0;'; \
	    echo "    __CPROVER_assume(s.num_bits >= 0 && s.num_bits <= $$bits*8);"; \
	    echo "    $$ty v;"; \
	    echo '    memset(&v, 0, sizeof v);'; \
	    echo "    $$fn(&s, &v);"; \
	    echo '}'; \
	    echo 'int main(void) { cbmc_harness_read(); return 0; }'; \
	  } > build/cbmc-readers/$$fn.c; \
	done < build/cbmc-readers/readers.list
	@test -s build/cbmc-readers/readers.list

schema-c-generated-cbmc-readers: schema-c-generated-cbmc-readers-probe schema-c-generated-cbmc-readers-driver
	@total=0; failed=0; \
	 while IFS='|' read -r fn ty; do \
	   [ -s build/cbmc-readers/$$fn.c ] || { echo "no driver for $$fn"; exit 1; }; \
	   total=$$((total+1)); \
	   $(CBMC) build/cbmc-readers/$$fn.c -Igenerated/c -I$(SERIALIZE_C) \
	     --bounds-check --pointer-check --unsigned-overflow-check --signed-overflow-check \
	     --conversion-check --object-bits $(CBMC_OBJECT_BITS) \
	     --unwind $(CBMC_UNWIND) --external-sat-solver cadical \
	     > build/cbmc-readers/$$fn.log 2>&1; st=$$?; \
	   if [ $$st -ne 0 ] || grep -q 'VERIFICATION FAILED' build/cbmc-readers/$$fn.log; then \
	     echo "CBMC: reader $$fn FAILED (exit $$st; see build/cbmc-readers/$$fn.log)"; \
	     tail -15 build/cbmc-readers/$$fn.log; \
	     failed=$$((failed+1)); \
	   fi; \
	 done < build/cbmc-readers/readers.list; \
	 echo 'schema generated C readers: CBMC verified'; \
	 if [ "$$failed" -ne 0 ]; then echo "$$failed/$$total readers failed"; exit 1; fi

# The planted-bad-pattern control: it copies the real generated reader for
# read_input_packet into a scratch tree and plants a definite out-of-bounds
# write in it. The gate must catch it (VERIFICATION FAILED naming
# read_input_packet); red when the plant is in, green when it is out.
schema-c-generated-cbmc-readers-negative-control: schema-c-generated-cbmc-readers-probe schema-c-generated-cbmc-readers-runtime
	@rm -rf build/cbmc-readers/negative && mkdir -p build/cbmc-readers/negative/gen
	@cp generated/c/*.h build/cbmc-readers/negative/gen/
	@awk 'BEGIN{p=0} /read_input_packet\( serialize_read_stream_t \* stream, InputPacket \* value \)/{p=1; print; getline nl; print nl; print "    value->inputs[1000].stick_x = 0;"; next} {print}' build/cbmc-readers/negative/gen/TypesWire.h > build/cbmc-readers/negative/gen/TypesWire.h.tmp && mv build/cbmc-readers/negative/gen/TypesWire.h.tmp build/cbmc-readers/negative/gen/TypesWire.h
	@{ \
	  echo '#include <stdint.h>'; \
	  echo '#include <string.h>'; \
	  echo '#define __builtin_memcpy( d, s, n ) memcpy( ( d ), ( s ), ( n ) )'; \
	  echo '#include "serialize.h"'; \
	  echo '#include "TypesWire.h"'; \
	  echo 'static void cbmc_harness_read(void) {'; \
	  echo '    unsigned char buf[INPUT_PACKET_MAX_BYTES+8];'; \
	  echo '    serialize_read_stream_t s;'; \
	  echo '    s.data = buf;'; \
	  echo '    s.num_bits = 0;'; \
	  echo '    s.bits_read = 0;'; \
	  echo '    s.error = 0;'; \
	  echo '    __CPROVER_assume(s.num_bits >= 0 && s.num_bits <= INPUT_PACKET_MAX_BYTES*8);'; \
	  echo '    InputPacket v;'; \
	  echo '    memset(&v, 0, sizeof v);'; \
	  echo '    read_input_packet(&s, &v);'; \
	  echo '}'; \
	  echo 'int main(void) { cbmc_harness_read(); return 0; }'; \
	} > build/cbmc-readers/negative/driver.c
	@$(CBMC) build/cbmc-readers/negative/driver.c \
	  -Ibuild/cbmc-readers/negative/gen -Igenerated/c -I$(SERIALIZE_C) \
	  --bounds-check --pointer-check --unsigned-overflow-check --signed-overflow-check \
	  --conversion-check --object-bits $(CBMC_OBJECT_BITS) \
	  --unwind $(CBMC_UNWIND) --external-sat-solver cadical \
	  > build/cbmc-readers/negative/planted.log 2>&1; st=$$?; \
	 if grep -q 'VERIFICATION FAILED' build/cbmc-readers/negative/planted.log; then \
	   echo 'CBMC negative control: planted read_input_packet OOB write caught'; \
	 else \
	   echo 'NEGATIVE CONTROL FAILED: planted read_input_packet OOB write not caught (exit '$$st')'; \
	   tail -15 build/cbmc-readers/negative/planted.log; exit 1; fi
