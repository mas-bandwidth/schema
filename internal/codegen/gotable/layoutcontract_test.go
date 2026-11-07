package gotable

import (
	"bytes"
	"regexp"
	"testing"
)

// layoutContractSchema declares one block whose projection carries a field
// after the one the test widens, so the widening moves an offset and the size.
const layoutContractSchema = `package probe
fixed table Frame
{
    id   uint32
    flag bool
    tick uint16
}
`

// layoutContractTest only has to load the package: the layout contract runs
// in the unit's init, before any test, and refuses by panicking there.
const layoutContractTest = `package probe
import "testing"

func TestLoads(t *testing.T) {
    if FrameBlockMaxBytes <= 0 {
        t.Fatal("the block's storage bound is not positive")
    }
}
`

var frameIDField = regexp.MustCompile(`(?m)^(\tID\s+)uint32$`)

// TestBlockLayoutContractRefusesADisagreeingRuntime is the Go cell of M11
// (docs/PORTING.md): the generated init holds the block projection's size,
// alignment and every field offset against the numbers the compiler derived
// (docs/SPEC-TABLES.md §19.3), so a runtime whose struct disagrees with the
// model is refused before any row is read. Both halves: the unit as emitted
// loads, and the same unit with one field widened in its struct — a producer
// whose layout is not the compiler's — stops at init naming the field.
func TestBlockLayoutContractRefusesADisagreeingRuntime(t *testing.T) {
	if out, err := runGeneratedEdited(t, layoutContractSchema, layoutContractTest, nil); err != nil {
		t.Fatalf("the unit as emitted does not load: %v\n%s", err, out)
	}
	out, err := runGeneratedEdited(t, layoutContractSchema, layoutContractTest, func(files map[string][]byte) {
		n := 0
		for name, data := range files {
			if bytes.Contains(data, []byte("type FrameBlockProjection struct")) {
				files[name] = frameIDField.ReplaceAll(data, []byte("${1}uint64"))
				if !bytes.Equal(files[name], data) {
					n++
				}
			}
		}
		if n != 1 {
			t.Fatalf("the projection's id field was widened in %d files, not one", n)
		}
	})
	if err == nil {
		t.Fatalf("a runtime whose layout disagrees with the compiler's model was not refused at init\n%s", out)
	}
	if !bytes.Contains(out, []byte("schema block layout: FrameBlockProjection")) {
		t.Fatalf("a runtime whose layout disagrees with the compiler's model failed, but not on the layout contract\n%s", out)
	}
}
