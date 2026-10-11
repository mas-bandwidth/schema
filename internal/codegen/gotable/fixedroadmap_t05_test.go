package gotable

// THE ROADMAP'S TEXT ROW ON THE GO LEG (docs/roadmap.sexp node `fixed-tables`,
// ROADMAP.md "NEW Fixed Tables"). Law: docs/FIXED-FORM-ALGORITHM.md §4.5 THE
// TEXT OP ("THE TEXT OP, in full"), §5.4's counter row ("`count`, `text` |
// `clamped` | once per entry per record, when the length or count was out of
// range"), §5.8's divergence row 8 ("ill-formed text in the USED units") and
// its fix 11 ("the length CLAMPS and counts, the CONTENT refuses by name"),
// docs/SPEC-TABLES.md §3.4's text rows, and docs/FIXED-FORM-VERSIONING-TESTS.md.
//
// GO/C3, GO/C4 AND GO/C5 ARE READ OFF THE EMITTED SOURCE (tableFixedRuntime,
// the package-scoped runtime the module carries). GO/R24 IS NOT: a string match
// over the source cannot bite on a validator that ACCEPTS ill-formed bytes, so
// t05GoR24 GENERATES the table unit and RUNS a probe against the package-scoped
// tableFixedTextIllFormed the gate's generated unit compiles, feeding it the
// bytes utf8.Valid refuses. The probe needs no sibling serialize.go — it touches
// the table emitter's own files only — so every subtest still RUNS under a bare
// `go test -run TestFixedRoadmapGoT05Evolution ./internal/codegen/gotable/`. One
// subtest per task id, table-driven, t.Parallel() first, and every assertion
// names the page sentence it implements.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestFixedRoadmapGoT05Evolution(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"go/C3", t05GoC3},
		{"go/C4", t05GoC4},
		{"go/C5", t05GoC5},
		{"go/R24", t05GoR24},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// t05Run is the emitted read loop, tableFixedRun, from the runtime the module
// carries. A missing symbol is red by name rather than vacuous.
func t05Run(t *testing.T) string {
	t.Helper()
	src := funcSource(tableFixedRuntime, "func tableFixedRun(")
	if src == "" {
		t.Fatal("t05: the emitted runtime carries no tableFixedRun")
	}
	return src
}

// t05TextOp is the `case tableFixedText:` arm of the read loop, from that case
// to the next, so an assertion reads the text op and only the text op.
func t05TextOp(t *testing.T) string {
	t.Helper()
	src := t05Run(t)
	i := strings.Index(src, "case tableFixedText:")
	if i < 0 {
		t.Fatal("t05: the read loop carries no `case tableFixedText:`")
	}
	rest := src[i+len("case tableFixedText:"):]
	if j := strings.Index(rest, "\n\t\tcase tableFixed"); j >= 0 {
		rest = rest[:j]
	}
	return rest
}

func t05Has(t *testing.T, got, want, sentence string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Errorf("t05: the emitted text op does not carry %q; it implements: %s", want, sentence)
	}
}

// t05GoC3 holds C3 ("text length clamp") to the emitted text op: the length is
// read as a SIGNED i32 and clamped into [0, cap], and the clamp counts once.
// §4.5 THE TEXT OP: "v := LE(4, src + p.src) read as SIGNED; if v < 0: v := 0 ;
// COUNT clamped; if v > cap: v := cap ; COUNT clamped" and the §5.4 row:
// "`count`, `text` | `clamped` | once per entry per record, when the length or
// count was out of range".
func t05GoC3(t *testing.T) {
	op := t05TextOp(t)
	t05Has(t, op, "v := int32(tableFixedGet32(src[p.Src:]))", "the length is read as a signed i32 (§4.5 THE TEXT OP)")
	t05Has(t, op, "if v < 0 {", "a negative length is the first clamp arm (§4.5 THE TEXT OP)")
	t05Has(t, op, "v = 0", "a negative length clamps UP to zero (§4.5 THE TEXT OP)")
	t05Has(t, op, "else if uint32(v) > capn {", "a length past the cap is the second clamp arm (§4.5 THE TEXT OP)")
	t05Has(t, op, "v = int32(capn)", "a length past the cap clamps DOWN to it (§4.5 THE TEXT OP)")
	if n := strings.Count(op, "report.Clamped++"); n != 2 {
		t.Errorf("t05: the text op counts clamped at %d sites, want the two clamp arms — once per field (§5.4)", n)
	}
}

