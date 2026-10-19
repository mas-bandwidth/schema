package darttable

// THE DART MESSAGE FUZZ ARM HONOURS WHAT THE LOAD RETURNS (docs/SPEC-TABLES.md
// §3.3, §4.2): the wire fuzzer's message arm reads one form-2 batch body from
// the mutant and, when the batch load fails without a refusal, reports the
// mutant malformed rather than saving it back as if whole. The reference arm
// (test/conformance/go/messages.go, run with fuzz=true) sets
//
//	rep.malformed = rep.malformed || !ok && !rep.refused
//
// where `ok` is the batch load's return (count >= 0). The Dart arm used to call
// `load(...)` and discard the returned count, computing `ok` from the report's
// malformed and refused flags alone, so a load that failed with neither flag
// set was saved back as if whole. The batch load answers its count, -1 on
// failure (§3.3); this test pins that the Dart fuzz arm captures that return
// and turns it into the malformed flag exactly as the Go reference does.
//
// The test is pure Go over the shipped driver source, so it is red at BASE and
// green with the fix on every machine, toolchain or not.

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestMessageFuzzArmHonoursLoadResultDart(t *testing.T) {
	t.Parallel()

	src, err := os.ReadFile("../../../test/conformance/dart/main.dart")
	if err != nil {
		t.Fatalf("the Dart driver is not readable: %v", err)
	}

	// THE FUZZ CLOSURE. Cut the `_messageRow` fuzz arm body, where the mutant's
	// one body is loaded and answered.
	start := strings.Index(string(src), "MessageFuzz fuzz = (Uint8List wire) {")
	if start < 0 {
		t.Fatal("the Dart driver declares no MessageFuzz fuzz arm (test/conformance/dart/main.dart)")
	}
	end := strings.Index(string(src[start:]), "return MessageCodec(")
	if end < 0 {
		t.Fatal("the Dart driver's fuzz arm never returns its MessageCodec")
	}
	arm := string(src[start : start+end])

	// THE LOAD'S RETURN IS CAPTURED (§3.3: the batch load answers its count,
	// -1 on failure). The arm must read the count the load returns, not call
	// `load` and throw it away.
	if !regexp.MustCompile(`count = load\(values, wire, vocab, rep as R\);`).MatchString(arm) {
		t.Error("the fuzz arm does not capture the batch load's returned count (docs/SPEC-TABLES.md §3.3)")
	}
	if !regexp.MustCompile(`final ok = count >= 0;`).MatchString(arm) {
		t.Error("the fuzz arm does not derive `ok` from the batch load's returned count (docs/SPEC-TABLES.md §3.3)")
	}

	// THE MALFORMED FLAG OWES THE FAILED LOAD (test/conformance/go/messages.go,
	// run with fuzz=true: `rep.malformed = rep.malformed || !ok && !rep.refused`).
	// A load that fails without a refusal is malformed.
	if !regexp.MustCompile(`malformed: \(rep\.malformed as bool\) \|\| !ok && !\(rep\.refused as bool\),`).MatchString(arm) {
		t.Error("the fuzz arm does not mark a load that failed without a refusal as malformed (test/conformance/go/messages.go)")
	}
	if regexp.MustCompile(`final ok = !\(rep\.malformed as bool\) && !\(rep\.refused as bool\);`).MatchString(arm) {
		t.Error("the fuzz arm still computes `ok` from the report's flags alone instead of the batch load's return (test/conformance/go/messages.go)")
	}
}
