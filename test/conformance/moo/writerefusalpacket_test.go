// Package moowriterefusalpacket is the moo leg of the write-refusal-packet
// surface (docs/SPEC.md §5.1; test/conformance/write-refusal-packet).
package moowriterefusalpacket

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/moo"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/moo/moorun"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	wrpacket "github.com/mas-bandwidth/schema/v2/test/conformance/write-refusal-packet"
)

// root is the repository root relative to this package.
const root = "../../.."

// TestWriteRefusalPacketMoo holds §5.1's packet writer on the moo leg: every
// caller-holdable value the declaration does not admit makes the generated
// Write return false (never raise), and the lawful value at the same boundary
// is written. The negative control relaxes the emitted isInt guard's upper
// bound comparison and requires the boundary case to go red.
func TestWriteRefusalPacketMoo(t *testing.T) {
	runWriteRefusalPacketMoo(t)

	out, failed := slowtest.RunSabotaged(t, root, "./test/conformance/moo",
		"^TestWriteRefusalPacketMooSurface$",
		slowtest.Sabotage{
			File: "internal/codegen/moo/moo.go",
			From: `x >= lo and x <= hi and`,
			To:   `x >= lo and x < hi and`,
		})
	if !failed {
		t.Fatalf("negative control: the upper-bound comparison moved and the moo packet surface stayed green\n%s", out)
	}
	if !strings.Contains(out, "FAILED: scalar_at_max") {
		t.Fatalf("negative control: the moo packet surface went red, but not at the boundary case\n%s", out)
	}
}

// TestWriteRefusalPacketMooSurface is the surface alone, so the negative
// control can run it under an overlay without re-entering the control.
func TestWriteRefusalPacketMooSurface(t *testing.T) {
	runWriteRefusalPacketMoo(t)
}

// runWriteRefusalPacketMoo generates the schema with the moo emitter (as the
// moo packet driver does), puts the probe beside the modules, requires
// moo-analyze to be silent over them, runs the probe under the pinned moo
// with rows.txt as inputs.rows, and holds its verdicts against the rows.
// moorun fails, never skips, when the pinned moo does not resolve.
func runWriteRefusalPacketMoo(t *testing.T) {
	t.Helper()
	slowtest.Gate(t, "the pinned moo interpreter (it runs the generated packet writer)")
	path := filepath.Join(root, wrpacket.Dir, "PacketWriteRefusal.schema")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	name := filepath.Base(path)
	tree, errs := parser.Parse(name, src)
	if len(errs) != 0 {
		t.Fatalf("parse %s: %v", path, errs)
	}
	u, errs := check.Unit([]check.SourceFile{{Path: path, Name: name, Base: strings.TrimSuffix(name, ".schema"), Bytes: src, AST: tree}})
	if len(errs) != 0 {
		t.Fatalf("check %s: %v", path, errs)
	}
	files, err := moo.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	probe, err := os.ReadFile("writerefusalpacket.moo")
	if err != nil {
		t.Fatal(err)
	}
	files["main.moo"] = probe
	dir := t.TempDir()
	var mods []string
	for n, data := range files {
		if err := os.WriteFile(filepath.Join(dir, n), data, 0o644); err != nil {
			t.Fatal(err)
		}
		mods = append(mods, n)
	}
	rowsText, err := os.ReadFile(wrpacket.RowsPath(root))
	if err != nil {
		t.Fatal(err)
	}
	inputs := map[string][]byte{"rows": rowsText}
	if err := os.WriteFile(filepath.Join(dir, "inputs.moo"), moorun.InputsModule(inputs), 0o644); err != nil {
		t.Fatal(err)
	}
	if lint := moorun.Analyze(t, dir, mods...); lint != "" {
		t.Fatalf("moo-analyze is not silent over the generated modules and the probe:\n%s", lint)
	}
	out := moorun.Run(t, dir, "main.moo", inputs)
	wrpacket.Check(t, string(out), wrpacket.Rows(t, root), nil)
}