// t05GoC4 holds C4 ("text content refuses BY NAME") to the emitted runtime: the
// USED units are validated and a violation REFUSES BY NAME. §5.4 fix 11: "the
// length CLAMPS and counts, the CONTENT refuses by name as the packet reader
// does", and §4.5 THE TEXT OP: "A content violation REFUSES BY NAME, the
// verdict the packet reader gives, this form having no `L` to continue past".
func t05GoC4(t *testing.T) {
	op := t05TextOp(t)
	t05Has(t, tableFixedRuntime, "func tableFixedTextIllFormed(", "the runtime validates text content over the used units (§5.4 fix 11)")
	t05Has(t, op, "tableFixedTextIllFormed(", "the text op asks whether the used units are the text its kind says (§4.5)")
	t05Has(t, op, `"text_ill_formed"`, "a content violation refuses BY NAME `text_ill_formed` (§4.5, §5.8 row 8)")
	// The refusal propagates: the run returns the reason and the load refuses
	// with it. §5.8 row 8: the ruling is "a refusal by name" and not the
	// reference's `malformed`.
	run := t05Run(t)
	t05Has(t, run, "return \"text_ill_formed\"", "the read loop hands the name back (§5.8 row 8)")
	load := t05GeneratedLoad(t)
	t05Has(t, load, "tableFixedRun(", "the generated load runs the read loop")
	t05Has(t, load, "tableFixedRefuse(report, why)", "the load refuses by the name the run reported (§5.8 row 8)")
}

// t05GoC5 holds C5 ("wide text code units") to the emitted text op: a
// `wstring(N)` rides as UTF-16 CODE UNITS, so the cap is `span / 2` and never
// `span`. §4.5 THE TEXT OP: "unit := 2 when the flavour is WIDE ... cap :=
// p.span / unit -- the CAP is in UNITS, the SPAN is in BYTES", and "a
// wstring(64) whose span is 128 bytes clamps at 64".
func t05GoC5(t *testing.T) {
	op := t05TextOp(t)
	t05Has(t, op, "unit := uint32(1)", "the narrow unit is one byte (§4.5 THE TEXT OP)")
	t05Has(t, op, "if p.Meta == tableFixedTextWide {", "the wide flavour selects the unit (§4.5 THE TEXT OP)")
	t05Has(t, op, "unit = 2", "a wstring(N) rides two bytes a code unit (§4.5 THE TEXT OP)")
	t05Has(t, op, "capn := p.Size / unit", "the cap is span/unit, so wstring(64)'s 128-byte span clamps at 64 (§4.5 THE TEXT OP)")
	t05Has(t, op, "dst[off] = 0", "the terminator is a STORE of ONE UNIT at the used length (§4.5 THE TEXT OP)")
}

