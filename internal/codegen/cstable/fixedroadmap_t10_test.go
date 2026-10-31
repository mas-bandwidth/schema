package cstable

// fixedroadmap_t10 — the cs leg's optional, default and text/flags/bytes rows of
// docs/roadmap.sexp's node `fixed-tables` (ROADMAP.md "NEW Fixed Tables"):
// optional-scalars, optional-arrays, scalar-defaults and
// text-bytes-flags-defaults, the four capability rows no other test of this leg
// asserts. The law is docs/SPEC-TABLES.md §3.4's message form and fixed form and
// docs/FIXED-FORM-ALGORITHM.md §3 (the prefill) and §4.6 (the bounds pass). One
// subtest per task id.
//
// A capability row is WRITE and READ in a RUNNING TARGET. Each subtest first
// holds the emitted source to the construct (the storage, the write body and the
// read slots), then reads its probe's verdict out of one dotnet build and run. A
// probe writes the construct with NON-DEFAULT and BOUNDARY values, asserts the
// record bytes, and asserts the same values land through a fixed save/load
// roundtrip — so a broken emitter is red by name, not by substring.
//
// The generated <Base>Table.cs files carry no serialize dependency (make/cs.mk's
// tables-cs-standalone gate), so the harness compiles the unit's own files with
// the SDK's Roslyn compiler (`dotnet exec csc.dll`) and runs the assembly with
// `dotnet exec` — no sibling serialize.cs, no csproj, no NuGet. One assembly,
// one build, one run: every probe is generated into its own namespace (the
// package is suffixed, so four fixtures that all declare `table T` coexist).

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/csharp"
)

const (
	// t10OptionalScalars is optional-scalars/cs/valid-data: a `?int32` and a
	// `?Grade`, a scalar and an enum, each off its default.
	t10OptionalScalars = `package probe

enum Grade { Bronze, Gold }

fixed table T
{
    number ?int32
    mark ?Grade
    tail int32 = 7
}
`
	// t10OptionalArrays is optional-arrays/cs/valid-data: `?[..2]Cell`, an
	// optional array whose present byte gates a count and the elements.
	t10OptionalArrays = `package probe

enum Cell { A, B, C }

fixed table T
{
    optional ?[..2]Cell
    tail int32 = 7
}
`
	// t10ScalarDefaults is scalar-defaults/cs/valid-data: an int32 and an enum
	// carrying a declared default.
	t10ScalarDefaults = `package probe

enum Grade { Bronze, Gold }

fixed table T
{
    x int32 = 42
    g Grade = Gold
}
`
	// t10TextBytesFlagsDefaults is text-bytes-flags-defaults/cs/valid-data:
	// a string, a byte-buffer and a flags value, each with a declared default.
	t10TextBytesFlagsDefaults = `package probe

flags Caps { Jump, Crouch }

fixed table T
{
    name string(8) = "untitled"
    tag  bytes(4) = "ab"
    caps Caps = { Jump, Crouch }
}
`
)

func TestFixedRoadmapCSharpT10Optionals(t *testing.T) {
	t.Parallel()
	t10Discover(t)
	out, err := t10Run(t)
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"optional-scalars/cs/valid-data", t10OptionalScalarsRow},
		{"optional-arrays/cs/valid-data", t10OptionalArraysRow},
		{"scalar-defaults/cs/valid-data", t10ScalarDefaultsRow},
		{"text-bytes-flags-defaults/cs/valid-data", t10TextBytesFlagsDefaultsRow},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
			t10Assert(t, out, err, tc.id)
		})
	}
}

// t10ResetBody reads the generated TableReset(T)'s statements.
func t10ResetBody(t *testing.T, src string) string {
	t.Helper()
	return t02Norm(t02Block(t, src, "public static void TableReset(T value)", "\n        }"))
}

// t10ReadSlots reads the generated TFixedSlots array back, so an assertion names
// the READ of a construct — the slot closures the load runs on the record body —
// and not the whole file.
func t10ReadSlots(t *testing.T, src string) string {
	t.Helper()
	return t02Norm(t02Block(t, src, "TFixedSlots = new TableFixedSlot<T>[] {", "};"))
}

// ---- the one dotnet build and run -------------------------------------------

type t10Probe struct {
	id   string // the task id, what ROW <id> prints
	src  string // the fixture's schema source
	body string // the C# assertions, over the generated namespace
}

func t10Probes() []t10Probe {
	return []t10Probe{
		{id: "optional-scalars/cs/valid-data", src: t10OptionalScalars, body: t10OptionalScalarsBody},
		{id: "optional-arrays/cs/valid-data", src: t10OptionalArrays, body: t10OptionalArraysBody},
		{id: "scalar-defaults/cs/valid-data", src: t10ScalarDefaults, body: t10ScalarDefaultsBody},
		{id: "text-bytes-flags-defaults/cs/valid-data", src: t10TextBytesFlagsDefaults, body: t10TextBytesFlagsDefaultsBody},
	}
}

