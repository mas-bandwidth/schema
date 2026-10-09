package javatable

// fixed_form_ragged_tail — schema#876 F8, the malformed row's ragged-tail half,
// on the java leg. F8 is a GAP on go, cpp and cs and only partial on c and
// elixir; java's row is the guard's SECOND arm, rest % recordSize != 0. It is
// F7's DIRECT SIBLING — the very next line of the same emitted function — and
// F7 closed on all nine legs (#1282..#1290) while go closed THIS row as #1291
// (internal/codegen/gotable/fixedform_ragged_tail_test.go). A record region
// that is not a whole number of records is MALFORMED, the residue of a bad
// file, never a refusal by name: `malformed` and `refused` are the reader's two
// answers and are NEVER both set. recordSize is `known_.recordBytes`, the
// SELECTED LINEAGE ENTRY's record size compiled into the reader, NOT on the
// wire, so the guard's FIRST arm (recordSize <= 8) is NOT REACHABLE FROM A
// FILE AT ALL. `rest` is data.length minus head.recordsAt, so THIS row forges
// only the second arm by appending 1..recordBytes-1 bytes to a real corpus
// file. extra == recordBytes is one more WHOLE record, not a ragged tail, and
// owes a refusal, not malformed; it is asserted separately. CONTROL 2 deletes
// the ragged arm and the row must go RED.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// fixedRecordBytes reads the OLD lineage entry's record size out of the emitted
// reader, never a number restated here: `known[0].recordBytes` is the generated
// name (§5.9 #2 lays the lineage OLDEST FIRST, the current layout LAST), and it
// is exactly the `known_.recordBytes` the ragged guard divides by when reading
// the old file.
func fixedRecordBytes(t *testing.T, dir, classes, pkg, root string) int {
	t.Helper()
	javac, javaBin := javaTools(t)
	src := filepath.Join(dir, "src")
	probe := fmt.Sprintf("public final class Probe_size {\n    public static void main(String[] args) {\n        System.out.println(\"SIZE \" + %s.%sFixed.known[0].recordBytes);\n    }\n}\n", pkg, root)
	if err := os.WriteFile(filepath.Join(src, "Probe_size.java"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(javac, "--release", "17", "-nowarn", "-cp", classes, "-d", classes, filepath.Join(src, "Probe_size.java"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("javac Probe_size: %v\n%s", err, out)
	}
	cmd = exec.Command(javaBin, "-ea", "-cp", classes, "Probe_size")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("java Probe_size: %v\n%s", err, out)
	}
	n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(string(out), "SIZE ")))
	if err != nil {
		t.Fatalf("Probe_size answered %q: %v", string(out), err)
	}
	return n
}

func TestFixedFormRaggedTail(t *testing.T) {
	corpus := corpusDir(t)
	oldFile := filepath.Join(corpus, "old_array_bounded_grow.bin")
	raw, err := os.ReadFile(oldFile)
	if err != nil {
		t.Fatalf("read %s: %v", oldFile, err)
	}

	dir := t.TempDir()
	builds, classes := buildRow(t, dir, "fixed_form_ragged_tail", []sideSpec{
		{key: "reads", schema: "VNEW_array_bounded_grow.schema", older: []string{"VOLD_array_bounded_grow.schema"}},
		{key: "refuses", schema: "VOLD_array_bounded_grow.schema"},
	})

	recordBytes := fixedRecordBytes(t, dir, classes, builds["reads"].pkg, builds["reads"].root)
	if recordBytes <= 8 {
		t.Fatalf("recordBytes=%d, want more than 8 — this row forges the ragged arm, not the <=8 arm", recordBytes)
	}

	// The full file must read cleanly FIRST, so a broken fixture cannot pass
	// this row by accident.
	full := runProbe(t, classes, "Probe_reads", oldFile)
	if full.n < 1 || full.refused || full.malformed {
		t.Fatalf("the full file does not read cleanly: n=%d refused=%v reason=%s malformed=%v",
			full.n, full.refused, full.reason, full.malformed)
	}

	for extra := 1; extra < recordBytes; extra++ {
		ragged := append(append([]byte(nil), raw...), make([]byte, extra)...)
		raggedFile := filepath.Join(t.TempDir(), "ragged.bin")
		if err := os.WriteFile(raggedFile, ragged, 0o644); err != nil {
			t.Fatal(err)
		}
		r := runProbe(t, classes, "Probe_reads", raggedFile)
		if !r.malformed {
			t.Errorf("extra=%d: malformed=%v, want true — %d trailing bytes are not a whole record", extra, r.malformed, extra)
		}
		if r.refused {
			t.Errorf("extra=%d: refused=%v, want false — a ragged tail is the residue, not a refusal by name", extra, r.refused)
		}
		if r.reason != "none" {
			t.Errorf("extra=%d: reason=%s, want none — the reason is untouched on a malformed read", extra, r.reason)
		}
		if r.n != -1 {
			t.Errorf("extra=%d: n=%d, want -1", extra, r.n)
		}
		if r.unknown != 0 || r.kindMismatch != 0 || r.widened != 0 || r.clamped != 0 {
			t.Errorf("extra=%d: counters moved: unknown=%d kindMismatch=%d widened=%d clamped=%d, want all 0",
				extra, r.unknown, r.kindMismatch, r.widened, r.clamped)
		}
		if r.layoutHash != "0x0" {
			t.Errorf("extra=%d: layoutHash=%s, want 0x0 — only the two layout refusals report a hash", extra, r.layoutHash)
		}

		// The destination is proved untouched on the NON-poisoned probe, where
		// "untouched" is "still every field of a fresh value" — assertFresh. The
		// reads probe is poisoned (0x5A through its non-final fields), so a
		// value==fresh comparison cannot serve there; the refuses probe is not.
		ref := runProbe(t, classes, "Probe_refuses", raggedFile)
		if !ref.malformed || ref.n != -1 {
			t.Fatalf("extra=%d: the refuses probe did not report malformed: n=%d malformed=%v", extra, ref.n, ref.malformed)
		}
		assertFresh(t, ref)
	}

	// extra == recordBytes is one more WHOLE record, not a ragged tail: the
	// guard's second arm does not fire, and the reader owes a DIFFERENT answer.
	// The appended bytes are all zero, so the extra record's hash word is zero
	// and step 11 refuses `noLayout` — a refusal by name, never malformed.
	whole := append(append([]byte(nil), raw...), make([]byte, recordBytes)...)
	wholeFile := filepath.Join(t.TempDir(), "whole.bin")
	if err := os.WriteFile(wholeFile, whole, 0o644); err != nil {
		t.Fatal(err)
	}
	w := runProbe(t, classes, "Probe_reads", wholeFile)
	if w.malformed {
		t.Errorf("extra==recordBytes: malformed=%v, want false — a whole extra record is not a ragged tail", w.malformed)
	}
	if !w.refused || w.reason != "noLayout" {
		t.Errorf("extra==recordBytes: refused=%v reason=%s, want a noLayout refusal for the zero-filled extra record", w.refused, w.reason)
	}
}
