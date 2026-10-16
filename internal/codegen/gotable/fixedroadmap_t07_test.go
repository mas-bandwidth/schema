package gotable

// THE ROADMAP'S LIVE-EXTENTS AND WRITING ROWS ON THE GO LEG
// (docs/roadmap.sexp node `fixed-tables`, work-sets `live-extents/go` and
// `writing/go`; ROADMAP.md "NEW Fixed Tables"). Law: docs/FIXED-FORM-ALGORITHM.md
// §3.1 (the write), §4.3 (the prefill), §4.6 (the bounds pass) and
// docs/SPEC-TABLES.md §3.4. One subtest per task id, table-driven, t.Parallel()
// first, and the assertion under each is the clause the task's own title states,
// read off the Go this leg emits or off the package runtime it emits into that
// unit. NOTHING HERE SHELLS OUT, so every subtest RUNS under a bare
// `go test ./internal/codegen/gotable/ -run TestFixedRoadmapGoT07Writes` and
// never through slowtest.Gate.

import (
	"strconv"
	"strings"
	"testing"
)

// the runtime's text flavours (fixedruntime.go TableFixedRuntime): utf8 is 1,
// bytes is 3.
const (
	t07TextUtf8  = 1
	t07TextBytes = 3
)

// t07GoSource is the one fixture the emitted-source subtests share: a fixed
// root carrying every construct the writing rows name — text, a counted array,
// a bytes(N), an optional, a union on a narrow and a wide arm, an enum and a
// ranged scalar.
const t07GoSource = `package probe

union Pick
{
    a int32
    b int64
}
enum Grade { Bronze, Gold }
fixed table Root {
    note  string(12)
    marks [..4]int32 | min = 0, max = 10
    blob  bytes(6)
    opt   ?int64
    onote ?int32 | min = 0, max = 10
    u     Pick
    g     Grade
    n     int32 | min = 0, max = 10
}
`

func TestFixedRoadmapGoT07Writes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"go/C14", t07GoC14},
		{"go/W4", t07GoW4},
		{"go/W9", t07GoW9},
		{"go/W1", t07GoW1},
		{"go/W3", t07GoW3},
		{"go/W5", t07GoW5},
		{"go/W8", t07GoW8},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// t07Emitted is the emitted Table.go for the shared fixture.
func t07Emitted(t *testing.T) string {
	t.Helper()
	return tableFile(generate(t, t07GoSource))
}

// ---- go/C14 -----------------------------------------------------------------

// t07GoC14 holds C14, "bounds pass walks live only", to the emitted pass:
// §4.6 "It walks only what a read can have written — a counted array's LIVE
// elements and never its slack, an optional's payload only when the present
// byte says so". The clamp body iterates the counted array's own count word,
// gates the optional on its present byte, and never walks the declared bound.
func t07GoC14(t *testing.T) {
	body := t07Emitted(t)
	clamp := funcSource(body, "func RootFixedClampBody(")
	if clamp == "" {
		t.Fatal("RootFixedClampBody is not emitted")
	}
	if !strings.Contains(clamp, "for i := int64(0); i < int64(value.MarksCount); i++ {") {
		t.Error("C14: the bounds pass must walk a counted array's LIVE count (§4.6)")
	}
	if strings.Contains(clamp, "i < 4") {
		t.Error("C14: the bounds pass must not walk the declared bound, its slack (§4.6)")
	}
	if !strings.Contains(clamp, "if value.OnotePresent {") {
		t.Error("C14: an optional's payload is held to a bound only when the present byte says so (§4.6)")
	}
	if !strings.Contains(clamp, "if uint32(value.U.Type) > 2 {") {
		t.Error("C14: a union tag past the arm count is held by the pass over storage (§4.6)")
	}
}

// ---- go/W4 ------------------------------------------------------------------