// t10Discover locates dotnet, the SDK's Roslyn compiler and the .NET ref pack.
// It is the host-free twin of `dotnet run`: the CLI's build/run commands run a
// first-time-use configurer that a sandboxed host refuses, so the harness calls
// the compiler and the host directly. A missing toolchain follows t06's idiom:
// skip by name when SCHEMA_REQUIRE_CORPUS is unset, fail by name when it is set
// (the fixed-rows target sets it, so a missing dotnet/csc/ref pack can never
// pass silently under that target).
var (
	t10dotnet, t10csc, t10refDir, t10refVersion string
)

func t10Discover(t *testing.T) {
	t.Helper()
	t10dotnet = os.Getenv("DOTNET")
	if t10dotnet == "" {
		t10dotnet = "dotnet"
	}
	var real string
	if resolved, err := exec.LookPath(t10dotnet); err != nil {
		t10MissingToolchain(t, "no dotnet on PATH (set DOTNET): "+err.Error())
		return
	} else if r, err := filepath.EvalSymlinks(resolved); err != nil {
		real = resolved
	} else {
		real = r
	}
	root := filepath.Dir(real)
	cscs, _ := filepath.Glob(filepath.Join(root, "sdk", "*", "Roslyn", "bincore", "csc.dll"))
	if len(cscs) == 0 {
		t10MissingToolchain(t, "no Roslyn csc.dll under "+root)
		return
	}
	t10csc = cscs[0]
	refs, _ := filepath.Glob(filepath.Join(root, "packs", "Microsoft.NETCore.App.Ref", "*", "ref", "net10.0"))
	if len(refs) == 0 {
		t10MissingToolchain(t, "no .NET ref pack under "+root)
		return
	}
	t10refDir = refs[0]
	t10refVersion = filepath.Base(filepath.Dir(filepath.Dir(refs[0])))
}

func t10MissingToolchain(t *testing.T, missing string) {
	t.Helper()
	if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
		t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and %s", missing)
	}
	t.Skip(missing)
}

var (
	t10Once sync.Once
	t10Out  []byte
	t10Err  error
)

func t10Run(t *testing.T) ([]byte, error) {
	t.Helper()
	t10Once.Do(func() { t10Out, t10Err = t10BuildAndRun(t) })
	return t10Out, t10Err
}

func t10BuildAndRun(t *testing.T) ([]byte, error) {
	t.Helper()
	dir := t.TempDir()
	var calls []string
	for i, p := range t10Probes() {
		u := unitFrom(t, csVersionSuffix(p.src, fmt.Sprintf("t10_%d", i)))
		pk, err := csharp.Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		tb, err := Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		gen := filepath.Join(dir, "gen", fmt.Sprintf("u%03d", i))
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
		probeClass := fmt.Sprintf("Probe%03d", i)
		body := strings.NewReplacer("@NS@", ns, "@NAME@", fmt.Sprintf("%q", p.id)).Replace(p.body)
		src := fmt.Sprintf(`// generated by internal/codegen/cstable's t10 harness: one probe per task id
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
`, ns, probeClass, body, fmt.Sprintf("%q", p.id))
		if err := os.WriteFile(filepath.Join(dir, probeClass+".cs"), []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
		calls = append(calls, fmt.Sprintf("            bad += %s.%s.Run();", ns, probeClass))
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
%s
        }
        catch (Exception e)
        {
            Console.WriteLine("ROW harness: FAILED " + e.ToString());
            return 1;
        }
        Console.WriteLine(bad == 0 ? "cs t10 optionals: every row green" : "cs t10 optionals: " + bad + " FAILED");
        return bad == 0 ? 0 : 1;
    }
}
`, strings.Join(calls, "\n"))
	if err := os.WriteFile(filepath.Join(dir, "ProbeMain.cs"), []byte(main), 0o600); err != nil {
		t.Fatal(err)
	}

	topFiles, _ := filepath.Glob(filepath.Join(dir, "*.cs"))
	genFiles, _ := filepath.Glob(filepath.Join(dir, "gen", "*", "*.cs"))
	refs, _ := filepath.Glob(filepath.Join(t10refDir, "*.dll"))
	args := []string{t10csc, "-nologo", "-langversion:latest", "-unsafe", "-out:" + filepath.Join(dir, "app.dll")}
	for _, r := range refs {
		args = append(args, "-r:"+r)
	}
	args = append(args, topFiles...)
	args = append(args, genFiles...)
	cmd := exec.Command(t10dotnet, append([]string{"exec"}, args...)...)
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
`, t10refVersion)
	if err := os.WriteFile(filepath.Join(dir, "app.runtimeconfig.json"), []byte(rtc), 0o600); err != nil {
		t.Fatal(err)
	}
	run := exec.Command(t10dotnet, "exec", filepath.Join(dir, "app.dll"))
	run.Dir = dir
	run.Env = append(os.Environ(), "DOTNET_NOLOGO=1", "DOTNET_CLI_TELEMETRY_OPTOUT=1")
	return run.CombinedOutput()
}