// t05GoR24 holds R24 to the emitted runtime and load: ill-formed text in the
// USED units refuses by name (`text_ill_formed`); text is counted in bytes with
// the clamp in units; slack is unspecified on read and not a refusal.
// docs/FIXED-FORM-ALGORITHM.md §4.5: "the CONTENT RULES apply to the USED UNITS
// and nothing else: UTF-8 validity over v bytes, never over N; wide code units
// over v, never 2N ... A content violation REFUSES BY NAME, the verdict the
// packet reader gives"; docs/SPEC-TABLES.md §3.4: "ZERO on write past the used
// bytes, UNSPECIFIED on read, never a refusal".
//
// THE SOURCE IS NOT THE PROOF. A string match cannot bite on a validator that
// ACCEPTS an ill-formed sequence, so this subtest also GENERATES the table unit
// and RUNS the package-scoped tableFixedTextIllFormed through the generated
// TFixedLoad, feeding it the bytes utf8.Valid refuses.
func t05GoR24(t *testing.T) {
	op := t05TextOp(t)
	ill := funcSource(tableFixedRuntime, "func tableFixedTextIllFormed(")
	if ill == "" {
		t.Fatal("t05: the emitted runtime carries no tableFixedTextIllFormed")
	}
	t05Has(t, ill, "case tableFixedTextUtf8:", "the narrow content rule is UTF-8 over the used bytes (§4.5)")
	t05Has(t, ill, "!utf8.Valid(p[:used])", "UTF-8 validity over the USED bytes, the packet reader's own rule (§4.5, §5.4 fix 11)")
	t05Has(t, ill, "case tableFixedTextWide:", "the wide content rule is over the used code units (§4.5)")
	t05Has(t, op, "tableFixedTextIllFormed(", "the text op consults the content gate (§5.8)")
	// (2) TEXT IS COUNTED IN BYTES WITH THE CLAMP IN UNITS: the wire length word
	// is the payload's own count and the capacity it clamps against is the byte
	// span over the unit — a byte capacity for string(N), N code units for
	// wstring(N)'s 2N-byte span (docs/SPEC-TABLES.md §3.4: "the length is in
	// BYTES and N is a byte capacity" / "the length is in UTF-16 CODE UNITS and
	// the payload is two bytes each"; §4.5: "unit := (meta == wide) ? 2 : 1;
	// cap := size / unit").
	t05Has(t, op, "unit := uint32(1)", "R24 a string's unit is one byte, so its cap is its byte span (§3.4)")
	t05Has(t, op, "capn := p.Size / unit", "R24 the clamp is the byte span over the unit — bytes narrow, code units wide (§4.5, §3.4)")
	t05Has(t, op, "else if uint32(v) > capn {", "R24 the wire length word is clamped against that unit cap (§4.5)")
	// BYTES is never a content refusal.
	if strings.Contains(ill, "tableFixedTextBytes") {
		t.Error("t05: bytes(N) is content-validated; bytes carries no text rule (§4.5)")
	}
	t05Has(t, t05GeneratedLoad(t), "tableFixedRefuse(report, why)", "the refusal is by name, the load's own path (§5.8 row 8)")
	// RUN it: every ill-formed sequence must land `text_ill_formed` through the
	// generated load, a valid wide pair must read, and slack past the used units
	// must not refuse.
	if out, err := t05RunProbe(t, t05IllFormedSchema, t05IllFormedProbe); err != nil {
		t.Fatalf("t05: R24 drove the emitted runtime and it did not hold (§4.5):\n%s", out)
	}
}

