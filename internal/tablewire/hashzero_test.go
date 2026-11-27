package tablewire_test

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"testing"

	"github.com/mas-bandwidth/schema/v3/compiler"
	"github.com/mas-bandwidth/schema/v3/internal/tabletext"
	"github.com/mas-bandwidth/schema/v3/internal/tablewire"
)

// A reference's position and the hash it names are different (§3, §19).
// This is the exact ArrayRoot wirefuzz witness: field reference 1 names hash 0.
// No source name with that hash is needed to read an unknown field.
func TestHashZeroIDIsOrdinary(t *testing.T) {
	paths, err := compiler.GatherPaths([]string{"../../testdata/conformance/feature-witnesses/full/Features.schema"})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(u)
	exact, err := hex.DecodeString("01010e0e04030300000011000000fbffffff0000000000000000000100000000000000")
	if err != nil {
		t.Fatal(err)
	}
	file := func(body []byte) []byte {
		wire := append([]byte{1}, body...)
		wire = binary.LittleEndian.AppendUint64(wire, 0) // one ordinary hash-zero ID
		return binary.LittleEndian.AppendUint64(wire, 1)
	}
	empty := binary.LittleEndian.AppendUint64([]byte{1, 0}, 0)
	for _, tc := range []struct {
		name    string
		wire    []byte
		unknown int
	}{
		{"exact-array-mutant", exact, 1},
		{"enum-reference-zero-None", file([]byte{1, 30, 0, 0}), 1},
		{"union-reference-zero-None", file([]byte{1, 15, 0, 0}), 1},
		{"body-reference-zero-terminator", empty, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, region := range []bool{false, true} {
				for _, retaining := range []bool{false, true} {
					inst := m.New(m.Lookup("ArrayRoot"))
					rt := &tablewire.Retain{Capacity: 4096, IdCapacity: 32}
					var r tabletext.Report
					var ok bool
					var err error
					switch {
					case region && retaining:
						ok, err = tablewire.DecodeRegionRetain(m, inst, tc.wire, rt, &r)
					case retaining:
						ok, err = tablewire.DecodeRetain(m, inst, tc.wire, rt, &r)
					case region:
						ok, err = tablewire.DecodeRegion(m, inst, tc.wire, &r)
					default:
						ok, err = tablewire.Decode(m, inst, tc.wire, &r)
					}
					if !ok || err != nil || r.Refused || r.Unknown != tc.unknown || r.RetainLost != 0 {
						t.Fatalf("region=%v retain=%v: ok=%v err=%v report=%+v", region, retaining, ok, err, r)
					}
					var saved []byte
					want := empty
					if retaining {
						var sr tabletext.Report
						saved, err = tablewire.EncodeRetain(m, inst, rt, &sr)
						if r.Retained != tc.unknown || sr.RetainLost != 0 {
							t.Fatalf("load=%+v save=%+v", r, sr)
						}
						want = tc.wire
					} else {
						saved, err = tablewire.Encode(m, inst)
					}
					if err != nil || !bytes.Equal(saved, want) {
						t.Fatalf("region=%v retain=%v: save=%x want=%x err=%v", region, retaining, saved, want, err)
					}
				}
			}
		})
	}
}