// t10Assert reads ONE probe's verdict out of the single run's output. A probe
// that did not print at all DID NOT RUN — the assembly did not build, or the
// name moved — and that is a failure and never a pass.
func t10Assert(t *testing.T, out []byte, runErr error, id string) {
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
			id, t10Tail(text, runErr))
	}
	t.Fatalf("%s:\n%s", id, strings.Join(lines, "\n"))
}

func t10Tail(text string, runErr error) string {
	const cap = 4000
	if len(text) > cap {
		text = text[len(text)-cap:]
	}
	if runErr != nil {
		return text + "\n" + runErr.Error()
	}
	return text
}

// ---- the probe bodies, one per task -----------------------------------------

// t10OptionalScalarsBody: optional-scalars [capability] "Optional scalar and
// enum fields" (§3.4 record, ?T; ALGORITHM §3). A `?T` is a PRESENT BYTE and
// then the payload, and the payload rides only when the byte is set.
const t10OptionalScalarsBody = `
            var v = new @NS@.T();
            v.Number = 42; v.NumberPresent = true;
            v.Mark = @NS@.Grade.Gold; v.MarkPresent = true;
            v.Tail = 99;

            byte[] body = new byte[@NS@.Schema.TFixedBodyBytes];
            @NS@.Schema.TFixedWriteBody(body, v);
            if (body[0] != 1) { bad += ProbeLog.Fail(@NAME@, "the scalar's present byte is not written: " + body[0]); }
            if (BinaryPrimitives.ReadInt32LittleEndian(body.AsSpan(1)) != 42) { bad += ProbeLog.Fail(@NAME@, "the scalar's payload did not ride: " + BinaryPrimitives.ReadInt32LittleEndian(body.AsSpan(1))); }
            if (body[5] != 1) { bad += ProbeLog.Fail(@NAME@, "the enum's present byte is not written: " + body[5]); }
            if (body[6] != 2) { bad += ProbeLog.Fail(@NAME@, "the enum's ordinal did not ride: " + body[6]); }
            if (BinaryPrimitives.ReadInt32LittleEndian(body.AsSpan(7)) != 99) { bad += ProbeLog.Fail(@NAME@, "the tail moved: " + BinaryPrimitives.ReadInt32LittleEndian(body.AsSpan(7))); }

            byte[] buf = new byte[@NS@.Schema.TFixedMeasure(1)];
            long n = @NS@.Schema.TFixedSave(v, buf);
            var r = new @NS@.T();
            var rep = new @NS@.TableReport();
            var plan = new @NS@.TableFixedEntry[8192];
            long m = @NS@.Schema.TFixedLoad(r, buf, plan, rep);
            if (n != buf.Length || m != 1) { bad += ProbeLog.Fail(@NAME@, "save/load did not roundtrip: n=" + n + " m=" + m); }
            if (!r.NumberPresent || r.Number != 42) { bad += ProbeLog.Fail(@NAME@, "the scalar did not land: " + r.NumberPresent + "/" + r.Number); }
            if (!r.MarkPresent || r.Mark != @NS@.Grade.Gold) { bad += ProbeLog.Fail(@NAME@, "the enum did not land: " + r.MarkPresent + "/" + r.Mark); }
            if (r.Tail != 99) { bad += ProbeLog.Fail(@NAME@, "the tail did not land: " + r.Tail); }

            var a = new @NS@.T();
            a.Tail = 99;
            byte[] ab = new byte[@NS@.Schema.TFixedBodyBytes];
            @NS@.Schema.TFixedWriteBody(ab, a);
            if (ab[0] != 0 || ab[5] != 0) { bad += ProbeLog.Fail(@NAME@, "an absent optional writes a present byte: " + ab[0] + "/" + ab[5]); }
            byte[] abuf = new byte[@NS@.Schema.TFixedMeasure(1)];
            @NS@.Schema.TFixedSave(a, abuf);
            var ar = new @NS@.T();
            long am = @NS@.Schema.TFixedLoad(ar, abuf, new @NS@.TableFixedEntry[8192], new @NS@.TableReport());
            if (am != 1 || ar.NumberPresent || ar.MarkPresent) { bad += ProbeLog.Fail(@NAME@, "an absent optional read back present"); }
            if (ar.Tail != 99) { bad += ProbeLog.Fail(@NAME@, "the absent roundtrip moved the tail: " + ar.Tail); }
`

