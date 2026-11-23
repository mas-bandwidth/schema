package tablecook_test

import (
	"encoding/binary"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// HEADER AND DIRECTORY WORDS ARE BOUNDED BEFORE ANY ARITHMETIC (TABLETEXT-7,
// TABLETEXT-7b). Every offset and length a cook carries is a word from a file
// the tool did not write, so a bound written as a SUM over one — `offset+size
// > data_length` — wraps for an offset near 2^63 and lets the entry through.
// Each case here forges one word of a valid cook built in memory and asserts a
// refusal and no panic.

// refusedWithoutPanic runs one call over forged bytes and reports a panic as a
// failure in its own right: a refusal is the only lawful answer.
func refusedWithoutPanic(t *testing.T, what string, call func() error) {
	t.Helper()
	err := func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("PANIC: %v", r)
			}
		}()
		return call()
	}()
	if err == nil {
		t.Errorf("%s: accepted, and it must refuse", what)
		return
	}
	if strings.HasPrefix(err.Error(), "PANIC:") {
		t.Errorf("%s: %v, and it must refuse", what, err)
	}
}

type cookFixture struct {
	u     *ir.Unit
	m     *tabletext.Model
	valid []byte
	h     tablecook.Header
	scene *ir.Struct
}

func newCookFixture(t *testing.T) cookFixture {
	t.Helper()
	u := unit(t, "../../tables/pointers")
	m := tabletext.NewModel(u)
	valid := hostileFixture(t, m)
	scene := m.Lookup("Scene")
	h, err := tablecook.ReadHeader(valid, ir.BuildVersion(u), ir.RecordLayout(u, scene).Align)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tablecook.Check(m, valid); err != nil {
		t.Fatalf("the valid fixture does not check: %v", err)
	}
	return cookFixture{u: u, m: m, valid: valid, h: h, scene: scene}
}

// withEntries returns the valid cook with directory entries appended, the
// attribution length word raised to cover them.
func (fx cookFixture) withEntries(entries ...tablecook.DirectoryEntry) []byte {
	ord := binary.LittleEndian
	f := append([]byte(nil), fx.valid...)
	for _, e := range entries {
		var raw [16]byte
		ord.PutUint64(raw[0:], uint64(e.Offset))
		ord.PutUint64(raw[8:], e.TypeId)
		f = append(f, raw[:]...)
	}
	ord.PutUint64(f[32:], uint64(fx.h.AttribLength)+uint64(16*len(entries)))
	return f
}

func TestCookCheckRefusesForgedDirectoryOffsets(t *testing.T) {
	t.Parallel()
	fx := newCookFixture(t)
	sceneId := ir.TableTypeId(fx.scene.WireName())
	size := ir.RecordLayout(fx.u, fx.scene).Size

	// near 2^63, aligned for any node (64 is the cap), and far enough up that
	// the sum the old bound computed wraps negative
	near := int64(math.MaxInt64) &^ 63
	if near+size >= 0 {
		t.Fatalf("the forged offset 0x%x plus the Scene record's %d bytes does not wrap: the forge proves nothing", near, size)
	}

	cases := []struct {
		name    string
		entries []tablecook.DirectoryEntry
	}{
		{"an offset near 2^63 whose end wraps", []tablecook.DirectoryEntry{{Offset: near, TypeId: sceneId}}},
		{"an offset near 2^63 naming a byte buffer", []tablecook.DirectoryEntry{{Offset: int64(math.MaxInt64) &^ 7, TypeId: ir.BytesTypeId}}},
		{"a negative offset", []tablecook.DirectoryEntry{{Offset: -64, TypeId: sceneId}}},
		{"a negative offset after a wrapping one", []tablecook.DirectoryEntry{{Offset: near, TypeId: sceneId}, {Offset: -64, TypeId: sceneId}}},
		{"an offset past the data length", []tablecook.DirectoryEntry{{Offset: fx.h.DataLength + 64, TypeId: sceneId}}},
		{"an offset at the data length", []tablecook.DirectoryEntry{{Offset: fx.h.DataLength, TypeId: sceneId}}},
	}
	for _, tc := range cases {
		f := fx.withEntries(tc.entries...)
		refusedWithoutPanic(t, "Check: "+tc.name, func() error {
			_, err := tablecook.Check(fx.m, f)
			return err
		})
		refusedWithoutPanic(t, "Uncook: "+tc.name, func() error {
			_, err := tablecook.Uncook(fx.m, fx.scene, f)
			return err
		})
		refusedWithoutPanic(t, "compiler.CookCheck: "+tc.name, func() error {
			_, err := compiler.New().CookCheck(fx.u, "", f)
			return err
		})
	}
}

// A reference slot's delta is a word from the file too: one near either end
// of int64 is refused, never added to its slot's offset.
func TestCookCheckRefusesForgedReferenceDeltas(t *testing.T) {
	t.Parallel()
	fx := newCookFixture(t)
	at := fx.h.DataOffset() + refSlot(t, fx.u, fx.m, "Scene", "head")
	for _, delta := range []uint64{math.MaxInt64, math.MaxInt64 - 7, 1 << 63, (1 << 63) + 8} {
		f := append([]byte(nil), fx.valid...)
		binary.LittleEndian.PutUint64(f[at:], delta)
		what := fmt.Sprintf("Scene.head delta 0x%016x", delta)
		refusedWithoutPanic(t, "Check: "+what, func() error {
			_, err := tablecook.Check(fx.m, f)
			return err
		})
		refusedWithoutPanic(t, "Uncook: "+what, func() error {
			_, err := tablecook.Uncook(fx.m, fx.scene, f)
			return err
		})
	}
}
