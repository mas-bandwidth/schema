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
# and the checks cover signed overflow, division by zero, pointer and bounds
# hazards across the memory-safety core.

CBMC ?= cbmc
CBMC_VERSION ?= 6.6.0
# Unwinding is per reader, because each reader's loops are the arrays and
# strings it decodes (max_count + 1 so --unwinding-assertions proves the loop
# terminates). A reader not named here decodes no array or string longer than
# 64 elements. The inner UTF-8 continuation loop (<= 3 bytes) and the
# word/tail loops of schema_interior_null_ get their exact loop bounds via
# --unwindset so nested loops do not multiply the outer bound.
CBMC_UNWIND ?= 65
CBMC_UNWIND_read_input_packet ?= 18        # MAX_INPUTS_PER_PACKET (16) + 2 (covers +1 negative control)
CBMC_UNWIND_read_render_block ?= 65        # RENDER_BLOCK_MAX_SPRITES (64) + 1
CBMC_UNWIND_read_chat ?= 257               # MAX_CHAT_LENGTH (256) + 1
CBMC_UNWIND_read_test_data ?= 256          # text_length (255) + 1
CBMC_UNWIND_read_block ?= 2                # bulk memcpy (no element loop)
export CBMC_UNWIND_read_input_packet CBMC_UNWIND_read_render_block CBMC_UNWIND_read_chat CBMC_UNWIND_read_test_data CBMC_UNWIND_read_block
CBMC_UNWINDSET ?= schema_utf8_valid_.0:4,schema_interior_null_.0:33,schema_interior_null_.1:8
# CBMC is heavy and can eat a machine: every per-reader run is bounded in wall
# time and address space, and a timeout is a failure, never a silent pass.
CBMC_TIME_LIMIT ?= 900
CBMC_MEM_LIMIT_KB ?= 6000000
CBMC_OBJECT_BITS ?= 12
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

# One scratch driver per generated reader. The stream's num_bits is left
# uninitialised, i.e. nondeterministic in CBMC, and constrained by the type's
# maximum encoded size plus 64 bytes of trailing wire slack so an off-by-one
# loop bound is not masked by EOF (an explicit `num_bits = 0` made every reader
# refuse on its first read and the run was vacuous). The buffer includes +8
# bytes for the runtime's required 64-bit word-read tail slack. Readers are
# run one per cbmc invocation, each with its own unwind bound, time and memory
# cap.
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
	    echo 'static inline void *cbmc_word_copy_(void *dst, const void *src, size_t n) {'; \
	    echo '    __CPROVER_assert(__CPROVER_r_ok(src, n), "memcpy source region readable");'; \
	    echo '    __CPROVER_assert(__CPROVER_w_ok(dst, n), "memcpy destination region writeable");'; \
	    echo '    if (n == 8) { uint64_t u; *(uint64_t *)dst = u; return dst; }'; \
	    echo '    if (n == 4) { float f; *(float *)dst = f; return dst; }'; \
	    echo '    return dst;'; \
	    echo '}'; \
	    echo 'static inline void *cbmc_bulk_copy_(void *dst, const void *src, size_t n) {'; \
	    echo '    __CPROVER_assert(__CPROVER_r_ok(src, n), "memcpy source region readable");'; \
	    echo '    __CPROVER_assert(__CPROVER_w_ok(dst, n), "memcpy destination region writeable");'; \
	    echo '    return dst;'; \
	    echo '}'; \
	    echo '#define __builtin_memcpy( d, s, n ) ( __builtin_constant_p( n ) ? cbmc_word_copy_( ( d ), ( s ), ( n ) ) : cbmc_bulk_copy_( ( d ), ( s ), ( n ) ) )'; \
	    echo '#include "serialize.h"'; \
	    echo '#undef memcpy'; \
	    echo '#define memcpy( d, s, n ) __builtin_memcpy( ( d ), ( s ), ( n ) )'; \
	    echo "#include \"$$base\""; \
	    echo 'static void cbmc_harness_read(void) {'; \
	    echo "    unsigned char buf[($$bits)+64+8];"; \
	    echo '    serialize_read_stream_t s;'; \
	    echo '    s.data = buf;'; \
	    echo '    s.bits_read = 0;'; \
	    echo '    s.error = 0;'; \
	    echo "    __CPROVER_assume(s.num_bits >= 0 && s.num_bits <= (($$bits)+64)*8);"; \
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
	   u=$$(eval echo \$$CBMC_UNWIND_$$fn); [ -n "$$u" ] || u=$(CBMC_UNWIND); \
	   start=$$(date +%s); \
	   ( ulimit -v $(CBMC_MEM_LIMIT_KB); \
	     timeout $(CBMC_TIME_LIMIT) $(CBMC) build/cbmc-readers/$$fn.c \
	       -Igenerated/c -I$(SERIALIZE_C) \
	       --bounds-check --pointer-check \
	       --signed-overflow-check --div-by-zero-check \
	       --object-bits $(CBMC_OBJECT_BITS) \
	       --unwind $$u --unwindset $(CBMC_UNWINDSET) --unwinding-assertions \
	       --slice-formula --external-sat-solver cadical ) \
	     > build/cbmc-readers/$$fn.log 2>&1; st=$$?; \
	   secs=$$(( $$(date +%s) - start )); \
	   echo "CBMC: $$fn unwind=$$u time=$${secs}s exit=$$st"; \
	   if [ $$st -ne 0 ] || grep -q 'VERIFICATION FAILED' build/cbmc-readers/$$fn.log; then \
	     echo "CBMC: reader $$fn FAILED (exit $$st; see build/cbmc-readers/$$fn.log)"; \
	     tail -15 build/cbmc-readers/$$fn.log; \
	     failed=$$((failed+1)); \
	   fi; \
	 done < build/cbmc-readers/readers.list; \
	 echo 'schema generated C readers: CBMC verified'; \
	 if [ "$$failed" -ne 0 ]; then echo "$$failed/$$total readers failed"; exit 1; fi

