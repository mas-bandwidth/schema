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

// TestCSharpFixedBatchHoldsARecord holds docs/SPEC-TABLES.md §22.6 and
// docs/FIXED-FORM-ALGORITHM.md §5.3 step 9 on the C# leg: a fixed-form file
// holds at least one record. The empty-batch vector is refused by every fixed
// read entry (the Span, array and single-value loads), the one-record file
// beside it still reads, and a save of zero records is refused by the leg's
// debug-only write assert.
func TestCSharpFixedBatchHoldsARecord(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the .NET toolchain")

	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	schema, err := os.ReadFile(filepath.Join(root, "test", "fixed-empty-batch", "Batch.schema"))
	if err != nil {
		t.Fatal(err)
	}
	u := csUnitOf(t, string(schema))
	files, err := csharp.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)

	dir := t.TempDir()
	csVersionWrite(t, filepath.Join(dir, "gen"), files)
	ns := capitalize(u.Package)
	table := csFixedRootName(t, u)
	probe := strings.NewReplacer("@NS@", ns, "@TABLE@", table).Replace(csFixedBatchProbe)
	if err := os.WriteFile(filepath.Join(dir, "Probe.cs"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}

	runtime, err := filepath.Abs("../../../../serialize.cs")
	if err != nil {
		t.Fatal(err)
	}
	project := fmt.Sprintf(`<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <OutputType>Exe</OutputType>
    <TargetFramework>net10.0</TargetFramework>
    <ImplicitUsings>disable</ImplicitUsings>
    <Nullable>annotations</Nullable>
    <EnableDefaultCompileItems>false</EnableDefaultCompileItems>
    <AllowUnsafeBlocks>true</AllowUnsafeBlocks>
    <AssemblyName>csfixedbatch</AssemblyName>
    <NoWarn>CS0162;CS0168;CS0219;CS8600;CS8602;CS8604;CS8625</NoWarn>
  </PropertyGroup>
  <ItemGroup>
    <Compile Include="*.cs" />
    <Compile Include="gen/**/*.cs" />
    <Compile Include="%s/src/Serialize.cs" />
    <Compile Include="%s/src/Int128Pair.cs" />
  </ItemGroup>
</Project>
`, runtime, runtime)
	if err := os.WriteFile(filepath.Join(dir, "csfixedbatch.csproj"), []byte(project), 0o600); err != nil {
		t.Fatal(err)
	}

	vector := filepath.Join(root, "testdata", "wire", "fixed-empty-batch")
	dotnet := os.Getenv("DOTNET")
	if dotnet == "" {
		dotnet = "dotnet"
	}
	run := func(mode string) ([]byte, error) {
		cmd := exec.Command(dotnet, "run", "--project", dir, "-v", "q", "--", mode)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "SCHEMA_FIXED_EMPTY_BATCH="+vector, "DOTNET_NOLOGO=1")
		return cmd.CombinedOutput()
	}

	readOut, readErr := run("read")
	if readErr != nil {
		t.Fatalf("the generated reader did not run cleanly: %v\n%s", readErr, readOut)
	}
	if !strings.Contains(string(readOut), "READ OK") {
		t.Fatalf("the empty-batch vector or the one-record file did not read as the rule says:\n%s", readOut)
	}

	saveOut, saveErr := run("save-empty")
	if saveErr == nil {
		t.Fatalf("a save of zero records returned instead of refusing in a debug build:\n%s", saveOut)
	}
	if !strings.Contains(string(saveOut), "SAVE-EMPTY-STARTED") {
		t.Fatalf("the empty save probe did not start:\n%s", saveOut)
	}
	if !strings.Contains(string(saveOut), "a fixed-form file holds at least one record") {
		t.Fatalf("a save of zero records did not hit the debug write assert:\n%s", saveOut)
	}
	if strings.Contains(string(saveOut), "SAVE-EMPTY-RETURNED") {
		t.Fatalf("a save of zero records returned instead of asserting:\n%s", saveOut)
	}
}

