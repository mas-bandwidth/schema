package cstable

// EVERY DEAD BYTE IN A FIXED RECORD IS ZERO, AND A NON-ZERO ONE REFUSES THE
// WHOLE LOAD (docs/rulings.json fixed-dead-bytes-are-zero;
// docs/FIXED-FORM-ALGORITHM.md §4.5, §5.3 step 11). The C# fixed reader reads
// a dead range for all-zero bytes and never as a live value: a counted array's
// element slots past its count, an absent optional's payload, a text or bytes
// field's units past its used length, and an inactive union arm.
//
// These are unit tests over the REAL emitted artifact: Generate is run over a
// genuine fixed table and the runtime and plan it produced are inspected. They
// take no interpreter, socket or network. The negative control strips the dead
// checks from that same artifact and requires the checks to go red.

import (
	"strings"
	"testing"
)

// csDeadRuntimeHolds are the emitted runtime's dead-range primitives: the
// DeadZero all-zero check, the Dead opcode and the BoolPresent marker.
var csDeadRuntimeHolds = []string{
	"public static string DeadZero(ReadOnlySpan<byte> src, uint start, uint end)",
	"public const byte Dead = 10;",
	"public const byte BoolPresent = 1;",
}

// csDeadSchemas are the dead-byte shapes: a counted bool array (slots past the
// count), an absent optional bool (its payload), a string (bytes past its used
// length), and a union whose narrow arm leaves its tail dead.
func csDeadSchemas() map[string]string {
	return map[string]string{
		"array": `package probe
fixed table Dead
{
    marks [..3]bool
}
`,
		"optional": `package probe
fixed table Dead
{
    opt ?bool
}
`,
		"text": `package probe
fixed table Dead
{
    label string(8)
}
`,
		"union": `package probe
type W {
    a int32
    b int32
}
union Pick {
    narrow int32
    wide W
}
fixed table Dead
{
    pick Pick
}
`,
	}
}

// csDeadPlanEntries returns the emitted identity-plan entries for the one
// table of a generated unit, as the raw "new TableFixedEntry(...)" rows.
func csDeadPlanEntries(t *testing.T, src string) []string {
	t.Helper()
	start := strings.Index(src, "FixedPlan = new TableFixedPlan")
	if start < 0 {
		t.Fatal("the emitted unit carries no identity plan")
	}
	body := src[start:]
	before, _, ok := strings.Cut(body, "});")
	if !ok {
		t.Fatal("the identity plan is not closed")
	}
	var out []string
	rest := before
	for {
		i := strings.Index(rest, "new TableFixedEntry(")
		if i < 0 {
			break
		}
		rest = rest[i+len("new TableFixedEntry("):]
		j := strings.Index(rest, ")")
		if j < 0 {
			t.Fatal("a plan entry is not closed")
		}
		out = append(out, rest[:j])
		rest = rest[j+1:]
	}
	if len(out) == 0 {
		t.Fatal("the identity plan carries no entries")
	}
	return out
}

// csDeadPlanArg is the n-th (0-based) comma-separated argument of one plan
// entry, trimmed.
func csDeadPlanArg(entry string, n int) string {
	parts := strings.Split(entry, ",")
	if n >= len(parts) {
		return ""
	}
	return strings.TrimSpace(parts[n])
}

// csDeadRuntimeCheck holds the emitted runtime to the dead primitives.
func csDeadRuntimeCheck(src string) error {
	for _, want := range csDeadRuntimeHolds {
		if !strings.Contains(src, want) {
			return errNoSuchText(want)
		}
	}
	if !strings.Contains(src, "case Dead:") {
		return errNoSuchText("case Dead:")
	}
	return nil
}

func errNoSuchText(want string) error {
	return &csMissingText{what: want}
}

type csMissingText struct{ what string }

func (e *csMissingText) Error() string { return "the emitted runtime carries no " + e.what }

