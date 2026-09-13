package lockfile_test

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/lockfile"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func makeLayoutEntry(id uint64, kind uint8, size uint32, children uint32) []byte {
	b := make([]byte, 17)
	binary.LittleEndian.PutUint64(b[0:8], id)
	b[8] = kind
	binary.LittleEndian.PutUint32(b[9:13], size)
	binary.LittleEndian.PutUint32(b[13:17], children)
	return b
}

func makeLayoutBytes(entries ...[]byte) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b[0:4], uint32(len(entries)))
	for _, e := range entries {
		b = append(b, e...)
	}
	return b
}

// TestReviewerPublicCheckRejectsMalformedHistoricalLayout is the exact reviewer
// witness test from Issue #992.
func TestReviewerPublicCheckRejectsMalformedHistoricalLayout(t *testing.T) {
	dir, paths := fixture(t, rowTable("    a int32\n    b int32"))
	if _, _, err := lockfile.Update(load(t, paths), paths); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Lock.schema"), []byte(rowTable("    a int32\n    b int32\n    c int32")), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := lockfile.Update(load(t, paths), paths); err != nil {
		t.Fatal(err)
	}
	lk, ok, err := lockfile.Open(paths)
	if err != nil || !ok {
		t.Fatalf("open: %v %v", ok, err)
	}
	entries := lockfile.Lineage(lk, "Row")
	if len(entries) != 2 {
		t.Fatal("want two entries")
	}
	bad := []byte{0, 0, 0, 0}
	entries[0] = lockfile.LineageEntry{Wire: ir.TableFixedLayoutHash(bad, nil), Layout: bad, Record: 0}
	file := filepath.Join(dir, lockfile.FileName)
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(data), "\n")
	replaced := false
	for i, line := range lines {
		if !replaced && strings.HasPrefix(strings.TrimSpace(line), "lineage ") {
			lines[i] = fmt.Sprintf("    lineage wire=0x%016x record=0 bytes=00000000", entries[0].Wire)
			replaced = true
		}
	}
	text := strings.Join(lines, "\n")
	text = regexp.MustCompile(`lineage=0x[0-9a-f]+`).ReplaceAllString(text, fmt.Sprintf("lineage=0x%016x", lockfile.LineageHash(entries)))
	if err := os.WriteFile(file, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	if errs := lockfile.Check(load(t, paths), paths); len(errs) == 0 {
		t.Fatal("public Check accepted a zero-count historical layout after its hash and rollup were recomputed")
	}
}

func testMalformedLayoutAtBoundary(t *testing.T, badLayout []byte, expectedReason string, targetHistorical bool) {
	t.Helper()
	dir, paths := fixture(t, rowTable("    a int32\n    b int32"))
	if _, _, err := lockfile.Update(load(t, paths), paths); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Lock.schema"), []byte(rowTable("    a int32\n    b int32\n    c int32")), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := lockfile.Update(load(t, paths), paths); err != nil {
		t.Fatal(err)
	}
	lk, ok, err := lockfile.Open(paths)
	if err != nil || !ok {
		t.Fatalf("open: %v %v", ok, err)
	}
	entries := lockfile.Lineage(lk, "Row")
	if len(entries) != 2 {
		t.Fatal("want two entries")
	}

	targetIdx := 0
	if !targetHistorical {
		targetIdx = 1
	}

	wire := ir.TableFixedLayoutHash(badLayout, nil)
	entries[targetIdx] = lockfile.LineageEntry{Wire: wire, Layout: badLayout, Record: int64(len(badLayout))}

	file := filepath.Join(dir, lockfile.FileName)
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(data), "\n")
	foundCount := 0
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "lineage ") {
			if foundCount == targetIdx {
				lines[i] = fmt.Sprintf("    lineage wire=0x%016x record=%d bytes=%x", wire, len(badLayout), badLayout)
				break
			}
			foundCount++
		}
	}
	text := strings.Join(lines, "\n")
	text = regexp.MustCompile(`lineage=0x[0-9a-f]+`).ReplaceAllString(text, fmt.Sprintf("lineage=0x%016x", lockfile.LineageHash(entries)))
	if err := os.WriteFile(file, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}

	// 1. Check refuses
	errs := lockfile.Check(load(t, paths), paths)
	if len(errs) == 0 {
		t.Fatalf("public Check accepted malformed layout (%s) after hash and rollup recomputed", expectedReason)
	}
	if !strings.Contains(errs[0].Error(), expectedReason) {
		t.Errorf("error %q does not contain expected reason %q", errs[0].Error(), expectedReason)
	}

	// 2. Update refuses and does not overwrite disk
	_, rewrote, uerr := lockfile.Update(load(t, paths), paths)
	if uerr == nil || rewrote {
		t.Fatalf("Update accepted malformed layout: rewrote=%v, err=%v", rewrote, uerr)
	}
	after, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != text {
		t.Error("Update modified the lockfile on refusal")
	}
}

