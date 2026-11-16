package compiler

import (
	"strings"
	"testing"
)

func TestPythonSurfaceOptionsDriver(t *testing.T) {
	u := unitFromSource(t, "package probe\nfixed table Root { value int32 }\n")
	c := New()
	full, err := c.Generate(u, "python", nil)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := c.Generate(u, "python", Options{"surface": "wire"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(wire["probe/Probe.py"]), "Root.block = None") {
		t.Fatal("wire-only emitted a block descriptor")
	}
	if !strings.Contains(string(full["probe/Probe.py"]), "Root.block = BlockTable(") {
		t.Fatal("default omitted the block descriptor")
	}
	if _, err := c.Generate(u, "python", Options{"surface": "wire,unknown"}); err == nil || !strings.Contains(err.Error(), "unknown surface") {
		t.Fatalf("unknown surface: %v", err)
	}
}

func TestPythonSurfaceOptionsFormsRefusal(t *testing.T) {
	u := unitFromSource(t, "package probe\ntable Root | forms = \"wire,json\" { value int32 }\n")
	_, err := New().Generate(u, "python", Options{"surface": "wire,cook"})
	if err == nil || !strings.Contains(err.Error(), "Root") || !strings.Contains(err.Error(), "cook") || !strings.Contains(err.Error(), "wire,json") {
		t.Fatalf("forms refusal: %v", err)
	}
}
