package javatable

// TestJavaFixedNoMalformedFlag holds the Java fixed form to
// docs/FIXED-FORM-ALGORITHM.md §5.3's refusal table, read from the generated
// source: framing damage refuses BY NAME and "There is no `malformed` flag on
// this form's report". No bytes, a short header, a header and layout with no
// record and a ragged tail refuse truncated; a nonzero reserved byte refuses
// reservedNotZero; an integrity trailer that does not hold refuses
// integrityMismatch (SPEC-TABLES §2.14); a hash in no lineage entry refuses
// layoutUnknown, and the retired layoutNewer is not a member at all (§5.3 step
// 5, "STEP 5's ANSWER IS layout_unknown"). TableFixed.Report carries no
// `damaged` member, and no fixed-form source writes one.
//
// TestJavaFixedNoMalformedFlagPlants puts each retired answer back into a copy
// of the real generated source and requires the same checks to go red.
//
// A unit test: it generates source and reads it, no JVM, no time, no files.

import (
	"strings"
	"testing"
)

const javaNoMalformedSchema = `package nomal

fixed table Plain
{
    x int32 = 0
    y int32 = 7
}

fixed table Sealed | integrity = "crc32c"
{
    x int32
}
`

// javaNoMalformedSources answers the fixed runtime and the two generated
// fixed classes, keyed runtime, plain, sealed.
func javaNoMalformedSources(t *testing.T) map[string]string {
	t.Helper()
	files, err := Generate(unitFrom(t, javaNoMalformedSchema))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	out := map[string]string{}
	for key, name := range map[string]string{"runtime": "TableFixed.java", "plain": "PlainFixed.java", "sealed": "SealedFixed.java"} {
		b, ok := files[name]
		if !ok {
			t.Fatalf("Generate emitted no %s", name)
		}
		// THE GENERATED JAVA IS FORMATTED, so every check reads it with each
		// whitespace run squeezed to one space.
		out[key] = strings.Join(strings.Fields(string(b)), " ")
	}
	return out
}

// javaBraceBody answers the brace-balanced body that follows needle, or "".
func javaBraceBody(text, needle string) string {
	i := strings.Index(text, needle)
	if i < 0 {
		return ""
	}
	open := strings.Index(text[i:], "{")
	if open < 0 {
		return ""
	}
	i += open
	depth := 0
	for j := i; j < len(text); j++ {
		switch text[j] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return text[i : j+1]
			}
		}
	}
	return ""
}

// javaNoMalformedCheck answers every way the sources break the rule.
func javaNoMalformedCheck(src map[string]string) []string {
	var bad []string
	need := func(ok bool, what string) {
		if !ok {
			bad = append(bad, what)
		}
	}
	report := javaBraceBody(src["runtime"], "public static final class Report")
	need(report != "", "TableFixed.Report is not in the runtime")
	need(!strings.Contains(report, "damaged"), "TableFixed.Report still carries a damaged (malformed) member")
	reasons := javaBraceBody(src["runtime"], "public enum Reason")
	need(reasons != "", "TableFixed.Reason is not in the runtime")
	for _, m := range []string{"truncated", "reservedNotZero", "layoutUnknown", "integrityMismatch"} {
		need(strings.Contains(reasons, " "+m+",") || strings.Contains(reasons, " "+m+" }"), "TableFixed.Reason has no member "+m)
	}
	need(!strings.Contains(reasons, "layoutNewer"), "TableFixed.Reason still has the retired layoutNewer")
	header := javaBraceBody(src["runtime"], "public static boolean readHeader(")
	need(strings.Count(header, "report.refuse(Reason.truncated);") == 2, "readHeader does not refuse truncated for no first byte and for a short header")
	need(strings.Contains(header, "report.refuse(Reason.reservedNotZero);"), "readHeader does not refuse a nonzero reserved byte as reservedNotZero")
	for _, key := range []string{"runtime", "plain", "sealed"} {
		need(!strings.Contains(src[key], ".damaged"), key+": a fixed-form source still writes or reads a damaged flag")
		need(!strings.Contains(src[key], "Reason.layoutNewer"), key+": a fixed-form source still answers layoutNewer")
	}
	for _, key := range []string{"plain", "sealed"} {
		need(strings.Contains(src[key], "rest % recordSize != 0) { report.refuse(TableFixed.Reason.truncated); return -1; }"), key+": a ragged tail does not refuse truncated")
		need(strings.Contains(src[key], "if (pick < 0) { report.refuseHash(TableFixed.Reason.layoutUnknown, fileHash); return -1; }"), key+": a hash in no lineage entry does not refuse layoutUnknown with the file's hash")
	}
	need(strings.Contains(src["sealed"], "if (!TableFixed.integrityHolds(data)) { report.refuse(TableFixed.Reason.integrityMismatch);"), "sealed: an integrity trailer that does not hold does not refuse integrityMismatch")
	return bad
}

func TestJavaFixedNoMalformedFlag(t *testing.T) {
	if bad := javaNoMalformedCheck(javaNoMalformedSources(t)); len(bad) != 0 {
		t.Fatalf("the Java fixed form answers outside §5.3's refusal table:\n%s", strings.Join(bad, "\n"))
	}
}

func TestJavaFixedNoMalformedFlagPlants(t *testing.T) {
	pristine := javaNoMalformedSources(t)
	if bad := javaNoMalformedCheck(pristine); len(bad) != 0 {
		t.Fatalf("the pristine sources already fail:\n%s", strings.Join(bad, "\n"))
	}
	plants := []struct {
		name, key, from, to string
	}{
		{"report-carries-damaged", "runtime",
			"public static final class Report {", "public static final class Report { public boolean damaged;"},
		{"layout-newer-is-back", "runtime", " layoutUnknown,", " layoutNewer, layoutUnknown,"},
		{"empty-input-sets-malformed", "runtime",
			"if (data == null || data.length < 1) { report.refuse(Reason.truncated);",
			"if (data == null || data.length < 1) { report.damaged = true;"},
		{"short-header-sets-malformed", "runtime",
			"if (data.length < fileHeaderBytes + 4) { report.refuse(Reason.truncated);",
			"if (data.length < fileHeaderBytes + 4) { report.damaged = true;"},
		{"reserved-byte-sets-malformed", "runtime", "report.refuse(Reason.reservedNotZero);", "report.damaged = true;"},
		{"ragged-tail-sets-malformed", "plain",
			"rest % recordSize != 0) { report.refuse(TableFixed.Reason.truncated); return -1; }",
			"rest % recordSize != 0) { report.damaged = true; return -1; }"},
		{"unknown-hash-answers-layout-newer", "plain",
			"report.refuseHash(TableFixed.Reason.layoutUnknown, fileHash)", "report.refuseHash(TableFixed.Reason.layoutNewer, fileHash)"},
		{"integrity-sets-malformed", "sealed",
			"report.refuse(TableFixed.Reason.integrityMismatch);", "report.damaged = true;"},
		{"run-loop-sets-malformed", "runtime",
			"report.refuse(Reason.layoutMalformed); return;", "report.damaged = true; return;"},
	}
	for _, p := range plants {
		t.Run(p.name, func(t *testing.T) {
			src := map[string]string{}
			for k, v := range pristine {
				src[k] = v
			}
			planted := strings.Replace(src[p.key], p.from, p.to, 1)
			if planted == src[p.key] {
				t.Fatalf("plant %s changed nothing: its needle is stale", p.name)
			}
			src[p.key] = planted
			if bad := javaNoMalformedCheck(src); len(bad) == 0 {
				t.Fatalf("PLANT %s stayed green", p.name)
			}
		})
	}
}
