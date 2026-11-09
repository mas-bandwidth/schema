package main

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type retainDeepBuilder struct {
	body []byte
	ids  []uint64
}

func (b *retainDeepBuilder) ref(name string) uint64 {
	id := ir.TableWireId(name)
	for i, entry := range b.ids {
		if entry == id {
			return uint64(i) + 1
		}
	}
	b.ids = append(b.ids, id)
	return uint64(len(b.ids))
}

func (b *retainDeepBuilder) u8(v uint8) { b.body = append(b.body, v) }

func (b *retainDeepBuilder) leb(v uint64) {
	for v >= 0x80 {
		b.u8(uint8(v) | 0x80)
		v >>= 7
	}
	b.u8(uint8(v))
}

func (b *retainDeepBuilder) openLen() int {
	at := len(b.body)
	b.u8(0)
	return at
}

func (b *retainDeepBuilder) closeLen(at int) {
	payload := len(b.body) - at - 1
	width := 1
	for v := uint64(payload); v >= 0x80; v >>= 7 {
		width++
	}
	if width > 1 {
		b.body = append(b.body, make([]byte, width-1)...)
		copy(b.body[at+width:], b.body[at+1:])
	}
	w, v := at, uint64(payload)
	for v >= 0x80 {
		b.body[w] = uint8(v) | 0x80
		w++
		v >>= 7
	}
	b.body[w] = uint8(v)
}

func (b *retainDeepBuilder) field(name string, kind uint8) {
	b.leb(b.ref(name))
	b.u8(kind)
}

func (b *retainDeepBuilder) end() { b.u8(0) }

func (b *retainDeepBuilder) finish() []byte {
	out := []byte{1}
	out = append(out, b.body...)
	for _, id := range b.ids {
		out = binary.LittleEndian.AppendUint64(out, id)
	}
	return binary.LittleEndian.AppendUint64(out, uint64(len(b.ids)))
}

func deepRetainWire(depth int) []byte {
	b := &retainDeepBuilder{}
	marks := make([]int, depth)
	b.field("future", ir.TableKindTable)
	marks[0] = b.openLen()
	for d := 1; d < depth; d++ {
		b.field("deeper", ir.TableKindTable)
		marks[d] = b.openLen()
	}
	b.field("hits", ir.TableKindI32)
	b.body = binary.LittleEndian.AppendUint32(b.body, 9)
	for d := depth - 1; d >= 0; d-- {
		b.end()
		b.closeLen(marks[d])
	}
	b.end()
	return b.finish()
}