// t07GoW4 holds W4, "read slack unspecified", to the ONE read loop this leg
// emits: §3.1 "Slack is UNSPECIFIED on read and not a refusal: a reader
// validates the USED UNITS only, and non-zero slack is not `malformed`, not a
// refusal, and moves no counter." The text op holds the length word to the
// plan's own capacity and counts that clamp; past the used length it copies the
// payload whole and never inspects, refuses or counts a slack byte.
func t07GoW4(t *testing.T) {
	block := t07RuntimeCase(t, "tableFixedText", "tableFixedOrdinal")
	if !strings.Contains(block, "capn := p.Size / unit") {
		t.Error("W4: the used length is held to the plan's own capacity (§3.1)")
	}
	if !strings.Contains(block, "} else if uint32(v) > capn {") || !strings.Contains(block, "report.Clamped++") {
		t.Error("W4: a length past the capacity clamps and counts, once (§3.1)")
	}
	if !strings.Contains(block, "binary.LittleEndian.PutUint32(dst[p.Dst:], uint32(v))") {
		t.Error("W4: the reader lands the USED length (§3.1)")
	}
	if strings.Contains(block, "Malformed") || strings.Contains(block, "Refuse") {
		t.Error("W4: non-zero slack is not malformed and not a refusal; the reader validates the used units only (§3.1)")
	}
}

// t07RuntimeCase is the text of the package runtime constant from `case <op>:`
// to `case <next>:`, the span the unit carries for one plan op.
func t07RuntimeCase(t *testing.T, op, next string) string {
	t.Helper()
	start := strings.Index(tableFixedRuntime, "case "+op+":")
	if start < 0 {
		t.Fatalf("the emitted runtime has no case %s:", op)
	}
	rest := tableFixedRuntime[start:]
	end := strings.Index(rest, "case "+next+":")
	if end < 0 {
		t.Fatalf("the emitted runtime has no case %s: after case %s:", next, op)
	}
	return rest[:end]
}

// ---- go/W9 ------------------------------------------------------------------

// t07GoW9 holds W9, "prefill unwritten ranges only", to the emitted load:
// §4.3 "The plan compiler computes the UNWRITTEN RANGES — the destination
// bytes no entry covers — and only those take the declared defaults before the
// loop". The prefill is the hole list, copied one hole at a time, and never a
// whole-record copy that would clobber what the plan lands.
func t07GoW9(t *testing.T) {
	load := funcSource(t07Emitted(t), "func RootFixedLoad(")
	if load == "" {
		t.Fatal("RootFixedLoad is not emitted")
	}
	if !strings.Contains(load, "holes = tableFixedHoles(entries, entryCount, uint32(len(defBytes)))") {
		t.Error("W9: the prefill ranges are the plan's UNWRITTEN ranges (§4.3)")
	}
	if !strings.Contains(load, "copy(dst[h.Off:h.Off+h.Size], defBytes[h.Off:h.Off+h.Size])") {
		t.Error("W9: only the holes take the declared defaults (§4.3)")
	}
	if strings.Contains(load, "copy(dst, defBytes)") {
		t.Error("W9: a whole-record prefill would overwrite the ranges the plan lands (§4.3)")
	}
}

// ---- go/W1 ------------------------------------------------------------------

// t07GoW1 holds W1, "write slack is template zeros", to the emitted save:
// §3.1 "The template is a compile-time constant ... which zero-fills every byte
// of declared slack without the writer touching it." The record body is cleared
// before the write body runs, so every byte the writer does not store — an
// absent optional's payload, the slack behind a narrow arm — is the template's
// zero.
func t07GoW1(t *testing.T) {
	save := funcSource(t07Emitted(t), "func RootFixedSave(")
	if save == "" {
		t.Fatal("RootFixedSave is not emitted")
	}
	if !strings.Contains(save, "clear(at[8 : 8+RootFixedBodyBytes])") {
		t.Error("W1: the writer lays a zeroed template before the stores (§3.1)")
	}
	if !strings.Contains(save, "RootFixedWriteBody(at[8:], &values[k])") {
		t.Error("W1: the write body runs over the zeroed template (§3.1)")
	}
}

// ---- go/W3 ------------------------------------------------------------------

// t07GoW3 holds W3, "zero behind a narrower arm", to the emitted union write:
// §3.1 "A union writes the tag and the taken arm only, zero behind a narrower
// one" and SPEC-TABLES §3.4's union row "ZERO on write behind a narrower arm".
// The tag is stored, the switch stores only the taken arm, and no store covers
// the whole union span.
func t07GoW3(t *testing.T) {
	body := t07Emitted(t)
	write := funcSource(body, "func RootFixedWriteBody(")
	if write == "" {
		t.Fatal("RootFixedWriteBody is not emitted")
	}
	if !strings.Contains(write, "switch value.U.Type {") {
		t.Error("W3: a union writes the tag and the taken arm only (§3.1)")
	}
	if !strings.Contains(write, "case PickTypeA:") || !strings.Contains(write, "case PickTypeB:") {
		t.Error("W3: each arm is its own case, so only the taken arm's bytes are stored (§3.1)")
	}
	if strings.Contains(write, "clear(b[55:64])") {
		t.Error("W3: the writer must not zero the union span it writes; the template's zeros stand behind the narrow arm (§3.1)")
	}
}