# The planted-bad-pattern control: it copies the real generated reader for
# read_input_packet into a scratch tree and plants the real bound bug there:
# inputs_count is accepted as MAX_INPUTS_PER_PACKET+1, one past the
# inputs[MAX_INPUTS_PER_PACKET] array. The gate must catch it (VERIFICATION
# FAILED naming read_input_packet); red when the plant is in, green when it is
# out. The plant is in the decode bound, not an unreachable store, so it
# needs a nondeterministic stream with room for the 17th Input to fire.
schema-c-generated-cbmc-readers-negative-control: schema-c-generated-cbmc-readers-probe schema-c-generated-cbmc-readers-runtime
	@rm -rf build/cbmc-readers/negative && mkdir -p build/cbmc-readers/negative/gen
	@cp generated/c/*.h build/cbmc-readers/negative/gen/
	@sed -i 's/&value->inputs_count, 0, MAX_INPUTS_PER_PACKET )/\&value->inputs_count, 0, MAX_INPUTS_PER_PACKET+1 )/' build/cbmc-readers/negative/gen/TypesWire.h
	@grep -q 'inputs_count, 0, MAX_INPUTS_PER_PACKET+1 )' build/cbmc-readers/negative/gen/TypesWire.h || { echo 'NEGATIVE CONTROL FAILED: the inputs_count bound plant did not apply'; exit 1; }
	@{ \
	  echo '#include <stdint.h>'; \
	  echo '#include <string.h>'; \
	  echo 'static inline void *cbmc_word_copy_(void *dst, const void *src, size_t n) {'; \
	  echo '    __CPROVER_assert(__CPROVER_r_ok(src, n), "memcpy source region readable");'; \
	  echo '    __CPROVER_assert(__CPROVER_w_ok(dst, n), "memcpy destination region writeable");'; \
	  echo '    if (n == 8) { uint64_t u; *(uint64_t *)dst = u; return dst; }'; \
	  echo '    if (n == 4) { float f; *(float *)dst = f; return dst; }'; \
	  echo '    return dst;'; \
	  echo '}'; \
	  echo 'static inline void *cbmc_bulk_copy_(void *dst, const void *src, size_t n) {'; \
	  echo '    __CPROVER_assert(__CPROVER_r_ok(src, n), "memcpy source region readable");'; \
	  echo '    __CPROVER_assert(__CPROVER_w_ok(dst, n), "memcpy destination region writeable");'; \
	  echo '    return dst;'; \
	  echo '}'; \
	  echo '#define __builtin_memcpy( d, s, n ) ( __builtin_constant_p( n ) ? cbmc_word_copy_( ( d ), ( s ), ( n ) ) : cbmc_bulk_copy_( ( d ), ( s ), ( n ) ) )'; \
	  echo '#include "serialize.h"'; \
	  echo '#undef memcpy'; \
	  echo '#define memcpy( d, s, n ) __builtin_memcpy( ( d ), ( s ), ( n ) )'; \
	  echo '#include "TypesWire.h"'; \
	  echo 'static void cbmc_harness_read(void) {'; \
	  echo '    unsigned char buf[INPUT_PACKET_MAX_BYTES+64+8];'; \
	  echo '    serialize_read_stream_t s;'; \
	  echo '    s.data = buf;'; \
	  echo '    s.bits_read = 0;'; \
	  echo '    s.error = 0;'; \
	  echo '    __CPROVER_assume(s.num_bits >= 0 && s.num_bits <= (INPUT_PACKET_MAX_BYTES+64)*8);'; \
	  echo '    InputPacket v;'; \
	  echo '    memset(&v, 0, sizeof v);'; \
	  echo '    read_input_packet(&s, &v);'; \
	  echo '}'; \
	  echo 'int main(void) { cbmc_harness_read(); return 0; }'; \
	} > build/cbmc-readers/negative/driver.c
	@( ulimit -v $(CBMC_MEM_LIMIT_KB); \
	   timeout $(CBMC_TIME_LIMIT) $(CBMC) build/cbmc-readers/negative/driver.c \
	     -Ibuild/cbmc-readers/negative/gen -Igenerated/c -I$(SERIALIZE_C) \
	     --bounds-check --pointer-check --signed-overflow-check --div-by-zero-check \
	     --object-bits $(CBMC_OBJECT_BITS) \
	     --unwind $(CBMC_UNWIND_read_input_packet) --unwindset $(CBMC_UNWINDSET) \
	     --unwinding-assertions --slice-formula --external-sat-solver cadical ) \
	  > build/cbmc-readers/negative/planted.log 2>&1; st=$$?; \
	 if grep -q 'VERIFICATION FAILED' build/cbmc-readers/negative/planted.log && \
	    grep -q 'read_input_packet' build/cbmc-readers/negative/planted.log; then \
	   echo 'CBMC negative control: planted inputs_count bound (MAX_INPUTS_PER_PACKET+1) caught in read_input_packet'; \
	 else \
	   echo 'NEGATIVE CONTROL FAILED: planted read_input_packet OOB write not caught (exit '$$st')'; \
	   tail -15 build/cbmc-readers/negative/planted.log; exit 1; fi