// t05RunProbe generates the table unit for schema and runs probe against it in
// package probe, returning the go test output. The probe uses the table
// emitter's own files, so no sibling serialize.go is needed and the subtest runs
// under a bare `go test`.
func t05RunProbe(t *testing.T, schema, probe string) ([]byte, error) {
	t.Helper()
	files, err := Generate(unitFrom(t, schema))
	if err != nil {
		t.Fatalf("t05: Generate: %v", err)
	}
	files["go.mod"] = []byte("module probe\n\ngo 1.26\n")
	files["probe_test.go"] = []byte(probe)
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", ".")
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

const t05IllFormedSchema = `package probe
fixed table T {
    s string(8)
    w wstring(8)
}
`

// t05IllFormedProbe drives the emitted tableFixedTextIllFormed through the
// generated load. The wire is a clean record patched in place: the string
// payload at body+4, its length in code units at body+0 for the wstring at
// body+12. Each ill-formed sequence must return -1 with Reason text_ill_formed
// (§4.5), a valid surrogate pair must read, and slack past the used string bytes
// is UNSPECIFIED and must not refuse (docs/SPEC-TABLES.md §3.4).
const t05IllFormedProbe = `package probe

import (
	"encoding/binary"
	"testing"
	"unsafe"
)

func TestIllFormed(t *testing.T) {
	var seed T
	seed.SLength = 3
	copy(seed.S[:], "abc")
	seed.WLength = 2
	seed.W[0], seed.W[1] = 0x0041, 0x0042
	n := TFixedMeasure(1)
	wire := make([]byte, n)
	if TFixedSave([]T{seed}, wire) != n {
		t.Fatal("save")
	}
	rec := int(n) - int(TFixedRecordBytes)
	sBase := rec + 8
	sPayload := sBase + 4
	wBase := sBase + 12
	wPayload := wBase + 4
	plan := make([]TableFixedEntry, TFixedPlan.Count)
	load := func(b []byte) (int64, TableReport) {
		var r TableReport
		return TFixedLoad(make([]T, 1), b, plan, &r), r
	}

	utf8Cases := []struct {
		name string
		b    []byte
	}{
		{"overlong3", []byte{0xE0, 0x80, 0x80}},
		{"overlong4", []byte{0xF0, 0x80, 0x80, 0x80}},
		{"surrogate", []byte{0xED, 0xA0, 0x80}},
		{"past10ffff", []byte{0xF4, 0x90, 0x80, 0x80}},
		{"invalid_lead_f5", []byte{0xF5, 0x80, 0x80, 0x80}},
		{"invalid_lead_f7", []byte{0xF7, 0xBF, 0xBF, 0xBF}},
		{"interior_zero", []byte{'a', 0x00, 'b'}},
		{"truncated", []byte{0xC3, 0x28}},
	}
	for _, c := range utf8Cases {
		w := append([]byte(nil), wire...)
		binary.LittleEndian.PutUint32(w[sBase:], uint32(len(c.b)))
		copy(w[sPayload:], c.b)
		if got, r := load(w); got >= 0 || r.Reason != "text_ill_formed" {
			t.Fatalf("utf8 %-15s: n=%d verdict=%v reason=%q, want text_ill_formed", c.name, got, r.Verdict, r.Reason)
		}
	}

	wideCases := []struct {
		name  string
		units []uint16
		want  bool
	}{
		{"valid_pair", []uint16{0xD83D, 0xDE00}, false},
		{"lone_high", []uint16{0xD83D}, true},
		{"lone_low", []uint16{0xDE00}, true},
		{"zero_unit", []uint16{0x0000}, true},
		{"high_then_bmp", []uint16{0xD83D, 0x0041}, true},
	}
	for _, c := range wideCases {
		w := append([]byte(nil), wire...)
		binary.LittleEndian.PutUint32(w[wBase:], uint32(len(c.units)))
		for i, u := range c.units {
			binary.LittleEndian.PutUint16(w[wPayload+i*2:], u)
		}
		got, r := load(w)
		if c.want && (got >= 0 || r.Reason != "text_ill_formed") {
			t.Fatalf("wide %-15s: n=%d verdict=%v reason=%q, want text_ill_formed", c.name, got, r.Verdict, r.Reason)
		}
		if !c.want && got < 0 {
			t.Fatalf("wide %-15s: valid surrogate pair refused: %q", c.name, r.Reason)
		}
	}

	slack := append([]byte(nil), wire...)
	for i := 3; i < 8; i++ {
		slack[sPayload+i] = 0xFF
	}
	if got, r := load(slack); got < 0 {
		t.Fatalf("slack: refused %q, want a clean read — slack is UNSPECIFIED, never a refusal (§3.4)", r.Reason)
	}

	// THE REFUSAL IS TOTAL (§5.8): "no counter moves, nothing is decoded, and
	// not one destination byte is written — the prefill included". The string
	// field s is valid, so an earlier plan entry would have landed it before
	// the wide field w refuses; the caller's storage, poisoned 0x5A, must
	// survive untouched.
	poison := make([]T, 1)
	pdst := tableFixedOverlay(unsafe.Pointer(&poison[0]), unsafe.Sizeof(poison[0]))
	for i := range pdst {
		pdst[i] = 0x5A
	}
	pw := append([]byte(nil), wire...)
	binary.LittleEndian.PutUint32(pw[sBase:], 1)
	pw[sPayload] = 'a'
	binary.LittleEndian.PutUint32(pw[wBase:], 1)
	binary.LittleEndian.PutUint16(pw[wPayload:], 0xD83D)
	var pr TableReport
	if got := TFixedLoad(poison, pw, plan, &pr); got >= 0 || pr.Reason != "text_ill_formed" {
		t.Fatalf("poison: n=%d reason=%q, want text_ill_formed", got, pr.Reason)
	}
	for i, b := range pdst {
		if b != 0x5A {
			t.Fatalf("poison: the text_ill_formed refusal wrote destination byte %d: 0x%02X, want 0x5A (§5.8: not one destination byte)", i, b)
		}
	}
}
`

// t05GeneratedLoad is the emitted `<T>FixedLoad` for the smallest text table,
// read off Generate (no toolchain), so R24's refusal is asserted where the
// reader runs it.
func t05GeneratedLoad(t *testing.T) string {
	t.Helper()
	u := unitFrom(t, `package probe
fixed table T {
    s string(8)
}
`)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("t05: Generate: %v", err)
	}
	load := funcSource(tableFile(files), "func TFixedLoad(")
	if load == "" {
		t.Fatal("t05: the module carries no TFixedLoad")
	}
	return load
}
