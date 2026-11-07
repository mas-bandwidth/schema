package gotable

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// A CHAIN OF STRUCTS THAT EACH END IN A STRUCT is the shape Go's type checker
// sizes in time exponential in its depth: it sizes a struct's last field once
// for the offsets and once more for the size, so every constant unsafe.Sizeof
// or unsafe.Offsetof over the chain's top doubles per level. The generated Go
// names no generated type in either, so the chain builds in time linear in its
// depth. chainDepthUnit is that chain at the fixed form's depth bound
// (docs/SPEC-TABLES.md §3.4's fixed_max_depth, 64): the fixed root is depth 0,
// D1 is depth 1 and D63's v is the deepest entry at depth 64. A variable table
// holds the same chain, so both forms' descriptors, plans and storage reach it.
const chainDepth = 64

func chainDepthUnit() string {
	var b strings.Builder
	b.WriteString("package probe\n\n")
	fmt.Fprintf(&b, "type D%d\n{\n    v int32 = 0 | min = 0, max = 200\n}\n\n", chainDepth-1)
	for i := chainDepth - 2; i >= 1; i-- {
		fmt.Fprintf(&b, "type D%d\n{\n    d D%d\n}\n\n", i, i+1)
	}
	b.WriteString("fixed table Root\n{\n    d D1\n}\n\n")
	b.WriteString("table Holder\n{\n    d D1\n}\n")
	return b.String()
}

func chainDepthFiles(t *testing.T) (*ir.Unit, map[string][]byte) {
	t.Helper()
	u := unitFrom(t, chainDepthUnit())
	files, err := golang.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	return u, files
}

// unsafeLayoutOfRe matches one unsafe layout builtin and its operand up to the
// first closing parenthesis, which is enough to see the type it names.
var unsafeLayoutOfRe = regexp.MustCompile(`unsafe\.(Sizeof|Offsetof|Alignof)\(([^)]*)\)`)

// TestChainDepthNamesNoGeneratedTypeInUnsafeLayout is the cheap half: no
// emitted unsafe.Sizeof, unsafe.Offsetof or unsafe.Alignof names one of the
// unit's own types, and the fixed form is emitted at the bound.
func TestChainDepthNamesNoGeneratedTypeInUnsafeLayout(t *testing.T) {
	t.Parallel()
	_, files := chainDepthFiles(t)
	// the unit's own types are the schema's: D1..D63, Root and Holder
	names := []string{"Root", "Holder"}
	for i := 1; i < chainDepth; i++ {
		names = append(names, fmt.Sprintf("D%d", i))
	}
	own := regexp.MustCompile(`\b(` + strings.Join(names, "|") + `)(Row)?\b`)
	naming := func(src string) []string {
		var hits []string
		for _, m := range unsafeLayoutOfRe.FindAllStringSubmatch(src, -1) {
			if own.MatchString(m[2]) {
				hits = append(hits, m[0])
			}
		}
		return hits
	}
	// the reversed witness: each spelling the emitter used to write is caught
	for _, old := range []string{"uint32(unsafe.Sizeof(D1{}))", "uint32(unsafe.Offsetof(D2Row{}.D))", "unsafe.Alignof(Root{})", "int64(unsafe.Sizeof(*(*D5)(nil)))"} {
		if len(naming(old)) != 1 {
			t.Fatalf("the scan does not catch %s", old)
		}
	}
	fixed := false
	for name, src := range files {
		for _, hit := range naming(string(src)) {
			t.Errorf("%s: %s names a generated type; the type checker sizes it in exponential time", name, hit)
		}
		fixed = fixed || strings.Contains(string(src), "func RootFixedLoad(")
	}
	if !fixed {
		t.Fatalf("the chain %d deep does not emit Root's fixed form", chainDepth)
	}
}

// chainDepthBuildBudget is far above the linear build's few seconds and far
// below what the exponential type check takes: a constant unsafe.Sizeof over
// this chain 26 deep, a quarter of the way to 64, alone took 95 s.
const chainDepthBuildBudget = 90 * time.Second

// TestChainDepthBuildsInLinearTime builds and vets the generated package of the
// chain at the fixed form's depth bound under chainDepthBuildBudget.
func TestChainDepthBuildsInLinearTime(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Go toolchain (it builds and vets the generated unit)")
	_, files := chainDepthFiles(t)
	dir := t.TempDir()
	runtime := slowtest.SerializeGo(t, "../../../")
	files["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\nreplace github.com/mas-bandwidth/serialize.go/v2 => %q\n", runtime))
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"build", "-o", os.DevNull, "."}, {"vet", "."}} {
		ctx, cancel := context.WithTimeout(context.Background(), chainDepthBuildBudget)
		cmd := exec.CommandContext(ctx, "go", args...)
		cmd.Dir = dir
		start := time.Now()
		out, err := cmd.CombinedOutput()
		spent := time.Since(start)
		cancel()
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			t.Fatalf("go %s of a chain %d deep ran past %v: the generated Go type-checks in exponential time", args[0], chainDepth, chainDepthBuildBudget)
		}
		if err != nil {
			t.Fatalf("go %s: %v\n%s", args[0], err, out)
		}
		t.Logf("go %s of a chain %d deep: %v", args[0], chainDepth, spent.Round(time.Millisecond))
	}
}