// t10OptionalArraysBody: optional-arrays [capability] "Optional arrays" (§3.4
// optional wrapper; ALGORITHM §3). `?[..N]T` is a PRESENT BYTE, then a count,
// then the elements, and the count and the elements ride only when the byte is
// set.
const t10OptionalArraysBody = `
            var v = new @NS@.T();
            v.OptionalPresent = true;
            v.OptionalCount = 2;
            v.Optional[0] = @NS@.Cell.A;
            v.Optional[1] = @NS@.Cell.C;
            v.Tail = 99;

            byte[] body = new byte[@NS@.Schema.TFixedBodyBytes];
            @NS@.Schema.TFixedWriteBody(body, v);
            if (body[0] != 1) { bad += ProbeLog.Fail(@NAME@, "the wrapper's present byte is not written: " + body[0]); }
            if (BinaryPrimitives.ReadInt32LittleEndian(body.AsSpan(1)) != 2) { bad += ProbeLog.Fail(@NAME@, "the count did not ride: " + BinaryPrimitives.ReadInt32LittleEndian(body.AsSpan(1))); }
            if (body[5] != 1 || body[6] != 3) { bad += ProbeLog.Fail(@NAME@, "the elements did not ride: " + body[5] + "/" + body[6]); }
            if (BinaryPrimitives.ReadInt32LittleEndian(body.AsSpan(7)) != 99) { bad += ProbeLog.Fail(@NAME@, "the tail moved: " + BinaryPrimitives.ReadInt32LittleEndian(body.AsSpan(7))); }

            byte[] buf = new byte[@NS@.Schema.TFixedMeasure(1)];
            long n = @NS@.Schema.TFixedSave(v, buf);
            var r = new @NS@.T();
            long m = @NS@.Schema.TFixedLoad(r, buf, new @NS@.TableFixedEntry[8192], new @NS@.TableReport());
            if (n != buf.Length || m != 1) { bad += ProbeLog.Fail(@NAME@, "save/load did not roundtrip: n=" + n + " m=" + m); }
            if (!r.OptionalPresent || r.OptionalCount != 2 || r.Optional[0] != @NS@.Cell.A || r.Optional[1] != @NS@.Cell.C) { bad += ProbeLog.Fail(@NAME@, "the elements did not land: " + r.OptionalCount + "/" + r.Optional[0] + "/" + r.Optional[1]); }
            if (r.Tail != 99) { bad += ProbeLog.Fail(@NAME@, "the tail did not land: " + r.Tail); }

            var a = new @NS@.T();
            a.Tail = 99;
            byte[] ab = new byte[@NS@.Schema.TFixedBodyBytes];
            @NS@.Schema.TFixedWriteBody(ab, a);
            if (ab[0] != 0) { bad += ProbeLog.Fail(@NAME@, "an absent wrapper writes a present byte: " + ab[0]); }
            byte[] abuf = new byte[@NS@.Schema.TFixedMeasure(1)];
            @NS@.Schema.TFixedSave(a, abuf);
            var ar = new @NS@.T();
            long am = @NS@.Schema.TFixedLoad(ar, abuf, new @NS@.TableFixedEntry[8192], new @NS@.TableReport());
            if (am != 1 || ar.OptionalPresent) { bad += ProbeLog.Fail(@NAME@, "an absent wrapper read back present"); }

            var e = new @NS@.T();
            e.OptionalPresent = true;
            e.OptionalCount = 0;
            e.Tail = 99;
            byte[] eb = new byte[@NS@.Schema.TFixedBodyBytes];
            @NS@.Schema.TFixedWriteBody(eb, e);
            if (eb[0] != 1 || BinaryPrimitives.ReadInt32LittleEndian(eb.AsSpan(1)) != 0) { bad += ProbeLog.Fail(@NAME@, "a present-empty wrapper did not write its zero count"); }
            byte[] ebuf = new byte[@NS@.Schema.TFixedMeasure(1)];
            @NS@.Schema.TFixedSave(e, ebuf);
            var er = new @NS@.T();
            long em = @NS@.Schema.TFixedLoad(er, ebuf, new @NS@.TableFixedEntry[8192], new @NS@.TableReport());
            if (em != 1 || !er.OptionalPresent || er.OptionalCount != 0) { bad += ProbeLog.Fail(@NAME@, "a present-empty wrapper did not roundtrip its zero count"); }
`

