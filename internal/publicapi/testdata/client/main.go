// Command client is a schema compiler built OUTSIDE the schema module, on the
// public API alone: it registers a generator of its own — the thing the
// built-in six cannot demonstrate, since they might have had a private path
// into the driver — and reports the unit through the public IR.
//
// It is a module of its own (see go.mod), so Go's internal rule applies to it
// exactly as it applies to a stranger's repository: an import of
// github.com/mas-bandwidth/schema/v2/internal/... does not compile here. What
// this program does is therefore what anyone can do.
package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// widths is a generator that emits nothing a language would compile — it
// reports every symbolically-declared range bound in the author's own
// spelling and a target spelling. The point is the shape: a type outside
// this module satisfying compiler.Generator, reading the IR through the
// public derived-parameter helpers and rendering declared expressions
// through the public expression surface.
type widths struct{}

// Names registers this generator as `--lang widths`.
func (widths) Names() []string { return []string{"widths"} }

// Generate reports every symbolically-declared bound.
func (widths) Generate(u *ir.Unit, _ compiler.Options) (map[string][]byte, error) {
	var b strings.Builder
	// Every range bound the author declared through named constants, rendered
	// as the source expression the resolved IntMax alone cannot give back —
	// once in the schema spelling, once the way a SCREAMING_SNAKE target
	// would emit it. An E.Max bound has no target twin, so it folds, exactly
	// as the built-in backends fold it.
	structs := make([]string, 0, len(u.Structs))
	for name := range u.Structs {
		structs = append(structs, name)
	}
	sort.Strings(structs)
	for _, name := range structs {
		for _, f := range u.Structs[name].Fields {
			if len(ir.ExprConsts(f.IntMaxExpr)) == 0 || ir.ExprHasEnumMax(f.IntMaxExpr) {
				continue
			}
			fmt.Fprintf(&b, "bound %s.%s max = %s | %s = %s\n", name, f.Name,
				ir.RenderExpr(f.IntMaxExpr),
				ir.RenderExprIdent(f.IntMaxExpr, ir.RustConstName), f.IntMax)
		}
	}
	return map[string][]byte{"widths.txt": []byte(b.String())}, nil
}

func main() {
	c := compiler.New()
	if err := c.Register(widths{}); err != nil {
		die(err)
	}
	paths, err := compiler.GatherPaths(os.Args[1:])
	if err != nil {
		die(err)
	}
	// No FormatInPlace: a tool that only reads a tree should not rewrite it.
	u, err := c.Load(paths)
	if err != nil {
		die(err)
	}
	fmt.Printf("package %s 0x%016x\n", u.Package, u.ProtocolId)
	fmt.Printf("targets %s\n", strings.Join(c.Targets(), " "))
	files, err := c.Generate(u, "widths", nil)
	if err != nil {
		die(err)
	}
	if _, err := os.Stdout.Write(files["widths.txt"]); err != nil {
		die(err)
	}
}

func die(err error) {
	fmt.Fprintln(os.Stderr, "client:", err)
	os.Exit(1)
}
