package ir_test

// THE JAVASCRIPT FIXED CLOSURE (ir/jsfixedclosure.go): which module DEFINES a
// union's table-arm class. A union a fixed root holds is the fixed home's; a
// union only a variable-form root holds is the variable home's, even when one of
// its arms is itself a fixed table (Stream's Frame holds the fixed Header and the
// pointered Chunk, and its class constructs StreamdemoVariable.js's of both).

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

const jsFixedClosureSource = `package demo

fixed table Login
{
    id uint32
}

union Payload
{
    login Login
}

fixed table Envelope
{
    payload Payload
}

table Node
{
    next *Node
}

union Frame
{
    login Login
    node  Node
}

table Feed
{
    frame Frame
}
`

func TestJSTableArmModuleNamesTheDefiningHome(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, jsFixedClosureSource)

	if got := ir.JSTableRuntimeHome(u); got != "Demo" {
		t.Fatalf("JSTableRuntimeHome = %q, want Demo", got)
	}

	var roots []string
	for _, f := range u.Files {
		for _, st := range ir.JSTableFixedRoots(f.Tables) {
			roots = append(roots, st.Name)
		}
	}
	if len(roots) != 2 || roots[0] != "Login" || roots[1] != "Envelope" {
		t.Fatalf("JSTableFixedRoots = %v, want [Login Envelope]", roots)
	}
	if why := ir.JSTableFixedRefusal(u.Tables["Feed"]); why == "" {
		t.Fatal("Feed reaches a pointer through its union and still carries the fixed form")
	}

	structs, unions := ir.JSTableFixedClosure(u)
	if len(structs) != 2 || !unions["Payload"] || unions["Frame"] {
		t.Fatalf("JSTableFixedClosure = %d structs, unions %v; want Login and Envelope, Payload alone", len(structs), unions)
	}

	for name, want := range map[string]string{"Payload": "DemoTable", "Frame": "DemoVariable"} {
		un := u.TableUnions[name]
		if un == nil {
			t.Fatalf("%s is not a table-closure union", name)
		}
		if got := ir.JSTableArmModule(u, un); got != want {
			t.Errorf("JSTableArmModule(%s) = %q, want %q", name, got, want)
		}
	}
}
