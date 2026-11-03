package cstable

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/csharp"
	"github.com/mas-bandwidth/schema/v2/internal/memceiling"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// csWideRecordDir holds Wide.schema, a record past 4 GiB (docs/SPEC-TABLES.md
// §11) that every leg generates and compiles: Part is 2 GiB, Huge is three of
// them and a trailing field.
const csWideRecordDir = "../../../testdata/wire/tables/storage-cap/wide"

// TestWideRecordCSharp holds every stored size and offset the C# leg keeps at
// 64 bits on Wide.schema, a record past 4 GiB. Huge's descriptor carries its
// storage size, the offset of its tail and the items list's element size as
// the layout's own values, each a literal past 4 GiB: narrowed to an int, any
// of them is a compile error on its own line, which fails the test.
//
// THE VALUE-TYPE FORMS CANNOT HOLD THE RECORD, and the test pins exactly why.
// The block and cook rows, which the region form reads through, spell a
// record as a C# struct, whose StructLayout Size is an int (CS0182, CS0266)
// and whose fixed-size buffer is at most int.MaxValue bytes (CS1664); and
// CoreCLR loads no value type of 2 GiB or more (TypeLoadException: a type's
// instance size is an int32). So the unit compiles everywhere but at those
// struct declarations: every error must sit on a StructLayout or a
// fixed-buffer line, and there must be at least one.
//
// Roslyn binds no initializer while a declaration is in error, so the
// descriptors are held in a second compile: the same unit with exactly those
// struct declarations shrunk (the Size argument dropped, each fixed buffer one
// element), run as a probe that reads Huge's descriptor back. Nothing builds a
// Huge.
func TestWideRecordCSharp(t *testing.T) {
	slowtest.Gate(t, "the .NET SDK")
	memceiling.Guard(t, memceiling.Default)
	src, err := os.ReadFile(filepath.Join(csWideRecordDir, "Wide.schema"))
	if err != nil {
		t.Fatal(err)
	}
	u := unitFrom(t, string(src))
	huge := ir.RecordLayout(u, u.Tables["Huge"])
	var tail int64 = -1
	for _, f := range huge.Fields {
		if f.Field.Name == "tail" {
			tail = f.Offset
		}
	}
	if huge.Size <= 1<<32 || tail <= 1<<32 {
		t.Fatalf("Wide.schema no longer reaches past 4 GiB: Huge %d, tail at %d", huge.Size, tail)
	}
	files := csWideRecordSources(t, u)
	var all strings.Builder
	for _, data := range files {
		all.Write(data)
	}
	for _, want := range []string{
		fmt.Sprintf("info.StorageSize = %d;", huge.Size),
		fmt.Sprintf("NativeOffset = %d,", tail),
		fmt.Sprintf("NativeElementSize = %d,", huge.Size),
	} {
		if !strings.Contains(all.String(), want) {
			t.Errorf("the generated C# does not carry %q", want)
		}
	}
	out, err := csWideRecordCompile(t, files, "")
	if err == nil {
		t.Fatal("Wide.schema compiled whole in C#; the struct size limit this test pins has moved, so hold the value-type forms here")
	}
	errs := regexp.MustCompile(`([A-Za-z0-9_]+\.cs)\((\d+),\d+\): error (CS\d+)`).FindAllStringSubmatch(out, -1)
	if len(errs) == 0 {
		t.Fatalf("the C# compile failed without a located error:\n%s", out)
	}
	for _, e := range errs {
		data, ok := files[e[1]]
		if !ok {
			t.Fatalf("an error in %s, which the unit did not generate:\n%s", e[1], out)
		}
		line, _ := strconv.Atoi(e[2])
		lines := strings.Split(string(data), "\n")
		if line < 1 || line > len(lines) {
			t.Fatalf("%s has no line %d", e[1], line)
		}
		text := lines[line-1]
		limit := strings.Contains(text, "StructLayout") || strings.Contains(text, " fixed ")
		if !slices.Contains([]string{"CS1664", "CS0266", "CS0182"}, e[3]) || !limit {
			t.Fatalf("%s(%d): %s is not the struct size limit: %s", e[1], line, e[3], strings.TrimSpace(text))
		}
		lines[line-1] = csShrinkStructLine(text)
		files[e[1]] = []byte(strings.Join(lines, "\n"))
	}
	body := fmt.Sprintf(`
        var info = Widerecord.Schema.HugeTableType();
        if (info.StorageSize != %[1]dL) { bad |= ProbeLog.Fail("Huge storage size " + info.StorageSize); }
        long tail = -1;
        foreach (var f in info.Fields) { if (f.Name == "tail") { tail = f.NativeOffset; } }
        if (tail != %[2]dL) { bad |= ProbeLog.Fail("Huge.tail offset " + tail); }
        long items = -1;
        foreach (var f in Widerecord.Schema.RootTableType().Fields) { if (f.Name == "items") { items = f.NativeElementSize; } }
        if (items != %[1]dL) { bad |= ProbeLog.Fail("items element size " + items); }
`, huge.Size, tail)
	out, err = csWideRecordCompile(t, files, body)
	if err != nil || strings.Contains(out, "FAIL ") || !strings.Contains(out, "PROBE OK") {
		t.Fatalf("the descriptors of a record past 4 GiB did not read back at 64 bits: %v\n%s", err, out)
	}
}

var (
	csStructLayoutSize = regexp.MustCompile(`, Size = \d+\)`)
	csFixedLength      = regexp.MustCompile(`\[\d+\];`)
)

// csShrinkStructLine drops a struct declaration's StructLayout Size, or cuts
// a fixed-size buffer to one element, so the rest of the unit binds.
func csShrinkStructLine(line string) string {
	if strings.Contains(line, "StructLayout") {
		return csStructLayoutSize.ReplaceAllString(line, ")")
	}
	return csFixedLength.ReplaceAllString(line, "[1];")
}

// csWideRecordSources is the C# a unit generates: the packet form and the
// table form over it, keyed by file name.
func csWideRecordSources(t *testing.T, u *ir.Unit) map[string][]byte {
	t.Helper()
	files, err := csharp.Generate(u)
	if err != nil {
		t.Fatalf("csharp.Generate: %v", err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	maps.Copy(files, tables)
	out := map[string][]byte{}
	for name, data := range files {
		if strings.HasSuffix(name, ".cs") {
			out[filepath.Base(name)] = data
		}
	}
	return out
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
