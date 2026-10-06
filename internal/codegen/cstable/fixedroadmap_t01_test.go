package cstable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// docs/roadmap.sexp node `fixed-tables`, the cs leg's framing rows: file-envelope
// (F4), batch-capacity (F9), plan-selection (R7, R9, R13). The law is
// docs/FIXED-FORM-ALGORITHM.md §5.3 (LOAD, steps 3, 5, 7, 9, 10, 11) and its
// refusal table; SPEC-TABLES §3.4.
//
// ONE ASSEMBLY, ONE BUILD, ONE RUN, as TestFixedVersioning: every probe below is
// generated, built into one dotnet project and run once, and each Go subtest reads
// its own probe's verdict out of that run. A probe that printed nothing DID NOT
// RUN, which csVersionAssert reports as a failure and never a pass.
//
// THE DESTINATION IS POISONED before every refused read and compared back after,
// so "not one destination byte is written" is measured on the storage and not
// inferred from the return value.

var (
	t01Once  sync.Once
	t01Out   []byte
	t01Err   error
	t01Src   = map[string]string{}
	t01Table = map[string]string{}
)

// t01Common is the helper set every probe body opens with: C# local functions in
// the probe's Run, so they share `bad`, `corpus` and the probe's table.
// @POISON@ and @UNTOUCHED@ are the table-specific halves, spliced by t01Body.
const t01Common = `            byte[] Corpus(string f) { return System.IO.File.ReadAllBytes(System.IO.Path.Combine(corpus, f)); }
            long Rd(byte[] b, int at, int width) { long v = 0; for (int i = 0; i < width; ++i) { v |= (long)b[at + i] << (8 * i); } return v; }
            void W(byte[] b, int at, long v, int width) { for (int i = 0; i < width; ++i) { b[at + i] = (byte)(v >> (8 * i)); } }
            byte[] Put(byte[] f, int at, long v, int width) { byte[] o = (byte[])f.Clone(); W(o, at, v, width); return o; }
            ulong HashOf(byte[] f) { return (ulong)Rd(f, 8, 8); }
            int LayoutBytes(byte[] f) { return (int)Rd(f, 16, 4); }
            int E(int i) { return 24 + 17 * i; }
            byte[] Records(byte[] f, int count)
            {
                int end = 20 + LayoutBytes(f);
                int each = f.Length - end;
                byte[] o = new byte[end + count * each];
                Array.Copy(f, 0, o, 0, end);
                for (int k = 0; k < count; ++k) { Array.Copy(f, end, o, end + k * each, each); }
                return o;
            }
            @TABLE@[] Fresh(int n)
            {
                @TABLE@[] a = new @TABLE@[n];
                for (int i = 0; i < n; ++i) { a[i] = new @TABLE@(); }
                return a;
            }
            void Poison(@TABLE@[] a) { foreach (@TABLE@ e in a) { @POISON@ } }
            bool Untouched(@TABLE@[] a)
            {
                foreach (@TABLE@ e in a) { if (!(@UNTOUCHED@)) { return false; } }
                return true;
            }
            bool Counters(TableReport r)
            {
                return r.Unknown != 0 || r.KindMismatch != 0 || r.Clamped != 0 || r.Duplicate != 0 || r.Widened != 0 || r.Retained != 0 || r.RetainLost != 0;
            }
            // Expect: the read REFUSES by name (reason != null) or is MALFORMED
            // (reason == null), and in both cases returns -1, never sets both
            // answers, moves no counter and leaves every poisoned byte as it was.
            void Expect(string label, byte[] file, int cap, string reason, ulong wantHash)
            {
                @TABLE@[] back = Fresh(cap);
                Poison(back);
                TableReport r = new TableReport();
                TableFixedEntry[] plan = new TableFixedEntry[8192];
                long n = Schema.@TABLE@FixedLoad(back, file, plan, r);
                if (n != -1) { bad += ProbeLog.Fail(@NAME@, label + ": must return -1, returned " + n + " reason=" + r.Reason); }
                if (reason == null)
                {
                    if (!r.Malformed || r.Refused || r.Reason != null || r.Verdict != Schema.TableWire.Verdict.Damaged)
                    {
                        bad += ProbeLog.Fail(@NAME@, label + ": the residue is malformed alone: malformed=" + r.Malformed + " refused=" + r.Refused + " reason=" + r.Reason + " verdict=" + r.Verdict);
                    }
                }
                else
                {
                    if (!r.Refused || r.Reason != reason || r.Verdict != Schema.TableWire.Verdict.Refused)
                    {
                        bad += ProbeLog.Fail(@NAME@, label + ": must refuse " + reason + " by name: refused=" + r.Refused + " reason=" + r.Reason + " verdict=" + r.Verdict);
                    }
                    if (r.Malformed) { bad += ProbeLog.Fail(@NAME@, label + ": refused+reason and malformed are never both set"); }
                }
                if (r.LayoutHash != wantHash) { bad += ProbeLog.Fail(@NAME@, label + ": layout hash 0x" + r.LayoutHash.ToString("x16") + ", want 0x" + wantHash.ToString("x16")); }
                if (Counters(r)) { bad += ProbeLog.Fail(@NAME@, label + ": a counter moved on a refusal: unknown=" + r.Unknown + " kind=" + r.KindMismatch + " clamped=" + r.Clamped + " duplicate=" + r.Duplicate + " widened=" + r.Widened + " retained=" + r.Retained + " lost=" + r.RetainLost); }
                if (!Untouched(back)) { bad += ProbeLog.Fail(@NAME@, label + ": the refused read wrote the destination"); }
            }
            // ReadsClean: the control that keeps every refusal above honest — the
            // unbroken fixture reads, and lands something over the poison.
            void ReadsClean(string label, byte[] file, int cap, int wantN)
            {
                @TABLE@[] back = Fresh(cap);
                Poison(back);
                TableReport r = new TableReport();
                TableFixedEntry[] plan = new TableFixedEntry[8192];
                long n = Schema.@TABLE@FixedLoad(back, file, plan, r);
                if (n != wantN || r.Refused || r.Malformed || r.Reason != null)
                {
                    bad += ProbeLog.Fail(@NAME@, label + ": must read " + wantN + " records: n=" + n + " refused=" + r.Refused + " malformed=" + r.Malformed + " reason=" + r.Reason);
                }
                if (wantN > 0 && Untouched(back)) { bad += ProbeLog.Fail(@NAME@, label + ": the clean read landed nothing"); }
            }
`

