package csharp

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const unionByteArraySchema = `package unionbytes

type Small {
    v uint8 | min = 0, max = 127
}

union U {
    a Small
}

type T {
    align
    u    U
    data [4]uint8
}
`

// TestCSharpUnionThenFixedByteArrayWire writes a packet whose fixed [4]uint8
// array follows a union at None. The union occupies one bit, so the four bytes
// ride in bits 1..32 and the packet is 33 bits ({0x02, 0x04, 0x06, 0x08, 0x00}).
func TestCSharpUnionThenFixedByteArrayWire(t *testing.T) {
	t.Parallel()

	f, perrs := parser.Parse("UnionBytes.schema", []byte(unionByteArraySchema))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "UnionBytes.schema", Name: "UnionBytes.schema", Base: "UnionBytes",
		Bytes: []byte(unionByteArraySchema), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}

	var tStruct *ir.Struct
	var dataField *ir.Field
	for _, st := range u.Structs {
		if st.Name == "T" {
			tStruct = st
			for _, fld := range st.Fields {
				if fld.Name == "data" {
					dataField = fld
				}
			}
		}
	}
	if tStruct == nil || dataField == nil {
		t.Fatal("missing T.data in unit")
	}
	if ir.AlignedFixedByteArrays(tStruct)[dataField] {
		t.Error("AlignedFixedByteArrays must not mark T.data after a union field")
	}

	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for _, fn := range []string{"private static bool WriteTBatch(", "private static bool ReadTBatch("} {
		body := methodBody(t, files, fn)
		if strings.Contains(body, "SerializeBytes(") {
			t.Errorf("%s used bulk SerializeBytes for T.data after a union field:\n%s", fn, body)
		}
		if !strings.Contains(body, "for (int i = 0; i < 4; i++)") {
			t.Errorf("%s missing per-byte loop for T.data:\n%s", fn, body)
		}
	}

	runUnionByteArrayCsProbe(t, files)
}

func TestCSharpUnionThenFixedByteArrayWireNegativeControl(t *testing.T) {
	t.Parallel()
	out, failed := slowtest.RunSabotaged(t, "../../..", "./internal/codegen/csharp", "^TestCSharpUnionThenFixedByteArrayWire$",
		slowtest.Sabotage{
			File: "ir/wire.go",
			From: "\tcase isUnion:\n\t\treturn wireUnknown\n\t}",
			To:   "\t}\n\t_ = isUnion",
		})
	if !failed {
		t.Fatalf("negative control passed with pre-fix alignment analysis:\n%s", out)
	}
}

const unionByteArrayCsDriver = `using System;
using Serialize;
using Unionbytes;

static class Program
{
    static void Fail(string msg)
    {
        Console.Error.WriteLine("FAILED: " + msg);
        Environment.Exit(1);
    }

    static int Main()
    {
        var value = new T();
        value.U.Type = UType.None;
        value.Data[0] = 1;
        value.Data[1] = 2;
        value.Data[2] = 3;
        value.Data[3] = 4;

        byte[] buffer = new byte[Schema.TMaxBytes];
        var writer = new WriteStream(buffer);
        if (!Schema.WriteT(writer, ref value))
            Fail("WriteT returned false");
        writer.Flush();

        if (writer.BitsProcessed != 33)
            Fail($"BitsProcessed = {writer.BitsProcessed}, want 33");
        if (writer.BytesProcessed != 5)
            Fail($"BytesProcessed = {writer.BytesProcessed}, want 5");

        byte[] expected = new byte[] { 0x02, 0x04, 0x06, 0x08, 0x00 };
        ReadOnlySpan<byte> wire = writer.Data.Slice(0, 5);
        if (!wire.SequenceEqual(expected))
            Fail($"wire = [{string.Join(", ", wire.ToArray())}], want [2, 4, 6, 8, 0]");

        var back = new T();
        back.U.Type = UType.A;
        var reader = new ReadStream(wire.ToArray());
        if (!Schema.ReadT(reader, ref back))
            Fail("ReadT returned false");
        if (back.U.Type != UType.None)
            Fail($"back.U.Type = {back.U.Type}, want None");
        for (int i = 0; i < 4; i++)
        {
            if (back.Data[i] != value.Data[i])
                Fail($"back.Data[{i}] = {back.Data[i]}, want {value.Data[i]}");
        }

        Console.WriteLine("T round trip: 5 bytes");
        return 0;
    }
}
`

