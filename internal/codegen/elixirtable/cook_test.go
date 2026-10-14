package elixirtable

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestElixirCookOptionalTextReadsItsLength(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		ft         ir.FieldType
		wantPieces [3]int64
	}{
		{"bytes8", ir.FieldType{Kind: ir.TBytes, Size: 8, Optional: true}, [3]int64{0, 8, 12}},
		{"string8", ir.FieldType{Kind: ir.TString, Size: 8, Optional: true}, [3]int64{0, 12, 16}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := &ir.Struct{
				Name:          "Root",
				IsTable:       true,
				FixedDeclared: true,
				Fields: []*ir.Field{
					{
						Name: "label",
						Type: tc.ft,
					},
				},
			}
			u := &ir.Unit{
				Package: "probe",
				Tables:  map[string]*ir.Struct{"Root": st},
				Files: []*ir.File{
					{
						Base:   "Probe",
						Tables: []*ir.Struct{st},
					},
				},
			}
			rl := ir.RecordLayout(u, st)
			if len(rl.Fields) != 1 {
				t.Fatalf("fields = %d, want 1", len(rl.Fields))
			}
			fl := rl.Fields[0]
			pieces := ir.BlockFieldPieceOffsets(u, fl.Field, fl.Offset, false)
			if len(pieces) != 3 ||
				pieces[0].Offset != tc.wantPieces[0] ||
				pieces[1].Offset != tc.wantPieces[1] ||
				pieces[2].Offset != tc.wantPieces[2] {
				t.Fatalf("pieces = %+v, want offsets %v", pieces, tc.wantPieces)
			}

			out, err := Generate(u)
			if err != nil {
				t.Fatalf("Generate: %v", err)
			}
			cookSrc := findGenerated(t, out, "def root_node_label(")
			wantLen := fmt.Sprintf("C.i32(region, node + %d)", tc.wantPieces[1])
			badPres := fmt.Sprintf("C.i32(region, node + %d)", tc.wantPieces[2])
			if !strings.Contains(cookSrc, wantLen) {
				t.Errorf("generated cook source does not read length %s:\n%s", wantLen, cookSrc)
			}
			if strings.Contains(cookSrc, badPres) {
				t.Errorf("generated cook source still reads presence byte %s as length:\n%s", badPres, cookSrc)
			}
		})
	}
}