// csDeadCountMark holds a counted bool array's count entry to Aux == 1 (the
// bool element's one source byte), which is how the read knows the dead span
// past the count.
func csDeadCountMark(entries []string) error {
	for _, e := range entries {
		if csDeadPlanArg(e, 5) == "TableFixedWire.Count" {
			if csDeadPlanArg(e, 3) != "1u" {
				return errNoSuchText("a count entry with Aux == 1 (the element's byte size)")
			}
			return nil
		}
	}
	return errNoSuchText("a TableFixedWire.Count entry")
}

// csDeadPresentMark holds an optional bool's present byte to Meta == 1 (the
// BoolPresent marker) and Aux == 1 (the payload's one source byte), which is
// how the read knows an absent optional's payload is dead.
func csDeadPresentMark(entries []string) error {
	for _, e := range entries {
		if csDeadPlanArg(e, 5) == "TableFixedWire.BoolByte" && csDeadPlanArg(e, 9) == "1" {
			if csDeadPlanArg(e, 3) != "1u" {
				return errNoSuchText("a present byte with Aux == 1 (the payload's byte size)")
			}
			return nil
		}
	}
	return errNoSuchText("a TableFixedWire.BoolByte entry carrying the present-byte Meta marker")
}

// csDeadUnionMark holds a union to its dead-range entries: a tag-0 dead entry
// (Arg 0) covering the whole arm span, and a narrower arm's tail entry.
func csDeadUnionMark(entries []string) error {
	sawTagZero, sawArmTail := false, false
	for _, e := range entries {
		if csDeadPlanArg(e, 5) != "TableFixedWire.Dead" {
			continue
		}
		if csDeadPlanArg(e, 6) == "0" {
			sawTagZero = true
		}
		if csDeadPlanArg(e, 6) != "0" && csDeadPlanArg(e, 4) != "TableFixedWire.NoGuard" {
			sawArmTail = true
		}
	}
	if !sawTagZero {
		return errNoSuchText("a union dead entry guarded by tag 0")
	}
	if !sawArmTail {
		return errNoSuchText("a union dead entry guarded by a taken arm's ordinal")
	}
	return nil
}

// TestCSharpFixedDeadBytesAreZero holds the emitted reader to the dead-range
// rule: the runtime carries the all-zero check and the plan marks each dead
// region the rule names.
func TestCSharpFixedDeadBytesAreZero(t *testing.T) {
	t.Parallel()
	if err := csDeadRuntimeCheck(tableFixedWireSource); err != nil {
		t.Fatalf("cs-fixed-dead-bytes runtime: %v", err)
	}
	for name, schema := range csDeadSchemas() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			files := generateFiles(t, "Probe", schema)
			var b strings.Builder
			for _, body := range files {
				b.Write(body)
			}
			src := b.String()
			entries := csDeadPlanEntries(t, src)
			var err error
			switch name {
			case "array":
				err = csDeadCountMark(entries)
			case "optional":
				err = csDeadPresentMark(entries)
			case "union":
				err = csDeadUnionMark(entries)
			}
			if err != nil {
				t.Fatalf("cs-fixed-dead-bytes %s: %v", name, err)
			}
		})
	}
}

// TestCSharpFixedDeadBytesAreZeroNegativeControl strips the dead primitives out
// of the real runtime and requires the same checks to go red.
func TestCSharpFixedDeadBytesAreZeroNegativeControl(t *testing.T) {
	t.Parallel()
	if err := csDeadRuntimeCheck(tableFixedWireSource); err != nil {
		t.Fatalf("NEGATIVE CONTROL setup: the real runtime is already red: %v", err)
	}
	sabotaged := tableFixedWireSource
	sabotaged = strings.Replace(sabotaged, "public static string DeadZero(ReadOnlySpan<byte> src, uint start, uint end)", "public static string DeadZeroRemoved(ReadOnlySpan<byte> src, uint start, uint end)", 1)
	if err := csDeadRuntimeCheck(sabotaged); err == nil {
		t.Fatal("NEGATIVE CONTROL FAILED: removing DeadZero left the runtime check green")
	}
}
