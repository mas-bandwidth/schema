package tablecook_test

import (
	"encoding/binary"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// A `*string` is a BYTE BUFFER node whose storage carries a NUL terminator
// after its bytes (§6.3, §7.2), and the C and C++ cooked readers open a .cook
// with no per-field validation, so cook-check and uncook are what hold that
// node to the terminator, exactly as a text slot's blob is (§7.4). A terminator
// byte that is not 0x00 would make a C++ reader's strlen read past the buffer,
// so both reads must refuse it.
func TestCookCheckAndUncookRefuseAStringBlobWithANonZeroTerminator(t *testing.T) {
	t.Parallel()
	u, m := unitFromSource(t, "package probe\ntable T {\n    s *string\n}\n")
	root := m.Lookup("T")
	inst := m.New(root)
	field(inst, "s").Cell.Blob = &tabletext.Blob{Data: []byte("hi")}

	cooked, err := tablecook.Cook(m, inst, tablecook.Options{})
	if err != nil {
		t.Fatal(err)
	}

	h, err := tablecook.ReadHeader(cooked, ir.BuildVersion(u))
	if err != nil {
		t.Fatal(err)
	}
	dir, err := h.Directory(cooked)
	if err != nil {
		t.Fatal(err)
	}
	le := binary.LittleEndian
	dataOff := h.DataOffset()

	blobIndex := -1
	for i, e := range dir {
		if e.TypeId == ir.StringTypeId {
			blobIndex = i
			break
		}
	}
	if blobIndex < 0 {
		t.Fatal("the cook carries no *string byte buffer node")
	}
	base := dir[blobIndex].Offset
	length := int64(le.Uint32(cooked[dataOff+base:]))
	terminatorAt := dataOff + base + tablecook.BlobHeader + length

	if cooked[terminatorAt] != 0 {
		t.Fatalf("the clean cook's terminator byte is 0x%02x, want 0x00", cooked[terminatorAt])
	}
	if _, err := tablecook.Check(m, cooked); err != nil {
		t.Fatalf("the clean cook is refused by cook-check: %v", err)
	}
	if _, err := tablecook.Uncook(m, root, cooked); err != nil {
		t.Fatalf("the clean cook is refused by uncook: %v", err)
	}

	bad := append([]byte(nil), cooked...)
	bad[terminatorAt] = 'X'

	if _, err := tablecook.Check(m, bad); err == nil {
		t.Error("FAILED: cook-check accepted a *string blob whose terminator byte is not NUL")
	}
	if _, err := tablecook.Uncook(m, root, bad); err == nil {
		t.Error("FAILED: uncook accepted a *string blob whose terminator byte is not NUL")
	}
}
