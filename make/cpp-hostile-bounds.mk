# THE C++ RESOURCE-BOUNDS TEST (test/cpp-hostile/bounds/): hostile inputs whose
# verdict is easy and whose cost was not, read by tables/scalars' generated
# reader on a 512 KiB stack, with work held by operation counts (memset bytes,
# memcmp calls, through the linker's --wrap) and never by a clock. The foreign
# build it reads is test/cpp-hostile/bounds/Probe.schema, whose own writer
# makes the announcement and the batch.
#
# THE FRAME CHECK beside it compiles the same reader at -O0 with -fstack-usage
# and holds every frame of the recursive walks (the pre-strict skip walk, the
# message skip walk and the message body reads) to CPP_BOUNDS_FRAME_BYTES: a walk
# bounded at 128 levels is bounded in bytes only when each level is small, and
# an 8 KiB array on one frame made 128 levels a megabyte.
#
# --wrap is GNU ld's and lld's, so the target runs on the Linux runners.
CPP_BOUNDS_BUILD := build/cpp-hostile-bounds
CPP_BOUNDS_SRC   := test/cpp-hostile/bounds
CPP_BOUNDS_FRAME_BYTES ?= 2048
CPP_BOUNDS_CXXFLAGS := -std=c++17 -Wall -Wextra -Werror -ffp-contract=off -pthread -I$(SERIALIZE)
# GCC on x86 clears a large local inline (rep stos), where the memset count
# cannot see it; its libcall strategy makes every clear a call. Clang already
# lowers a large clear to memset and has no such flag.
CPP_BOUNDS_LIBCALL := $(shell $(CXX) -mstringop-strategy=libcall -E -x c++ /dev/null >/dev/null 2>&1 && echo -mstringop-strategy=libcall)

$(CPP_BOUNDS_BUILD)/.stamp: bin/schema make/cpp-hostile-bounds.mk $(CPP_BOUNDS_SRC)/Probe.schema $(wildcard tables/scalars/*.schema)
	@rm -rf $(CPP_BOUNDS_BUILD) && mkdir -p $(CPP_BOUNDS_BUILD)/scalars $(CPP_BOUNDS_BUILD)/probe
	./bin/schema generate --lang cpp --out $(CPP_BOUNDS_BUILD)/scalars tables/scalars >/dev/null
	./bin/schema generate --lang cpp --out $(CPP_BOUNDS_BUILD)/probe $(CPP_BOUNDS_SRC)/Probe.schema >/dev/null
	@touch $@

$(CPP_BOUNDS_BUILD)/writer: $(CPP_BOUNDS_BUILD)/.stamp $(CPP_BOUNDS_SRC)/writer.cpp
	$(CXX) $(CPP_BOUNDS_CXXFLAGS) -O2 -I$(CPP_BOUNDS_BUILD)/probe $(CPP_BOUNDS_SRC)/writer.cpp $(wildcard $(CPP_BOUNDS_BUILD)/probe/*.cpp) -o $@

# THE READER RUNS AT -O0, where no sibling call is taken: g++ -O2 turns the
# pre-strict walk's union-in-union recursion into a loop and hides exactly the
# unbounded depth a clang or a debug build overflows on. One -O0 compile gives
# both the binary and the stack-usage file the frame check reads.
$(CPP_BOUNDS_BUILD)/bounds.o: $(CPP_BOUNDS_BUILD)/.stamp $(CPP_BOUNDS_SRC)/bounds.cpp
	$(CXX) $(CPP_BOUNDS_CXXFLAGS) $(CPP_BOUNDS_LIBCALL) -O0 -fstack-usage -I$(CPP_BOUNDS_BUILD)/scalars -c $(CPP_BOUNDS_SRC)/bounds.cpp -o $(CPP_BOUNDS_BUILD)/bounds.o

$(CPP_BOUNDS_BUILD)/bounds.su: $(CPP_BOUNDS_BUILD)/bounds.o
	@test -s $@

$(CPP_BOUNDS_BUILD)/bounds: $(CPP_BOUNDS_BUILD)/bounds.o
	$(CXX) $(CPP_BOUNDS_CXXFLAGS) $(CPP_BOUNDS_LIBCALL) -O0 -I$(CPP_BOUNDS_BUILD)/scalars $(CPP_BOUNDS_BUILD)/bounds.o $(wildcard $(CPP_BOUNDS_BUILD)/scalars/*.cpp) \
		-Wl,--wrap=memset -Wl,--wrap=memcmp -o $@

.PHONY: cpp-hostile-bounds cpp-hostile-bounds-frames
cpp-hostile-bounds: $(CPP_BOUNDS_BUILD)/writer $(CPP_BOUNDS_BUILD)/bounds cpp-hostile-bounds-frames
	./$(CPP_BOUNDS_BUILD)/writer $(CPP_BOUNDS_BUILD)
	./$(CPP_BOUNDS_BUILD)/bounds $(CPP_BOUNDS_BUILD)

# Each walk function's frame, by name, from the -O0 stack-usage file: a name
# the file does not carry at all is a failure too, so a rename cannot leave the
# check holding nothing.
cpp-hostile-bounds-frames: $(CPP_BOUNDS_BUILD)/bounds.su
	@status=0; \
	for fn in badElementKindContent badElementKindUnion badElementKindTable TableMessageSkipBody 'TableMessageSkip(' TableMessageSkipElement SimStateLoadMessageBody; do \
		lines=$$(grep -F "$$fn" $< || true); \
		if [ -z "$$lines" ]; then echo "FAIL frame $$fn: not in $<"; status=1; continue; fi; \
		worst=$$(printf '%s\n' "$$lines" | awk -F'\t' '{ if ( $$2 + 0 > m ) m = $$2 + 0 } END { print m + 0 }'); \
		if [ "$$worst" -gt $(CPP_BOUNDS_FRAME_BYTES) ]; then echo "FAIL frame $$fn: $$worst bytes, over $(CPP_BOUNDS_FRAME_BYTES)"; status=1; \
		else echo "PASS frame $$fn: $$worst bytes"; fi; \
	done; exit $$status

test: cpp-hostile-bounds
_verify-cpp-bench: cpp-hostile-bounds