// t10ScalarDefaultsBody: scalar-defaults [capability] "Scalar and enum defaults"
// (§3.4 template; ALGORITHM §3). A declared default is the value the storage is
// born with, what TableReset lays down, and what the write body writes when no
// other value is set.
const t10ScalarDefaultsBody = `
            var v = new @NS@.T();
            byte[] body = new byte[@NS@.Schema.TFixedBodyBytes];
            @NS@.Schema.TFixedWriteBody(body, v);
            if (BinaryPrimitives.ReadInt32LittleEndian(body.AsSpan(0)) != 42) { bad += ProbeLog.Fail(@NAME@, "the scalar default did not ride: " + BinaryPrimitives.ReadInt32LittleEndian(body.AsSpan(0))); }
            if (body[4] != 2) { bad += ProbeLog.Fail(@NAME@, "the enum default did not ride: " + body[4]); }

            byte[] buf = new byte[@NS@.Schema.TFixedMeasure(1)];
            long n = @NS@.Schema.TFixedSave(v, buf);
            var r = new @NS@.T();
            long m = @NS@.Schema.TFixedLoad(r, buf, new @NS@.TableFixedEntry[8192], new @NS@.TableReport());
            if (n != buf.Length || m != 1) { bad += ProbeLog.Fail(@NAME@, "save/load did not roundtrip: n=" + n + " m=" + m); }
            if (r.X != 42 || r.G != @NS@.Grade.Gold) { bad += ProbeLog.Fail(@NAME@, "the defaults did not land: " + r.X + "/" + r.G); }

            var o = new @NS@.T();
            o.X = 7;
            o.G = @NS@.Grade.Bronze;
            byte[] ob = new byte[@NS@.Schema.TFixedBodyBytes];
            @NS@.Schema.TFixedWriteBody(ob, o);
            if (BinaryPrimitives.ReadInt32LittleEndian(ob.AsSpan(0)) != 7) { bad += ProbeLog.Fail(@NAME@, "a non-default scalar did not override the default: " + BinaryPrimitives.ReadInt32LittleEndian(ob.AsSpan(0))); }
            if (ob[4] != 1) { bad += ProbeLog.Fail(@NAME@, "a non-default enum did not override the default: " + ob[4]); }
            byte[] obuf = new byte[@NS@.Schema.TFixedMeasure(1)];
            @NS@.Schema.TFixedSave(o, obuf);
            var or = new @NS@.T();
            long om = @NS@.Schema.TFixedLoad(or, obuf, new @NS@.TableFixedEntry[8192], new @NS@.TableReport());
            if (om != 1 || or.X != 7 || or.G != @NS@.Grade.Bronze) { bad += ProbeLog.Fail(@NAME@, "a non-default override did not land: " + or.X + "/" + or.G); }
`

// t10TextBytesFlagsDefaultsBody: text-bytes-flags-defaults [capability] "String,
// byte-buffer and flags defaults" (ALGORITHM §3 prefill). A string(N) default is
// its bytes with the matching length, a bytes(N) default is its bytes with the
// matching length, and a flags default is its bitset.
const t10TextBytesFlagsDefaultsBody = `
            var v = new @NS@.T();
            byte[] body = new byte[@NS@.Schema.TFixedBodyBytes];
            @NS@.Schema.TFixedWriteBody(body, v);
            if (BinaryPrimitives.ReadInt32LittleEndian(body.AsSpan(0)) != 8) { bad += ProbeLog.Fail(@NAME@, "the string default length did not ride: " + BinaryPrimitives.ReadInt32LittleEndian(body.AsSpan(0))); }
            if (body[4] != 0x75 || body[11] != 0x64) { bad += ProbeLog.Fail(@NAME@, "the string default bytes did not ride"); }
            if (BinaryPrimitives.ReadInt32LittleEndian(body.AsSpan(12)) != 2) { bad += ProbeLog.Fail(@NAME@, "the byte-buffer default length did not ride: " + BinaryPrimitives.ReadInt32LittleEndian(body.AsSpan(12))); }
            if (body[16] != 0x61 || body[17] != 0x62) { bad += ProbeLog.Fail(@NAME@, "the byte-buffer default bytes did not ride"); }
            if (BinaryPrimitives.ReadUInt64LittleEndian(body.AsSpan(20)) != 3) { bad += ProbeLog.Fail(@NAME@, "the flags default bitset did not ride: " + BinaryPrimitives.ReadUInt64LittleEndian(body.AsSpan(20))); }

            byte[] buf = new byte[@NS@.Schema.TFixedMeasure(1)];
            long n = @NS@.Schema.TFixedSave(v, buf);
            var r = new @NS@.T();
            long m = @NS@.Schema.TFixedLoad(r, buf, new @NS@.TableFixedEntry[8192], new @NS@.TableReport());
            if (n != buf.Length || m != 1) { bad += ProbeLog.Fail(@NAME@, "save/load did not roundtrip: n=" + n + " m=" + m); }
            if (r.NameLength != 8 || r.Name[0] != 0x75 || r.Name[7] != 0x64) { bad += ProbeLog.Fail(@NAME@, "the string default did not land"); }
            if (r.TagLength != 2 || r.Tag[0] != 0x61 || r.Tag[1] != 0x62) { bad += ProbeLog.Fail(@NAME@, "the byte-buffer default did not land"); }
            if ((ulong)r.Caps != 3) { bad += ProbeLog.Fail(@NAME@, "the flags default did not land: " + r.Caps); }

            var o = new @NS@.T();
            o.Name[0] = 0x78;
            o.NameLength = 1;
            o.Tag[0] = 0x7a;
            o.Tag[1] = 0x79;
            o.TagLength = 2;
            o.Caps = @NS@.Caps.Jump;
            byte[] ob = new byte[@NS@.Schema.TFixedBodyBytes];
            @NS@.Schema.TFixedWriteBody(ob, o);
            if (BinaryPrimitives.ReadInt32LittleEndian(ob.AsSpan(0)) != 1 || ob[4] != 0x78) { bad += ProbeLog.Fail(@NAME@, "a shortened string did not override the default"); }
            if (ob[16] != 0x7a || ob[17] != 0x79) { bad += ProbeLog.Fail(@NAME@, "the byte-buffer override did not ride"); }
            if (BinaryPrimitives.ReadUInt64LittleEndian(ob.AsSpan(20)) != 1) { bad += ProbeLog.Fail(@NAME@, "the flags override did not ride: " + BinaryPrimitives.ReadUInt64LittleEndian(ob.AsSpan(20))); }
            byte[] obuf = new byte[@NS@.Schema.TFixedMeasure(1)];
            @NS@.Schema.TFixedSave(o, obuf);
            var or = new @NS@.T();
            long om = @NS@.Schema.TFixedLoad(or, obuf, new @NS@.TableFixedEntry[8192], new @NS@.TableReport());
            if (om != 1 || or.NameLength != 1 || or.Name[0] != 0x78 || or.TagLength != 2 || or.Tag[0] != 0x7a || or.Tag[1] != 0x79 || (ulong)or.Caps != 1) { bad += ProbeLog.Fail(@NAME@, "the overrides did not land"); }
`