const (
	t01PoisonLineage    = `e.X = 0x5A5A5A5A; e.Y = 0x5A5A5A5A; e.Z = 0x5A5A5A5A; e.W = 0x5A5A5A5A;`
	t01UntouchedLineage = `e.X == 0x5A5A5A5A && e.Y == 0x5A5A5A5A && e.Z == 0x5A5A5A5A && e.W == 0x5A5A5A5A`
	t01PoisonCensus     = `e.Lead = 0x5A5A5A5Au; e.Trail = 0x5A5A5A5Au; for (int i = 0; i < 4; ++i) { e.Items[i].A = 0x5A5A5A5A; }`
	t01UntouchedCensus  = `e.Lead == 0x5A5A5A5Au && e.Trail == 0x5A5A5A5Au && e.Items[0].A == 0x5A5A5A5A && e.Items[1].A == 0x5A5A5A5A && e.Items[2].A == 0x5A5A5A5A && e.Items[3].A == 0x5A5A5A5A`
	t01PoisonFloor      = `e.A = 0x5A5A5A5A; e.B = 0x5A5A5A5A; e.C = 0x5A5A5A5A;`
	t01UntouchedFloor   = `e.A == 0x5A5A5A5A && e.B == 0x5A5A5A5A && e.C == 0x5A5A5A5A`
)

// t01Body splices a probe's table-specific poison into the common helpers and
// puts the probe's own steps after them.
func t01Body(poison, untouched, steps string) string {
	common := strings.NewReplacer("@POISON@", poison, "@UNTOUCHED@", untouched).Replace(t01Common)
	return common + steps
}

// t01Refusals is the REFUSE set shared by R13's two tables. Each case is a
// distinct refusal of §5.3's table (or the malformed residue) broken in one place
// over a file that reads cleanly: the per-record hash first (step 11), a batch
// past capacity (step 10), a hash in no lineage entry (step 5), a known hash
// over other layout bytes (step 7), a layout past the file (step 3), the three
// form-byte directions (step 1), and the residues that are malformed ALONE: a
// ragged tail (step 9) and a reserved byte (step 2).
const t01Refusals = `            foreach (string name in new string[] { @FILES@ })
            {
                byte[] g = Corpus(name);
                ulong h = HashOf(g);
                int end = 20 + LayoutBytes(g);
                ReadsClean(name + " unbroken", g, 1, 1);
                byte[] noLayout = (byte[])g.Clone();
                for (int i = 0; i < 8; ++i) { noLayout[end + i] ^= 0xFF; }
                byte[] ragged = new byte[g.Length + 1];
                Array.Copy(g, ragged, g.Length);
                byte[] shortLayout = new byte[g.Length - 1];
                Array.Copy(g, 0, shortLayout, 0, end - 1);
                Array.Copy(g, end, shortLayout, end - 1, g.Length - end);
                W(shortLayout, 16, LayoutBytes(g) - 1, 4);
                Expect(name + " no_layout", noLayout, 1, "no_layout", 0);
                Expect(name + " batch_too_large", Records(g, 2), 1, "batch_too_large", 0);
                Expect(name + " layout_newer", Put(g, 8, 0x1122334455667788L, 8), 1, "layout_newer", 0x1122334455667788UL);
                Expect(name + " layout_malformed bytes", Put(g, 24, Rd(g, 24, 4) ^ 0xFF, 4), 1, "layout_malformed", 0);
                Expect(name + " layout_malformed length", shortLayout, 1, "layout_malformed", 0);
                Expect(name + " layout_malformed past the file", Put(g, 16, g.Length, 4), 1, "layout_malformed", 0);
                Expect(name + " previous_form", Put(g, 0, 1, 1), 1, "previous_form", 0);
                Expect(name + " message_form_as_file", Put(g, 0, 2, 1), 1, "message_form_as_file", 0);
                Expect(name + " newer_form", Put(g, 0, 9, 1), 1, "newer_form", 0);
                Expect(name + " ragged tail", ragged, 1, null, 0);
                Expect(name + " reserved byte", Put(g, 3, 1, 1), 1, null, 0);
            }
`

