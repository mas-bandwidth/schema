package cstable

// TestFixedRoadmapCSharpT13SecondLayoutForAHeldHash is the cs leg's card
// fixed-cs-t13-layout-hash-collision-6, row cs/F12 "second layout for a held
// hash" of docs/roadmap.sexp's node `fixed-tables`. A fixed-form file's header
// carries the form byte, seven reserved bytes, the layout hash (bytes 8..15),
// the layout length L (bytes 16..19) and the layout bytes (from byte 20); the
// reader's steps are docs/FIXED-FORM-ALGORITHM.md §5.3 — step 3 refuses
// `20 + L` past the file as layout_malformed, step 4 takes the header's hash as
// given, step 5 selects a known layout by that hash, and step 7 refuses a known
// hash whose bytes differ from the held layout as layout_malformed. The card
// adds no refusal name: both corruptions answer `layout_malformed`, as a
// refusal by name, before any record is parsed, and the destination is
// untouched.
//
// The behaviour is already in the C# reader (fixedruntime.go's load, and the
// emitted TFixedLoad at fixedform.go) and only its assertion is missing here, so
// this test is the assertion. It generates a small fixed table in process, builds
// a valid fixed file for its layout in memory, and reads it under the generated
// C# reader (functionally, under dotnet) exactly like TestFixedRoadmapT10's
// harness does. One subtest, cs/F12.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/csharp"
)

const t13Schema = `package probe

fixed table T
{
    x int32
    y int32
}
`

// t13Body is the probe body. The file is built in memory from the generated
// schema's own layout bytes (Schema.TFixedLayout0) and hash (Schema.TFixedHash),
// so the test needs no corpus and no go-side layout construction: the byte for
// byte shape under test is the generated reader's own.
const t13Body = `
            int n = 2;
            byte[] layout = Schema.TFixedKnown[0].Layout;
            ulong h = Schema.TFixedHash;
            int L = layout.Length;
            long recBytes = Schema.TFixedRecordBytes;
            long bodyBytes = Schema.TFixedBodyBytes;
            if (L < 1 || recBytes <= 8) { bad += ProbeLog.Fail(@NAME@, "the premise: the layout and its record"); }
            byte[] file = new byte[20 + L + n * (int)recBytes];
            file[0] = (byte)Schema.TableFixedWire.Form;
            for (int i = 1; i < 8; ++i) { file[i] = 0; }
            BinaryPrimitives.WriteUInt64LittleEndian(file.AsSpan(8), h);
            BinaryPrimitives.WriteUInt32LittleEndian(file.AsSpan(16), (uint)L);
            Array.Copy(layout, 0, file, 20, L);
            for (int k = 0; k < n; ++k)
            {
                T rec = new T();
                rec.X = 1 + k; rec.Y = 2 + k;
                byte[] b = new byte[bodyBytes];
                Schema.TFixedWriteBody(b, rec);
                BinaryPrimitives.WriteUInt64LittleEndian(file.AsSpan(20 + L + k * (int)recBytes), h);
                Array.Copy(b, 0, file, 20 + L + k * (int)recBytes + 8, bodyBytes);
            }

            bool Counters(TableReport r)
            {
                return r.Unknown != 0 || r.KindMismatch != 0 || r.Clamped != 0 || r.Duplicate != 0 || r.Widened != 0 || r.Retained != 0 || r.RetainLost != 0;
            }
            void Poison(T[] a) { foreach (T e in a) { e.X = 0x5A5A5A5A; e.Y = 0x5A5A5A5A; } }
            bool Untouched(T[] a) { foreach (T e in a) { if (e.X != 0x5A5A5A5A || e.Y != 0x5A5A5A5A) { return false; } } return true; }

            // the control: the unbroken file reads its two records and lands them
            {
                T[] back = { new T(), new T() };
                Poison(back);
                TableReport r = new TableReport();
                long got = Schema.TFixedLoad(back, file, new TableFixedEntry[8192], r);
                if (got != n || r.Refused || r.Malformed || r.Reason != null) { bad += ProbeLog.Fail(@NAME@, "the unbroken file did not read: n=" + got + " refused=" + r.Refused + " malformed=" + r.Malformed + " reason=" + r.Reason); }
                if (Untouched(back)) { bad += ProbeLog.Fail(@NAME@, "the unbroken read landed nothing"); }
            }

            void Expect(string label, byte[] f)
            {
                T[] back = { new T(), new T() };
                Poison(back);
                TableReport r = new TableReport();
                long got = Schema.TFixedLoad(back, f, new TableFixedEntry[8192], r);
                if (got != -1) { bad += ProbeLog.Fail(@NAME@, label + ": must return -1, returned " + got + " reason=" + r.Reason); }
                if (!r.Refused || r.Reason != "layout_malformed" || r.Verdict != Schema.TableWire.Verdict.Refused)
                {
                    bad += ProbeLog.Fail(@NAME@, label + ": must refuse layout_malformed by name: refused=" + r.Refused + " reason=" + r.Reason + " verdict=" + r.Verdict);
                }
                if (r.Malformed) { bad += ProbeLog.Fail(@NAME@, label + ": refused+reason and malformed are never both set"); }
                if (r.LayoutHash != 0) { bad += ProbeLog.Fail(@NAME@, label + ": layout_malformed publishes no hash: 0x" + r.LayoutHash.ToString("x16")); }
                if (Counters(r)) { bad += ProbeLog.Fail(@NAME@, label + ": a counter moved on a refusal"); }
                if (!Untouched(back)) { bad += ProbeLog.Fail(@NAME@, label + ": the refused read wrote the destination"); }
            }

            // a second layout under the held hash: same length, other layout bytes
            {
                byte[] clone = (byte[])file.Clone();
                clone[20 + 4] ^= 0x01;
                Expect("same length, other bytes", clone);
            }
            // a second layout under the held hash: the layout length L is one off
            {
                byte[] clone = (byte[])file.Clone();
                BinaryPrimitives.WriteUInt32LittleEndian(clone.AsSpan(16), (uint)L + 1);
                Expect("length one off", clone);
            }
`

