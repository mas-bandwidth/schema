package cstable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// §5.3 `fixed_form_under_20_bytes`, schema#876 item F7 — a GAP on all nine legs,
// one of the six joint-worst rows in that issue. Five legs have already landed
// it (go, c, dart, js, cpp); cs is one of the four that remain. The two answers
// the fixed reader owes are `refused` plus a `reason` on one hand and `malformed`
// on the other, and they are NEVER both set. A short file is the RESIDUE, not a
// refusal by name, so `Reason` stays untouched and asserting `layout_malformed`
// here would be wrong — that name belongs to a layout that lies about a known
// version, not to a buffer that ends before the header does. This leg also
// answers a field the other eight do not: `report.Verdict` is set to
// `TableWire.Verdict.Damaged` alongside `Malformed`. k == 0 is guarded by its own
// earlier clause (`data.Length < 1`) rather than the twenty-byte clause, so I
// measure both and require the same answer. The destination is poisoned to a
// recognisable sentinel before each short read and compared back afterwards. The
// whole value of this card is CONTROL 2, which deletes the twenty-byte guard and
// must turn the row red with an out-of-range exception out of the reserved-byte
// loop.
func TestFixedFormUnder20Bytes(t *testing.T) {
	corpus := csFixedCorpus(t)
	dir := t.TempDir()

	probes := []csVersionProbe{
		{name: "fixed_form_under_20_bytes", reader: "VNEW_array_bounded_grow",
			older: []string{"VOLD_array_bounded_grow"}, file: "old_array_bounded_grow.bin",
			body: `            byte[] data = System.IO.File.ReadAllBytes(System.IO.Path.Combine(corpus, @FILE@));
            // THE FIXTURE READS CLEANLY AT ITS FULL LENGTH FIRST, so a broken
            // fixture cannot pass this row by accident.
            @TABLE@[] full = new @TABLE@[8];
            for (int i = 0; i < full.Length; ++i) { full[i] = new @TABLE@(); }
            TableReport rf = new TableReport();
            TableFixedEntry[] planf = new TableFixedEntry[8192];
            long nf = Schema.@TABLE@FixedLoad(full, data, planf, rf);
            if (nf != 1 || rf.Refused || rf.Malformed || rf.Reason != null)
            {
                bad += ProbeLog.Fail(@NAME@, "the fixture does not read cleanly at its full length: n=" + nf + " refused=" + rf.Refused + " malformed=" + rf.Malformed + " reason=" + rf.Reason);
            }
            // data.AsSpan(0, k) is the whole truncation: k runs over every
            // length below TableFixedWire.HeaderBytes + 4 (twenty bytes), the
            // two bytes the header and the layout's own u32 length occupy
            // together. k == 0 is the empty span, k == 19 the last short one.
            int limit = Schema.TableFixedWire.HeaderBytes + 4;
            for (int k = 0; k < limit; ++k)
            {
                @TABLE@[] back = new @TABLE@[8];
                for (int i = 0; i < back.Length; ++i) { back[i] = new @TABLE@(); }
                // THE POISON: every field of back[0] carries a recognisable
                // sentinel, so a read that returns -1 can be told apart from one
                // that wrote the destination. 0x5A5A5A5A is neither the lead,
                // the trail, nor any declared default.
                back[0].Lead = 0x5A5A5A5Au;
                back[0].ValsCount = 0x5A5A5A5A;
                for (int i = 0; i < 8; ++i) { back[0].Vals[i] = 0x5A5A5A5A; }
                back[0].Trail = 0x5A5A5A5Au;
                TableReport r = new TableReport();
                TableFixedEntry[] plan = new TableFixedEntry[8192];
                long n = Schema.@TABLE@FixedLoad(back, data.AsSpan(0, k), plan, r);
                if (n != -1)
                {
                    bad += ProbeLog.Fail(@NAME@, "a file shorter than the header must be malformed and return -1: k=" + k + " n=" + n + " malformed=" + r.Malformed + " refused=" + r.Refused);
                    continue;
                }
                if (!r.Malformed)
                {
                    bad += ProbeLog.Fail(@NAME@, "a short file is malformed, never clean: k=" + k + " malformed=" + r.Malformed);
                }
                if (r.Refused)
                {
                    bad += ProbeLog.Fail(@NAME@, "a short file is the residue, not a refusal by name: k=" + k + " refused=" + r.Refused + " reason=" + r.Reason);
                }
                if (r.Reason != null)
                {
                    bad += ProbeLog.Fail(@NAME@, "reason stays untouched on a malformed read: k=" + k + " reason=" + r.Reason);
                }
                if (r.Verdict != Schema.TableWire.Verdict.Damaged)
                {
                    bad += ProbeLog.Fail(@NAME@, "this leg answers Verdict.Damaged alongside Malformed: k=" + k + " verdict=" + r.Verdict);
                }
                if (r.Unknown != 0 || r.KindMismatch != 0 || r.Clamped != 0 || r.Duplicate != 0 || r.Widened != 0 || r.Retained != 0 || r.RetainLost != 0)
                {
                    bad += ProbeLog.Fail(@NAME@, "every counter is zero on a malformed read: k=" + k + " unknown=" + r.Unknown + " kind=" + r.KindMismatch + " clamped=" + r.Clamped + " duplicate=" + r.Duplicate + " widened=" + r.Widened + " retained=" + r.Retained + " lost=" + r.RetainLost);
                }
                if (r.LayoutHash != 0)
                {
                    bad += ProbeLog.Fail(@NAME@, "malformed publishes no hash: k=" + k + " hash=0x" + r.LayoutHash.ToString("x16"));
                }
                if (back[0].Lead != 0x5A5A5A5Au || back[0].ValsCount != 0x5A5A5A5A || back[0].Trail != 0x5A5A5A5Au)
                {
                    bad += ProbeLog.Fail(@NAME@, "the malformed read wrote the destination: k=" + k + " lead=" + back[0].Lead + " valsCount=" + back[0].ValsCount + " trail=" + back[0].Trail);
                }
                for (int i = 0; i < 8; ++i)
                {
                    if (back[0].Vals[i] != 0x5A5A5A5A)
                    {
                        bad += ProbeLog.Fail(@NAME@, "the malformed read wrote vals[" + i + "]: k=" + k + " v=" + back[0].Vals[i]);
                    }
                }
            }`},
	}

	var calls []string
	for i, p := range probes {
		ns, table, files := csVersionGenerate(t, p)
		gen := filepath.Join(dir, "gen", fmt.Sprintf("u%03d", i))
		csVersionWrite(t, gen, files)
		probeClass := fmt.Sprintf("Probe%03d", i)
		body := strings.NewReplacer(
			"@NAME@", fmt.Sprintf("%q", p.name),
			"@TABLE@", table,
			"@FILE@", fmt.Sprintf("%q", p.file),
		).Replace(p.body)
		src := fmt.Sprintf(`// generated by internal/codegen/cstable's versioning harness: one probe per row per COLUMN
using System;

namespace %s
{
    static class %s
    {
        public static int Run(string corpus)
        {
            int bad = 0;
%s
            if (bad == 0) { Console.WriteLine("ROW " + %s + ": ok"); }
            return bad;
        }
    }
}
`, ns, probeClass, body, fmt.Sprintf("%q", p.name))
		if err := os.WriteFile(filepath.Join(dir, probeClass+".cs"), []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
		calls = append(calls, fmt.Sprintf("            bad += %s.%s.Run(corpus);", ns, probeClass))
	}

	main := fmt.Sprintf(`using System;

static class ProbeLog
{
    public static int Fail(string row, string what)
    {
        Console.WriteLine("ROW " + row + ": FAILED " + what);
        return 1;
    }
}

static class ProbeMain
{
    static int Main()
    {
        string corpus = Environment.GetEnvironmentVariable("SCHEMA_FIXEDFORM_CORPUS");
        int bad = 0;
        try
        {
%s
        }
        catch (Exception e)
        {
            Console.WriteLine("ROW harness: FAILED " + e.ToString());
            return 1;
        }
        Console.WriteLine(bad == 0 ? "cs versioning: every row green" : "cs versioning: " + bad + " FAILED");
        return bad == 0 ? 0 : 1;
    }
}
`, strings.Join(calls, "\n"))
	if err := os.WriteFile(filepath.Join(dir, "ProbeMain.cs"), []byte(main), 0o600); err != nil {
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
    <AssemblyName>csversioning</AssemblyName>
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
	if err := os.WriteFile(filepath.Join(dir, "csversioning.csproj"), []byte(proj), 0o600); err != nil {
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

	csVersionAssert(t, out, runErr, "fixed_form_under_20_bytes")
}