const csFixedBatchProbe = `using System;
using Schema = @NS@.Schema;
using @NS@;

static class FixedBatchProbe
{
    static int bad = 0;

    static void Fail(string what)
    {
        Console.WriteLine("FAILED " + what);
        bad++;
    }

    static @TABLE@[] Fresh(int n)
    {
        @TABLE@[] values = new @TABLE@[n];
        for (int i = 0; i < n; ++i) { values[i] = new @TABLE@(); }
        return values;
    }

    static int Main(string[] args)
    {
        string vector = Environment.GetEnvironmentVariable("SCHEMA_FIXED_EMPTY_BATCH");
        if (args.Length > 0 && args[0] == "save-empty") { return SaveEmpty(); }
        return Read(vector);
    }

    static int Read(string vector)
    {
        byte[] none = System.IO.File.ReadAllBytes(System.IO.Path.Combine(vector, "no-record.bin"));
        byte[] one = System.IO.File.ReadAllBytes(System.IO.Path.Combine(vector, "one-record.bin"));
        TableFixedEntry[] plan = new TableFixedEntry[8192];

        // THE EMPTY-BATCH FILE IS REFUSED BY EVERY READ ENTRY.
        {
            @TABLE@[] values = Fresh(4);
            TableReport report = new TableReport();
            long n = Schema.@TABLE@FixedLoad(values.AsSpan(), none, plan, report);
            if (n != -1) { Fail("the Span read of the empty-batch file returned " + n); }
            if (report.Verdict == Schema.TableWire.Verdict.Ok) { Fail("the Span read of the empty-batch file answered Ok"); }
        }
        {
            @TABLE@[] values = Fresh(4);
            TableReport report = new TableReport();
            long n = Schema.@TABLE@FixedLoad(values, none, plan, report);
            if (n != -1) { Fail("the array read of the empty-batch file returned " + n); }
            if (report.Verdict == Schema.TableWire.Verdict.Ok) { Fail("the array read of the empty-batch file answered Ok"); }
        }
        {
            @TABLE@ value = new @TABLE@();
            TableReport report = new TableReport();
            long n = Schema.@TABLE@FixedLoad(value, none, plan, report);
            if (n != -1) { Fail("the single-value read of the empty-batch file returned " + n); }
            if (report.Verdict == Schema.TableWire.Verdict.Ok) { Fail("the single-value read of the empty-batch file answered Ok"); }
        }

        // THE ONE-RECORD FILE STILL READS THROUGH EVERY ENTRY.
        {
            @TABLE@[] values = Fresh(4);
            TableReport report = new TableReport();
            long n = Schema.@TABLE@FixedLoad(values.AsSpan(), one, plan, report);
            if (n != 1 || values[0].A != 7 || System.Text.Encoding.UTF8.GetString(values[0].Name, 0, values[0].NameLength) != "hello") { Fail("the Span read of the one-record file: n=" + n + " a=" + values[0].A + " name=" + System.Text.Encoding.UTF8.GetString(values[0].Name, 0, values[0].NameLength)); }
        }
        {
            @TABLE@[] values = Fresh(4);
            TableReport report = new TableReport();
            long n = Schema.@TABLE@FixedLoad(values, one, plan, report);
            if (n != 1 || values[0].A != 7 || System.Text.Encoding.UTF8.GetString(values[0].Name, 0, values[0].NameLength) != "hello") { Fail("the array read of the one-record file: n=" + n + " a=" + values[0].A + " name=" + System.Text.Encoding.UTF8.GetString(values[0].Name, 0, values[0].NameLength)); }
        }
        {
            @TABLE@ value = new @TABLE@();
            TableReport report = new TableReport();
            long n = Schema.@TABLE@FixedLoad(value, one, plan, report);
            if (n != 1 || value.A != 7 || System.Text.Encoding.UTF8.GetString(value.Name, 0, value.NameLength) != "hello") { Fail("the single-value read of the one-record file: n=" + n + " a=" + value.A + " name=" + System.Text.Encoding.UTF8.GetString(value.Name, 0, value.NameLength)); }
        }

        if (bad == 0) { Console.WriteLine("READ OK"); }
        return bad == 0 ? 0 : 1;
    }

    static int SaveEmpty()
    {
        Console.WriteLine("SAVE-EMPTY-STARTED");
        Console.Out.Flush();
        byte[] buffer = new byte[Schema.@TABLE@FixedMeasure(0)];
        long n = Schema.@TABLE@FixedSave(Array.Empty<@TABLE@>(), buffer);
        Console.WriteLine("SAVE-EMPTY-RETURNED " + n);
        return n == -1 ? 0 : 1;
    }
}
`
