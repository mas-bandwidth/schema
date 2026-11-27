package compiler

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/java"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/javatable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobstertable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luatable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/pythontable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/rubytable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// The emitters' name rules run at check (SPEC §4.6): each target's whole
// name check, as its generate runs it, under every surface a generate can
// select. A name one of them refuses is refused by schema check for every
// target, naming the target, instead of at generate.
func init() {
	check.RegisterEmitterNameRule("C#", csNames)
	check.RegisterEmitterNameRule("Goose", gooseNameRule)
	check.RegisterEmitterNameRule("Java", javaNameRule)
	check.RegisterEmitterNameRule("Python", pythontable.CheckNamesAtCheck)
	check.RegisterEmitterNameRule("Ruby", rubytable.CheckNamesAtCheck)
}

// gooseNameRule is every name refusal Goose's generate makes: the namespace
// check, then the target's whole generate, through the driver as `schema
// generate --lang goose` runs it, whose refusals by name (goose.NameError: a
// name two writers of the one namespace both declare, such as a table
// module's _<e>Variants and the view's own rows; a schema file a module of
// the target lands on) are lifted. Each Declare is a refusal by name, so
// none can be missed by a list kept here. A refusal of a construct Goose does
// not carry yet is no name refusal and is not lifted.
func gooseNameRule(u *ir.Unit) error {
	if err := goose.CheckUnit(u, nil); err != nil {
		return err
	}
	_, err := gooseCompiler().Generate(u, "goose", Options{})
	if ne := (*goose.NameError)(nil); errors.As(err, &ne) {
		return err
	}
	return nil
}

var gooseCompiler = sync.OnceValue(New)

// csConstraintKeywords are the C# contextual keywords the generated code
// spells as a generic constraint (`where T : unmanaged`, the table runtime
// and region code of both C# targets). A constraint keyword means itself only
// while no type of that name is in scope, and every declaration of the unit
// is a type of the unit's namespace, the one the constraint is written in:
// an enum unmanaged turns `where T : unmanaged` into a constraint on the enum,
// CS0701 (name-buildability n_internal_75). A field or an arm of that name is
// a member, never in scope as a type, so only a declaration is refused.
var csConstraintKeywords = map[string]bool{"unmanaged": true}

// csNames refuses a unit that declares a type named like a C# constraint
// keyword. Both C# targets (cs and unity) run it at generate, and schema check
// runs it for every target (SPEC §4.6, Emitter name rules).
func csNames(u *ir.Unit) error {
	for _, decl := range slices.Sorted(maps.Keys(u.DeclFile)) {
		if _, isConst := u.Consts[decl]; isConst {
			continue // a const is a member of the constants class, not a type
		}
		if csConstraintKeywords[decl] {
			return fmt.Errorf("declaration %s is a type of the C# namespace %s, where the generated generic constraint `where T : %s` would name it instead of the constraint (CS0701); rename the declaration", decl, u.Package, decl)
		}
	}
	return nil
}

// Lua and Lobster run the file- and name-rules of their own emitters (SPEC
// §4.6); C reads its namespace off the target (emitter_cnames.go).
func init() {
	check.RegisterEmitterNameRule("C", cNames)
	check.RegisterEmitterNameRule("Lua", func(u *ir.Unit) error {
		if err := lua.CheckBases(u); err != nil {
			return err
		}
		return luatable.CheckBases(u)
	})
	check.RegisterEmitterNameRule("Lobster", func(u *ir.Unit) error {
		if err := lobster.CheckNames(u); err != nil {
			return err
		}
		if err := lobster.CheckBases(u, lobster.Runtime()); err != nil {
			return err
		}
		return lobstertable.Refuse(u)
	})
}

// javaNameRule is every name refusal Java's generate makes: the packet
// emitter's and the table backend's own name checks, then the one public
// class per file rule (compiler/target_java.go), which refuses a generated
// file two declarations claim (a type named like its file's outer class, a
// base named like a table runtime class such as TableBytes or TableRetain).
// The claims are read off the files the two emitters write, so the rule
// cannot drift from them; neither emitter calls the checker.
func javaNameRule(u *ir.Unit) error {
	if err := java.CheckNames(u); err != nil {
		return err
	}
	if err := javatable.CheckNames(u); err != nil {
		return err
	}
	files, err := java.OutputFileNames(u)
	if err != nil {
		return nil // not a name refusal
	}
	tables, err := javatable.GenerateLineageRaw(u, nil)
	if err != nil {
		return nil
	}
	for name := range tables {
		if _, dup := files[name]; dup {
			return fmt.Errorf("generated file %s is claimed twice: a schema file, a table runtime type and a declaration's generated name all land in the Java package's one-public-class-per-file namespace; rename one of them (docs/SPEC-TABLES.md §11)", name)
		}
	}
	return nil
}