// t10OptionalScalarsRow: optional-scalars [capability] "Optional scalar and enum
// fields: valid-data write/read acceptance" (§3.4 record, ?T; ALGORITHM §3).
// A `?T` is a PRESENT BYTE and then the payload, and the payload rides only when
// the byte is set: the generated storage has a `<name>Present` bool beside the
// value, TableReset clears both, and the write body writes the present byte and
// gates the payload on it — for the scalar and the enum alike.
func t10OptionalScalarsRow(t *testing.T) {
	_, src := t02Table(t, t10OptionalScalars)
	n := t02Norm(src)
	for _, want := range []string{
		"public int Number;",
		"public bool NumberPresent; // ?int32: absent until set",
		"public Grade Mark = Grade.None;",
		"public bool MarkPresent; // ?Grade: absent until set",
	} {
		t02Has(t, src, want, "optional-scalars: the storage carries the value and its present byte")
	}
	t02Has(t, n, "b[0] = (byte)(value.NumberPresent ? 1 : 0);", "optional-scalars: the scalar's present byte is written")
	t02Has(t, n, "if (value.NumberPresent) { BinaryPrimitives.WriteInt32LittleEndian(b.Slice(1), (int)value.Number); }",
		"optional-scalars: the scalar's payload rides only when present")
	t02Has(t, n, "b[5] = (byte)(value.MarkPresent ? 1 : 0);", "optional-scalars: the enum's present byte is written")
	t02Has(t, n, "if (value.MarkPresent) { b.Slice(6)[0] = (byte)value.Mark; }",
		"optional-scalars: the enum's payload rides only when present")
	reset := t10ResetBody(t, src)
	for _, want := range []string{"value.NumberPresent = false;", "value.MarkPresent = false;"} {
		if !contains(reset, want) {
			t.Errorf("optional-scalars: TableReset does not clear the present byte: %q absent", want)
		}
	}
	// THE READ SIDE. The load runs TFixedSlots on the record body: the present
	// byte's slot sets the bool, and the payload's slot lands only when the load
	// selected it — so the construct is READ with the same gate it is written
	// with.
	slots := t10ReadSlots(t, src)
	t02Has(t, slots, "new TableFixedSlot<T>(setRaw: (t, v) => t.NumberPresent = v != 0, reset: (t) => { t.NumberPresent = false; }),",
		"optional-scalars: the scalar's present byte is read into the present bool")
	t02Has(t, slots, "t.Number = unchecked((int)v)",
		"optional-scalars: the scalar's payload is read into the value")
	t02Has(t, slots, "new TableFixedSlot<T>(setRaw: (t, v) => t.MarkPresent = v != 0, reset: (t) => { t.MarkPresent = false; }),",
		"optional-scalars: the enum's present byte is read into the present bool")
	t02Has(t, slots, "(t, v, rep) => { t.Mark = (Grade)v; }",
		"optional-scalars: the enum ordinal lands raw and the bounds pass clamps it (§5.4)")
}

