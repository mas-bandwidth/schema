package cstable

// fixedroadmap_t18 — cs/R16 "the counters exactly" (docs/FIXED-FORM-ALGORITHM.md
// §5.4, docs/roadmap.sexp node `fixed-tables`). One fixture per row of the §5.4
// table, each WRITTEN and READ by this leg's own toolchain — the OLD generation's
// `Schema.<Root>FixedSave` writes the records, the NEW generation's
// `Schema.<Root>FixedLoad` reads them through the lineage, and the report's exact
// counters are asserted. This is the same one-build/one-run shape csVersionRun
// uses, but the bytes are written here (never read from build/fixedform-corpus),
// so the row runs wherever the .NET SDK does.

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/csharp"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// csT18Pair is the OLD and NEW generation of one row.
type csT18Pair struct {
	oldNS, newNS string
	root         string
}

// csT18Generate stages VOLD_/VNEW_<row>, generates the OLD unit (writer) and the
// NEW unit (reader with the OLD entry as its lineage), and writes both into
// `dir`/gen/old and `dir`/gen/new, answering their namespaces.
func csT18Generate(t *testing.T, dir, row string) csT18Pair {
	t.Helper()
	oldU := csUnitOf(t, csReadSchema(t, "VOLD_"+row))
	newU := csUnitOf(t, csReadSchema(t, "VNEW_"+row))

	lineage := map[string][]FixedLineageEntry{}
	for _, st := range ir.TableFixedRoots(oldU) {
		e, ok := FixedLineageOf(oldU, st.Name)
		if !ok {
			t.Fatalf("no lineage entry for %s", st.Name)
		}
		lineage[st.Name] = append(lineage[st.Name], e)
	}

	oldFiles, err := csharp.Generate(oldU)
	if err != nil {
		t.Fatal(err)
	}
	oldTables, err := GenerateLineage(oldU, nil)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(oldFiles, oldTables)

	newFiles, err := csharp.Generate(newU)
	if err != nil {
		t.Fatal(err)
	}
	newTables, err := GenerateLineage(newU, lineage)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(newFiles, newTables)

	csT18Write(t, filepath.Join(dir, "gen", "old"), oldFiles)
	csT18Write(t, filepath.Join(dir, "gen", "new"), newFiles)
	return csT18Pair{oldNS: capitalize(oldU.Package), newNS: capitalize(newU.Package), root: csFixedRootName(t, newU)}
}

