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
	"github.com/mas-bandwidth/schema/v2/internal/fixedvector"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// EVERY DEAD BYTE IN A FIXED RECORD IS ZERO, AND A NON-ZERO ONE REFUSES THE
// WHOLE LOAD (docs/rulings.json fixed-dead-bytes-are-zero; docs/SPEC-TABLES.md
// §3.4, §13.10; docs/FIXED-FORM-ALGORITHM.md §3.1, §4.5, §5.3). The dead bytes
// are a counted array's slots past its count, an absent optional's payload, a
// string(N)'s or bytes(N)'s bytes past its used length, and an inactive union
// arm: the bytes under tag 0 and the bytes past a narrower taken arm.
//
// The vectors are the reference writer's (fixedvector.DeadBytes): each control
// is the same record with every dead byte zero, and each changed file is one
// dead region made non-zero. The generated C# load refuses the changed files
// whole before any record lands — the verdict only, never a reason — and leaves
// the caller's destination as it was; a control reads and saves back byte for
// byte.
func TestFixedDeadBytesAreZero(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dotnet (it runs the generated reader over the dead-byte corpus)")
	corpus := csFixedCorpus(t)
	u := csUnitOf(t, csReadSchema(t, "DeadBytes"))
	files, err := csharp.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := GenerateLineage(u, map[string][]FixedLineageEntry{})
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	ns := capitalize(u.Package)
	table := "DeadRoot"
	dir := t.TempDir()
	csVersionWrite(t, filepath.Join(dir, "gen", "u000"), files)

	var nameList, readList []string
	for _, vec := range fixedvector.DeadBytes {
		nameList = append(nameList, fmt.Sprintf("%q", vec.File))
		readList = append(readList, fmt.Sprintf("%t", vec.Reads))
	}
	body := strings.NewReplacer(
		"@TABLE@", table,
		"@NAME@", `"dead_bytes"`,
		"@FILES@", strings.Join(nameList, ", "),
		"@READS@", strings.Join(readList, ", "),
	).Replace(`
            string[] files = { @FILES@ };
            bool[] reads = { @READS@ };
            for (int v = 0; v < files.Length; ++v)
            {
                string f = files[v];
                byte[] data = System.IO.File.ReadAllBytes(System.IO.Path.Combine(corpus, f));
                @TABLE@[] back = new @TABLE@[1];
                back[0] = new @TABLE@();
                // THE DESTINATION IS POISONED IN EVERY MEMBER AND EVERY
                // ELEMENT: a refusal must leave the COMPLETE value alone,
                // including the absent optional's payload, which is dead and
                // never a value to land.
                back[0].OptPresent = true;
                back[0].Opt.N = 0x5A5A5A5A;
                for (int i = 0; i < back[0].Opt.Label.Length; ++i) { back[0].Opt.Label[i] = (byte)(0xA0 + i); }
                back[0].Opt.LabelLength = 3;
                back[0].Opt.Heat = 123.5f;
                back[0].Opt.Flag = true;
                back[0].Opt.Kind = DeadKind.Gamma;
                for (int i = 0; i < back[0].Opt.Nums.Length; ++i) { back[0].Opt.Nums[i] = (short)(100 + i); }
                back[0].Opt.NumsCount = 2;
                back[0].Opt.ChildPresent = true;
                back[0].Opt.Child = 0x5A5A5A5A;
                for (int i = 0; i < back[0].Arr.Length; ++i) { back[0].Arr[i] = (short)(200 + i); }
                back[0].ArrCount = 2;
                for (int i = 0; i < back[0].Objs.Length; ++i)
                {
                    back[0].Objs[i].X = 300 + i;
                    back[0].Objs[i].Y = 400 + i;
                }
                back[0].ObjsCount = 2;
                for (int i = 0; i < back[0].Text.Length; ++i) { back[0].Text[i] = (byte)(65 + i); }
                back[0].TextLength = 3;
                for (int i = 0; i < back[0].Blob.Length; ++i) { back[0].Blob[i] = (byte)(90 + i); }
                back[0].BlobLength = 2;
                back[0].Pick.Type = DeadPickType.Wide;
                back[0].Pick.Narrow = 7;
                back[0].Pick.Wide.A = 501;
                back[0].Pick.Wide.B = 502;
                back[0].Pick.Wide.C = 503;
                string before = DestinationSnapshot.Take(back[0]);
                TableReport r = new TableReport();
                TableFixedEntry[] plan = new TableFixedEntry[8192];
                long n = Schema.@TABLE@FixedLoad(back, data, plan, r);
                if (reads[v])
                {
                    if (n != 1 || r.Refused || r.Verdict != Schema.TableWire.Verdict.Ok || r.Reason != null)
                    {
                        bad += ProbeLog.Fail(@NAME@, f + ": a control must read: n=" + n + " reason=" + r.Reason);
                        continue;
                    }
                    long need = Schema.@TABLE@FixedMeasure(1);
                    byte[] saved = new byte[need];
                    long got = Schema.@TABLE@FixedSave(back[0], saved);
                    if (got != need || !saved.AsSpan(0, (int)got).SequenceEqual(data))
                    {
                        bad += ProbeLog.Fail(@NAME@, f + ": a control does not save back byte for byte: got=" + got + " need=" + need);
                    }
                }
                else
                {
                    if (n != -1 || !r.Refused || r.Verdict != Schema.TableWire.Verdict.Refused)
                    {
                        bad += ProbeLog.Fail(@NAME@, f + ": a non-zero dead byte must refuse the whole load: n=" + n + " verdict=" + r.Verdict + " reason=" + r.Reason);
                    }
                    string after = DestinationSnapshot.Take(back[0]);
                    if (after != before)
                    {
                        bad += ProbeLog.Fail(@NAME@, f + ": a refusal wrote the caller's destination: " + before + " -> " + after);
                    }
                }
            }
`)

	src := fmt.Sprintf(`using System;
using System.Reflection;

static class ProbeLog
{
    public static int Fail(string row, string what)
    {
        Console.WriteLine("ROW " + row + ": FAILED " + what);
        return 1;
    }
}

// DestinationSnapshot writes every public field of a destination value,
// recursively through arrays and nested objects, so a refusal is held to the
// COMPLETE value rather than a handpicked member list.
static class DestinationSnapshot
{
    public static string Take(object value)
    {
        System.Text.StringBuilder sb = new System.Text.StringBuilder();
        Write(sb, value);
        return sb.ToString();
    }

    private static void Write(System.Text.StringBuilder sb, object value)
    {
        if (value == null) { sb.Append("null"); return; }
        Type t = value.GetType();
        Array a = value as Array;
        if (a != null)
        {
            sb.Append('[').Append(a.Length).Append(':');
            for (int i = 0; i < a.Length; ++i)
            {
                if (i != 0) { sb.Append(','); }
                Write(sb, a.GetValue(i));
            }
            sb.Append(']');
            return;
        }
        if (t.IsEnum)
        {
            sb.Append(Convert.ToInt64(value, System.Globalization.CultureInfo.InvariantCulture));
            return;
        }
        if (value is float f)
        {
            sb.Append(BitConverter.SingleToInt32Bits(f));
            return;
        }
        if (value is double d)
        {
            sb.Append(BitConverter.DoubleToInt64Bits(d));
            return;
        }
        if (t.IsPrimitive || value is string || value is decimal)
        {
            sb.Append(Convert.ToString(value, System.Globalization.CultureInfo.InvariantCulture));
            return;
        }
        sb.Append(t.FullName).Append('{');
        FieldInfo[] fields = t.GetFields(BindingFlags.Public | BindingFlags.Instance);
        Array.Sort(fields, (x, y) => string.CompareOrdinal(x.Name, y.Name));
        for (int i = 0; i < fields.Length; ++i)
        {
            if (i != 0) { sb.Append(','); }
            sb.Append(fields[i].Name).Append('=');
            Write(sb, fields[i].GetValue(value));
        }
        sb.Append('}');
    }
}

namespace %[1]s
{
    static class Probe000
    {
        public static int Run(string corpus)
        {
            int bad = 0;
%[2]s
            if (bad == 0) { Console.WriteLine("ROW " + %[3]q + ": ok"); }
            return bad;
        }
    }
}

static class ProbeMain
{
    static int Main()
    {
        return %[1]s.Probe000.Run(Environment.GetEnvironmentVariable("SCHEMA_FIXEDFORM_CORPUS")) == 0 ? 0 : 1;
    }
}
`, ns, body, "dead_bytes")
	if err := os.WriteFile(filepath.Join(dir, "Probe.cs"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	runtime, err := filepath.Abs("../../../../serialize.cs")
	if err != nil {
		t.Fatal(err)
	}
	proj := fmt.Sprintf(`<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <OutputType>Exe</OutputType>
    <TargetFramework>net10.0</TargetFramework>
    <ImplicitUsings>disable</ImplicitUsings>
    <Nullable>annotations</Nullable>
    <EnableDefaultCompileItems>false</EnableDefaultCompileItems>
    <AllowUnsafeBlocks>true</AllowUnsafeBlocks>
    <AssemblyName>csdeadbytes</AssemblyName>
    <NoWarn>CS0162;CS0168;CS0219;CS8600;CS8602;CS8604;CS8625</NoWarn>
  </PropertyGroup>
  <ItemGroup>
    <Compile Include="*.cs" />
    <Compile Include="gen/**/*.cs" />
    <Compile Include="%[1]s/src/Serialize.cs" />
    <Compile Include="%[1]s/src/Int128Pair.cs" />
  </ItemGroup>
</Project>
`, runtime)
	if err := os.WriteFile(filepath.Join(dir, "csdeadbytes.csproj"), []byte(proj), 0o600); err != nil {
		t.Fatal(err)
	}
	dotnet := os.Getenv("DOTNET")
	if dotnet == "" {
		dotnet = "dotnet"
	}
	cmd := exec.Command(dotnet, "run", "--project", dir, "-v", "q", "--nologo")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "SCHEMA_FIXEDFORM_CORPUS="+corpus, "DOTNET_NOLOGO=1")
	out, runErr := cmd.CombinedOutput()
	if strings.Contains(string(out), "ROW dead_bytes: ok") {
		return
	}
	t.Fatalf("the dead-byte probe did not go green:\n%s", csVersionTail(string(out), runErr))
}
