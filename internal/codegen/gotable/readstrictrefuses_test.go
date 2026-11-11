package gotable

import "testing"

func TestGoReadStrictRefusesBeforeAllocating(t *testing.T) {
	t.Parallel()
	runGeneratedTables(t, oneReadSchema, readStrictRefusesBeforeAllocating)
}

const readStrictRefusesBeforeAllocating = `package probe
import("encoding/base64";"testing")

func validWire(t *testing.T) []byte {
	t.Helper()
	var b NodeBuilder
	if !b.Init() { t.Fatal("builder init") }
	defer b.Shutdown()
	b.GetRoot().N = 7
	size := NodeMeasure(b.GetRoot(), &b.Arena)
	if size < 0 { t.Fatal("measure refused") }
	wire := make([]byte, size)
	if NodeSave(b.GetRoot(), wire, &b.Arena) != size { t.Fatal("save") }
	return wire
}

func TestReadStrictRefusesBeforeAllocating(t *testing.T) {
	wires := []struct { name string; wire []byte }{
		{"empty", []byte{}},
		{"one_byte", []byte{1}},
		{"unknown_form", []byte{2}},
	}
	for _, c := range wires {
		wire := c.wire
		if value, err := NodeReadStrict(wire); err == nil || value != nil {
			t.Fatalf("%s: NodeReadStrict returned a value", c.name)
		}
		if allocs := testing.AllocsPerRun(100, func() {
			if value, err := NodeReadStrict(wire); err == nil || value != nil { panic("ReadStrict did not refuse") }
		}); allocs != 0 {
			t.Fatalf("%s: NodeReadStrict allocated %v times", c.name, allocs)
		}
		text := base64.StdEncoding.EncodeToString(wire)
		if value, err := NodeLoadText(text, nil); err == nil || value != nil {
			t.Fatalf("%s: NodeLoadText returned a value", c.name)
		}
		if allocs := testing.AllocsPerRun(100, func() {
			if value, err := NodeLoadText(text, nil); err == nil || value != nil { panic("LoadText did not refuse") }
		}); allocs != 0 {
			t.Fatalf("%s: NodeLoadText allocated %v times", c.name, allocs)
		}
	}
	wire := validWire(t)
	if value, err := NodeReadStrict(wire); err != nil || value == nil || value.N != 7 {
		t.Fatalf("ReadStrict valid wire: %v %v", value, err)
	}
	if value, err := NodeLoadText(base64.StdEncoding.EncodeToString(wire), nil); err != nil || value == nil || value.N != 7 {
		t.Fatalf("LoadText valid wire: %v %v", value, err)
	}
}
`