// t13Discover locates dotnet and the Roslyn toolchain, skipping the test when
// the toolchain is absent (or failing by name under SCHEMA_REQUIRE_CORPUS, the
// same discipline the corpus-dependent harnesses use: a promised toolchain that
// is not here is a failure, never a silent pass).
var (
	t13dotnet, t13csc, t13refDir, t13refVersion string
)

func t13Discover(t *testing.T) {
	t.Helper()
	require := os.Getenv("SCHEMA_REQUIRE_CORPUS") != ""
	t13dotnet = os.Getenv("DOTNET")
	if t13dotnet == "" {
		t13dotnet = "dotnet"
	}
	resolved, err := exec.LookPath(t13dotnet)
	if err != nil {
		if require {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and no dotnet on PATH: %v", err)
		}
		t.Skipf("no dotnet on PATH (set DOTNET): %v", err)
	}
	real, err := filepath.EvalSymlinks(resolved)
	if err != nil {
		real = resolved
	}
	root := filepath.Dir(real)
	cscs, _ := filepath.Glob(filepath.Join(root, "sdk", "*", "Roslyn", "bincore", "csc.dll"))
	if len(cscs) == 0 {
		if require {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and no Roslyn csc.dll under %s", root)
		}
		t.Skipf("no Roslyn csc.dll under %s", root)
	}
	t13csc = cscs[0]
	refs, _ := filepath.Glob(filepath.Join(root, "packs", "Microsoft.NETCore.App.Ref", "*", "ref", "net10.0"))
	if len(refs) == 0 {
		if require {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and no .NET ref pack under %s", root)
		}
		t.Skipf("no .NET ref pack under %s", root)
	}
	t13refDir = refs[0]
	t13refVersion = filepath.Base(filepath.Dir(filepath.Dir(refs[0])))
}

func t13Run(t *testing.T) ([]byte, error) {
	t.Helper()
	t13Discover(t)
	return t13BuildAndRun(t)
}

