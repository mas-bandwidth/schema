package compiler

import (
	"fmt"

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
	check.RegisterEmitterNameRule("Goose", func(u *ir.Unit) error { return goose.CheckUnit(u, nil) })
	check.RegisterEmitterNameRule("Java", javaNameRule)
	check.RegisterEmitterNameRule("Python", pythontable.CheckNamesAtCheck)
	check.RegisterEmitterNameRule("Ruby", rubytable.CheckNamesAtCheck)
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
	files, err := java.Generate(u)
	if err != nil {
		return nil // not a name refusal
	}
	tables, err := javatable.GenerateLineage(u, nil)
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