func runUnionByteArrayCsProbe(t *testing.T, files map[string][]byte) {
	t.Helper()
	slowtest.Gate(t, "dotnet (it compiles and runs the generated C# packet probe)")

	dir := t.TempDir()
	var sources []string
	for name, data := range files {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, path)
	}

	runtimeDir := os.Getenv("SERIALIZE_CS")
	if runtimeDir == "" {
		runtimeDir = "../../../../serialize.cs"
	}
	runtimeDir, err := filepath.Abs(runtimeDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Serialize.cs", "Int128Pair.cs"} {
		path := filepath.Join(runtimeDir, "src", name)
		if _, err := os.Stat(path); err != nil {
			if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
				t.Fatalf("serialize.cs runtime missing at %s: %v", path, err)
			}
			t.Skipf("serialize.cs runtime not staged at %s: %v", path, err)
		}
		sources = append(sources, path)
	}

	driverPath := filepath.Join(dir, "Program.cs")
	if err := os.WriteFile(driverPath, []byte(unionByteArrayCsDriver), 0o600); err != nil {
		t.Fatal(err)
	}
	sources = append(sources, driverPath)

	csc, refs, runtime := csUnionBytesToolchain(t)
	dotnet := csUnionBytesDotnet()
	app := filepath.Join(dir, "probe.dll")
	args := []string{"exec", csc, "-nologo", "-noconfig", "-target:exe", "-optimize+", "-unsafe",
		"-nullable:annotations", "-langversion:latest", "-nostdlib+", "-out:" + app}
	for _, ref := range refs {
		args = append(args, "-r:"+ref)
	}
	args = append(args, sources...)
	compile := exec.Command(dotnet, args...)
	compile.Env, compile.Dir = csUnionBytesEnv(dir, dir), dir
	if out, err := compile.CombinedOutput(); err != nil {
		t.Fatalf("csc compile failed: %v\n%s", err, out)
	}

	cfg := fmt.Sprintf("{\"runtimeOptions\":{\"tfm\":\"net10.0\",\"framework\":{\"name\":\"Microsoft.NETCore.App\",\"version\":%q}}}\n", runtime)
	if err := os.WriteFile(filepath.Join(dir, "probe.runtimeconfig.json"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	run := exec.Command(dotnet, "exec", app)
	run.Env, run.Dir = csUnionBytesEnv(dir, dir), dir
	out, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("probe failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "T round trip: 5 bytes") {
		t.Fatalf("unexpected probe output:\n%s", out)
	}
}

func csUnionBytesDotnet() string {
	if dotnet := os.Getenv("DOTNET"); dotnet != "" {
		return dotnet
	}
	return "dotnet"
}

func csUnionBytesToolchain(t *testing.T) (csc string, refs []string, runtime string) {
	t.Helper()
	path, err := exec.LookPath(csUnionBytesDotnet())
	if err != nil {
		t.Skipf("dotnet not found: %v", err)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		resolved = path
	}
	root := filepath.Dir(resolved)
	cscs, _ := filepath.Glob(filepath.Join(root, "sdk", "*", "Roslyn", "bincore", "csc.dll"))
	if len(cscs) == 0 {
		t.Fatalf("no csc.dll under %s", root)
	}
	sort.Strings(cscs)
	csc = cscs[len(cscs)-1]
	refDirs, _ := filepath.Glob(filepath.Join(root, "packs", "Microsoft.NETCore.App.Ref", "*", "ref", "net*"))
	if len(refDirs) == 0 {
		t.Fatalf("no reference assemblies under %s", root)
	}
	sort.Strings(refDirs)
	refs, _ = filepath.Glob(filepath.Join(refDirs[len(refDirs)-1], "*.dll"))
	if len(refs) == 0 {
		t.Fatalf("no reference assemblies in %s", refDirs[len(refDirs)-1])
	}
	runtimes, _ := filepath.Glob(filepath.Join(root, "shared", "Microsoft.NETCore.App", "*"))
	if len(runtimes) == 0 {
		t.Fatalf("no shared runtime under %s", root)
	}
	sort.Strings(runtimes)
	runtime = filepath.Base(runtimes[len(runtimes)-1])
	return csc, refs, runtime
}

func csUnionBytesEnv(home, tmp string) []string {
	env := make([]string, 0, len(os.Environ())+8)
	for _, e := range os.Environ() {
		switch {
		case strings.HasPrefix(e, "HOME="),
			strings.HasPrefix(e, "TMPDIR="),
			strings.HasPrefix(e, "TMP="),
			strings.HasPrefix(e, "TEMP="),
			strings.HasPrefix(e, "DOTNET_"):
			continue
		}
		env = append(env, e)
	}
	return append(env,
		"DOTNET_NOLOGO=1",
		"DOTNET_SKIP_FIRST_TIME_EXPERIENCE=1",
		"HOME="+home,
		"DOTNET_CLI_HOME="+home,
		"TMPDIR="+tmp,
		"TMP="+tmp,
		"TEMP="+tmp,
	)
}
