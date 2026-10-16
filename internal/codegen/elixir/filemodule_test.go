package elixir

import (
	"bytes"
	"os"
	"path/filepath"
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

// The corpus the build generates generated/elixir from (make/elixir.mk:
// ./bin/schema generate --lang elixir --out generated/elixir examples), and
// the committed output it must reproduce. Paths are relative to this
// package's directory, which is where go test runs it.
const (
	corpusDir       = "../../../examples"
	committedElixir = "../../../generated/elixir"
)

// loadUnitFromDir reads every *.schema of dir into one unit, by basename as
// the compiler's own Load names them.
func loadUnitFromDir(t *testing.T, dir string) *ir.Unit {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".schema") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		files[e.Name()] = string(data)
	}
	if len(files) == 0 {
		t.Fatalf("no .schema files under %s", dir)
	}
	return loadUnitFromSrc(t, files)
}

func TestFileModuleStepsAsideByteIdenticalToBase(t *testing.T) {
	t.Parallel()
	// The step-aside must only ever touch the colliding case: on the corpus,
	// where no file base is taken, the generator's output must be BYTE-IDENTICAL
	// to the COMMITTED generated/elixir files on disk — never merely to a
	// second run of this same generator, which a rename on every file would
	// also agree with.
	out, err := Generate(loadUnitFromDir(t, corpusDir))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	entries, err := os.ReadDir(committedElixir)
	if err != nil {
		t.Fatal(err)
	}
	committed := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".ex") {
			continue
		}
		committed[e.Name()] = true
		want, err := os.ReadFile(filepath.Join(committedElixir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		got, ok := out[e.Name()]
		if !ok {
			t.Errorf("%s is committed under generated/elixir but the generator no longer emits it", e.Name())
			continue
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s: generator output differs from the committed generated/elixir/%s (%d bytes generated, %d committed) — a rename leaked into a no-collision unit, or run: make generated/elixir/.stamp", e.Name(), e.Name(), len(got), len(want))
		}
	}
	for name := range out {
		if !committed[name] {
			t.Errorf("generator emits %s, which is not committed under generated/elixir", name)
		}
	}
}

func TestFileModuleYieldsToAFixedTableValueModule(t *testing.T) {
	t.Parallel()
	// A `fixed table Caption` in Caption.schema emits its value struct as
	// <Ns>.Caption from the fixed backend (fixedelixir.go structModule); the
	// file module must step aside to <Ns>.CaptionFile rather than merge with
	// the table's value module.
	out, err := emitUnit(t, map[string]string{
		"Caption.schema": `package Armdemo

fixed table Caption
{
    x int32
    y int32
}
`,
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !strings.Contains(string(out["Caption.ex"]), "defmodule Armdemo.CaptionFile do") {
		t.Fatalf("file module must step aside to <Ns>.CaptionFile for a fixed table of the same name, got:\n%s", out["Caption.ex"])
	}
	if strings.Contains(string(out["Caption.ex"]), "defmodule Armdemo.Caption do") {
		t.Fatalf("file module must not keep <Ns>.Caption (the fixed table's value module):\n%s", out["Caption.ex"])
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