func csT18Write(t *testing.T, dir string, files map[string][]byte) {
	t.Helper()
	for name, data := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// csT18Head is the C# preamble each probe shares: read `buffer` as bytes, the
// OLD and NEW generation's `Schema` classes at hand.
const csT18Head = `            byte[] data = System.IO.File.ReadAllBytes(System.IO.Path.Combine(corpus, @FILE@));
`

// TestFixedRoadmapCSharpT18ExactCounters is cs/R16.
func TestFixedRoadmapCSharpT18ExactCounters(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the dotnet SDK (the leg's own toolchain)")
	t.Run("cs/R16", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()

		type fixture struct {
			name string
			row  string
			body string // C# with @OLD@/@NEW@/@ROOT@ substituted, and @N@ the record count
		}
		fixtures := []fixture{
			{"widen", "int_widen", `            @OLD@.@ROOT@[] want = new @OLD@.@ROOT@[4];
            for (int k = 0; k < 4; ++k) { want[k] = new @OLD@.@ROOT@(); want[k].Lead = 1u; want[k].V = (short)(k + 1); want[k].Trail = 2u; }
            byte[] buf = new byte[@OLD@.Schema.@ROOT@FixedMeasure(4)];
            if (@OLD@.Schema.@ROOT@FixedSave(want, buf) != buf.Length) { bad += ProbeLog.Fail(@NAME@, "save did not fill"); }
            @NEW@.@ROOT@[] back = new @NEW@.@ROOT@[4];
            for (int i = 0; i < back.Length; ++i) { back[i] = new @NEW@.@ROOT@(); }
            @NEW@.TableReport r = new @NEW@.TableReport();
            long n = @NEW@.Schema.@ROOT@FixedLoad(back, buf, new @NEW@.TableFixedEntry[8192], r);
            if (n != 4) { bad += ProbeLog.Fail(@NAME@, "n=" + n); }
            if (r.Widened != 4) { bad += ProbeLog.Fail(@NAME@, "widened=" + r.Widened + " want 4"); }
            if (r.Unknown != 0 || r.KindMismatch != 0 || r.Clamped != 0) { bad += ProbeLog.Fail(@NAME@, "a counter moved"); }`},

			{"elem_widen", "array_elem_widen", `            @OLD@.@ROOT@ want = new @OLD@.@ROOT@();
            want.Lead = 1u; want.Trail = 2u; want.ValsCount = 4;
            for (int k = 0; k < 4; ++k) { want.Vals[k] = (short)(100 + k); }
            byte[] buf = new byte[@OLD@.Schema.@ROOT@FixedMeasure(1)];
            if (@OLD@.Schema.@ROOT@FixedSave(want, buf) != buf.Length) { bad += ProbeLog.Fail(@NAME@, "save did not fill"); }
            @NEW@.@ROOT@ back = new @NEW@.@ROOT@();
            @NEW@.TableReport r = new @NEW@.TableReport();
            long n = @NEW@.Schema.@ROOT@FixedLoad(back, buf, new @NEW@.TableFixedEntry[8192], r);
            if (n != 1) { bad += ProbeLog.Fail(@NAME@, "n=" + n); }
            if (r.Widened != 4) { bad += ProbeLog.Fail(@NAME@, "widened=" + r.Widened + " want 4"); }
            if (r.Unknown != 0 || r.KindMismatch != 0 || r.Clamped != 0) { bad += ProbeLog.Fail(@NAME@, "a counter moved"); }`},

			{"clamped_count", "array_bounded_grow", `            @OLD@.@ROOT@[] want = new @OLD@.@ROOT@[3];
            for (int k = 0; k < 3; ++k) { want[k] = new @OLD@.@ROOT@(); want[k].Lead = 1u; want[k].Trail = 2u; want[k].ValsCount = 4; for (int j = 0; j < 4; ++j) want[k].Vals[j] = 1000 + j; }
            byte[] buf = new byte[@OLD@.Schema.@ROOT@FixedMeasure(3)];
            if (@OLD@.Schema.@ROOT@FixedSave(want, buf) != buf.Length) { bad += ProbeLog.Fail(@NAME@, "save did not fill"); }
            {
                byte[] needle = { 0x01, 0x00, 0x00, 0x00, 0x04, 0x00, 0x00, 0x00 };
                int found = 0;
                for (int i = 0; i + 8 <= buf.Length; ++i) { if (buf.AsSpan(i, 8).SequenceEqual(needle)) { buf[i + 4] = 0x07; buf[i + 5] = 0; buf[i + 6] = 0; buf[i + 7] = 0; found++; } }
                if (found != 3) { bad += ProbeLog.Fail(@NAME@, "locator matched " + found + " times, want 3"); }
            }
            @NEW@.@ROOT@[] back = new @NEW@.@ROOT@[3];
            for (int i = 0; i < back.Length; ++i) { back[i] = new @NEW@.@ROOT@(); }
            @NEW@.TableReport r = new @NEW@.TableReport();
            long n = @NEW@.Schema.@ROOT@FixedLoad(back, buf, new @NEW@.TableFixedEntry[8192], r);
            if (n != 3) { bad += ProbeLog.Fail(@NAME@, "n=" + n); }
            if (r.Clamped != 3) { bad += ProbeLog.Fail(@NAME@, "clamped=" + r.Clamped + " want 3"); }
            if (r.Unknown != 0 || r.KindMismatch != 0 || r.Widened != 0) { bad += ProbeLog.Fail(@NAME@, "a counter moved"); }`},

			{"forged_ordinal", "enum_append", `            @OLD@.@ROOT@ want = new @OLD@.@ROOT@();
            want.Tier = @OLD@.Tier.Gold; want.Seq = 9;
            byte[] buf = new byte[@OLD@.Schema.@ROOT@FixedMeasure(1)];
            if (@OLD@.Schema.@ROOT@FixedSave(want, buf) != buf.Length) { bad += ProbeLog.Fail(@NAME@, "save did not fill"); }
            {
                byte[] needle = { 0x03, 0x09, 0x00, 0x00, 0x00 };
                int found = 0;
                for (int i = 0; i + 5 <= buf.Length; ++i) { if (buf.AsSpan(i, 5).SequenceEqual(needle)) { buf[i] = 4; found++; } }
                if (found != 1) { bad += ProbeLog.Fail(@NAME@, "locator matched " + found + " times, want 1"); }
            }
            {
                @NEW@.@ROOT@ back = new @NEW@.@ROOT@();
                @NEW@.TableReport r = new @NEW@.TableReport();
                long n = @NEW@.Schema.@ROOT@FixedLoad(back, buf, new @NEW@.TableFixedEntry[8192], r);
                if (n != 1) { bad += ProbeLog.Fail(@NAME@, "compiled n=" + n); }
                if ((int)back.Tier != 0) { bad += ProbeLog.Fail(@NAME@, "compiled lands " + (int)back.Tier + ", not None"); }
                if (r.Clamped != 1) { bad += ProbeLog.Fail(@NAME@, "compiled clamped=" + r.Clamped + " want 1"); }
            }
            {
                @OLD@.@ROOT@ back = new @OLD@.@ROOT@();
                @OLD@.TableReport r = new @OLD@.TableReport();
                long n = @OLD@.Schema.@ROOT@FixedLoad(back, buf, new @OLD@.TableFixedEntry[8192], r);
                if (n != 1) { bad += ProbeLog.Fail(@NAME@, "identity n=" + n); }
                if ((int)back.Tier != 0) { bad += ProbeLog.Fail(@NAME@, "identity lands " + (int)back.Tier + ", not None"); }
                if (r.Clamped != 1) { bad += ProbeLog.Fail(@NAME@, "identity clamped=" + r.Clamped + " want 1"); }
            }`},

			{"clean_append", "field_append", `            @OLD@.@ROOT@ want = new @OLD@.@ROOT@();
            want.X = 11; want.Y = 22; want.Z = 33;
            byte[] buf = new byte[@OLD@.Schema.@ROOT@FixedMeasure(1)];
            if (@OLD@.Schema.@ROOT@FixedSave(want, buf) != buf.Length) { bad += ProbeLog.Fail(@NAME@, "save did not fill"); }
            @NEW@.@ROOT@ back = new @NEW@.@ROOT@();
            @NEW@.TableReport r = new @NEW@.TableReport();
            long n = @NEW@.Schema.@ROOT@FixedLoad(back, buf, new @NEW@.TableFixedEntry[8192], r);
            if (n != 1) { bad += ProbeLog.Fail(@NAME@, "n=" + n); }
            if (back.X != 11 || back.Y != 22 || back.Z != 33 || back.W != 77) { bad += ProbeLog.Fail(@NAME@, "values did not land"); }
            if (r.Unknown != 0 || r.KindMismatch != 0 || r.Widened != 0 || r.Clamped != 0) { bad += ProbeLog.Fail(@NAME@, "a counter moved"); }`},

			{"unknown", "unknown_census", `            @OLD@.@ROOT@ want = new @OLD@.@ROOT@();
            want.Lead = 1u; want.Trail = 2u;
            for (int k = 0; k < 4; ++k) { want.Items[k].A = 10 + k; want.Items[k].Drop = 50 + k; }
            byte[] buf = new byte[@OLD@.Schema.@ROOT@FixedMeasure(1)];
            if (@OLD@.Schema.@ROOT@FixedSave(want, buf) != buf.Length) { bad += ProbeLog.Fail(@NAME@, "save did not fill"); }
            @NEW@.@ROOT@ back = new @NEW@.@ROOT@();
            @NEW@.TableReport r = new @NEW@.TableReport();
            long n = @NEW@.Schema.@ROOT@FixedLoad(back, buf, new @NEW@.TableFixedEntry[8192], r);
            if (n != 1) { bad += ProbeLog.Fail(@NAME@, "n=" + n); }
            if (r.Unknown != 1) { bad += ProbeLog.Fail(@NAME@, "unknown=" + r.Unknown + " want 1"); }
            if (r.KindMismatch != 0 || r.Widened != 0 || r.Clamped != 0) { bad += ProbeLog.Fail(@NAME@, "a counter moved"); }`},
		}

		var calls []string
		for i, fx := range fixtures {
			gen := filepath.Join(dir, "gen", fmt.Sprintf("u%03d", i))
			pair := csT18Generate(t, gen, fx.row)
			probeClass := fmt.Sprintf("Probe%03d", i)
			body := strings.NewReplacer(
				"@NAME@", fmt.Sprintf("%q", fx.name),
				"@OLD@", pair.oldNS,
				"@NEW@", pair.newNS,
				"@ROOT@", pair.root,
			).Replace(fx.body)
			src := fmt.Sprintf(`// generated by internal/codegen/cstable's t18 harness
using System;
using System.Buffers.Binary;

namespace csT18
{
    static class %s
    {
        public static int Run(int bad)
        {
%s
            if (bad == 0) { Console.WriteLine("ROW " + %s + ": ok"); }
            return bad;
        }
    }
}
`, probeClass, body, fmt.Sprintf("%q", fx.name))
			if err := os.WriteFile(filepath.Join(dir, probeClass+".cs"), []byte(src), 0o600); err != nil {
				t.Fatal(err)
			}
			calls = append(calls, fmt.Sprintf("            bad = csT18.%s.Run(bad);", probeClass))
		}

		main := fmt.Sprintf(`using System;
using System.Buffers.Binary;

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
        Console.WriteLine(bad == 0 ? "cs t18: every fixture green" : "cs t18: " + bad + " FAILED");
        return bad == 0 ? 0 : 1;
    }
}
`, strings.Join(calls, "\n"))
		if err := os.WriteFile(filepath.Join(dir, "ProbeMain.cs"), []byte(main), 0o600); err != nil {
			t.Fatal(err)
		}

		var sources []string
		for _, fx := range fixtures {
			_ = fx
		}
		_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && strings.HasSuffix(path, ".cs") {
				sources = append(sources, path)
			}
			return nil
		})
		sort.Strings(sources)

		// The Roslyn compiler is invoked through the host directly — the SDK's
		// own compiler without the CLI's first-run configuration or NuGet's
		// restore, neither of which a project referencing no package needs, and
		// neither of which the sandbox (no /tmp writes) allows.
		csc, refs, runtime := csDotnetToolchain(t)
		dotnet := csDotnet()
		app := filepath.Join(dir, "cst18.dll")
		args := []string{"exec", csc, "-nologo", "-noconfig", "-target:exe", "-unsafe",
			"-nullable:annotations", "-langversion:latest", "-nostdlib+", "-out:" + app}
		for _, ref := range refs {
			args = append(args, "-r:"+ref)
		}
		args = append(args, sources...)
		compile := exec.Command(dotnet, args...)
		compile.Env, compile.Dir = csDotnetEnv(dir, dir), dir
		compileOut, compileErr := compile.CombinedOutput()
		if compileErr != nil {
			t.Fatalf("cs t18: compile failed: %v\n%s", compileErr, compileOut)
		}
		cfg := fmt.Sprintf("{\"runtimeOptions\":{\"tfm\":\"net10.0\",\"framework\":{\"name\":\"Microsoft.NETCore.App\",\"version\":%q}}}\n", runtime)
		if err := os.WriteFile(filepath.Join(dir, "cst18.runtimeconfig.json"), []byte(cfg), 0o600); err != nil {
			t.Fatal(err)
		}
		run := exec.Command(dotnet, "exec", app)
		run.Env, run.Dir = csDotnetEnv(dir, dir), dir
		out, runErr := run.CombinedOutput()
		for _, fx := range fixtures {
			csVersionAssert(t, out, runErr, fx.name)
		}
	})
}

var _ = sort.Strings
