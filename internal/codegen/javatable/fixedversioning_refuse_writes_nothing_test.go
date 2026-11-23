package javatable

// `refuse_writes_nothing`, §5.8 row 9 of docs/FIXED-FORM-VERSIONING-TESTS.md, on
// the java leg. `VOLD_/VNEW_nested_append` as they stand — the appended nested
// field `Vec.w = 88` is a NONZERO prefill, the only thing that can tell a prefill
// that ran from one that did not — plus a forged `old_nested_append.bin` whose
// PER-RECORD hash word (the record's first eight bytes) is inverted, the header's
// hash untouched.
//
// The NEW build reads it: the HEADER's hash selects the OLD lineage entry, so a
// COMPILED plan with a NONEMPTY fill list is in hand when §5.3 step 11 compares
// the RECORD's hash. The refusal is `no_layout`, and what it owes is
// (ruling R4, which replaces "writes nothing") that every row the caller
// handed in, poisoned with 0x5A before the load, holds its declared default
// after it (v.w at 88, no 0x5A left). REFUSE IS TOTAL: every counter is 0.
import (
	"encoding/binary"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// the 0x5A poison as a java int32 reads back as this one number.
const poison32 = "1515870810" // 0x5A5A5A5A

func TestFixedVersioningRefuseWritesNothing(t *testing.T) {
	slowtest.Gate(t, "an external toolchain")

	corpus := corpusDir(t)
	oldFile := filepath.Join(corpus, "old_nested_append.bin")
	raw, err := os.ReadFile(oldFile)
	if err != nil {
		t.Fatalf("read %s: %v", oldFile, err)
	}

	// THE FORGE: the records begin at 16 (the header) + 4 (the layout length)
	// + the layout, and a record's first eight bytes are its own hash word.
	// Inverting them leaves the HEADER's hash — the one that selects the plan —
	// exactly as it was, which is the whole point: a compiled plan with a
	// nonempty fill list is in hand when step 11 refuses.
	const headerBytes, lengthBytes = 16, 4
	if len(raw) < headerBytes+lengthBytes {
		t.Fatalf("%s is %d bytes: too short to carry a header", oldFile, len(raw))
	}
	layoutLen := int(binary.LittleEndian.Uint32(raw[headerBytes:]))
	recordsAt := headerBytes + lengthBytes + layoutLen
	if recordsAt+8 > len(raw) {
		t.Fatalf("%s: records begin at %d but the file is %d bytes", oldFile, recordsAt, len(raw))
	}
	forged := append([]byte(nil), raw...)
	for i := range 8 {
		forged[recordsAt+i] ^= 0xFF // the forge
	}
	if binary.LittleEndian.Uint64(forged[8:]) != binary.LittleEndian.Uint64(raw[8:]) {
		t.Fatalf("the forge moved the HEADER's hash, which it must not")
	}
	nolayout := filepath.Join(t.TempDir(), "nolayout_nested_append.bin")
	if err := os.WriteFile(nolayout, forged, 0o644); err != nil {
		t.Fatal(err)
	}

	_, classes := buildRow(t, t.TempDir(), "nested_append", []sideSpec{
		{key: "reads", schema: "VNEW_nested_append.schema", older: []string{"VOLD_nested_append.schema"}},
		{key: "refuses", schema: "VOLD_nested_append.schema"},
	})

	// Probe_reads is the poisoned one. It is asked for the TOTAL poison ("deep"):
	// a nested value is emitted as a FINAL field, so the shared shallow poison
	// never reaches it and the value keeps its own field initializers — which read
	// back exactly like a prefill that ran, and are precisely what this row has to
	// tell apart.
	_, javaBin := javaTools(t)
	out, err := testproc.Command(t, javaBin, "-ea", "-cp", classes, "Probe_reads", nolayout, "deep").CombinedOutput()
	if err != nil {
		t.Fatalf("java Probe_reads %s deep: %v\n%s", filepath.Base(nolayout), err, out)
	}
	r := parseProbe(t, string(out))

	if r.n != -1 {
		t.Errorf("n=%d, want -1", r.n)
	}
	if !r.refused {
		t.Errorf("refused=%v, want true", r.refused)
	}
	if r.reason != "noLayout" {
		t.Errorf("reason=%s, want noLayout", r.reason)
	}
	if r.damaged {
		t.Errorf("malformed=%v, want FALSE — a refusal is never damage (the joint assertion of §5.3)", r.damaged)
	}
	if r.layoutHash != "0x0" {
		t.Errorf("layoutHash=%s, want untouched (0): only the two LAYOUT refusals carry the file's hash", r.layoutHash)
	}
	if r.unknown != 0 || r.widened != 0 {
		t.Errorf("a counter moved on a refusal: unknown=%d widened=%d, want all 0 — REFUSE IS TOTAL",
			r.unknown, r.widened)
	}

	// THE DESTINATION, WHICH IS EVERY ROW HANDED IN (ruling R4, which replaces
	// "writes nothing"): every one of the 64 poisoned rows holds its declared
	// default after the refusal — every field equal to a FRESH value's, the
	// appended v.w at its declared 88, and no 0x5A poison left anywhere.
	if got := r.value["v.w"]; got != "88" {
		t.Errorf("v.w=%s after the refusal, want its declared default 88", got)
	}
	rows := map[string]map[string]string{"VALUE": r.value}
	for line := range strings.SplitSeq(string(out), "\n") {
		tag, rest, ok := strings.Cut(line, " ")
		if !ok || !strings.HasPrefix(tag, "ROW") {
			continue
		}
		k, v, _ := strings.Cut(rest, "=")
		if rows[tag] == nil {
			rows[tag] = map[string]string{}
		}
		rows[tag][k] = v
	}
	if len(rows) != 64 {
		t.Fatalf("the probe dumped %d rows, want all 64 handed in", len(rows))
	}
	for tag, row := range rows {
		for _, field := range []string{"v.x", "v.y", "v.z", "v.w", "seq"} {
			got, ok := row[field]
			if !ok {
				t.Errorf("%s: the probe dumped no %s at all", tag, field)
				continue
			}
			if want := r.fresh[field]; got != want || got == poison32 {
				t.Errorf("%s: %s=%s after the refusal, want its declared default %s", tag, field, got, want)
			}
		}
	}
}
