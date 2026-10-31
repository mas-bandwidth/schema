// The Squirrel target refuses every construct its table codec does not carry
// BY NAME (docs/SPEC-TABLES.md §11, docs/PORT-SQUIRREL.md): each refusal names
// the field and the target, so a schema author is told which port lacks what.
//
// This test reads the target's refusal chain from its SOURCE
// (compiler/target_squirrel.go) and runs that chain: every refuser the
// target's Generate calls, with refuseUnported opened into its own, must have
// a construct here it refuses naming the field and squirrel. The same
// constructs also go through Generate("squirrel") whole.
package compiler

import (
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// refusersCalled is the names of the refusers fn calls as `if err := f(u,
// target); err != nil` or `return f(u, target)`, in source order.
func refusersCalled(t *testing.T, fn *ast.FuncDecl) []string {
	t.Helper()
	var names []string
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) != 2 {
			return true
		}
		switch f := call.Fun.(type) {
		case *ast.Ident:
			if strings.HasPrefix(f.Name, "refuse") {
				names = append(names, f.Name)
			}
		case *ast.SelectorExpr:
			if x, ok := f.X.(*ast.Ident); ok && x.Name == "ir" && strings.HasPrefix(f.Sel.Name, "Refuse") {
				names = append(names, "ir."+f.Sel.Name)
			}
		}
		return true
	})
	return names
}

func funcDecl(t *testing.T, path, recv, name string) *ast.FuncDecl {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range file.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Name.Name != name {
			continue
		}
		if recv == "" && fn.Recv == nil {
			return fn
		}
		if recv != "" && fn.Recv != nil && len(fn.Recv.List) == 1 {
			if id, ok := fn.Recv.List[0].Type.(*ast.Ident); ok && id.Name == recv {
				return fn
			}
		}
	}
	t.Fatalf("%s declares no %s.%s", path, recv, name)
	return nil
}

