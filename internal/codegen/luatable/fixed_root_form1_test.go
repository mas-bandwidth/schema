package luatable

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
)

// A FIXED-CLASS ROOT'S FILE IS FORM 3 (docs/SPEC-TABLES.md §3.4): the engine
// refuses a form-1 file at it as previous_form with nothing decoded
// (internal/tablewire/fixed.go decodeFixed). The generated Go and Lua strict
// reads must hold the same rule — one read, the whole value or a refusal —
// so a fixed root never reads a form-1 file as a value, while a variable root
// stays on form 1.
//
// This is a unit test. It reads the generated source. It does not run the Lua
// interpreter, take real time, open a socket, or use the network.
const fixedRootForm1Schema = `package probe

fixed table Point
{
    x int32
    y int32
}

table Note
{
    label string(8)
}
`

// luaFormOneHolds is the rule over the generated Lua runtime: the strict read
// (load.lua Load and refusal.lua ReadStrict) refuses a form-1 file at a fixed
// class as previous_form.
func luaFormOneHolds(files map[string][]byte) error {
	loadSrc := string(files["load.lua"])
	refuseSrc := string(files["refusal.lua"])
	if !strings.Contains(loadSrc, "not desc.variable") || !strings.Contains(loadSrc, "\"previous_form\"") {
		return fmt.Errorf("load.lua does not refuse a form-1 file at a fixed root as previous_form")
	}
	if !strings.Contains(refuseSrc, "if fixedclass and form == 1 then refuse(\"previous_form\"") {
		return fmt.Errorf("refusal.lua does not refuse form 1 at a fixed class as previous_form")
	}
	return nil
}

// goRootLoadFn extracts the emitted <Root>Load function body from the
// generated Go artifact.
func goRootLoadFn(files map[string][]byte, root string) string {
	for _, body := range files {
		src := string(body)
		i := strings.Index(src, "\nfunc "+root+"Load(")
		if i < 0 {
			continue
		}
		rest := src[i+1:]
		if j := strings.Index(rest[1:], "\nfunc "); j >= 0 {
			return rest[:1+j]
		}
		return rest
	}
	return ""
}

// goFixedLoadFnHolds is the rule over one generated fixed root's Load: it
// refuses a form-1 file as previous_form before it decodes anything.
func goFixedLoadFnHolds(fn, root string) error {
	if !strings.Contains(fn, "TableRefusePreviousForm") {
		return fmt.Errorf("%sLoad does not refuse a form-1 file as previous_form", root)
	}
	if !strings.Contains(fn, "data[0] == 1") {
		return fmt.Errorf("%sLoad does not guard the form byte for form 1", root)
	}
	return nil
}

// goVariableLoadFnStaysFormOne is the rule over one generated variable root's
// Load: it must not refuse form 1, which is its own file form.
func goVariableLoadFnStaysFormOne(fn, root string) error {
	if strings.Contains(fn, "TableRefusePreviousForm") {
		return fmt.Errorf("variable root %s wrongly refuses a form-1 file as previous_form", root)
	}
	return nil
}

func TestFixedRootForm1RefusedLuaAndGo(t *testing.T) {
	t.Parallel()
	u, files := declaredFormsUnit(t, fixedRootForm1Schema)

	// Generated Lua refuses a form-1 file at a fixed root.
	if err := luaFormOneHolds(files); err != nil {
		t.Fatal(err)
	}

	// Generated Go: the fixed root refuses form 1; the variable root stays on
	// form 1.
	gofiles, err := gotable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	point := goRootLoadFn(gofiles, "Point")
	if point == "" {
		t.Fatal("no generated PointLoad function")
	}
	if err := goFixedLoadFnHolds(point, "Point"); err != nil {
		t.Fatal(err)
	}
	note := goRootLoadFn(gofiles, "Note")
	if note == "" {
		t.Fatal("no generated NoteLoad function")
	}
	if err := goVariableLoadFnStaysFormOne(note, "Note"); err != nil {
		t.Fatal(err)
	}
}

// TestFixedRootForm1RefusedLuaAndGoNegativeControl plants the form-1 read fault
// back into the real generated artifacts — the emitted fixed root's Load and
// the emitted load.lua — and requires the same rule to fail. A control that
// does not touch those artifacts proves nothing.
func TestFixedRootForm1RefusedLuaAndGoNegativeControl(t *testing.T) {
	t.Parallel()
	u, files := declaredFormsUnit(t, fixedRootForm1Schema)
	if err := luaFormOneHolds(files); err != nil {
		t.Fatalf("the real Lua artifact does not hold the rule, so the control has nothing to break: %v", err)
	}
	gofiles, err := gotable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	point := goRootLoadFn(gofiles, "Point")
	if point == "" {
		t.Fatal("no generated PointLoad function")
	}
	if err := goFixedLoadFnHolds(point, "Point"); err != nil {
		t.Fatalf("the real Go artifact does not hold the rule, so the control has nothing to break: %v", err)
	}

	// THE PLANTED GO FAULT: the fixed root's Load no longer refuses form 1.
	goPlanted := strings.Replace(point, "data[0] == 1", "len(data) < 0", 1)
	if goPlanted == point {
		t.Fatal("the negative control planted no Go fault")
	}
	if err := goFixedLoadFnHolds(goPlanted, "Point"); err == nil {
		t.Fatal("negative control passed: a PointLoad without the form-1 guard still held the rule")
	}

	// THE PLANTED LUA FAULT: load.lua no longer refuses form 1 at a fixed root.
	luaPlanted := strings.Replace(string(files["load.lua"]), "not desc.variable", "false", 1)
	if luaPlanted == string(files["load.lua"]) {
		t.Fatal("the negative control planted no Lua fault")
	}
	files["load.lua"] = []byte(luaPlanted)
	if err := luaFormOneHolds(files); err == nil {
		t.Fatal("negative control passed: a load.lua without the form-1 guard still held the rule")
	}
}
