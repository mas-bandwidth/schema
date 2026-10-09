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
// the RECORD's hash. The refusal is `no_layout`, and §5.3's plainest promise is
// that it writes NOTHING: the caller's storage, poisoned with 0x5A before the
// load, is 0x5A in every byte after it — not the prefill's 88 in `v.w`, not a
// zero anywhere. REFUSE IS TOTAL, the prefill included, and every counter is 0.
import (
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// the 0x5A poison as a java int32 reads back as this one number.
const poison32 = "1515870810" // 0x5A5A5A5A

func TestFixedVersioningRefuseWritesNothing(t *testing.T) {
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
	out, err := exec.Command(javaBin, "-ea", "-cp", classes, "Probe_reads", nolayout, "deep").CombinedOutput()
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
	if r.malformed {
		t.Errorf("malformed=%v, want FALSE — a refusal is never damage (the joint assertion of §5.3)", r.malformed)
	}
	if r.layoutHash != "0x0" {
		t.Errorf("layoutHash=%s, want untouched (0): only the two LAYOUT refusals carry the file's hash", r.layoutHash)
	}
	if r.unknown != 0 || r.kindMismatch != 0 || r.widened != 0 || r.clamped != 0 {
		t.Errorf("a counter moved on a refusal: unknown=%d kindMismatch=%d widened=%d clamped=%d, want all 0 — REFUSE IS TOTAL",
			r.unknown, r.kindMismatch, r.widened, r.clamped)
	}

	// THE DESTINATION, WHICH IS THE ROW. Every field of the poisoned value must
	// still read 0x5A5A5A5A: not the prefill's 88 in v.w, not a zero anywhere.
	for _, field := range []string{"v.x", "v.y", "v.z", "v.w", "seq"} {
		got, ok := r.value[field]
		if !ok {
			t.Errorf("the probe dumped no %s at all", field)
			continue
		}
		if got != poison32 {
			t.Errorf("refuse wrote the destination: %s=%s, want the 0x5A poison %s everywhere", field, got, poison32)
		}
	}
}