func TestSquirrelRefusesUnportedConstructsByName(t *testing.T) {
	t.Parallel()
	refusers := map[string]func(*ir.Unit, string) error{
		"refuseWideText":       refuseWideText,
		"refuseUnported":       refuseUnported,
		"refuseOptionalArrays": refuseOptionalArrays,
		"refuseMaps":           refuseMaps,
		"refuseLists":          refuseLists,
		"refuseValueDefaults":  refuseValueDefaults,
		"refuseWasRows":        refuseWasRows,
		"refuseTableArms":      refuseTableArms,
		"refuseVoidArms":       refuseVoidArms,
		"refuseUnionArrays":    refuseUnionArrays,
		"refuseBytes":          refuseBytes,
		"refuseStrings":        refuseStrings,
	}
	// the target's chain, from its source, with refuseUnported opened up
	top := refusersCalled(t, funcDecl(t, "target_squirrel.go", "squirrelTarget", "Generate"))
	inner := refusersCalled(t, funcDecl(t, "builtin.go", "", "refuseUnported"))
	if len(top) == 0 || !slices.Contains(top, "refuseUnported") || len(inner) == 0 {
		t.Fatalf("the squirrel chain did not parse: target %v, refuseUnported %v", top, inner)
	}
	var leaves []string
	for _, name := range top {
		if name == "refuseUnported" {
			leaves = append(leaves, inner...)
			continue
		}
		leaves = append(leaves, name)
	}
	for _, name := range append(top, inner...) {
		if refusers[name] == nil {
			t.Fatalf("compiler/target_squirrel.go calls refuser %s, which this test has no case for: add one", name)
		}
	}
	chain := func(u *ir.Unit) error {
		for _, name := range top {
			if err := refusers[name](u, "squirrel"); err != nil {
				return err
			}
		}
		return nil
	}

	cases := []refusalRow{
		// this construct is LIFTED (compiler/target_squirrel.go init): the
		// target carries wstring(N) on the packet wire and as table kind 33,
		// so the refuser accepts squirrel (SPEC §4.12, docs/SPEC-TABLES.md §3).
		{"refuseWideText", "wide text", "package probe\n\ntable L\n{\n    w wstring(8)\n}\n", "L.w", true},
		// this construct is LIFTED (compiler/target_squirrel_blobs.go): the
		// tagged target carries the byte and string buffers, so the refusers
		// accept squirrel and still refuse a target that has not registered
		// (docs/SPEC-TABLES.md §2.5).
		{"refuseBytes", "byte buffer", "package probe\n\ntable L\n{\n    b *bytes\n}\n", "L.b", true},
		{"refuseStrings", "string buffer", "package probe\n\ntable L\n{\n    s *string\n}\n", "L.s", true},
		{refuser: "refuseMaps", name: "map", src: "package probe\n\ntable L\n{\n    index map[string(8)]int32\n}\n", field: "L.index", carried: true},
		// this construct is LIFTED (compiler/target_squirrel_lists.go): the
		// tagged target carries it, so the refuser accepts squirrel and still
		// refuses a target that has not registered (docs/SPEC-TABLES.md §2.9).
		{"refuseLists", "unbounded array", "package probe\n\ntable L\n{\n    repos []int32\n}\n", "L.repos", true},
		{"refuseTableArms", "union table arm", "package probe\n\ntype Inner\n{\n    x int32\n}\nunion Pick\n{\n    alpha Inner\n    beta int32\n}\ntable L\n{\n    u Pick\n}\n", "Pick", true},
		{"refuseValueDefaults", "value default", "package probe\n\ntable L\n{\n    s string(8) = \"hi\"\n}\n", "L.s", false},
		{"refuseWasRows", "was on a variant", "package probe\n\nenum Grade\n{\n    Bronze,\n    Argent | was = \"Silver\"\n}\n\nfixed table L\n{\n    grade Grade\n}\n", "Grade.Argent", false},
		{"refuseVoidArms", "payload-free arm", "package probe\n\ntype Fire\n{\n    target uint16\n}\n\nunion Weapon\n{\n    laser Fire\n    ram\n}\n\ntable L\n{\n    fire Weapon\n}\n", "Weapon", true},
		{"refuseUnionArrays", "array of unions", "package probe\n\ntype Ping\n{\n    nonce uint32\n}\n\ntype Pong\n{\n    nonce uint32\n}\n\nunion Reply\n{\n    ping Ping\n    pong Pong\n}\n\nfixed table L\n{\n    history [..4]Reply\n}\n", "L.history", true},
	}
	covered := map[string]bool{}
	named := func(tc string, err error, field string) {
		t.Helper()
		if err == nil {
			t.Errorf("%s: squirrel accepted the construct", tc)
			return
		}
		if msg := err.Error(); !strings.Contains(msg, field) || !strings.Contains(msg, "squirrel") {
			t.Errorf("%s: refusal does not name the construct %s and the target squirrel: %s", tc, field, msg)
		}
	}
	var c *Compiler
	for _, g := range builtins() {
		if slices.Contains(g.Names(), "squirrel") {
			c = New() // the tagged build: the target is registered
		}
	}
	for _, tc := range refusalTable(cases) {
		u := unitFromSource(t, tc.src)
		// docs/SPEC-TABLES.md §5: the tagged build registers squirrel as a
		// carrier of the three `was` rows (target_squirrel_was.go), so the row
		// literal's `carried` is read off that registration, not hand-set.
		// The value default (docs/SPEC-TABLES.md §4, §16.2) is read the same way off
		// valueDefaultTargets, which compiler/target_squirrel_defaults.go
		// (tagged) appends squirrel to.
		if wasCarried := (tc.refuser == "refuseWasRows" && slices.Contains(wasRowTargets, "squirrel")) || (tc.refuser == "refuseValueDefaults" && slices.Contains(valueDefaultTargets, "squirrel")); tc.carried || wasCarried {
			// a construct the Squirrel table codec now carries is accepted, not
			// refused: the refuser's nil means it rides (docs/SPEC-TABLES.md
			// §2.6, §2.8, §2.9). The registration is tagged for some constructs,
			// so the check follows the build; the refuser still refuses a target
			// that has not registered.
			registered := true
			if tc.refuser == "refuseLists" {
				registered = slices.Contains(listTargets, "squirrel")
			}
			if tc.refuser == "refuseBytes" {
				registered = slices.Contains(blobTargets, "squirrel")
			}
			if tc.refuser == "refuseStrings" {
				registered = slices.Contains(stringTargets, "squirrel")
			}
			if slices.Contains([]string{"refuseTableArms", "refuseVoidArms", "refuseUnionArrays"}, tc.refuser) {
				registered = c != nil
			}
			if registered {
				if err := refusers[tc.refuser](u, "squirrel"); err != nil {
					t.Errorf("%s: squirrel refused a construct it now carries: %v", tc.name+" ("+tc.refuser+")", err)
				}
				if err := chain(u); err != nil {
					t.Errorf("%s (the squirrel chain): squirrel refused a construct it now carries: %v", tc.name, err)
				}
				if c != nil {
					if _, err := c.Generate(u, "squirrel", nil); err != nil {
						t.Errorf("%s (Generate squirrel): squirrel refused a construct it now carries: %v", tc.name, err)
					}
				}
				if err := refusers[tc.refuser](u, "nonesuch"); err == nil || !strings.Contains(err.Error(), "nonesuch") {
					t.Errorf("%s: an unregistered target was not refused by name: %v", tc.name, err)
				}
			} else {
				named(tc.name+" ("+tc.refuser+")", refusers[tc.refuser](u, "squirrel"), tc.field)
				named(tc.name+" (the squirrel chain)", chain(u), tc.field)
				if c != nil {
					_, err := c.Generate(u, "squirrel", nil)
					named(tc.name+" (Generate squirrel)", err, tc.field)
				}
			}
			// the refuser still refuses an unregistered target, by name
			if slices.Contains([]string{"refuseTableArms", "refuseVoidArms", "refuseUnionArrays"}, tc.refuser) {
				if err := refusers[tc.refuser](u, "unregistered-target"); err == nil || !strings.Contains(err.Error(), "unregistered-target") {
					t.Errorf("%s: an unregistered target was not refused by name: %v", tc.name+" ("+tc.refuser+")", err)
				}
			}
			covered[tc.refuser] = true
			continue
		}
		// the refuser this construct is for, alone
		named(tc.name+" ("+tc.refuser+")", refusers[tc.refuser](u, "squirrel"), tc.field)
		// the target's whole chain, in its order
		named(tc.name+" (the squirrel chain)", chain(u), tc.field)
		if c != nil {
			_, err := c.Generate(u, "squirrel", nil)
			named(tc.name+" (Generate squirrel)", err, tc.field)
		}
		covered[tc.refuser] = true
	}
	rseen := map[string]bool{}
	for _, tc := range refusalTable(cases) {
		if rseen[tc.refuser] {
			t.Errorf("refusal row for refuser %q is registered twice", tc.refuser)
		}
		rseen[tc.refuser] = true
	}
	for _, name := range leaves {
		if !covered[name] {
			t.Errorf("compiler/target_squirrel.go refuses through %s, and no construct here is refused by it", name)
		}
	}
	// THE WIDE TABLE KINDS (docs/SPEC-TABLES.md §3, §15, §16.2;
	// ir.WideTableKindScope): squirrel carries fixed-point and 128-bit
	// table-wire kinds, so the squirrel chain and Generate("squirrel")
	// accept a uint128 table field, while ir.RefuseWideTableKinds still
	// refuses an unported backend by name.
	wideUnit := unitFromSource(t, "package probe\n\ntable L\n{\n    big uint128\n}\n")
	if err := chain(wideUnit); err != nil {
		t.Errorf("wide kind (the squirrel chain): squirrel refused a construct it now carries: %v", err)
	}
	if c != nil {
		if _, err := c.Generate(wideUnit, "squirrel", nil); err != nil {
			t.Errorf("wide kind (Generate squirrel): squirrel refused a construct it now carries: %v", err)
		}
	}
	if err := ir.RefuseWideTableKinds(wideUnit, "unregistered"); err == nil || !strings.Contains(err.Error(), "L.big") || !strings.Contains(err.Error(), "unregistered") {
		t.Errorf("wide kind: ir.RefuseWideTableKinds did not refuse an unregistered backend by name: %v", err)
	}
	// THE LIFTED CONSTRUCTS (docs/SPEC-TABLES.md §2.13, §16.10): squirrel
	// carries the bounded text list and the array root now, so refuseTextForms
	// lets it through; an unregistered target is still refused by name. The
	// registration is tagged, so this half of the rule runs with the tag only.
	if carries("squirrel", carryTextList) && carries("squirrel", carryArrayRoot) {
		for _, tc := range []struct{ name, src string }{
			{"text list", "package probe\n\ntable L\n{\n    names [..4]string(8)\n}\n"},
			{"array root", "package probe\n\ntable Item\n{\n    x int32\n}\n\ntable L | json = \"array\"\n{\n    items [..4]Item\n}\n"},
		} {
			u := unitFromSource(t, tc.src)
			if err := refuseTextForms(u, "squirrel"); err != nil {
				t.Errorf("%s: squirrel carries the construct, but refuseTextForms refused it: %v", tc.name, err)
			}
			if err := refuseTextForms(u, "c"); err == nil || !strings.Contains(err.Error(), "c") {
				t.Errorf("%s: an unregistered target was not refused by name: %v", tc.name, err)
			}
		}
	}
	// The one-sided integer range is no longer one of the target's refusals
	// (docs/SPEC-TABLES.md §4.3): the tagged target carries it, while the
	// central refuser still refuses every target that is not registered for it.
	if c != nil {
		if !carries("squirrel", carryOneSidedRange) {
			t.Error("the tagged squirrel target does not register the one-sided integer range it carries")
		}
		oneSided := unitFromSource(t, "package probe\n\ntable OneSided\n{\n    a int64 | min = 0\n}\n")
		if _, err := c.Generate(oneSided, "squirrel", nil); err != nil {
			t.Errorf("squirrel refused the one-sided integer range it carries: %v", err)
		}
		for _, target := range c.Targets() {
			if carries(target, carryOneSidedRange) {
				continue
			}
			_, err := c.Generate(oneSided, target, nil)
			if err == nil {
				t.Errorf("%s accepted a one-sided integer range", target)
			} else if msg := err.Error(); !strings.Contains(msg, target) || !strings.Contains(msg, "one-sided") {
				t.Errorf("%s refusal does not name the target and the construct: %s", target, msg)
			}
		}
	}
	t.Logf("%d constructs over %d refusers %v; the tagged target %v", len(cases), len(leaves), leaves, c != nil)

	// The central optional-variable-closure gate (compiler/generate.go) is not
	// in the chain above: Squirrel carries `?T` over a variable closure now
	// (docs/SPEC-TABLES.md §2.3), and a target that does not register it still
	// refuses the construct by name.
	closure := unitFromSource(t, "package probe\ntable Leaf\n{\n    n int32\n}\ntable Inner\n{\n    child *Leaf\n}\ntable Outer\n{\n    inner ?Inner\n}\n")
	if c != nil {
		if _, err := c.Generate(closure, "squirrel", nil); err != nil {
			t.Errorf("squirrel still refuses a construct it carries: %v", err)
		}
	}
	refused := 0
	for _, target := range New().Targets() {
		if carries(target, carryOptionalVariableClosure) {
			continue
		}
		refused++
		if _, err := New().Generate(closure, target, nil); err == nil || !strings.Contains(err.Error(), "optional over a variable closure") {
			t.Errorf("%s does not register ?T over a variable closure and no longer refuses it by name: %v", target, err)
		}
	}
	if refused == 0 {
		t.Log("every built-in target carries ?T over a variable closure; the refusal half has no target to ask")
	}
}