// t10OptionalArraysRow: optional-arrays [capability] "Optional arrays: valid-data
// write/read acceptance" (§3.4 optional wrapper; ALGORITHM §3). `?[..N]T` is a
// PRESENT BYTE, then a count, then the elements, and the count and the elements
// ride only when the byte is set: the generated storage has the buffer, a count
// and a `<name>Present` bool; the write body writes the byte and gates the count
// and element copy on it; and the read-side bounds pass visits every element.
func t10OptionalArraysRow(t *testing.T) {
	_, src := t02Table(t, t10OptionalArrays)
	n := t02Norm(src)
	t02Has(t, src, "public bool OptionalPresent; // ?Cell: absent until set",
		"optional-arrays: the wrapper carries its present byte")
	t02Has(t, n, "b[0] = (byte)(value.OptionalPresent ? 1 : 0);",
		"optional-arrays: the present byte is written")
	t02Has(t, n, "if (value.OptionalPresent) { int count_optional = value.OptionalCount;",
		"optional-arrays: the count rides behind the present byte")
	t02Has(t, n, "TableFixedWire.WriteLittle<Cell>(value.Optional.AsSpan(0, Math.Min(value.Optional.Length, count_optional)), b.Slice(5));",
		"optional-arrays: the elements ride only when present")
	t02Has(t, n, "BinaryPrimitives.WriteInt32LittleEndian(b.Slice(1), count_optional);",
		"optional-arrays: the count is written behind the present byte, bounded by the declared count")
	t02Has(t, n, "System.Diagnostics.Debug.Assert(count_optional >= 0 && count_optional <= 2);",
		"optional-arrays: the declared count bounds the wire count")
	// The bounds pass is per ELEMENT: an ordinal past the enum's top counts once
	// for each element the wrapper carries.
	t02Has(t, n, "for (int i = 0; i < value.OptionalCount && i < value.Optional.Length; ++i) { if ((ulong)value.Optional[i] > 3) { value.Optional[i] = (Cell)0; clamped++; } }",
		"optional-arrays: the bounds pass visits every present element")
	reset := t10ResetBody(t, src)
	for _, want := range []string{"value.OptionalCount = 0;", "value.OptionalPresent = false;"} {
		if !contains(reset, want) {
			t.Errorf("optional-arrays: TableReset does not clear the wrapper: %q absent", want)
		}
	}
	// THE READ SIDE. The load runs the present slot, then the count slot, then the
	// element slot: the wrapper is READ by the same three slots the writer gates
	// on, and the element slot casts the record bytes back into the buffer.
	slots := t10ReadSlots(t, src)
	t02Has(t, slots, "new TableFixedSlot<T>(setRaw: (t, v) => t.OptionalPresent = v != 0, reset: (t) => { t.OptionalPresent = false; }),",
		"optional-arrays: the present byte is read into the present bool")
	t02Has(t, slots, "new TableFixedSlot<T>(setRaw: (t, v) => t.OptionalCount = (int)v, reset: (t) => { t.OptionalCount = 0; }),",
		"optional-arrays: the count is read behind the present byte")
	t02Has(t, slots, "TableFixedWire.ReadLittle<Cell>(b.Slice(0, Math.Min(b.Length, t.Optional.Length * 1)), t.Optional)",
		"optional-arrays: the elements are read back into the wrapper's buffer")
}

// t10ScalarDefaultsRow: scalar-defaults [capability] "Scalar and enum defaults:
// valid-data write/read acceptance" (§3.4 template; ALGORITHM §3). A declared
// default is the value TableReset lays down, the value the storage is born with,
// and the value the write body writes when no other value is set.
func t10ScalarDefaultsRow(t *testing.T) {
	_, src := t02Table(t, t10ScalarDefaults)
	n := t02Norm(src)
	t02Has(t, src, "public int X = 42;", "scalar-defaults: the scalar's storage is born at its default")
	t02Has(t, src, "public Grade G = Grade.Gold;", "scalar-defaults: the enum's storage is born at its default")
	reset := t10ResetBody(t, src)
	t02Has(t, reset, "value.X = 42;", "scalar-defaults: TableReset restores the scalar's default")
	t02Has(t, reset, "value.G = global::Probe.Grade.Gold;", "scalar-defaults: TableReset restores the enum's default")
	t02Has(t, n, "BinaryPrimitives.WriteInt32LittleEndian(b, (int)value.X);", "scalar-defaults: the write body writes the scalar")
	t02Has(t, n, "b.Slice(4)[0] = (byte)value.G;", "scalar-defaults: the write body writes the enum")
	// THE READ SIDE. The load's slots carry the same declared default as the
	// prefill, and the enum ordinal is read bounded at its declared top.
	slots := t10ReadSlots(t, src)
	t02Has(t, slots, "new TableFixedSlot<T>(setRaw: (t, v) => t.X = unchecked((int)v), reset: (t) => { t.X = 42; }),",
		"scalar-defaults: the scalar's read slot restores its declared default")
	t02Has(t, slots, "reset: (t) => { t.G = global::Probe.Grade.Gold; }",
		"scalar-defaults: the enum's read slot restores its declared default")
}