// TestRecordedLayoutValidControl verifies that valid layouts pass Check and Update
// when hashes and rollups are properly aligned.
func TestRecordedLayoutValidControl(t *testing.T) {
	dir, paths := fixture(t, rowTable("    a int32\n    b int32"))
	if _, _, err := lockfile.Update(load(t, paths), paths); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Lock.schema"), []byte(rowTable("    a int32\n    b int32\n    c int32")), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := lockfile.Update(load(t, paths), paths); err != nil {
		t.Fatal(err)
	}
	lk, ok, err := lockfile.Open(paths)
	if err != nil || !ok {
		t.Fatalf("open: %v %v", ok, err)
	}
	entries := lockfile.Lineage(lk, "Row")
	if len(entries) != 2 {
		t.Fatal("want two entries")
	}

	// Construct an alternate valid 1-field layout for the historical entry (index 0)
	validAlt := makeLayoutBytes(
		makeLayoutEntry(1, 13, 4, 1), // table, size 4, 1 field
		makeLayoutEntry(2, 4, 4, 0),  // a int32, size 4, 0 kids
	)
	if reason := lockfile.ValidateLayout(validAlt); reason != "" {
		t.Fatalf("validAlt failed ValidateLayout: %s", reason)
	}

	wire := ir.TableFixedLayoutHash(validAlt, nil)
	entries[0] = lockfile.LineageEntry{Wire: wire, Layout: validAlt, Record: 4}

	file := filepath.Join(dir, lockfile.FileName)
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "lineage ") {
			lines[i] = fmt.Sprintf("    lineage wire=0x%016x record=4 bytes=%x", wire, validAlt)
			break
		}
	}
	text := strings.Join(lines, "\n")
	text = regexp.MustCompile(`lineage=0x[0-9a-f]+`).ReplaceAllString(text, fmt.Sprintf("lineage=0x%016x", lockfile.LineageHash(entries)))
	if err := os.WriteFile(file, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}

	// Check must pass clean
	if errs := lockfile.Check(load(t, paths), paths); len(errs) != 0 {
		t.Fatalf("Check failed on valid control: %v", errs)
	}

	// Update must succeed (idempotent)
	if _, rewrote, err := lockfile.Update(load(t, paths), paths); err != nil || rewrote {
		t.Fatalf("Update failed on valid control: rewrote=%v err=%v", rewrote, err)
	}
}

// TestRecordedLayoutRule1CountMismatch tests §1.1 Rule 1.
func TestRecordedLayoutRule1CountMismatch(t *testing.T) {
	// Zero count
	badZero := []byte{0, 0, 0, 0}
	testMalformedLayoutAtBoundary(t, badZero, "layout_count_mismatch", true)
	testMalformedLayoutAtBoundary(t, badZero, "layout_count_mismatch", false)

	// Count states 2 entries, but only 1 entry present (len 21 != 38)
	badMismatch := append([]byte{2, 0, 0, 0}, makeLayoutEntry(1, 13, 4, 1)...)
	testMalformedLayoutAtBoundary(t, badMismatch, "layout_count_mismatch", true)
}

// TestRecordedLayoutResidueMalformed tests residue shorter than 4-byte header.
func TestRecordedLayoutResidueMalformed(t *testing.T) {
	badResidue := []byte{1, 2, 3}
	testMalformedLayoutAtBoundary(t, badResidue, "layout_malformed", true)
}

// TestRecordedLayoutRule2KindUnknown tests §1.1 Rule 2.
func TestRecordedLayoutRule2KindUnknown(t *testing.T) {
	// Kind 200 is outside closed kind set
	badKind := makeLayoutBytes(
		makeLayoutEntry(1, 13, 4, 1),
		makeLayoutEntry(2, 200, 4, 0),
	)
	testMalformedLayoutAtBoundary(t, badKind, "layout_kind_unknown", true)
	testMalformedLayoutAtBoundary(t, badKind, "layout_kind_unknown", false)

	// Kind 31 (§3 framing escape) is outside closed kind set
	badEscape := makeLayoutBytes(
		makeLayoutEntry(1, 13, 4, 1),
		makeLayoutEntry(2, 31, 4, 0),
	)
	testMalformedLayoutAtBoundary(t, badEscape, "layout_kind_unknown", true)

	// Kind 34 (reserved float16) is outside closed kind set
	badReserved := makeLayoutBytes(
		makeLayoutEntry(1, 13, 4, 1),
		makeLayoutEntry(2, 34, 4, 0),
	)
	testMalformedLayoutAtBoundary(t, badReserved, "layout_kind_unknown", true)
}

