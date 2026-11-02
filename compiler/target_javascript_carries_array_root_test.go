package compiler

import "testing"

func init() { nonGoCarriers["js"] = append(nonGoCarriers["js"], carryArrayRoot) }

func TestJavaScriptCarriesArrayRoot(t *testing.T) {
	const src = `package probe
 table Goal { x int32 }
 table GoalList | json = "array" { goals []Goal }
 table Holder { group GoalList }
`
	u := unitFromSource(t, src)
	files, err := New().Generate(u, "js", nil)
	if err != nil || len(files) == 0 {
		t.Fatalf("public js array-root generation: files=%d err=%v", len(files), err)
	}
	if !carries("js", carryArrayRoot) {
		t.Fatal("missing JS array-root registration")
	}
}
