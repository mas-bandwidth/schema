package javatable

// EVERY DEAD BYTE IS ZERO ON THE LINEAGE READ TOO (docs/rulings.json
// fixed-dead-bytes-zero; docs/SPEC-TABLES.md §3.4), on the java leg: the slack
// is the WRITER's. The NEW build ([..8]int32) reads the OLD file ([..4]int32,
// count 4, vals 1000..1003) through its lineage. The forge drops the count to
// 2, so the old writer's slots 2 and 3 become slack while still holding 1002
// and 1003: the read refuses the whole load as wireDamaged. The control zeroes
// those two slots too, and the same compiled plan then reads count 2.
import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestFixedVersioningDeadBytesLineage(t *testing.T) {
	corpus := corpusDir(t)
	oldFile := filepath.Join(corpus, "old_array_bounded_grow.bin")
	raw, err := os.ReadFile(oldFile)
	if err != nil {
		t.Fatalf("read %s: %v", oldFile, err)
	}

	// THE LOCATOR: lead = 0xAAAAAAAA, then the count 4; it must occur exactly once.
	needle := []byte{0xAA, 0xAA, 0xAA, 0xAA, 0x04, 0x00, 0x00, 0x00}
	at := bytes.Index(raw, needle)
	if at < 0 {
		t.Fatalf("the forge needle % x is not in %s", needle, oldFile)
	}
	if bytes.Contains(raw[at+1:], needle) {
		t.Fatalf("the forge needle % x occurs more than once in %s", needle, oldFile)
	}
	if binary.LittleEndian.Uint32(raw[at+16:]) != 1002 || binary.LittleEndian.Uint32(raw[at+20:]) != 1003 {
		t.Fatalf("CONTROL: the writer's slots 2 and 3 are not 1002 and 1003 in %s", oldFile)
	}

	// THE STAIN: count 2, so slots 2 and 3 (1002, 1003) are the writer's slack.
	stained := append([]byte(nil), raw...)
	stained[at+4] = 0x02
	stainedFile := filepath.Join(t.TempDir(), "stained_array_bounded_grow.bin")
	if err := os.WriteFile(stainedFile, stained, 0o644); err != nil {
		t.Fatal(err)
	}

	// THE CONTROL: the same count with the slack zeroed.
	clean := append([]byte(nil), stained...)
	for i := at + 16; i < at+24; i++ {
		clean[i] = 0
	}
	cleanFile := filepath.Join(t.TempDir(), "clean_array_bounded_grow.bin")
	if err := os.WriteFile(cleanFile, clean, 0o644); err != nil {
		t.Fatal(err)
	}

	_, classes := buildRow(t, t.TempDir(), "array_bounded_grow", []sideSpec{
		{key: "reads", schema: "VNEW_array_bounded_grow.schema", older: []string{"VOLD_array_bounded_grow.schema"}},
	})

	requireNamedRefusal(t, "stained array slack", runProbe(t, classes, "Probe_reads", stainedFile), "wireDamaged")

	r := runProbe(t, classes, "Probe_reads", cleanFile)
	if r.n != 1 || r.refused || r.damaged {
		t.Errorf("CONTROL: zeroed slack does not read: n=%d refused=%v reason=%s damaged=%v", r.n, r.refused, r.reason, r.damaged)
	}
	if got := r.value["valsCount"]; got != "2" {
		t.Errorf("CONTROL: valsCount=%s, want the forged live count 2", got)
	}
}