// TestRecordedLayoutRule3SizeMismatch tests §1.1 Rule 3.
func TestRecordedLayoutRule3SizeMismatch(t *testing.T) {
	// Child kind 4 (int32) states size 5 (instead of 4)
	badLeafSize := makeLayoutBytes(
		makeLayoutEntry(1, 13, 5, 1),
		makeLayoutEntry(2, 4, 5, 0),
	)
	testMalformedLayoutAtBoundary(t, badLeafSize, "layout_size_mismatch", true)
	testMalformedLayoutAtBoundary(t, badLeafSize, "layout_size_mismatch", false)

	// Table root states size 8, but sum of child sizes is 4
	badSumSize := makeLayoutBytes(
		makeLayoutEntry(1, 13, 8, 1),
		makeLayoutEntry(2, 4, 4, 0),
	)
	testMalformedLayoutAtBoundary(t, badSumSize, "layout_size_mismatch", true)
}

// TestRecordedLayoutRule4KindInvalid tests §1.1 Rule 4.
func TestRecordedLayoutRule4KindInvalid(t *testing.T) {
	// Root is kind 14 (array), not kind 13 (table)
	badRoot := makeLayoutBytes(
		makeLayoutEntry(1, 14, 4, 1),
		makeLayoutEntry(2, 4, 4, 0),
	)
	testMalformedLayoutAtBoundary(t, badRoot, "layout_kind_invalid", true)
	testMalformedLayoutAtBoundary(t, badRoot, "layout_kind_invalid", false)

	// Leaf kind 4 (int32) claims 1 child (leaves must have children == 0)
	badLeafKids := makeLayoutBytes(
		makeLayoutEntry(1, 13, 4, 1),
		makeLayoutEntry(2, 4, 4, 1),
		makeLayoutEntry(3, 4, 4, 0),
	)
	testMalformedLayoutAtBoundary(t, badLeafKids, "layout_kind_invalid", true)
}

// TestRecordedLayoutRule5TreeUnclosed tests §1.1 Rule 5.
func TestRecordedLayoutRule5TreeUnclosed(t *testing.T) {
	// Table claims 2 children, but layout only provides 1 child
	badUnclosed := makeLayoutBytes(
		makeLayoutEntry(1, 13, 4, 2),
		makeLayoutEntry(2, 4, 4, 0),
	)
	testMalformedLayoutAtBoundary(t, badUnclosed, "layout_tree_unclosed", true)
	testMalformedLayoutAtBoundary(t, badUnclosed, "layout_tree_unclosed", false)

	// Table claims 0 children, but layout provides 2 entries (entry left over)
	badLeftover := makeLayoutBytes(
		makeLayoutEntry(1, 13, 0, 0),
		makeLayoutEntry(2, 4, 4, 0),
	)
	testMalformedLayoutAtBoundary(t, badLeftover, "layout_tree_unclosed", true)
}

// TestRecordedLayoutRule6RecordTooLarge tests §1.1 Rule 6.
func TestRecordedLayoutRule6RecordTooLarge(t *testing.T) {
	// Root size exceeds 65536
	badRootSize := makeLayoutBytes(
		makeLayoutEntry(1, 13, 65537, 0),
	)
	testMalformedLayoutAtBoundary(t, badRootSize, "layout_record_too_large", true)
	testMalformedLayoutAtBoundary(t, badRootSize, "layout_record_too_large", false)

	// Child size exceeds 65536
	badChildSize := makeLayoutBytes(
		makeLayoutEntry(1, 13, 65537, 1),
		makeLayoutEntry(2, 12, 65537, 0), // string(N) size 65537
	)
	testMalformedLayoutAtBoundary(t, badChildSize, "layout_record_too_large", true)

	// Partial sum exceeds 65536
	badPartialSum := makeLayoutBytes(
		makeLayoutEntry(1, 13, 65536, 2),
		makeLayoutEntry(2, 12, 40000, 0), // string(40000)
		makeLayoutEntry(3, 12, 40000, 0), // string(40000); partial sum 80000 > 65536
	)
	testMalformedLayoutAtBoundary(t, badPartialSum, "layout_record_too_large", true)
}

// TestRecordedLayoutRule7TooDeep tests §1.1 Rule 7.
func TestRecordedLayoutRule7TooDeep(t *testing.T) {
	// Nest 66 optional wrappers (kind 35) to exceed max depth 64
	entries := make([][]byte, 66)
	// Leaf at the bottom (depth 65)
	entries[65] = makeLayoutEntry(66, 4, 4, 0) // int32, size 4, 0 kids
	curSize := uint32(4)
	for d := 64; d >= 1; d-- {
		curSize++ // optional wrapper is child size + 1
		entries[d] = makeLayoutEntry(uint64(d+1), 35, curSize, 1)
	}
	// Root table (depth 0)
	entries[0] = makeLayoutEntry(1, 13, curSize, 1)

	badDeep := makeLayoutBytes(entries...)
	testMalformedLayoutAtBoundary(t, badDeep, "layout_too_deep", true)
	testMalformedLayoutAtBoundary(t, badDeep, "layout_too_deep", false)
}
