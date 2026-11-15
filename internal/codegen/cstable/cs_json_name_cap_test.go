package cstable

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/csharp"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// The read-closed unit: the same flags, enum, union, string-keyed map and
// string the vectors under testdata/wire/json-read-closed/ read, plus the
// pointered root for the `&` name's site.
const csJsonNameCapSchema = `package jsonclosed

enum Kind { Alpha, Beta }

flags Perms { Read, Write }

type Buff
{
    mult float32 = 1.0
}

union Effect
{
    buff Buff
    ping
}

table Node
{
    value int32 = 0
}

table Scene
{
    head *Node
}

table Closed
{
    kind   Kind
    perms  Perms
    effect Effect
    tags   map[string]int32
    note   string(300)
}
`

// TestCSharpJsonNameCap holds the C# JSON read to §16.2's 255-byte cap on every
// schema-name string — a field key, a skipped object's key, an enum variant
// name, a flags element, a union arm key and a pointer's first key — while a
// long map key and a long string value still read. The verdict is the contract;
// a refusal's reason is the leg's own diagnostic and is not compared.
func TestCSharpJsonNameCap(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the .NET SDK and the read-closed vectors")

	dir := filepath.Join("..", "..", "..", "testdata", "wire", "json-read-closed")
	data, err := os.ReadFile(filepath.Join(dir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatalf("the read-closed manifest: %v", err)
	}
	nameCap := []string{
		"field-key-", "skipped-key-", "enum-name-", "flags-element-",
		"union-arm-", "ampersand-", "map-key-long", "string-value-long",
	}
	isNameCap := func(name string) bool {
		for _, prefix := range nameCap {
			if strings.HasPrefix(name, prefix) {
				return true
			}
		}
		return false
	}
	type vector struct {
		name    string
		root    string
		path    string
		verdict string
	}
	var vectors []vector
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 5 {
			t.Fatalf("%s/MANIFEST.tsv:%d: want five columns, got %d", dir, i+1, len(fields))
		}
		if !isNameCap(fields[0]) {
			continue
		}
		path := filepath.Join(dir, fields[2])
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("%s: %v", fields[2], err)
		}
		vectors = append(vectors, vector{name: fields[0], root: fields[1], path: path, verdict: fields[3]})
	}
	if len(vectors) == 0 {
		t.Fatal("no name-cap vectors read; the gate would pass over an empty set")
	}

	u := unitFrom(t, csJsonNameCapSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	var sources []string
	var packetRuntime []string
	if len(u.Enums) != 0 || len(u.Structs) != 0 || len(u.Unions) != 0 {
		packet, err := csharp.Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		maps.Copy(files, packet)
		runtimeDir, err := filepath.Abs("../../../../serialize.cs/src")
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"Serialize.cs", "Int128Pair.cs"} {
			path := filepath.Join(runtimeDir, name)
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("C# packet runtime prerequisite: %v", err)
			}
			packetRuntime = append(packetRuntime, path)
		}
	}
	dirOut := t.TempDir()
	for name, body := range files {
		if !strings.HasSuffix(name, ".cs") {
			continue
		}
		path := filepath.Join(dirOut, name)
		if err := os.WriteFile(path, body, 0o600); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, path)
	}

	const driver = `using System;
using System.IO;
using Jsonclosed;
class JsonNameCapProbe {
 static void Main(string[] args) {
  for (int i = 0; i + 1 < args.Length; i += 2) {
   bool ok; var report = new TableReport();
   if (args[i] == "Scene") { var value = new Scene(); ok = Schema.SceneFromJson(value, File.ReadAllBytes(args[i + 1]), report); }
   else { var value = new Closed(); ok = Schema.ClosedFromJson(value, File.ReadAllBytes(args[i + 1]), report); }
   Console.WriteLine(ok ? "read" : "refused");
  }
 }
}`
	main := filepath.Join(dirOut, "JsonNameCapProbe.cs")
	if err := os.WriteFile(main, []byte(driver), 0o600); err != nil {
		t.Fatal(err)
	}
	sources = append(sources, main)
	sources = append(sources, packetRuntime...)

	csc, refs, runtime := csDotnetToolchain(t)
	args := append([]string{"exec", csc, "-nologo", "-unsafe", "-nowarn:CS8500", "-out:" + filepath.Join(dirOut, "probe.dll")}, sources...)
	for _, ref := range refs {
		args = append(args, "-r:"+ref)
	}
	cmd := exec.Command(csDotnet(), args...)
	cmd.Env = csDotnetEnv(dirOut, dirOut)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, output)
	}
	config := fmt.Sprintf(`{"runtimeOptions":{"tfm":"net10.0","framework":{"name":"Microsoft.NETCore.App","version":%q}}}`, runtime)
	if err := os.WriteFile(filepath.Join(dirOut, "probe.runtimeconfig.json"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	var runArgs []string
	for _, v := range vectors {
		runArgs = append(runArgs, v.root, v.path)
	}
	cmd = exec.Command(csDotnet(), append([]string{"exec", filepath.Join(dirOut, "probe.dll")}, runArgs...)...)
	cmd.Env = csDotnetEnv(dirOut, dirOut)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run: %v\n%s", err, output)
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) != len(vectors) {
		t.Fatalf("got %d verdicts, want %d: %s", len(lines), len(vectors), output)
	}
	for i, v := range vectors {
		got := strings.TrimRight(lines[i], "\r")
		if got != v.verdict {
			t.Errorf("%s: got %q, want %q", v.name, got, v.verdict)
		}
	}
}