// TestUnionArmRefusalsNameTheUnion pins the two table union-arm refusals to
// name the union and target (docs/SPEC-TABLES.md §11). Squirrel carries both
// forms now (compiler/target_squirrel_unions.go), so the refusers are read
// against an unregistered target, which they still refuse by name.
func TestUnionArmRefusalsNameTheUnion(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, src, field string
		refuse           func(*ir.Unit, string) error
	}{
		{
			"table arm",
			"package probe\n\ntype Inner\n{\n    x int32\n}\nunion Pick\n{\n    alpha Inner\n    beta int32\n}\ntable L\n{\n    u Pick\n}\n",
			"Pick",
			refuseTableArms,
		},
		{
			"payload-free arm",
			"package probe\n\ntype Fire\n{\n    target uint16\n}\n\nunion Weapon\n{\n    laser Fire\n    ram\n}\n\ntable L\n{\n    fire Weapon\n}\n",
			"Weapon",
			refuseVoidArms,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			u := unitFromSource(t, tc.src)
			err := tc.refuse(u, "unregistered-target")
			if err == nil {
				t.Fatalf("an unregistered target accepted the construct")
			}
			if msg := err.Error(); !strings.Contains(msg, tc.field) || !strings.Contains(msg, "unregistered-target") {
				t.Errorf("refusal does not name the union %s and the target unregistered-target: %s", tc.field, msg)
			}
		})
	}
}