func t01Probes() []csVersionProbe {
	lineage := func(name, steps string) csVersionProbe {
		return csVersionProbe{name: name, reader: "VNEW_field_append", older: []string{"VOLD_field_append"},
			suffix: strings.NewReplacer("cs/", "", "_", "").Replace(strings.ToLower(name)),
			file:   "new_field_append.bin", body: t01Body(t01PoisonLineage, t01UntouchedLineage, steps)}
	}
	census := func(name, steps string) csVersionProbe {
		return csVersionProbe{name: name, reader: "VNEW_unknown_census", older: []string{"VOLD_unknown_census"},
			suffix: strings.NewReplacer("cs/", "", "_", "").Replace(strings.ToLower(name)),
			file:   "old_unknown_census.bin", body: t01Body(t01PoisonCensus, t01UntouchedCensus, steps)}
	}
	_ = census
	files := `"new_field_append.bin", "old_field_append.bin"`
	return []csVersionProbe{
		// F4, ALGORITHM §5.3 step 3: `L := LE(4, file+16); if 20 + L > len(file): REFUSE
		// layout_malformed`. Every cut inside the layout, and every length word past the
		// file, over the identity file and the lineage file.
		lineage("cs/F4", `            foreach (string name in new string[] { `+files+` })
            {
                byte[] g = Corpus(name);
                int end = 20 + LayoutBytes(g);
                ReadsClean(name + " unbroken", g, 1, 1);
                // The boundary: the file ends exactly after its layout. The layout
                // is whole, so this is a zero-record read and no refusal.
                ReadsClean(name + " ends at the layout", Records(g, 0), 1, 0);
                for (int cut = 20; cut < end; ++cut)
                {
                    Expect(name + " cut at " + cut, Records(g, 1).AsSpan(0, cut).ToArray(), 1, "layout_malformed", 0);
                }
                Expect(name + " length word one past the file", Put(g, 16, g.Length - 20 + 1, 4), 1, "layout_malformed", 0);
                Expect(name + " length word 0x7FFFFFFF", Put(g, 16, 0x7FFFFFFFL, 4), 1, "layout_malformed", 0);
                Expect(name + " length word 0xFFFFFFFF", Put(g, 16, 0xFFFFFFFFL, 4), 1, "layout_malformed", 0);
            }
`),
	}
}

func t01Run(t *testing.T, corpus string) ([]byte, error) {
	t.Helper()
	t01Once.Do(func() {
		dir := t.TempDir()
		var calls []string
		for i, p := range t01Probes() {
			ns, table, files := csVersionGenerate(t, p)
			gen := filepath.Join(dir, "gen", fmt.Sprintf("u%03d", i))
			csVersionWrite(t, gen, files)
			joined := ""
			for _, data := range files {
				joined += string(data)
			}
			t01Src[p.name], t01Table[p.name] = joined, table
			probeClass := fmt.Sprintf("Probe%03d", i)
			body := strings.NewReplacer(
				"@NAME@", fmt.Sprintf("%q", p.name),
				"@TABLE@", table,
				"@FILE@", fmt.Sprintf("%q", p.file),
			).Replace(p.body)
			src := fmt.Sprintf(`// generated by internal/codegen/cstable's framing harness
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
        Console.WriteLine(bad == 0 ? "cs framing: every row green" : "cs framing: " + bad + " FAILED");
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
    <AssemblyName>csframing</AssemblyName>
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
		if err := os.WriteFile(filepath.Join(dir, "csframing.csproj"), []byte(proj), 0o600); err != nil {
			t.Fatal(err)
		}
		dotnet := os.Getenv("DOTNET")
		if dotnet == "" {
			dotnet = "dotnet"
		}
		cmd := exec.Command(dotnet, "run", "--project", dir, "-v", "q", "--nologo")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "SCHEMA_FIXEDFORM_CORPUS="+corpus, "DOTNET_NOLOGO=1")
		t01Out, t01Err = cmd.CombinedOutput()
	})
	return t01Out, t01Err
}

func TestFixedRoadmapT01Framing(t *testing.T) {
	t.Parallel()
	corpus := csFixedCorpus(t)
	out, err := t01Run(t, corpus)

	// each task id names the probes that prove it
	rows := []struct {
		id     string
		probes []string
		check  func(t *testing.T)
	}{
		{id: "cs/F4", probes: []string{"cs/F4"}},
	}
	for _, r := range rows {
		t.Run(r.id, func(t *testing.T) {
			t.Parallel()
			for _, p := range r.probes {
				csVersionAssert(t, out, err, p)
			}
			if r.check != nil {
				r.check(t)
			}
		})
	}
}
