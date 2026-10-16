package elixir

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// loadUnitFromSrc builds a unit from in-memory schema sources — the packet
// emitter's own input, with no Elixir toolchain and no disk beyond a unit
// test's temp space.
func loadUnitFromSrc(t *testing.T, files map[string]string) *ir.Unit {
	t.Helper()
	var srcs []check.SourceFile
	for name, src := range files {
		base := strings.TrimSuffix(name, ".schema")
		ast, errs := parser.Parse(name, []byte(src))
		if len(errs) != 0 {
			t.Fatalf("parse %s: %v", name, errs)
		}
		srcs = append(srcs, check.SourceFile{Path: name, Name: name, Base: base, Bytes: []byte(src), AST: ast})
	}
	u, errs := check.Unit(srcs)
	if len(errs) != 0 {
		t.Fatalf("check: %v", errs)
	}
	return u
}

func emitUnit(t *testing.T, files map[string]string) (map[string][]byte, error) {
	t.Helper()
	return Generate(loadUnitFromSrc(t, files))
}

// carryCarryFile is the shape of tables/arms/Carry.schema inlined: a unit
// whose file Carry.schema declares union Carry, so the declaration's module
// and the file-scope module would both be <Ns>.Carry — the collision that
// makes the file module step aside to <Ns>.CarryFile. The second file
// references the const cross-file, which reaches Carry's file module.
var carryCarryFile = map[string]string{
	"Carry.schema": `package Armdemo

type Leaf { v int32 }
type Plain { v int32 }

union Carry
{
    leaf Leaf
    plain Plain
}

const FIVE = 5
`,
	"Other.schema": `package Armdemo

const TEN = FIVE * 2
`,
}

func TestFileModuleYieldsToASameNamedDeclaration(t *testing.T) {
	t.Parallel()
	out, err := emitUnit(t, carryCarryFile)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !strings.Contains(string(out["Carry.ex"]), "defmodule Armdemo.Carry do") {
		t.Fatalf("union module missing: the declaration's own module must keep <Ns>.Carry")
	}
	if !strings.Contains(string(out["Carry.ex"]), "defmodule Armdemo.CarryFile do") {
		t.Fatalf("file module missing: the file-scope module must step aside to <Ns>.CarryFile")
	}
	if !strings.Contains(string(out["Other.ex"]), "Armdemo.CarryFile.five()") {
		t.Fatalf("cross-file codec reference must name <Ns>.CarryFile, got:\n%s", out["Other.ex"])
	}
	if strings.Contains(string(out["Other.ex"]), "Armdemo.Carry.five()") {
		t.Fatalf("cross-file codec reference must never name <Ns>.Carry (the union module):\n%s", out["Other.ex"])
	}
}

func TestFileModuleNoCollisionKeepsBaseName(t *testing.T) {
	t.Parallel()
	out, err := emitUnit(t, map[string]string{
		"Plain.schema": `package Armdemo

type Foo
{
    x int32
}
`,
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !strings.Contains(string(out["Plain.ex"]), "defmodule Armdemo.Plain do") {
		t.Fatalf("no-collision file module must keep <Ns>.<Base>, got:\n%s", out["Plain.ex"])
	}
	if strings.Contains(string(out["Plain.ex"]), "defmodule Armdemo.PlainFile do") {
		t.Fatalf("no-collision file module must NOT be renamed to <Ns>.<Base>File:\n%s", out["Plain.ex"])
	}
}

func TestFileModuleStepsAsideByteIdenticalToBase(t *testing.T) {
	t.Parallel()
	// A no-collision unit emits byte-identical output whether it is generated
	// once or twice: the rename must only ever touch the colliding case.
	a, err := emitUnit(t, map[string]string{
		"A.schema": `package Armdemo

type A { x int32 }
`,
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	b, err := emitUnit(t, map[string]string{
		"A.schema": `package Armdemo

type A { x int32 }
`,
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if string(a["A.ex"]) != string(b["A.ex"]) {
		t.Fatalf("byte-identical output expected for a no-collision unit")
	}
}

func TestFileModuleRefusesATakenBaseFileName(t *testing.T) {
	t.Parallel()
	// The file module steps aside to CarryFile, so a declaration that takes
	// CarryFile is a fresh collision and must still be refused, by name.
	_, err := emitUnit(t, map[string]string{
		"Carry.schema": `package Armdemo

type Leaf { v int32 }

union Carry
{
    leaf Leaf
}

enum CarryFile { a, b }
`,
	})
	if err == nil {
		t.Fatalf("expected a refusal when <Base>File is itself taken")
	}
	if !strings.Contains(err.Error(), "CarryFile") {
		t.Fatalf("refusal must name the colliding <Base>File module, got: %v", err)
	}
}

