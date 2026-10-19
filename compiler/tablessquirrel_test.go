// The Squirrel target refuses every construct its table codec does not carry
// BY NAME (docs/SPEC-TABLES.md §11, docs/PORT-SQUIRREL.md): each refusal names
// the field and the target, so a schema author is told which port lacks what.
//
// The target itself is behind the schema_leg_squirrel tag
// (compiler/target_squirrel.go), so this test reads the target's refusal chain
// from its SOURCE and runs that chain, which needs no tag: every refuser the
// target's Generate calls, with refuseUnported opened into its own, must have
// a construct here it refuses naming the field and squirrel. Under the tag the
// same constructs also go through Generate("squirrel") whole.
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

// squirrelWasVariantProbe is the `was`-on-a-variant probe the refusal test and
// TestSquirrelCarriesWasRows share (docs/SPEC-TABLES.md §5). It is a
// variable-class table so the emitted module can also be driven through
// internal/tablewire's form 1.
const squirrelWasVariantProbe = `package probe

enum Grade
{
    Bronze,
    Argent | was = "Silver"
}

table L
{
    grade Grade
}
`

func TestSquirrelRefusesUnportedConstructsByName(t *testing.T) {
	t.Parallel()
	refusers := map[string]func(*ir.Unit, string) error{
		"ir.RefuseWideTableKinds": ir.RefuseWideTableKinds,
		"refuseWideText":          refuseWideText,
		"refuseUnported":          refuseUnported,
		"refuseOptionalArrays":    refuseOptionalArrays,
		"refuseMaps":              refuseMaps,
		"refuseLists":             refuseLists,
		"refuseValueDefaults":     refuseValueDefaults,
		"refuseWasRows":           refuseWasRows,
		"refuseTableArms":         refuseTableArms,
		"refuseVoidArms":          refuseVoidArms,
		"refuseUnionArrays":       refuseUnionArrays,
		"refuseBytes":             refuseBytes,
		"refuseStrings":           refuseStrings,
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

	cases := []struct {
		refuser, name, src, field string
	}{
		{"ir.RefuseWideTableKinds", "wide kind", "package probe\n\ntable L\n{\n    big uint128\n}\n", "L.big"},
		{"refuseWideText", "wide text", "package probe\n\ntable L\n{\n    w wstring(8)\n}\n", "L.w"},
		{"refuseBytes", "byte buffer", "package probe\n\ntable L\n{\n    b *bytes\n}\n", "L.b"},
		{"refuseStrings", "string buffer", "package probe\n\ntable L\n{\n    s *string\n}\n", "L.s"},
		{"refuseMaps", "map", "package probe\n\ntable L\n{\n    index map[string(8)]int32\n}\n", "L.index"},
		{"refuseLists", "unbounded array", "package probe\n\ntable L\n{\n    repos []int32\n}\n", "L.repos"},
		{"refuseOptionalArrays", "optional array", "package probe\n\ntable L\n{\n    maybe ?[..4]int32\n}\n", "L.maybe"},
		{"refuseTableArms", "union table arm", "package probe\n\ntype Inner\n{\n    x int32\n}\nunion Pick\n{\n    alpha Inner\n    beta int32\n}\ntable L\n{\n    u Pick\n}\n", "Pick"},
		{"refuseValueDefaults", "value default", "package probe\n\ntable L\n{\n    s string(8) = \"hi\"\n}\n", "L.s"},
		{"refuseWasRows", "was on a variant", squirrelWasVariantProbe, "Grade.Argent"},
		{"refuseVoidArms", "payload-free arm", "package probe\n\ntype Fire\n{\n    target uint16\n}\n\nunion Weapon\n{\n    laser Fire\n    ram\n}\n\ntable L\n{\n    fire Weapon\n}\n", "Weapon"},
		{"refuseUnionArrays", "array of unions", "package probe\n\ntype Ping\n{\n    nonce uint32\n}\n\ntype Pong\n{\n    nonce uint32\n}\n\nunion Reply\n{\n    ping Ping\n    pong Pong\n}\n\nfixed table L\n{\n    history [..4]Reply\n}\n", "L.history"},
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
	for _, tc := range cases {
		u := unitFromSource(t, tc.src)
		if tc.refuser == "refuseWasRows" {
			// docs/SPEC-TABLES.md §5: squirrel CARRIES the three rows now
			// (compiler/target_squirrel_was.go). The refuser still refuses a
			// target that is not registered.
			if err := refusers[tc.refuser](u, "zig"); err == nil {
				t.Errorf("%s: the refuser accepted the unregistered target zig", tc.name)
			} else if msg := err.Error(); !strings.Contains(msg, tc.field) || !strings.Contains(msg, "zig") {
				t.Errorf("%s: the unregistered-target refusal does not name the field %s and zig: %s", tc.name, tc.field, msg)
			}
			if c != nil {
				if err := chain(u); err != nil {
					t.Errorf("%s: the squirrel chain refused the construct squirrel carries: %v", tc.name, err)
				}
				if _, err := c.Generate(u, "squirrel", nil); err != nil {
					t.Errorf("%s: Generate squirrel refused the construct squirrel carries: %v", tc.name, err)
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
	for _, name := range leaves {
		if !covered[name] {
			t.Errorf("compiler/target_squirrel.go refuses through %s, and no construct here is refused by it", name)
		}
	}
	t.Logf("%d constructs over %d refusers %v; the tagged target %v", len(cases), len(leaves), leaves, c != nil)
}

// TestUnionArmRefusalsNameTheUnion pins the two table union-arm refusals to
// name the union and target (docs/SPEC-TABLES.md §11).
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
			err := tc.refuse(u, "squirrel")
			if err == nil {
				t.Fatalf("squirrel accepted the construct")
			}
			if msg := err.Error(); !strings.Contains(msg, tc.field) || !strings.Contains(msg, "squirrel") {
				t.Errorf("refusal does not name the union %s and the target squirrel: %s", tc.field, msg)
			}
		})
	}
}
