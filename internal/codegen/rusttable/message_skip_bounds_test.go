package rusttable

import (
	"strings"
	"testing"
)

// TestMessageSkipFramedLengthCannotOverflowTheCursor holds the message
// runtime's skip of a framed kind and of a union arm (message_runtime.rs,
// emitted for every unit with a fixed root) to a length check that cannot
// overflow. `*at + (n as usize) > bytes.len()` adds a length read from the
// wire to the cursor: a LEB128 near u64::MAX wraps it, a debug build panics
// "attempt to add with overflow" and a release build accepts the frame and
// moves the cursor past the end. The nightly fuzzer found it through
// announce_read (test/rust-nightly-fuzz-corpus/message-crash-skip-union-len).
// The check compares the length against what is left, which read_leb keeps
// non-negative (it never moves the cursor past bytes.len()).
func TestMessageSkipFramedLengthCannotOverflowTheCursor(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, `package probe

fixed table Root
{
    value uint32
}
`)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	code := string(files[MessageRuntimeModule+".rs"])
	if code == "" {
		t.Fatalf("a unit with a fixed root emitted no %s.rs", MessageRuntimeModule)
	}
	if strings.Contains(code, "*at + (n as usize) > bytes.len()") {
		t.Error("the message skip adds a wire length to the cursor before comparing it: a huge length overflows")
	}
	// Each site is held by its own context, so one guard cannot stand in for
	// the other: the framed kinds and the union arm after its kind byte.
	const guard = "            if !ok || n > (bytes.len() - *at) as u64 {\n"
	for site, line := range map[string]string{
		"framed kinds": "        12 | 13 | 14 | 16 | 31 | 32 | 33 => {\n            let (n, ok) = read_leb(bytes, at);\n" + guard,
		"union arm":    "            *at += 1;\n            let (n, ok) = read_leb(bytes, at);\n" + guard,
	} {
		if !strings.Contains(code, line) {
			t.Errorf("message skip: the %s length check does not compare against the bytes left", site)
		}
	}
}