// t10TextBytesFlagsDefaultsRow: text-bytes-flags-defaults [capability] "String,
// byte-buffer and flags defaults: valid-data write/read acceptance" (ALGORITHM
// §3 prefill; landed PR847). A string(N) default is its bytes with the matching
// length, a bytes(N) default is its bytes with the matching length, and a flags
// default is its bitset: all three ride the prefill, and the write body copies
// the stored bytes and the flags value.
func t10TextBytesFlagsDefaultsRow(t *testing.T) {
	_, src := t02Table(t, t10TextBytesFlagsDefaults)
	reset := t10ResetBody(t, src)
	// "untitled" is eight bytes; the length default is 8.
	t02Has(t, reset, "value.Name[0] = 0x75;", "text-bytes-flags-defaults: the string prefill's first byte")
	t02Has(t, reset, "value.Name[7] = 0x64;", "text-bytes-flags-defaults: the string prefill's last byte")
	t02Has(t, reset, "value.NameLength = 8;", "text-bytes-flags-defaults: the string prefill's length")
	// bytes(4) "ab": two bytes, length 2.
	for _, want := range []string{"value.Tag[0] = 0x61;", "value.Tag[1] = 0x62;", "value.TagLength = 2;"} {
		t02Has(t, reset, want, "text-bytes-flags-defaults: the byte-buffer prefill")
	}
	// Caps { Jump, Crouch }: bitsets ride as the mask, so Jump|Crouch is 3.
	t02Has(t, reset, "value.Caps = (Caps)3ul;", "text-bytes-flags-defaults: the flags prefill is the bitset")
	t02Has(t, src, "public Caps Caps = (Caps)3ul;", "text-bytes-flags-defaults: the flags storage is born at its bitset")
	n := t02Norm(src)
	t02Has(t, n, "value.Name.AsSpan(0, len_name).CopyTo(b.Slice(4));",
		"text-bytes-flags-defaults: the write body copies the string bytes")
	t02Has(t, n, "value.Tag.AsSpan(0, len_tag).CopyTo(b.Slice(16));",
		"text-bytes-flags-defaults: the write body copies the byte-buffer bytes")
	t02Has(t, n, "BinaryPrimitives.WriteUInt64LittleEndian(b.Slice(20), (ulong)value.Caps);",
		"text-bytes-flags-defaults: the write body writes the flags bitset")
	// THE READ SIDE. The load's slots carry the declared default for the length,
	// the bytes and the flags: a record the writer left at its default reads back
	// as the declared default.
	slots := t10ReadSlots(t, src)
	t02Has(t, slots, "new TableFixedSlot<T>(setRaw: (t, v) => t.NameLength = (int)v, reset: (t) => { t.NameLength = 8; }),",
		"text-bytes-flags-defaults: the string length's read slot restores its default")
	t02Has(t, slots, "reset: (t) => { if (t.Name != null) { Array.Clear(t.Name, 0, t.Name.Length); t.Name[0] = 0x75; t.Name[1] = 0x6e; t.Name[2] = 0x74; t.Name[3] = 0x69; t.Name[4] = 0x74; t.Name[5] = 0x6c; t.Name[6] = 0x65; t.Name[7] = 0x64; } }",
		"text-bytes-flags-defaults: the string bytes' read slot restores its default")
	t02Has(t, slots, "new TableFixedSlot<T>(setRaw: (t, v) => t.TagLength = (int)v, reset: (t) => { t.TagLength = 2; }),",
		"text-bytes-flags-defaults: the byte-buffer length's read slot restores its default")
	t02Has(t, slots, "reset: (t) => { if (t.Tag != null) { Array.Clear(t.Tag, 0, t.Tag.Length); t.Tag[0] = 0x61; t.Tag[1] = 0x62; } }",
		"text-bytes-flags-defaults: the byte-buffer bytes' read slot restores its default")
	t02Has(t, slots, "new TableFixedSlot<T>(setRaw: (t, v) => t.Caps = (global::Probe.Caps)v, reset: (t) => { t.Caps = (global::Probe.Caps)3ul; }),",
		"text-bytes-flags-defaults: the flags read slot restores its default")
}

// contains is strings.Contains, named so a missing clause reads as a clause.
func contains(hay, needle string) bool {
	return len(needle) == 0 || (len(hay) >= len(needle) && indexOf(hay, needle) >= 0)
}

func indexOf(hay, needle string) int {
	for i := 0; i+len(needle) <= len(hay); i++ {
		if hay[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