// TestLimitFramingLeb128Bytes holds the three pinned LEB128 framing vectors
// (docs/SPEC-TABLES.md §3, leb128_max_bytes = 10) to their byte scripts and
// tests both the LEB128 decoder boundary (10 bytes with 0x01 is 2^64-1,
// 10 bytes with 0x02 overflows 64 bits, 11 bytes exceeds the 10-byte limit)
// and the strict file-form verdict on each vector.
func TestLimitFramingLeb128Bytes(t *testing.T) {
	m, _, u := corpus(t)
	unit, err := framingVariableUnit(m, "tblr1", "Cfg")
	if err != nil {
		t.Fatal(err)
	}
	model := tabletext.NewModel(unit)
	def := model.Lookup("Cfg")
	if def == nil {
		t.Fatal("tblr1 declares no Cfg")
	}
	for _, tc := range []struct {
		name    string
		lebOK   bool
		lebVal  uint64
		lebSize int
	}{
		{"frame_leb_ten_bytes_skipped", true, math.MaxUint64, 10},
		{"frame_leb_tenth_byte_overflow_skipped", false, 0, 0},
		{"frame_leb_eleven_bytes_skipped", false, 0, 0},
	} {
		data, err := os.ReadFile(filepath.Join(framingDir, tc.name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		// Byte 0 is form 1, byte 1 is ref 16, byte 2 is kind 17, byte 3 starts the LEB128.
		val, next, ok := strictReadLeb(data, 3)
		if ok != tc.lebOK {
			t.Fatalf("%s: strictReadLeb ok=%v, want %v", tc.name, ok, tc.lebOK)
		}
		if tc.lebOK && (val != tc.lebVal || next-3 != tc.lebSize) {
			t.Fatalf("%s: strictReadLeb val=%x size=%d, want %x size=%d", tc.name, val, next-3, tc.lebVal, tc.lebSize)
		}
		// Under strict-only, all three vectors refuse on whole-file decode:
		// 10-byte 2^64-1 is an unknown pointer past the node table (R2, T7),
		// and the other two fail F10 at the LEB128 decoder.
		inst := model.New(def)
		var report tabletext.Report
		accepted, _ := strictDecode(model, inst, data, &report)
		if accepted || !report.Refused {
			t.Fatalf("%s: strictDecode accepted=%v report=%+v, want refused", tc.name, accepted, report)
		}
	}
	_ = u
}

// TestLimitRetainWalkDepthVectors holds the pinned retain walk depth vectors
// (docs/SPEC-TABLES.md §4, §6.6, retain_walk_depth = 128) to their byte
// scripts and verifies the engine's strict verdict at and past the 128-level
// bound.
func TestLimitRetainWalkDepthVectors(t *testing.T) {
	_, _, u := corpus(t)
	unit, err := u.get("tblrt1")
	if err != nil {
		t.Fatal(err)
	}
	model := tabletext.NewModel(unit)
	def := model.Lookup("Node")
	if def == nil {
		t.Fatal("tblrt1 declares no Node")
	}
	for _, tc := range []struct {
		name     string
		depth    int
		accepted bool
		retained int
	}{
		{"retain_walk_depth_64", 64, true, 1},
		{"retain_walk_depth_65", 65, true, 1},
		{"retain_walk_depth_128", 128, true, 1},
		{"retain_walk_depth_129", 129, false, 0},
	} {
		built := deepRetainWire(tc.depth)
		path := filepath.Join("testdata", "wire", "tables", tc.name+".bin")
		pinned, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if !bytes.Equal(built, pinned) {
			t.Fatalf("%s: pinned file does not match deepRetainWire(%d)", tc.name, tc.depth)
		}
		inst := model.New(def)
		retain := tablewire.Retain{Capacity: 1 << 16, IdCapacity: 1024}
		var report tabletext.Report
		ok, derr := strictDecodeRetain(model, inst, pinned, &retain, &report)
		if !tc.accepted {
			if ok || derr == nil || !report.Refused {
				t.Fatalf("%s: strictDecodeRetain ok=%v err=%v report=%+v, want refused", tc.name, ok, derr, report)
			}
			continue
		}
		if derr != nil || !ok || report.Refused {
			t.Fatalf("%s: strictDecodeRetain ok=%v err=%v report=%+v, want accepted", tc.name, ok, derr, report)
		}
		if report.Retained != tc.retained || report.RetainLost != 0 {
			t.Fatalf("%s: retained=%d lost=%d, want %d/0", tc.name, report.Retained, report.RetainLost, tc.retained)
		}
		var saveReport tabletext.Report
		saved, err := tablewire.EncodeRetain(model, inst, &retain, &saveReport)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(saved, pinned) || saveReport.RetainLost != 0 {
			t.Fatalf("%s: saved bytes differ or lost=%d", tc.name, saveReport.RetainLost)
		}
	}
}

// TestLimitStorageCapVectors holds the shared storage-cap vectors
// (testdata/wire/tables/storage-cap, docs/SPEC-TABLES.md §11, storage_cap =
// 2^40): list_at.bin and nodes_at.bin measure at or under 2^40 bytes, while
// list_over.bin and nodes_over.bin measure past 2^40 bytes and are refused by
// the oracle without materializing the terabyte model.
func TestLimitStorageCapVectors(t *testing.T) {
	corpus(t)
	dir := filepath.Join("testdata", "wire", "tables", "storage-cap")
	paths, err := compiler.GatherPaths([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	model := tabletext.NewModel(u)
	rootDef := u.Tables["Root"]
	if rootDef == nil {
		t.Fatal("Cap.schema declares no Root")
	}
	wroot := &wireRoot{
		unit:     "capcheck",
		root:     "Root",
		model:    model,
		def:      rootDef,
		variable: true,
	}
	for _, tc := range []struct {
		file string
		over bool
	}{
		{"list_at.bin", false},
		{"list_over.bin", true},
		{"nodes_at.bin", false},
		{"nodes_over.bin", true},
	} {
		wire, err := os.ReadFile(filepath.Join(dir, tc.file))
		if err != nil {
			t.Fatal(err)
		}
		n, whole, refused := tablewire.FileRegionMeasure(u, rootDef, wire)
		if tc.over {
			if !whole || (!refused && n <= ir.MaxSizeBytes) {
				t.Fatalf("%s: measure=%d whole=%v refused=%v, want past storage_cap (%d)", tc.file, n, whole, refused, ir.MaxSizeBytes)
			}
			ans, err := wroot.oracle(wire)
			if err != nil {
				t.Fatal(err)
			}
			if ans.accepted || !ans.report.Refused {
				t.Fatalf("%s: oracle accepted=%v report=%+v, want refused", tc.file, ans.accepted, ans.report)
			}
			continue
		}
		if !whole || refused || n <= 0 || n > ir.MaxSizeBytes {
			t.Fatalf("%s: measure=%d whole=%v refused=%v, want in (0, %d]", tc.file, n, whole, refused, ir.MaxSizeBytes)
		}
	}
}

// TestLimitFileIdTableVectors holds the shared file-form id-table limit vectors
// (testdata/wire/tables/fuzz-vectors/id_table_{4096,4097}*.bin,
// docs/SPEC-TABLES.md §3, id_table_max = 4096) to the oracle's strict verdict.
func TestLimitFileIdTableVectors(t *testing.T) {
	m, _, units := corpus(t)
	for _, tc := range []struct {
		name     string
		unit     string
		root     string
		accepted bool
	}{
		{"id_table_4096", "graphdemo", "Scene", true},
		{"id_table_4097", "graphdemo", "Scene", false},
		{"id_table_4096_value", "tblr1", "Cfg", true},
		{"id_table_4097_value", "tblr1", "Cfg", false},
	} {
		wire, err := os.ReadFile(filepath.Join("testdata", "wire", "tables", "fuzz-vectors", tc.name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		u, err := units.get(tc.unit)
		if err != nil {
			t.Fatal(err)
		}
		if st := u.Tables[tc.root]; st != nil && st.FixedDeclared {
			u, err = framingVariableUnit(m, tc.unit, tc.root)
			if err != nil {
				t.Fatal(err)
			}
		}
		model := tabletext.NewModel(u)
		def := model.Lookup(tc.root)
		inst := model.New(def)
		var report tabletext.Report
		accepted, _ := strictDecode(model, inst, wire, &report)
		if accepted != tc.accepted {
			t.Fatalf("%s: strictDecode accepted=%v, want %v (report=%+v)", tc.name, accepted, tc.accepted, report)
		}
	}
}