func t13BuildAndRun(t *testing.T) ([]byte, error) {
	t.Helper()
	dir := t.TempDir()
	u := unitFrom(t, t13Schema)
	pk, err := csharp.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tb, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	gen := filepath.Join(dir, "gen", "u000")
	if err := os.MkdirAll(gen, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, data := range pk {
		if err := os.WriteFile(filepath.Join(gen, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for name, data := range tb {
		if err := os.WriteFile(filepath.Join(gen, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ns := capitalize(u.Package)
	probeClass := "Probe000"
	id := "cs/F12"
	body := strings.NewReplacer("@NAME@", fmt.Sprintf("%q", id)).Replace(t13Body)
	src := fmt.Sprintf(`// generated by internal/codegen/cstable's t13 harness: one probe per task id
using System;
using System.Buffers.Binary;

namespace %s
{
    static class %s
    {
        public static int Run()
        {
            int bad = 0;
%s
            if (bad == 0) { Console.WriteLine("ROW " + %s + ": ok"); }
            return bad;
        }
    }
}
`, ns, probeClass, body, fmt.Sprintf("%q", id))
	if err := os.WriteFile(filepath.Join(dir, probeClass+".cs"), []byte(src), 0o600); err != nil {
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
        int bad = 0;
        try
        {
            bad += %s.%s.Run();
        }
        catch (Exception e)
        {
            Console.WriteLine("ROW harness: FAILED " + e.ToString());
            return 1;
        }
        Console.WriteLine(bad == 0 ? "cs t13 second layout: green" : "cs t13 second layout: " + bad + " FAILED");
        return bad == 0 ? 0 : 1;
    }
}
`, ns, probeClass)
	if err := os.WriteFile(filepath.Join(dir, "ProbeMain.cs"), []byte(main), 0o600); err != nil {
		t.Fatal(err)
	}

	topFiles, _ := filepath.Glob(filepath.Join(dir, "*.cs"))
	genFiles, _ := filepath.Glob(filepath.Join(dir, "gen", "*", "*.cs"))
	refs, _ := filepath.Glob(filepath.Join(t13refDir, "*.dll"))
	args := []string{t13csc, "-nologo", "-langversion:latest", "-unsafe", "-out:" + filepath.Join(dir, "app.dll")}
	for _, r := range refs {
		args = append(args, "-r:"+r)
	}
	args = append(args, topFiles...)
	args = append(args, genFiles...)
	cmd := exec.Command(t13dotnet, append([]string{"exec"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "DOTNET_NOLOGO=1", "DOTNET_CLI_TELEMETRY_OPTOUT=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		return out, err
	}

	rtc := fmt.Sprintf(`{
  "runtimeOptions": {
    "tfm": "net10.0",
    "framework": { "name": "Microsoft.NETCore.App", "version": %q }
  }
}
`, t13refVersion)
	if err := os.WriteFile(filepath.Join(dir, "app.runtimeconfig.json"), []byte(rtc), 0o600); err != nil {
		t.Fatal(err)
	}
	run := exec.Command(t13dotnet, "exec", filepath.Join(dir, "app.dll"))
	run.Dir = dir
	run.Env = append(os.Environ(), "DOTNET_NOLOGO=1", "DOTNET_CLI_TELEMETRY_OPTOUT=1")
	return run.CombinedOutput()
}

func TestFixedRoadmapCSharpT13SecondLayoutForAHeldHash(t *testing.T) {
	t.Parallel()
	out, err := t13Run(t)
	t.Run("cs/F12", func(t *testing.T) {
		t.Parallel()
		t13Assert(t, out, err, "cs/F12")
	})
}

func t13Assert(t *testing.T, out []byte, runErr error, id string) {
	t.Helper()
	text := string(out)
	if strings.Contains(text, "ROW "+id+": ok") {
		return
	}
	var lines []string
	for line := range strings.SplitSeq(text, "\n") {
		if strings.Contains(line, id) {
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		t.Fatalf("%s DID NOT RUN: the probe assembly did not build, or the name moved\n%s",
			id, t13Tail(text, runErr))
	}
	t.Fatalf("%s:\n%s", id, strings.Join(lines, "\n"))
}

func t13Tail(text string, runErr error) string {
	const cap = 4000
	if len(text) > cap {
		text = text[len(text)-cap:]
	}
	if runErr != nil {
		return text + "\n" + runErr.Error()
	}
	return text
}
