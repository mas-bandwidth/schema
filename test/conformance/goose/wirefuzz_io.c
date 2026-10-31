/* wirefuzz_io.c — the wire-fuzz leg's one read of stdin (test/conformance/
 * goose/wirefuzz.goose; test/conformance/README.md, "The wire-fuzz driver").
 *
 * The pipe is a conversation: the harness writes the roster and waits for
 * the reply, then writes one mutant and waits for its reply. The standard
 * library's stdin reads are read_stdin (everything to end of input) and
 * read_line (to a newline), and neither returns at a frame boundary, so the
 * leg reads EXACTLY n bytes through this sidecar (the pinned Goose release's packages guide, section 3, a
 * `wirefuzz.build` naming it) and appends them to a Goose builder. Writes
 * stay Goose's own (write_stdout, flush_stdout). */
#include <stdint.h>
#include <stdio.h>

#include "goose.h"

/* extern "schema_goose_wirefuzz_read" fn read_exact(out: u8[>..]&, n: i64) -> i64;
 * appends up to n bytes of stdin to out and answers how many arrived: fewer
 * than n only at end of input. */
int64_t schema_goose_wirefuzz_read(gs_rref out, int64_t n) {
	char buf[65536];
	int64_t got = 0;
	while (got < n) {
		size_t want = (size_t)(n - got < (int64_t)sizeof buf ? n - got : (int64_t)sizeof buf);
		size_t k = fread(buf, 1, want, stdin);
		if (k == 0) {
			break;
		}
		gs_bld_append(out, buf, (int64_t)k);
		got += (int64_t)k;
	}
	return got;
}
