package gotable

import (
	"bytes"
	"strings"
	"testing"
)

// accessorDescriptorSchema declares one block (a fixed table) and one cook (a
// variable table with a pointer edge), so both reading tiers are exercised:
// the block's projection record, and the cook's node record with its pointer
// SLOT.
const accessorDescriptorSchema = `package probe
fixed table Block
{
    id   uint32
    flag bool
}
table Node
{
    value int32
    next  *Node
}
`

// accessorDescriptorTest reads every field of both records TWICE — once
// through the generated accessor the declaration named, once through the
// descriptor's own offset — and refuses when the two spellings of one layout
// disagree. The pointer slot is held separately, because its position is what
// a self-relative delta is relative to.
const accessorDescriptorTest = `package probe
import (
    "testing"
    "unsafe"
)

func TestAccessorDescriptorAgreement(t *testing.T) {
    // ---- the BLOCK projection, accessor against descriptor ----
    var storage BlockBlockStorage
    if !storage.Create(TableBlockDefaultAllocator()) {
        t.Fatal("block storage")
    }
    defer storage.Destroy()
    var blk BlockBlock
    if !BlockBlockBegin(&blk, &storage, BlockCounts{}, nil) {
        t.Fatal("block begin")
    }
    blk.Projection.Id = 0x11223344
    blk.Projection.Flag = true

    for i := range blk.Type().Fields {
        f := &blk.Type().Fields[i]
        switch f.Name {
        case "id":
            if unsafe.Offsetof(BlockBlockProjection{}.Id) != uintptr(f.Offset) {
                t.Fatalf("block.%s: the accessor's offset is not the descriptor's", f.Name)
            }
            if via := *(*uint32)(unsafe.Add(blk.Base, uintptr(f.Offset))); blk.Projection.Id != via {
                t.Fatalf("block.%s: the accessor and the descriptor disagree about the value", f.Name)
            }
        case "flag":
            if unsafe.Offsetof(BlockBlockProjection{}.Flag) != uintptr(f.Offset) {
                t.Fatalf("block.%s: the accessor's offset is not the descriptor's", f.Name)
            }
            if via := *(*byte)(unsafe.Add(blk.Base, uintptr(f.Offset))) != 0; blk.Projection.Flag != via {
                t.Fatalf("block.%s: the accessor and the descriptor disagree about the value", f.Name)
            }
        }
    }

    // ---- the COOK node, accessor against descriptor, and its pointer SLOT ----
    var node Node
    node.Value = 7
    node.Next = 0
    cookBytes := make([]byte, NodeCookMeasure(&node))
    if !NodeCookFrom(&node, cookBytes, TableByteOrderLittle) {
        t.Fatal("cook from")
    }
    var cook NodeCook
    if !NodeOpen(&cook, unsafe.Pointer(&cookBytes[0]), int64(len(cookBytes))) {
        t.Fatal("cook open")
    }
    row := cook.Root()
    for i := range cook.Type().Fields {
        f := &cook.Type().Fields[i]
        switch f.Name {
        case "value":
            if unsafe.Offsetof(NodeRow{}.Value) != uintptr(f.Offset) {
                t.Fatalf("cook.%s: the accessor's offset is not the descriptor's", f.Name)
            }
            if via := *(*int32)(unsafe.Add(unsafe.Pointer(row), uintptr(f.Offset))); row.Value != via {
                t.Fatalf("cook.%s: the accessor and the descriptor disagree about the value", f.Name)
            }
        case "next":
            if unsafe.Offsetof(NodeRow{}.Next) != uintptr(f.Offset) {
                t.Fatalf("cook.%s: the slot accessor's offset is not the descriptor's", f.Name)
            }
            if via := *(*int64)(unsafe.Add(unsafe.Pointer(row), uintptr(f.Offset))); row.Next != via {
                t.Fatalf("cook.%s: the accessor and the descriptor disagree about the delta", f.Name)
            }
        }
    }
}
`

// TestAccessorDescriptorAgreement is the Go half of the J1 technique
// (docs/PORTING.md, schema#421). The generated ACCESSOR and the generated
// DESCRIPTOR are two independent derivations of one layout, and a reading tier
// that only ever walks the descriptors could read the descriptors twice and
// never know. This reads both ways and requires agreement, so a moved
// accessor or a moved descriptor offset is seen without a pinned dump that
// happens to cover the field.
func TestAccessorDescriptorAgreement(t *testing.T) {
	runGenerated(t, accessorDescriptorSchema, accessorDescriptorTest)
}

// TestAccessorDescriptorAgreementScalarNegativeControl moves a generated
// block scalar's descriptor offset four bytes and requires the gate to go red.
// Without it the accessor half could be reading the descriptors twice and
// nobody would know.
func TestAccessorDescriptorAgreementScalarNegativeControl(t *testing.T) {
	out, err := runGeneratedEdited(t, accessorDescriptorSchema, accessorDescriptorTest, func(files map[string][]byte) {
		patch(t, files,
			`Name: "id", Offset: 24,`,
			`Name: "id", Offset: 28, /* SABOTAGED */`)
	})
	if err == nil {
		t.Fatalf("NEGATIVE CONTROL FAILED: a block scalar offset four bytes off left the gate GREEN\n%s", out)
	}
	if !bytes.Contains(out, []byte("block.id: the accessor's offset is not the descriptor's")) {
		t.Fatalf("NEGATIVE CONTROL FAILED: the gate went red, but not on the accessor/descriptor disagreement\n%s", out)
	}
}

// TestAccessorDescriptorAgreementSlotNegativeControl moves a generated cook
// pointer slot's own offset eight bytes — the position a self-relative delta
// is relative to (§6.3) — and requires the gate to go red on the slot.
func TestAccessorDescriptorAgreementSlotNegativeControl(t *testing.T) {
	out, err := runGeneratedEdited(t, accessorDescriptorSchema, accessorDescriptorTest, func(files map[string][]byte) {
		patch(t, files,
			`Name: "next", Offset: 8,`,
			`Name: "next", Offset: 16, /* SABOTAGED */`)
	})
	if err == nil {
		t.Fatalf("NEGATIVE CONTROL FAILED: a pointer slot eight bytes off left the gate GREEN\n%s", out)
	}
	if !bytes.Contains(out, []byte("cook.next: the slot accessor's offset is not the descriptor's")) {
		t.Fatalf("NEGATIVE CONTROL FAILED: the gate went red, but not on the pointer slot\n%s", out)
	}
}

// patch replaces one substring across every generated file and refuses a
// control whose sabotage patched nothing.
func patch(t *testing.T, files map[string][]byte, old, new string) {
	t.Helper()
	n := 0
	for name, data := range files {
		s := string(data)
		if !strings.Contains(s, old) {
			continue
		}
		files[name] = []byte(strings.ReplaceAll(s, old, new))
		n++
	}
	if n == 0 {
		t.Fatalf("NEGATIVE CONTROL FAILED: the sabotage %q patched nothing", old)
	}
}
