package gotable

import (
	"fmt"
	"strings"
	"testing"
)

func tableOpenMixSlot(id uint64) uint32 {
	h := id
	h ^= h >> 33
	h *= 0xff51afd7ed558ccd
	h ^= h >> 33
	return uint32(h) & 511
}

func TestGoTableOpenDistinctnessIsBounded(t *testing.T) {
	files := generate(t, "package probe\ntable Root { x uint32 }\n")
	body := ""
	for name, data := range files {
		if strings.HasSuffix(name, "Table.go") {
			body += string(data)
		}
	}
	for _, want := range []string{
		"n > 256",
		"var seen [512]uint64",
		"var used [512]uint8",
		"probes < 512",
		"seen[slot] == id",
		"for i := 0; i < len(r.Ids); i += 8 {",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("tableOpen is missing %q", want)
		}
	}
}

func TestGoTableOpenDistinctnessVerdict(t *testing.T) {
	var collideA, collideB uint64
	found := false
	for a := uint64(1); a < 4096 && !found; a++ {
		for b := a + 1; b < 4096; b++ {
			if tableOpenMixSlot(a) == tableOpenMixSlot(b) {
				collideA, collideB = a, b
				found = true
				break
			}
		}
	}
	if !found {
		t.Fatal("could not find two ids that share a 512-slot mix")
	}
	src := strings.ReplaceAll(openDistinctnessVerdictTest, "COLLIDE_A", fmt.Sprint(collideA))
	src = strings.ReplaceAll(src, "COLLIDE_B", fmt.Sprint(collideB))
	runGenerated(t, "package probe\ntable Root { x uint32 }\n", src)
}

const openDistinctnessVerdictTest = `package probe
import ("encoding/binary"; "testing")

func loadIds(ids []uint64) (ok bool, report TableReport) {
	buf := []byte{1, 0}
	for _, id := range ids {
		buf = binary.LittleEndian.AppendUint64(buf, id)
	}
	buf = binary.LittleEndian.AppendUint64(buf, uint64(len(ids)))
	var value Root
	ok = RootLoad(&value, buf, &report)
	return
}

func TestOpenDistinctnessVerdict(t *testing.T) {
	ok, report := loadIds([]uint64{1, 2})
	if !ok || report.Malformed || report.Verdict != TableOpenOk {
		t.Fatalf("distinct pair: %+v", report)
	}
	ok, report = loadIds([]uint64{1, 1})
	if ok || !report.Malformed || report.Verdict != TableOpenDamaged {
		t.Fatalf("duplicate: %+v", report)
	}
	ok, report = loadIds([]uint64{0, 0})
	if ok || !report.Malformed || report.Verdict != TableOpenDamaged {
		t.Fatalf("zero duplicate: %+v", report)
	}
	ok, report = loadIds([]uint64{COLLIDE_A, COLLIDE_B})
	if !ok || report.Malformed || report.Verdict != TableOpenOk {
		t.Fatalf("hash-slot collision of distinct ids: %+v", report)
	}
	over := make([]uint64, 257)
	for i := range over {
		over[i] = uint64(i + 1)
	}
	ok, report = loadIds(over)
	if !ok || report.Malformed || report.Verdict != TableOpenOk {
		t.Fatalf("257 distinct pairwise fallback: %+v", report)
	}
	over[256] = 1
	ok, report = loadIds(over)
	if ok || !report.Malformed || report.Verdict != TableOpenDamaged {
		t.Fatalf("257 with duplicate: %+v", report)
	}
	over = over[:256]
	over[0] = 2
	ok, report = loadIds(over)
	if ok || !report.Malformed || report.Verdict != TableOpenDamaged {
		t.Fatalf("256 with duplicate: %+v", report)
	}
}
`