// ---- go/W5 ------------------------------------------------------------------

// t07GoW5 holds W5, "write checks DEBUG only", to the emitted write body:
// §3.1 "Write-side bound checks are DEBUG ONLY, by rule ... a release build
// removes them ... and clamps nothing." The write body carries no comparison
// against the declared bound, so the caller's out-of-contract count or length
// rides the wire unchanged.
func t07GoW5(t *testing.T) {
	write := funcSource(t07Emitted(t), "func RootFixedWriteBody(")
	if write == "" {
		t.Fatal("RootFixedWriteBody is not emitted")
	}
	for _, banned := range []string{"MarksCount >", "NoteLength >", "BlobLength >", "MarksCount <", "NoteLength <"} {
		if strings.Contains(write, banned) {
			t.Errorf("W5: the write side holds no bound check, by rule; found %q (§3.1)", banned)
		}
	}
	if !strings.Contains(write, "tableFixedPut32(b[16:], uint32(value.MarksCount))") {
		t.Error("W5: the caller's count rides the wire unchanged (§3.1)")
	}
}

// ---- go/W8 ------------------------------------------------------------------

// t07GoW8 holds W8, "bytes(N) takes the array row", to the emitted destination
// rows: `bytes(N)` rides this form as an array of u8 (FIXED-FORM-ALGORITHM §1,
// kind 14), so its row has stride 1 and a live count with the BUFFER as dst and
// the LENGTH as aux — the array's convention. A text field's row is the other
// way round (dst the length, aux the buffer) and its meta is utf8.
func t07GoW8(t *testing.T) {
	body := t07Emitted(t)
	rows := t07DstRows(t, body)
	blob, ok := rows["blob"]
	if !ok {
		t.Fatal("W8: the emitted RootFixedDst names no blob row")
	}
	// meta 3 is tableFixedTextBytes and meta 1 is tableFixedTextUtf8 in the
	// runtime the unit carries (fixedruntime.go's tableFixedRuntime const).
	if blob[1] != "1" || blob[3] != "1" || blob[5] != strconv.Itoa(t07TextBytes) {
		t.Errorf("W8: bytes(N) takes the ARRAY row — stride 1, a live count, meta bytes — got %v", blob)
	}
	note, ok := rows["note"]
	if !ok {
		t.Fatal("W8: the emitted RootFixedDst names no note row")
	}
	if note[1] != "0" || note[3] != "0" || note[5] != strconv.Itoa(t07TextUtf8) {
		t.Errorf("W8: a text field's row is the other way round — no stride, no count, meta utf8 — got %v", note)
	}
}

// t07DstRows reads the emitted `var RootFixedDst = []TableFixedDst{ … }` into a
// map from each row's trailing name to its six comma-separated fields.
func t07DstRows(t *testing.T, src string) map[string][]string {
	t.Helper()
	const open = "var RootFixedDst = []TableFixedDst{"
	_, after, ok := strings.Cut(src, open)
	if !ok {
		t.Fatal("the emitted surface carries no RootFixedDst")
	}
	rest := after
	before0, _, ok0 := strings.Cut(rest, "\n}")
	if !ok0 {
		t.Fatal("RootFixedDst does not close")
	}
	out := map[string][]string{}
	for line := range strings.SplitSeq(before0, "\n") {
		name := ""
		if _, after, ok := strings.Cut(line, "// "); ok {
			name = strings.TrimSpace(after)
		}
		if name == "" {
			continue
		}
		brace := strings.Index(line, "{")
		end := strings.LastIndex(line, "}")
		if brace < 0 || end <= brace {
			continue
		}
		fields := strings.Split(line[brace+1:end], ",")
		for k := range fields {
			fields[k] = strings.TrimSpace(fields[k])
		}
		out[name] = fields
	}
	return out
}
