package rusttable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// The three §5.2 facts a cold read of this leg found wrong in the emitted
// runtime, held here as SHAPE: each is one line of the plan compiler or the run
// loop, and the corpus row that would catch it either does not exist yet
// (a signed kind admitting two widths) or needs a forged file to reach (the
// eight-byte tag). The forged TEXT LENGTH has a real fixture beside it, in
// fixedversioning_test.go's `string_grow_forged_length` probe.

// TestFixedTextCapIsTheSpanInUnits: §5.2 TEXT says `span := min(me.size,
// te.size) - 4` and `cap := span / unit`. The READER's own bound is the wrong
// number — under it a length between the writer's bound and the reader's passes
// the clamp, lands as the length, and reads bytes the entry never copied.
func TestFixedTextCapIsTheSpanInUnits(t *testing.T) {
	if !strings.Contains(fixedRuntimeBody, "aux: bytes.checked_div(unit).unwrap_or(0),") {
		t.Error("the text entry's cap is not the SPAN's, in units (§5.2 TEXT)")
	}
	if strings.Contains(fixedRuntimeBody, "aux: me.size.saturating_sub(4).checked_div(unit)") {
		t.Error("the text entry's cap is still the READER's bound (§5.2 TEXT)")
	}
}

// TestFixedConstIsNotAFourByteLane: §5.8 row 5 — no byte lane anywhere, and no
// four-byte one either. A union tag is one, two, four or EIGHT bytes wide, so a
// `const` of a size past four is LAWFUL and not malformed, and the value is
// taken apart out of a 64-bit temporary (a u32 shifted by 32 overflows).
func TestFixedConstIsNotAFourByteLane(t *testing.T) {
	body := fixedRuntimeBody[strings.Index(fixedRuntimeBody, "TableFixedOp::Const => {"):]
	body = body[:strings.Index(body, "\n            }")]
	if strings.Contains(body, "n > 4") {
		t.Error("a lawful eight-byte tag row still reads malformed (§5.8 row 5)")
	}
	if !strings.Contains(body, "n > 8") || !strings.Contains(body, "u64::from(p.aux)") {
		t.Error("the const lane is not full width out of a 64-bit temporary (§5.8 row 5)")
	}
}

// TestFixedSameKindWidenZeroExtends: §5.2 — ladder widen, sign by the WRITER's
// kind; SAME-KIND widen and enum widen, ZERO. Inferring the sign from the kind
// agrees by accident today and is a different rule, which diverges the first day
// a signed kind admits two widths.
func TestFixedSameKindWidenZeroExtends(t *testing.T) {
	const head = "} else if te.size < me.size && me.size <= 8 {"
	at := strings.Index(fixedRuntimeBody, head)
	if at < 0 {
		t.Fatal("the same-kind widen arm moved; this test names it by its condition")
	}
	arm := fixedRuntimeBody[at:]
	arm = arm[:strings.Index(arm, "..TableFixedEntry::default()")]
	if strings.Contains(arm, "signed_kind(") {
		t.Error("the same-kind widen still infers its sign from the kind (§5.2)")
	}
	if !strings.Contains(arm, "sign: 0,") {
		t.Error("the same-kind widen does not ZERO-extend (§5.2)")
	}
	// THE LADDER'S WIDEN IS THE OTHER HALF OF THE SAME RULE and keeps its sign:
	// across the ladder the source sign-extends when THE WRITER's kind is signed.
	if !strings.Contains(fixedRuntimeBody, "sign: u8::from(signed_kind(te.kind)),") {
		t.Error("the LADDER widen lost the writer's sign (§5.2)")
	}
}

// TestFixedIdentityReadForcesNoPlanCompile: §5.9 #20 — the one per-process plan
// build happens AT THE FIRST LOAD THAT SELECTS A NON-IDENTITY ENTRY. A deref of
// the lazy beside the identity test compiles every older plan the first time
// anybody reads their OWN layout, which is the common read. Held by shape: the
// deref lands inside the non-identity branch and never above it.
func TestFixedIdentityReadForcesNoPlanCompile(t *testing.T) {
	u := versionUnit(t, versionSchema(t, "VNEW_field_append"))
	older := versionUnit(t, versionSchema(t, "VOLD_field_append"))
	lineage := map[string][]FixedLineageEntry{}
	for _, st := range ir.TableFixedRoots(older) {
		e, ok := FixedLineageOf(older, st.Name)
		if !ok {
			t.Fatalf("no lineage entry for %s", st.Name)
		}
		lineage[st.Name] = append(lineage[st.Name], e)
	}
	tables, err := GenerateLineage(u, lineage)
	if err != nil {
		t.Fatalf("GenerateLineage: %v", err)
	}
	for name, data := range tables {
		text := string(data)
		at := strings.Index(text, "let identity = found ==")
		if at < 0 {
			continue
		}
		deref := strings.Index(text, "let plans = &*")
		if deref < 0 {
			t.Fatalf("%s: no plan lazy at all", name)
		}
		if deref < at {
			t.Errorf("%s: the plan compiler is forced BEFORE identity is consulted (§5.9 #20)", name)
		}
		if !strings.Contains(text[at:deref], "if identity {") {
			t.Errorf("%s: the lazy is not forced inside the non-identity branch (§5.9 #20)", name)
		}
		return
	}
	t.Fatal("no load function with a lineage of more than one was emitted")
}
