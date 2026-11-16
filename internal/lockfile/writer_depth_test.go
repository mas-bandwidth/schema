package lockfile

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestWriterSnapshotDepthBoundary(t *testing.T) {
	t.Parallel()
	for _, depth := range []int{64, 65} {
		t.Run(fmt.Sprint(depth), func(t *testing.T) {
			t.Parallel()
			leaf := &ir.Struct{Name: "Leaf", Fields: []*ir.Field{{Name: "n", Type: ir.FieldType{Kind: ir.TInt, Signed: true, Width: 32}}}}
			for i := range depth - 2 {
				leaf = &ir.Struct{Name: fmt.Sprintf("Layer%d", i), Fields: []*ir.Field{{Name: "value", Type: ir.FieldType{Kind: ir.TNamed, Name: leaf.Name, Ref: leaf}}}}
			}
			root := &ir.Struct{Name: "Row", IsTable: true, FixedDeclared: true, Fields: []*ir.Field{{Name: "value", Type: ir.FieldType{Kind: ir.TNamed, Name: leaf.Name, Ref: leaf}}}}
			entries := ir.TableFixedWalkRoot(root)
			layout := ir.TableFixedLayoutBytes(entries)
			if got := ir.TableFixedLayoutDepth(layout); got != depth {
				t.Fatalf("fixture layout depth %d, want %d", got, depth)
			}
			restored, err := parseWriter(writerBytes(root), len(entries))
			if depth == 64 {
				if err != nil || restored == nil {
					t.Fatalf("lawful depth64 snapshot refused: %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "over-depth") {
				t.Fatalf("depth65 must refuse before IR layout recomputation: %v", err)
			}
		})
	}
}
