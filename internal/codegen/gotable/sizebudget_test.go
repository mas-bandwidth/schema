package gotable

import (
	"bytes"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// The generated Go size budget is measured, not guessed (docs/SPEC-TABLES.md
// §8.4, GO's shared runtime): both counts are the wc -l of every emitted .go
// file with the shared runtime package counted once in the shared total.
// Measured 2026-10-18 at b1cfd0df380a5164fa573ec79f947635d6a23de4. Each
// ceiling is the measured count rounded up to the next multiple of 250 lines
// (a count already on a step is its own ceiling): the test fails when the
// output grows past the ceiling or shrinks 250 lines below it, so a card edits
// a constant only when its measurement crosses a step and adds no comment line
// for it. The history stays in git. Glenn's contraction item targets 2500 and
// 40000 lines. The ceilings rose when the fixed form's refusal vocabulary
// folded into TableRefuseReason (§6.5): each member adds a constant and an
// Error() case to every emitted Go file.
const (
<<<<<<< HEAD
	// +9 lines from this card's DEPRECATED SLOT default image (§3.4), measured at sprint/schema-go ac4dc34.
	// +4: the variable reader names the form byte on its refusal (schema-go-prose-reasons-become-names)
	// +22: a truncated fixed load answers malformed by class and truncated by name (schema-go-fixed-load-truncated-refusal-matches-strict-test, 703169488)
	// -13: the emitted loops range over their counts and the clamps call min/max (modernize-clean generated Go)
	// +2: the open's bounded-work cap and the unsigned-overflow ceiling clamp (schema-shared landing, SPEC-TABLES §3, schema#611)
	// +19: table floats are finite only (SPEC-TABLES §3.3, Glenn 2026-10-27): the NonFiniteFloat report slot, the refusal's two runtime helpers and Err() arm, the cook writer's and the retain capture's raw-bits tests (tables-finite/go)
	// +10: the message reader and the batch open and close are one shape in every unit, so tables/goruntime carries them and a unit aliases them (variable tables carry no message form)
	// +25: damage discards the whole load (SPEC-TABLES §4): the tolerant reads reset the value or hand back no root on malformed, and the message batch delivers no body
	// +2: cycle metadata in the shared type descriptor; directory and wire stay unchanged.
	// +28: TableTextMalformed, TableMalformedField, and (TableReport).Err's malformed-field check live in tables/goruntime so units with and without unbounded text share one TableReport.
	// -4: variable region and retain loads report newer_form on any form byte != 2 (SPEC-TABLES §6.5)
	// +5: a builder load's count past the int32 storage cap names count_over_extent_cap in the report and in (TableReport).Err (SPEC-TABLES §2.9)
	// +223: the one read (SPEC-TABLES §4, §6.5): the wire-alone check over every byte, the node table and the pointer graph, run before every variable-form body read
	// +9: a message body refuses a field id it has carried (SPEC-TABLES §3.3)
	// +4: a flags name twice in a JSON array refuses (SPEC-TABLES §16.2)
	// +44: the fixed pre-scan reads used units only, never the slack past a count or under an absent optional (SPEC-TABLES §3.4)
	// +9: the message save refuses a ranged value or a flags bit its bits would truncate (SPEC-TABLES §3.3, §5)
	// +13: the wire check holds the binary nesting bound (SPEC-TABLES §4, binary_max_depth)
	// +26: the JSON read refuses a repeated decoded key it cannot place (SPEC-TABLES §16.2)
	// +16: a union with a structural arm is a level on the wire check and the retain walk (SPEC-TABLES §4)
	// +61: a file without a node table is checked in the body read's one walk (strict-engine.w5 STEP 5)
	// +2: carry structural depth into the node-free unknown-payload walk (SPEC-TABLES §4)
	// -33: the open runs no separate tableWireVerify prepass; the node table is
	// checked by the region load that carries it (strict-engine.w7)
	// +39: the region and builder loads hold a record of a type the reader
	// cannot name and the decoded graph in the same walk; no tableWireVerify
	// prepass is left (strict-engine.w8)
	// -1: a message body tests a repeated field id with slices.Contains (modernize-clean generated Go)
	// +71: a duplicate key in a skipped JSON object refuses the whole read (SPEC-TABLES §16.2)
	// +3: tableWireSkipFramed checks unknown keyed-array slots and union arms (SPEC-TABLES §4, §6.5)
	// +95: the fixed read lands scalars little-endian-to-native in caller storage (green-conf-go-big-endian-2)
	// +6: a fixed-class root's Load refuses a form-1 file as previous_form (§3.4), the strict read
	// -5: the announcement and the message entries carry the message closure alone (SPEC-TABLES §3.3, #2529)
	// -1161: wide text is retired: kind 33's storage, codecs, JSON and text form are gone (SPEC.md §4.12)
	// +95: the generated JSON reader bounds a long mantissa before strconv (SPEC-TABLES §16.2)
	// +7: a union tag's Max is defined from its last arm, and a 128-bit widen carries the source's sign (measured together at the merged head)
	// -15: a fixed table's plain Save, Measure and Load are the fixed form's
	// (§3.4): the variable form's measure walk and save header are gone at a fixed root, and a root with no file form refuses its save (§3.4)
	// +14: the JSON walk reads a bytes body as canonical base64 only (§16.8): padded, pad bits zero
	// +1: measured over at main 4f02b465e by a hand-landed card, unattributed
	// +16: a schema-name string past 255 bytes refuses in a skipped object and a skipped pointer definition (§16.2)
	// -4: the comment skip holds each comment to the text rule in one bytes.Index pass, in every JSON read entry (SPEC-TABLES §16.2)
	// +71: a wide integer is a JSON string of digits, and every integer takes an integer token (SPEC-TABLES §16.2)
	// +21: a message-form integer is held to this reader's kind domain while it is still wide (SPEC-TABLES §3.3)
	// +59: the same hold reaches every integer kind's high lane, and a reconstruction past 128 bits refuses before it narrows (SPEC-TABLES §3.3)
	goOneTableLineBudget = 10265
	// +369: the strict measure's damage checks in the emitted runtime (schema-go-fuzz-measure-accepts-malformed)
	// +109: the variable reader names the form byte on its refusal (schema-go-prose-reasons-become-names)
	// +22: a truncated fixed load answers malformed by class and truncated by name (schema-go-fixed-load-truncated-refusal-matches-strict-test, 703169488)
	// -31: the emitted loops range over their counts and the clamps call min/max (modernize-clean generated Go)
	// +2: the open's bounded-work cap and the unsigned-overflow ceiling clamp (schema-shared landing, SPEC-TABLES §3, schema#611)
	// +821: table floats are finite only (SPEC-TABLES §3.3, Glenn 2026-10-27): every float read tests its raw bits, every tolerant load and message batch refuses a non-finite one, and every fixed type holding a float gains its finite pass and its writer clause (tables-finite/go)
	// -7200: variable tables carry no message form: no body codecs, region-message or retain-message runtime for them
	// +431: damage discards the whole load (SPEC-TABLES §4): every fixed tolerant read resets its value on malformed, the region, builder and text reads discard, and the message batch delivers no body
	// +257: iterative all-node cycle check and guarded/container edge walk, measured by TestGoGeneratedSizeBudget.
	// +1: align the cooked node extent before placing nested containers.
	// +28: TableTextMalformed, TableMalformedField, and (TableReport).Err's malformed-field check live in tables/goruntime so units with and without unbounded text share one TableReport.
	// -100: variable loads report newer_form on any form byte != 2 (SPEC-TABLES §6.5)
	// +8: a builder load's count past the int32 storage cap names count_over_extent_cap in the report and in (TableReport).Err (SPEC-TABLES §2.9)
	// -5027: the one read: no tolerant twins, no clamp or kind-mismatch counting, the wire-alone check shared in the runtime
	// +2961: a fixed table whose closure holds a union carries the fixed form in a regional unit (RootConfig, LoadoutConfig, ArchiveConfig and their closure: leaves, write bodies, checks, plans)
	// +252: a message body refuses a repeated field id, and an enum-keyed array a repeated slot (SPEC-TABLES §3.2, §3.3)
	// +4: a flags name twice in a JSON array refuses (SPEC-TABLES §16.2)
	// +44: the fixed pre-scan reads used units only, never the slack past a count or under an absent optional (SPEC-TABLES §3.4)
	// +168: the message save refuses a value outside its declaration, and a ranged value or a flags bit its bits would truncate (SPEC-TABLES §3.3, §5)
	// +13: the wire check holds the binary nesting bound (SPEC-TABLES §4, binary_max_depth)
	// +26: the JSON read refuses a repeated decoded key it cannot place (SPEC-TABLES §16.2)
	// +38: a union with a structural arm is a level on the wire check and the retain walk (SPEC-TABLES §4)
	// +1087: a file without a node table is checked in the body read's one walk (strict-engine.w5 STEP 5)
	// +390: structural depth at declared table/array/union entries (SPEC-TABLES §4)
	// -33: the open runs no separate tableWireVerify prepass (strict-engine.w7)
	// +121: the region and builder loads hold a record of a type the reader
	// cannot name and the decoded graph in the same walk; no tableWireVerify
	// prepass is left (strict-engine.w8)
	// -50: a message body tests a repeated field id or keyed slot with slices.Contains and ranges over its count (modernize-clean generated Go)
	// +71: a duplicate key in a skipped JSON object refuses the whole read (SPEC-TABLES §16.2)
	// +226: strict wire-fuzz checks for trailing body bytes, unknown framed slots/arms, and shared blobs (SPEC-TABLES §4, §6.5)
	// +95: the fixed read lands scalars little-endian-to-native in caller storage (green-conf-go-big-endian-2)
	// +231: a message save refuses ill-formed UTF-8 and UTF-16 text (NUL, a stray byte, an unpaired surrogate or a zero unit) the way the fixed writer does (SPEC.md §5.1 row 8, SPEC-TABLES §5)
	// +110: a fixed-class root's Load refuses a form-1 file as previous_form (§3.4), the strict read
	// -29: the announcement and the message entries carry the message closure alone (SPEC-TABLES §3.3, #2529)
	// -161: wide text is retired: kind 33's storage, codecs, JSON and text form are gone (SPEC.md §4.12)
	// +95: the generated JSON reader bounds a long mantissa before strconv (SPEC-TABLES §16.2)
	// +7: a union tag's Max is defined from its last arm, and a 128-bit widen carries the source's sign (measured together at the merged head)
	// +39: the strict read and the text read return the measure's refusal before the builder, and a small text decodes into a stack buffer so a refusal allocates nothing.
	// -153: the same, across every fixed root in the examples (WideBlob has no file form; a fixed Measure checks the value as its Save does)
	// +14: the JSON walk reads a bytes body as canonical base64 only (§16.8): padded, pad bits zero
	// +1: measured over at main 4f02b465e by a hand-landed card, unattributed
	// +16: a schema-name string past 255 bytes refuses in a skipped object and a skipped pointer definition (§16.2)
	// -4: the comment skip holds each comment to the text rule in one bytes.Index pass, in every JSON read entry (SPEC-TABLES §16.2)
	// +71: a wide integer is a JSON string of digits, and every integer takes an integer token (SPEC-TABLES §16.2)
	// +380: the same kind-domain hold across every message-bearing table in the examples (SPEC-TABLES §3.3)
	// +432: the high-lane hold and the 128-bit refusal reconstruction reach every message-bearing table (SPEC-TABLES §3.3)
	goExamplesLineBudget = 60988
=======
	// The ceiling is the measured count rounded up to the next multiple of 250
	// lines: a card edits a constant only when its measurement crosses a step,
	// and adds no comment line for it.
	goOneTableLineBudget = 10250
	goExamplesLineBudget = 60250
>>>>>>> 1aa01b74572cdc9a04d3211970c5a474b759f7dc
)

// TestGoGeneratedSizeBudget pins the size of the Go table emitter's output at
// head (docs/SPEC-TABLES.md §8.4): a one-table fixed unit and every schema file
// under tables/examples as one unit, generated with runtime=shared named
// explicitly so the pin does not move when the default runtime does. The
// reversed witness is the budget check itself: each ceiling is shown to refuse
// one line more than was measured and its floor to refuse 250 lines below, so a
// ceiling that silently stopped biting fails.
//
// The standalone one-table unit is smaller than the shared total: for one table
// the embedded runtime is smaller than the shared package, which is only
// amortised by more tables. Standalone is reported, never pinned here.
func TestGoGeneratedSizeBudget(t *testing.T) {
	t.Parallel()
	oneTable := unitFrom(t, "package probe\nfixed table Pos {\n\tx int32\n\ty int32\n}\n")
	examples := loadUnitPaths(t, "../../..", []string{"tables/examples"})

	oneLines := goGeneratedLines(t, oneTable, RuntimeShared)
	exampleLines := goGeneratedLines(t, examples, RuntimeShared)
	standalone := goGeneratedLines(t, oneTable, RuntimeStandalone)
	t.Logf("one-table %d lines (ceiling %d), standalone %d lines, examples %d lines (ceiling %d)",
		oneLines, goOneTableLineBudget, standalone, exampleLines, goExamplesLineBudget)
	for _, c := range []struct {
		name    string
		lines   int
		ceiling int
	}{
		{"one-table", oneLines, goOneTableLineBudget},
		{"examples", exampleLines, goExamplesLineBudget},
	} {
		if verdict := goGeneratedSizeVerdict(c.lines, c.ceiling); verdict != "" {
			t.Fatalf("generated Go is %s the %s ceiling: measured %d lines, ceiling %d, set the ceiling to %d",
				verdict, c.name, c.lines, c.ceiling, roundUp250(c.lines))
		}
		if goGeneratedSizeVerdict(c.ceiling+1, c.ceiling) != "over" {
			t.Fatalf("the %s ceiling (%d lines) accepts %d lines: the ceiling no longer bites",
				c.name, c.ceiling, c.ceiling+1)
		}
		if goGeneratedSizeVerdict(c.ceiling-250, c.ceiling) != "lower" {
			t.Fatalf("the %s floor (%d lines) accepts %d lines: the floor no longer bites",
				c.name, c.ceiling, c.ceiling-250)
		}
	}
}

// goGeneratedSizeVerdict reports "" when a measured count is on the step for
// its ceiling, "over" when it has grown past the ceiling, and "lower" when it
// has shrunk 250 or more lines below it.
func goGeneratedSizeVerdict(lines, ceiling int) string {
	switch {
	case lines > ceiling:
		return "over"
	case ceiling-lines >= 250:
		return "lower"
	default:
		return ""
	}
}

// roundUp250 is the ceiling a measured count sets: the next multiple of 250,
// or the count itself when it is already on a step.
func roundUp250(lines int) int {
	return (lines + 249) / 250 * 250
}

// TestGoGeneratedSizeCeilingSteps pins the stepped ceiling rule: exactly at the
// ceiling, one line over, one line under, 249 under, 250 under, and a count
// that is an exact multiple of 250.
func TestGoGeneratedSizeCeilingSteps(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name    string
		lines   int
		ceiling int
		want    string
	}{
		{"exactly at the ceiling", 1000, 1000, ""},
		{"one over", 1001, 1000, "over"},
		{"one under", 999, 1000, ""},
		{"249 under", 751, 1000, ""},
		{"250 under", 750, 1000, "lower"},
		{"exact multiple of 250", 750, 750, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := goGeneratedSizeVerdict(c.lines, c.ceiling); got != c.want {
				t.Fatalf("goGeneratedSizeVerdict(%d, %d) = %q, want %q", c.lines, c.ceiling, got, c.want)
			}
		})
	}
}

// goGeneratedLines counts wc -l over the generated .go files of one unit,
// written in a t.TempDir(). The shared runtime package is counted once, and
// only for runtime=shared: a standalone unit carries its own machinery
// embedded and does not use the shared package.
func goGeneratedLines(t *testing.T, u *ir.Unit, rt Runtime) int {
	t.Helper()
	files, err := GenerateRuntime(u, nil, rt)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if rt == RuntimeShared {
		shared, err := SharedRuntimeFiles()
		if err != nil {
			t.Fatalf("shared runtime: %v", err)
		}
		maps.Copy(files, shared)
	}
	dir := t.TempDir()
	lines := 0
	for name, data := range files {
		if !strings.HasSuffix(name, ".go") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		lines += bytes.Count(data, []byte("\n"))
	}
	return lines
}
