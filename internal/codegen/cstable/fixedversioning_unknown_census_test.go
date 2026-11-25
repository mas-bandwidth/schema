package cstable

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// §5.8 row 11, unknown_census: the NEW reader (Item.drop REMOVED) is handed the
// OLD writer's layout as a lineage entry (§5.1 refuses the removal, so there is
// no lock). A lawful older writer never carries a field the reader lacks, so
// the entry is UNLAWFUL: COMPILE lays no plan for it and the load refuses
// `layout_malformed` with every counter at zero — `unknown` included, because
// there is no compile census (docs/FIXED-FORM-ALGORITHM.md §5.2 MATCH, §5.4,
// §5.9 #30). The dropped field sits inside an array of tables, so the rule is
// read in every element's slot. After the refusal every row handed in, poisoned
// first, is back at its declared defaults (SPEC-TABLES §6.5), and a second load
// refuses the same way.
func TestFixedVersioningUnknownCensus(t *testing.T) {
	slowtest.Gate(t, "an external toolchain")
	corpus := csFixedCorpusFile(t, "old_unknown_census.bin")
	csRunOneVersionProbe(t, corpus, csVersionProbe{
		name:   "unknown_census/new_reads_old",
		reader: "VNEW_unknown_census",
		older:  []string{"VOLD_unknown_census"},
		file:   "old_unknown_census.bin",
		body: csUnlawfulBody(
			`back[k].Lead = 99u; back[k].Trail = 88u; for (int i = 0; i < 4; ++i) { back[k].Items[i].A = 5 + i; }`,
			`back[k].Lead == 1u && back[k].Trail == 2u && back[k].Items[0].A == 0 && back[k].Items[1].A == 0 && back[k].Items[2].A == 0 && back[k].Items[3].A == 0`),
	})
}

// csFixedCorpusFile is csFixedCorpus for a probe that reads one named file.
// No corpus at all is csFixedCorpus's ledgered skip (or its failure under
// SCHEMA_REQUIRE_CORPUS). A corpus that IS built but lacks this file is stale
// or broken, and that is a failure in every mode, as in the java, elixir and
// luau legs: a skip here would report green over a row that never ran.
func csFixedCorpusFile(t *testing.T, file string) string {
	t.Helper()
	corpus := csFixedCorpus(t)
	if _, err := os.Stat(filepath.Join(corpus, file)); err != nil {
		t.Fatalf("the reference's byte oracle is built in %s but has no %s (a stale corpus: make tables-fixedform-corpus): %v", corpus, file, err)
	}
	return corpus
}

// csUnlawfulBody is the probe body every unlawful-entry row shares: TWO loads,
// each over rows POISONED first, each refusing `layout_malformed` by name,
// landing nothing, moving no counter and leaving every row handed in at its
// declared defaults. `poison` sets non-default values in back[k]; `check` is
// true when back[k] is at its declared defaults.
func csUnlawfulBody(poison, check string) string {
	return `            byte[] data = System.IO.File.ReadAllBytes(System.IO.Path.Combine(corpus, @FILE@));
            @TABLE@[] back = new @TABLE@[8];
            for (int i = 0; i < back.Length; ++i) { back[i] = new @TABLE@(); }
            TableFixedEntry[] plan = new TableFixedEntry[8192];
            for (int pass = 0; pass < 2; ++pass)
            {
                for (int k = 0; k < back.Length; ++k) { ` + poison + ` }
                TableReport r = new TableReport();
                long n = Schema.@TABLE@FixedLoad(back, data, plan, r);
                if (n >= 0)
                {
                    bad += ProbeLog.Fail(@NAME@, "pass " + pass + ": the unlawful entry LOADED " + n + " records");
                }
                if (!r.Refused || r.Verdict != Schema.TableWire.Verdict.Refused || r.Reason != "layout_malformed")
                {
                    bad += ProbeLog.Fail(@NAME@, "pass " + pass + ": not refused layout_malformed: refused=" + r.Refused + " reason=" + r.Reason + " verdict=" + r.Verdict);
                }
                if (r.Unknown != 0 || r.Widened != 0 || r.Retained != 0 || r.RetainLost != 0)
                {
                    bad += ProbeLog.Fail(@NAME@, "pass " + pass + ": a refusal moved a counter: unknown=" + r.Unknown + " widened=" + r.Widened + " retained=" + r.Retained + " lost=" + r.RetainLost);
                }
                for (int k = 0; k < back.Length; ++k)
                {
                    if (!(` + check + `))
                    {
                        bad += ProbeLog.Fail(@NAME@, "pass " + pass + ": row " + k + " is not at its declared defaults");
                    }
                }
            }`
}

// csRunOneVersionProbe generates the probe's reader with its handed-in lineage,
// builds one probe assembly over it and runs it, asserting `ROW <name>: ok`.
func csRunOneVersionProbe(t *testing.T, corpus string, probe csVersionProbe) {
	t.Helper()
	dir := t.TempDir()
	ns, table, files := csVersionGenerate(t, probe)
	gen := filepath.Join(dir, "gen", "u000")
	csVersionWrite(t, gen, files)
	joined := ""
	for _, data := range files {
		joined += string(data)
	}
	body := strings.ReplaceAll(probe.body, "@JSON@", csVersionJSON(joined, table))
	body = strings.NewReplacer(
		"@NAME@", fmt.Sprintf("%q", probe.name),
		"@TABLE@", table,
		"@FILE@", fmt.Sprintf("%q", probe.file),
	).Replace(body)
	src := fmt.Sprintf(`// generated by internal/codegen/cstable's versioning harness: one probe per row per COLUMN
using System;

namespace %s
{
    static class Probe000
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
`, ns, body, fmt.Sprintf("%q", probe.name))
	if err := os.WriteFile(filepath.Join(dir, "Probe000.cs"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
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
            bad += %s.Probe000.Run(corpus);
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
`, ns)
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
	cmd := testproc.Command(t, dotnet, "run", "--project", dir, "-v", "q", "--nologo")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "SCHEMA_FIXEDFORM_CORPUS="+corpus, "DOTNET_NOLOGO=1")
	out, runErr := cmd.CombinedOutput()

	csVersionAssert(t, out, runErr, probe.name)
}
