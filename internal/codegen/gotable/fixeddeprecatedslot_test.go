package gotable

// §3.4 DEPRECATED SLOT (docs/SPEC-TABLES.md): a deprecated field keeps its
// slot; the writer emits the field's declared default there (the type's zero
// when none is declared), and the reader yields the declared default whatever
// the record carries. TestFixedDeprecatedSlotIsTheDeclaredDefault pins both
// halves on the fixed form with a deprecated scalar field that declares a
// default and one that declares none.

import (
	"testing"
)

func TestFixedDeprecatedSlotIsTheDeclaredDefault(t *testing.T) {
	t.Parallel()
	runGenerated(t, `package probe
fixed table T
{
    a int32
    b int32 = 7 | deprecated
    c uint16 | deprecated
    d int32
}
`, `package probe
import ("testing";"encoding/binary")
func TestDeprecatedDefault(t *testing.T) {
	var v T
	TReset(&v)
	v.A = 11
	v.B = 99
	v.C = 5
	v.D = 22
	buf := make([]byte, TFixedMeasure(1))
	if n := TFixedSave([]T{v}, buf); n != int64(len(buf)) {
		t.Fatalf("save: n=%d want %d", n, len(buf))
	}
	layoutLen := int(binary.LittleEndian.Uint32(buf[TableFixedHeaderBytes : TableFixedHeaderBytes+4]))
	rec := buf[TableFixedHeaderBytes+4+layoutLen+8:]
	// writer emits the declared default into the deprecated slot, whatever the value holds
	if got := binary.LittleEndian.Uint32(rec[4:8]); got != 7 {
		t.Fatalf("writer: deprecated b slot = %d, want declared default 7", got)
	}
	if got := binary.LittleEndian.Uint16(rec[8:10]); got != 0 {
		t.Fatalf("writer: deprecated c slot = %d, want zero (no declared default)", got)
	}
	// reader yields the declared default whatever the record carries there
	buf2 := append([]byte(nil), buf...)
	binary.LittleEndian.PutUint32(buf2[TableFixedHeaderBytes+4+layoutLen+8+4:], 12345)
	binary.LittleEndian.PutUint16(buf2[TableFixedHeaderBytes+4+layoutLen+8+8:], 54321)
	got := make([]T, 1)
	var r TableReport
	plan := make([]TableFixedEntry, 4096)
	if n := TFixedLoadTolerant(got, buf2, plan, &r); n != 1 {
		t.Fatalf("load: n=%d report=%+v", n, r)
	}
	if got[0].B != 7 {
		t.Fatalf("reader: deprecated b = %d, want declared default 7", got[0].B)
	}
	if got[0].C != 0 {
		t.Fatalf("reader: deprecated c = %d, want zero", got[0].C)
	}
	if got[0].A != 11 || got[0].D != 22 {
		t.Fatalf("reader: live fields did not round-trip: %+v", got[0])
	}
}
`)
}
