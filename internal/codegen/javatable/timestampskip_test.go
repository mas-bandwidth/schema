package javatable

import (
	"regexp"
	"testing"
)

// TestTimestampKindIsSkippedAtEightBytesJava: kind 36, the timestamp, is an
// eight-byte value (docs/SPEC-TABLES.md §3, §2.11), so the Java reader's skip
// of a field it cannot name steps over it at its width, as the Go oracle and
// the harness's frame scanner do (f107c3b7b). A Java skip that called kind 36
// framing damage reported malformed where the oracle did not (cards wire
// fuzz, seed 24845619678, pass kind #109).
func TestTimestampKindIsSkippedAtEightBytesJava(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, `package probe
table Root
{
    value int32 = 0
}
`)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	src := string(files["TableVariable.java"])
	if src == "" {
		t.Fatal("no TableVariable.java emitted")
	}
	skip := regexp.MustCompile(`(?s)public void skip\(int kind\) \{.*?case 28:\s*case 36:\s*if \(has\(8\)\) \{\s*off \+= 8;`)
	if !skip.MatchString(src) {
		t.Error("TableVariable.Reader.skip does not step over kind 36 at eight bytes")
	}
}
