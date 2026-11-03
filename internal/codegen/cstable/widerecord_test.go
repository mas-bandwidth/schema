package cstable

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/csharp"
	"github.com/mas-bandwidth/schema/v2/internal/memceiling"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// csWideRecordDir holds Wide.schema, a record past 4 GiB (docs/SPEC-TABLES.md
// §11) that every leg generates and compiles where its platform can hold it:
// Part is 2 GiB, Huge is three of them and a trailing field.
const csWideRecordDir = "../../../testdata/wire/tables/storage-cap/wide"

// TestWideRecordCSharp: C# spells every record as a struct too (the block and
// cook rows), and CoreCLR loads none past its value-type bounds, so the C#
// generator refuses Wide.schema by name, cs_value_type_too_large, naming the
// record and its size, rather than emitting code that does not compile.
func TestWideRecordCSharp(t *testing.T) {
	defer memceiling.Heap(memceiling.Default)()
	src, err := os.ReadFile(filepath.Join(csWideRecordDir, "Wide.schema"))
	if err != nil {
		t.Fatal(err)
	}
	u := unitFrom(t, string(src))
	files, err := Generate(u)
	var refusal *ValueTypeRefusal
	if !errors.As(err, &refusal) {
		t.Fatalf("Wide.schema generated %d C# files with error %v, want the %s refusal", len(files), err, CSValueTypeTooLarge)
	}
	if files != nil {
		t.Fatal("the refusal emitted files beside it")
	}
	part := ir.RecordLayout(u, u.Tables["Part"]).Size
	want := fmt.Sprintf("%s: Part's C# struct PartRow is %d bytes", CSValueTypeTooLarge, part)
	if msg := err.Error(); !strings.HasPrefix(msg, want) || !strings.Contains(msg, "docs/SPEC-TABLES.md §11") {
		t.Fatalf("the refusal reads %q, want it to open %q and cite §11", msg, want)
	}
}

// csBoundUnit is a unit whose one table's cooked row starts its last field at
// offset `at`, after a byte buffer, or, with at < 0, a row of one buffer of
// `bytes` bytes in `width`-byte elements. Neither is built: the generator
// decides on the layout alone.
func csBoundUnit(t *testing.T, at, bytes, width int64) *ir.Unit {
	t.Helper()
	if at >= 0 {
		return unitFrom(t, fmt.Sprintf("package bound\ntable Edge\n{\n    lead [%d]uint8\n    tail uint8\n}\n", at))
	}
	return unitFrom(t, fmt.Sprintf("package bound\ntable Edge\n{\n    lead [%d]uint%d\n}\n", bytes/width, width*8))
}

// TestCSharpValueTypeBound holds the two bounds at their edges: a struct of
// 2^31-1 bytes and a field at offset 2^27-8 are generated, and one byte more
// of either is refused by name. The accepted edges are compiled and their
// rows loaded by CoreCLR (Unsafe.SizeOf), which allocates nothing.
func TestCSharpValueTypeBound(t *testing.T) {
	memceiling.Guard(t, memceiling.Default)
	cases := []struct {
		name    string
		u       *ir.Unit
		refused bool
		size    int64
	}{
		{"field at 2^27-8", csBoundUnit(t, csValueTypeMaxFieldOffset, 0, 0), false, csValueTypeMaxFieldOffset + 1},
		{"field at 2^27-7", csBoundUnit(t, csValueTypeMaxFieldOffset+1, 0, 0), true, 0},
		{"2^31-1 bytes", csBoundUnit(t, -1, csValueTypeMaxSize, 1), false, csValueTypeMaxSize},
		{"2^31 bytes", csBoundUnit(t, -1, csValueTypeMaxSize+1, 2), true, 0},
	}
	for _, c := range cases {
		files, err := Generate(c.u)
		var refusal *ValueTypeRefusal
		if c.refused {
			if !errors.As(err, &refusal) || refusal.Record != "Edge" || !strings.HasPrefix(err.Error(), CSValueTypeTooLarge+": ") {
				t.Errorf("%s: generated with error %v, want the %s refusal of Edge", c.name, err, CSValueTypeTooLarge)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: refused: %v", c.name, err)
			continue
		}
		if got := ir.RecordLayout(c.u, c.u.Tables["Edge"]).Size; got != c.size {
			t.Fatalf("%s: Edge is %d bytes, the case means %d", c.name, got, c.size)
		}
		if !slowtest.Enabled() {
			continue
		}
		packet, perr := csharp.Generate(c.u)
		if perr != nil {
			t.Fatal(perr)
		}
		maps.Copy(packet, files)
		sources := map[string][]byte{}
		for name, data := range packet {
			if strings.HasSuffix(name, ".cs") {
				sources[filepath.Base(name)] = data
			}
		}
		body := fmt.Sprintf(`
        long size = System.Runtime.CompilerServices.Unsafe.SizeOf<Bound.EdgeRow>();
        if (size != %dL) { bad |= ProbeLog.Fail("EdgeRow loads at " + size); }
`, c.size)
		out, err := csWideRecordCompile(t, sources, body)
		if err != nil || strings.Contains(out, "FAIL ") || !strings.Contains(out, "PROBE OK") {
			t.Errorf("%s: the accepted edge did not compile and load: %v\n%s", c.name, err, out)
		}
	}
}

// csWideRecordCompile compiles files with the SDK's csc and answers its
// output: as a library when body is empty, else with a probe Main around body,
// which it runs, answering the probe's output.
func csWideRecordCompile(t *testing.T, files map[string][]byte, body string) (string, error) {
	t.Helper()
	runtimeDir, err := filepath.Abs("../../../../serialize.cs")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(runtimeDir, "src", "Serialize.cs")); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and the C# runtime checkout is not at %s", runtimeDir)
		}
		t.Skipf("the C# runtime checkout is not at %s", runtimeDir)
	}
	dir := t.TempDir()
	var sources []string
	for name, data := range files {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, p)
	}
	target := "-target:library"
	if body != "" {
		main := filepath.Join(dir, "ProbeMain.cs")
		if err := os.WriteFile(main, []byte(fmt.Sprintf(t08Main, body)), 0o600); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, main)
		target = "-target:exe"
	}
	for _, rt := range []string{"Serialize.cs", "Int128Pair.cs"} {
		if p := filepath.Join(runtimeDir, "src", rt); fileExists(p) {
			sources = append(sources, p)
		}
	}
	csc, refs, runtime := csDotnetToolchain(t)
	args := []string{"exec", csc, "-nologo", "-unsafe", "-nowarn:CS8500", target, "-out:" + filepath.Join(dir, "wide.dll")}
	args = append(args, sources...)
	for _, r := range refs {
		args = append(args, "-r:"+r)
	}
	cmd := exec.Command(csDotnet(), args...)
	cmd.Env = csDotnetEnv(dir, dir)
	out, err := cmd.CombinedOutput()
	if err != nil || body == "" {
		return string(out), err
	}
	rc := fmt.Sprintf(`{ "runtimeOptions": { "tfm": "net10.0", "framework": { "name": "Microsoft.NETCore.App", "version": %q } } }`, runtime)
	if err := os.WriteFile(filepath.Join(dir, "wide.runtimeconfig.json"), []byte(rc), 0o600); err != nil {
		t.Fatal(err)
	}
	run := exec.Command(csDotnet(), "exec", filepath.Join(dir, "wide.dll"))
	run.Env = csDotnetEnv(dir, dir)
	ran, err := run.CombinedOutput()
	return string(ran), err
}
