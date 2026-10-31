package ci

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// Run the real probe recipe and compiler in a small isolated repository. A
// supported port may produce Go source importing its runtime; the compiler's
// final go test ./... must not discover that temporary package as its own.
func TestWideScalarProbeModuleBoundary(t *testing.T) {
	if _, err := exec.LookPath("make"); err != nil {
		t.Skipf("probe recipe requires make: %v", err)
	}
	root, fixture := repoRoot(t), t.TempDir()
	for _, dir := range []string{"cmd", "internal", "ir", "compiler", "bin", "tables"} {
		if err := os.Mkdir(filepath.Join(fixture, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, dir := range []string{"make", "tables/scalars"} {
		if err := os.CopyFS(filepath.Join(fixture, dir), os.DirFS(filepath.Join(root, dir))); err != nil {
			t.Fatal(err)
		}
	}
	// make/squirrel-serialize.mk, make/squirrel-conformance.mk,
	// make/squirrel-fuzz.mk, make/schema-squirrel-nightly-fuzz.mk and
	// make/schema-squirrel-all-port-differential.mk are not language backends
	// (docs/PORT-SQUIRREL.md, THE RUNTIME LAYOUT: the type wire's bottom layer;
	// THE CARD MAP: the conformance leg, the wire fuzzer, the nightly fuzz and
	// the all-port differential). They carry no compiler/target_*.go and
	// refuse no unit, so they are not legs of the wide-scalar probe; drop them
	// from the fixture before the probe runs.
	for _, mk := range []string{
		"squirrel-serialize.mk", "squirrel-conformance.mk", "squirrel-fuzz.mk",
		"schema-squirrel-nightly-fuzz.mk", "schema-squirrel-all-port-differential.mk",
	} {
		if err := os.Remove(filepath.Join(fixture, "make", mk)); err != nil {
			t.Fatal(err)
		}
	}
	// a tagged port in the real tree (a `schema new-leg` leg, make/<lang>.mk
	// beside a build-tagged compiler/target_<lang>.go) is identified by the
	// target file's first line, so the fixture carries those files too
	targets, err := filepath.Glob(filepath.Join(root, "compiler", "target_*.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range targets {
		data, err := os.ReadFile(target)
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(string(data), "//go:build schema_leg_") {
			if err := os.WriteFile(filepath.Join(fixture, "compiler", filepath.Base(target)), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	makefile, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	for path, data := range map[string][]byte{
		"Makefile":                       makefile,
		"go.mod":                         []byte("module probefixture\n\ngo 1.26\n"),
		"root.go":                        []byte("package probefixture\n"),
		"make/zig.mk":                    {},
		"make/schema-go-example-gate.mk": []byte(".PHONY: schema-go-example-gate\nschema-go-example-gate:\n\t@true\n"),
		"compiler/target_zig.go":         []byte("//go:build schema_leg_zig\n\npackage compiler\n"),
	} {
		if err := os.WriteFile(filepath.Join(fixture, path), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// The planted skeleton is zig, a name no target carries, so the compiler
	// built from this tree refuses --lang zig exactly as it refuses a fresh
	// `schema new-leg zig` leg. A real `schema new-leg` leg in the tree
	// (make/<lang>.mk beside a compiler/target_<lang>.go behind its
	// schema_leg_<lang> tag, such as goose, docs/PORT-GOOSE.md) is a skeleton
	// here too: its marker file is carried into the fixture, so the gate skips
	// it by the same rule. A builtin target (lua among them) carries no tag and
	// is probed like every other port.
	skeletons := map[string]bool{"zig": true} // the planted zig
	tagged, err := filepath.Glob(filepath.Join(root, "compiler", "target_*.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, src := range tagged {
		lang := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(src), "target_"), ".go")
		data, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		first, _, _ := strings.Cut(string(data), "\n")
		if first != "//go:build schema_leg_"+lang {
			continue
		}
		if _, err := os.Stat(filepath.Join(fixture, "make", lang+".mk")); err != nil {
			continue
		}
		if err := os.WriteFile(filepath.Join(fixture, "compiler", filepath.Base(src)), []byte(first+"\n\npackage compiler\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		skeletons[lang] = true
	}
	run := func(dir, name string, args ...string) string {
		t.Helper()
		cmd := exec.Command(name, args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s %v: %v\n%s", name, args, err, out)
		}
		return string(out)
	}
	run(root, "go", "build", "-o", filepath.Join(fixture, "bin", "schema"), "./cmd/schema")
	out := run(fixture, "make", "--no-print-directory", "tables-ports-refuse-wide-scalars")
	t.Log(strings.TrimSpace(out))
	if !strings.Contains(out, "zig has no --lang yet (schema new-leg skeleton)") {
		t.Fatalf("tagged scaffold was not identified:\n%s", out)
	}
	counts := regexp.MustCompile(`(\d+) ports refuse the unit by name.*; (\d+) carry the kinds`).FindStringSubmatch(out)
	if len(counts) != 3 {
		t.Fatalf("probe did not report both outcomes:\n%s", out)
	}
	refused, err := strconv.Atoi(counts[1])
	if err != nil {
		t.Fatal(err)
	}
	carried, err := strconv.Atoi(counts[2])
	if err != nil {
		t.Fatal(err)
	}
	mkFiles, err := filepath.Glob(filepath.Join(fixture, "make", "*.mk"))
	if err != nil {
		t.Fatal(err)
	}
	// A hyphenated make/<leg>-<surface>.mk is a companion fragment of a leg
	// (make/luau-serialize.mk, docs/PORT-LUAU.md), not a port.
	var legs []string
	for _, mk := range mkFiles {
		stem := strings.TrimSuffix(filepath.Base(mk), ".mk")
		if !strings.Contains(stem, "-") {
			legs = append(legs, mk)
		}
	}
	if len(legs) == 0 || refused+carried != len(legs)-len(skeletons) {
		t.Fatalf("probe checked %d ports, registry has %d", refused+carried, len(legs))
	}
	if _, err := os.Stat(filepath.Join(fixture, "build", "tables-wide-refusal", "go", "Scalars.go")); err != nil {
		t.Fatalf("supported Go port did not produce its probe: %v", err)
	}
	packages := func() []string { return strings.Fields(run(fixture, "go", "list", "-e", "./...")) }
	if got := packages(); !slices.Equal(got, []string{"probefixture"}) {
		t.Fatalf("root discovery included probe packages: %v", got)
	}
	// The control proves that the generated package exists and would leak into
	// recursive discovery without the boundary; ignoring an empty tree is no gate.
	if err := os.Remove(filepath.Join(fixture, "build", "tables-wide-refusal", "go.mod")); err != nil {
		t.Fatal(err)
	}
	if got := packages(); !slices.Contains(got, "probefixture/build/tables-wide-refusal/go") {
		t.Fatalf("removing the module boundary did not expose the probe: %v", got)
	}

	// An unregistered port is skippable only when its target file proves that it
	// is an intentional new-leg scaffold. The same compiler error without that
	// exact marker is a disappeared live backend and must keep the gate red.
	target := filepath.Join(fixture, "compiler", "target_zig.go")
	if err := os.WriteFile(target, []byte("package compiler\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(fixture, "bin", "schema")
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	newer := info.ModTime().AddDate(0, 0, 1)
	if err := os.Chtimes(bin, newer, newer); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("make", "--no-print-directory", "tables-ports-refuse-wide-scalars")
	cmd.Dir = fixture
	outBytes, err := cmd.CombinedOutput()
	out = string(outBytes)
	if err == nil {
		t.Fatalf("unmarked unregistered backend passed the gate:\n%s", out)
	}
	// a tagged leg in the tree (goose) is still a skeleton; the unmarked zig
	// must not be
	if strings.Contains(out, "zig has no --lang yet (schema new-leg skeleton)") || !strings.Contains(out, "the zig backend stopped for another reason") {
		t.Fatalf("unmarked backend did not fail as an ordinary backend:\n%s", out)
	}
}
