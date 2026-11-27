package cstable

import (
	"fmt"
	"github.com/mas-bandwidth/schema/v3/internal/testproc"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/codegen/csharp"
	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
)

// THE 2026-11-21 REVIEW'S C# (modern) READ RULES, driven through the generated
// reader. Two units are compiled into one assembly: rcv, the reader under test,
// and snd, a sender whose announced shapes differ from rcv's where a rule is
// about the announced shape (bytes(N)'s minimum, a ranged value's announced
// kind, a union field this reader does not declare). The sender's writer is a
// Release build, so it writes what its declaration would assert against, and
// where a rule needs a spelling no writer produces the program sets those bits
// of the sender's batch directly. Each Go subtest reads its own row's verdict
// out of the one run; a row that printed nothing DID NOT RUN, which fails.
func TestCSharpReview1121Reads(t *testing.T) {
	t.Parallel()
	out, err := csReview1121Run(t)
	for _, row := range []string{
		"cs1_bytes_adds_announced_min",
		"cs2_message_count_below_min_refused",
		"cs2_variable_count_below_min_refused",
		"cs3_fixed_count_below_min_refused",
		"cs3_fixed_count_at_min_reads",
		"cs5_born_at_min",
		"cs5_reset_to_min",
		"cs5_absent_variable_array_reads_born",
		"cs5_absent_message_array_reads_born",
		"cs5_message_birth_spelled_out_refused",
		"json_count_below_min_refused",
		"json_float32_range_at_float32",
		"negzero_json_read_lands_positive",
		"negzero_variable_read_lands_positive",
		"negzero_fixed_write_stores_positive",
		"message_ranged_held_to_announced_kind",
		"message_skipped_union_field_arm0_refused",
	} {
		t.Run(row, func(t *testing.T) {
			t.Parallel()
			csHeldAssert(t, out, err, row)
		})
	}
}

// CS-4: the JSON integer read takes an integer token only, so the double
// conversion that judged a fractional token for an integer field is dead and
// is not emitted, nor the comment that described integral floats landing.
func TestCSharpJsonIntegerReadHasNoFractionalBranch(t *testing.T) {
	t.Parallel()
	files, err := Generate(csUnitOf(t, "package probe\ntable Root { n int32 }\n"))
	if err != nil {
		t.Fatal(err)
	}
	var all strings.Builder
	for _, data := range files {
		all.Write(data)
	}
	for _, gone := range []string{"JSON HAS ONE NUMBER TYPE", "d != (double)(long)d", "d != (double)(ulong)d"} {
		if strings.Contains(all.String(), gone) {
			t.Errorf("the JSON integer read still carries %q", gone)
		}
	}
}

const csReview1121Rcv = `package rcv

fixed table Root
{
    born [2..4]int32
    b bytes(8)
    v uint16
    f float32 = 0 | min = 0, max = 0.1, resolution = 0.01
}

table Var
{
    born [2..4]int32
    f float32 = 0 | min = 0, max = 0.1, resolution = 0.01
}
`

const csReview1121Snd = `package snd

union U
{
    on
    n int32
}

fixed table Root
{
    born [0..4]int32
    b [2..8]uint8
    v uint8 = 250 | min = 250, max = 255
    u U
}
`

const csReview1121Main = `using System;
using System.Text;

static class P
{
    public static int Row(string name, bool ok, string why)
    {
        Console.WriteLine("ROW " + name + ": " + (ok ? "ok" : "FAILED " + why));
        return ok ? 0 : 1;
    }
    // n bits of v at bit offset at, least significant bit first, as the
    // message form's bit writer lays them
    public static void SetBits(byte[] b, long at, int n, ulong v)
    {
        for (int i = 0; i < n; i++)
        {
            long p = at + i;
            if (((v >> i) & 1) != 0) { b[p >> 3] |= (byte)(1 << (int)(p & 7)); }
            else { b[p >> 3] &= (byte)~(1 << (int)(p & 7)); }
        }
    }
    public static byte[] Send(Snd.Root v)
    {
        byte[] buf = new byte[4096];
        long n = Snd.Schema.RootSaveMessages(new Snd.Root[] { v }, buf);
        return n < 0 ? new byte[0] : buf.AsSpan(0, (int)n).ToArray();
    }
    public static Rcv.TableVocabulary FromSnd()
    {
        var voc = new Rcv.TableVocabulary();
        Rcv.Schema.AnnounceRead(voc, Snd.Schema.Announce(), new Rcv.TableReport());
        return voc;
    }
    public static Rcv.TableVocabulary Own()
    {
        var voc = new Rcv.TableVocabulary();
        Rcv.Schema.AnnounceRead(voc, Rcv.Schema.Announce(), new Rcv.TableReport());
        return voc;
    }
    public static Rcv.Schema.TableWire.Verdict Receive(byte[] bytes, Rcv.TableVocabulary voc, Rcv.Root into, Rcv.TableReport rep)
    {
        return Rcv.Schema.RootLoadMessages(new Rcv.Root[] { into }, bytes, voc, rep, out int count);
    }
    public static byte[] SaveVar(Rcv.Var v)
    {
        byte[] buf = new byte[Rcv.Schema.VarMeasure(v)];
        long n = Rcv.Schema.VarSave(v, buf);
        return buf.AsSpan(0, (int)Math.Max(0, n)).ToArray();
    }
    public static byte[] SaveFixed(Rcv.Root v)
    {
        byte[] buf = new byte[Rcv.Schema.RootFixedMeasure(1)];
        long n = Rcv.Schema.RootFixedSave(v, buf);
        return buf.AsSpan(0, (int)Math.Max(0, n)).ToArray();
    }
    public static string Reason(Rcv.TableReport r) { return r.Reason ?? "(none)"; }
}

static class ProbeMain
{
    static int Main()
    {
        int bad = 0;
        var Ok = Rcv.Schema.TableWire.Verdict.Ok;
        var Refused = Rcv.Schema.TableWire.Verdict.Refused;
        int refBits = P.FromSnd().RefBits;

        {
            // bytes(N) and [..N]uint8 are one kind: the count rides as
            // count - announced min, so the reader adds the min back
            var s = new Snd.Root(); s.B[0] = 7; s.B[1] = 8; s.B[2] = 9; s.BCount = 3;
            var r = new Rcv.Root(); var rep = new Rcv.TableReport();
            var v = P.Receive(P.Send(s), P.FromSnd(), r, rep);
            bad += P.Row("cs1_bytes_adds_announced_min", v == Ok && r.BLength == 3 && r.B[0] == 7 && r.B[1] == 8 && r.B[2] == 9,
                "verdict=" + v + " reason=" + P.Reason(rep) + " length=" + r.BLength);
        }
        {
            var s = new Snd.Root(); s.Born[0] = 5; s.BornCount = 1;
            var r = new Rcv.Root(); var rep = new Rcv.TableReport();
            var v = P.Receive(P.Send(s), P.FromSnd(), r, rep);
            bad += P.Row("cs2_message_count_below_min_refused", v == Refused && P.Reason(rep).StartsWith("count_over_bound"),
                "verdict=" + v + " reason=" + P.Reason(rep) + " count=" + r.BornCount);
        }
        {
            var w = new Rcv.Var(); w.Born[0] = 5; w.BornCount = 1;
            var back = new Rcv.Var(); var rep = new Rcv.TableReport();
            bool ok = Rcv.Schema.VarLoad(back, P.SaveVar(w), rep);
            bad += P.Row("cs2_variable_count_below_min_refused", !ok && rep.Refused && P.Reason(rep).StartsWith("count_over_bound"),
                "ok=" + ok + " reason=" + P.Reason(rep) + " count=" + back.BornCount);
        }
        {
            var w = new Rcv.Root(); w.Born[0] = 5; w.BornCount = 1;
            var back = new Rcv.Root[] { new Rcv.Root() }; var rep = new Rcv.TableReport();
            long n = Rcv.Schema.RootFixedLoad(back, P.SaveFixed(w), new Rcv.TableFixedEntry[8192], rep);
            bad += P.Row("cs3_fixed_count_below_min_refused", n < 1 && rep.Refused,
                "n=" + n + " reason=" + P.Reason(rep) + " count=" + back[0].BornCount);
        }
        {
            var w = new Rcv.Root(); w.Born[0] = 5; w.Born[1] = 6; w.BornCount = 2;
            var back = new Rcv.Root[] { new Rcv.Root() }; var rep = new Rcv.TableReport();
            long n = Rcv.Schema.RootFixedLoad(back, P.SaveFixed(w), new Rcv.TableFixedEntry[8192], rep);
            bad += P.Row("cs3_fixed_count_at_min_reads", n == 1 && !rep.Refused && back[0].BornCount == 2 && back[0].Born[1] == 6,
                "n=" + n + " reason=" + P.Reason(rep) + " count=" + back[0].BornCount);
        }
        {
            bool ok = new Rcv.Var().BornCount == 2 && new Rcv.Root().BornCount == 2;
            bad += P.Row("cs5_born_at_min", ok, "var=" + new Rcv.Var().BornCount + " root=" + new Rcv.Root().BornCount);
        }
        {
            var w = new Rcv.Var(); w.BornCount = 4; w.Born[3] = 9; Rcv.Schema.TableReset(w);
            var f = new Rcv.Root(); f.BornCount = 0; Rcv.Schema.TableReset(f);
            bad += P.Row("cs5_reset_to_min", w.BornCount == 2 && w.Born[3] == 0 && f.BornCount == 2,
                "var=" + w.BornCount + " root=" + f.BornCount);
        }
        {
            // a value at its birth elides the array; read back, it is born again
            var back = new Rcv.Var(); back.BornCount = 4; var rep = new Rcv.TableReport();
            bool ok = Rcv.Schema.VarLoad(back, P.SaveVar(new Rcv.Var()), rep);
            bad += P.Row("cs5_absent_variable_array_reads_born", ok && back.BornCount == 2 && back.Born[0] == 0 && back.Born[1] == 0,
                "ok=" + ok + " reason=" + P.Reason(rep) + " count=" + back.BornCount);
        }
        {
            var r = new Rcv.Root(); r.BornCount = 4; var rep = new Rcv.TableReport();
            byte[] buf = new byte[4096];
            long n = Rcv.Schema.RootSaveMessages(new Rcv.Root[] { new Rcv.Root() }, buf);
            var v = n < 0 ? Refused : P.Receive(buf.AsSpan(0, (int)n).ToArray(), P.Own(), r, rep);
            bad += P.Row("cs5_absent_message_array_reads_born", v == Ok && r.BornCount == 2,
                "verdict=" + v + " reason=" + P.Reason(rep) + " count=" + r.BornCount);
        }
        {
            // a same-build batch spelling the birth value out is a spelling no
            // conforming writer produces: born {0, 1} with its second element
            // cleared to 0 is the birth value, written
            var w = new Rcv.Root(); w.Born[0] = 0; w.Born[1] = 1; w.BornCount = 2;
            byte[] buf = new byte[4096];
            long n = Rcv.Schema.RootSaveMessages(new Rcv.Root[] { w }, buf);
            byte[] bytes = buf.AsSpan(0, (int)Math.Max(0, n)).ToArray();
            var own = P.Own();
            int ownBits = own.RefBits;
            // [slot][count - 2: 2 bits][int32][int32][end]
            if (bytes.Length > 0) { P.SetBits(bytes, 16 + ownBits + 2 + 32, 32, 0); }
            var r = new Rcv.Root(); var rep = new Rcv.TableReport();
            var v = P.Receive(bytes, own, r, rep);
            bad += P.Row("cs5_message_birth_spelled_out_refused", v != Ok, "verdict=" + v + " count=" + r.BornCount);
        }
        {
            var w = new Rcv.Var(); var rep = new Rcv.TableReport();
            bool below = Rcv.Schema.VarFromJson(w, Encoding.ASCII.GetBytes("{\"born\":[1]}"), rep);
            string why = rep.Reason;
            var w2 = new Rcv.Var(); var rep2 = new Rcv.TableReport();
            bool at = Rcv.Schema.VarFromJson(w2, Encoding.ASCII.GetBytes("{\"born\":[1,2]}"), rep2);
            bad += P.Row("json_count_below_min_refused", !below && why != null && why.StartsWith("count_over_bound") && at && w2.BornCount == 2,
                "below=" + below + " reason=" + why + " at=" + at + " reason=" + rep2.Reason);
        }
        {
            // the top grid step is float32(0.1), above the double 0.1: held at
            // float32, it is in range, and the writer's own text reads back
            var w = new Rcv.Var(); w.F = 0.1f; w.BornCount = 2;
            byte[] text = new byte[4096];
            long n = Rcv.Schema.VarToJson(w, text);
            var back = new Rcv.Var(); var rep = new Rcv.TableReport();
            bool ok = n > 0 && Rcv.Schema.VarFromJson(back, text.AsSpan(0, (int)n), rep);
            bad += P.Row("json_float32_range_at_float32", ok && BitConverter.SingleToUInt32Bits(back.F) == BitConverter.SingleToUInt32Bits(0.1f),
                "n=" + n + " ok=" + ok + " reason=" + P.Reason(rep));
        }
        {
            var back = new Rcv.Var(); var rep = new Rcv.TableReport();
            bool ok = Rcv.Schema.VarFromJson(back, Encoding.ASCII.GetBytes("{\"f\":-0.0}"), rep);
            bad += P.Row("negzero_json_read_lands_positive", ok && BitConverter.SingleToUInt32Bits(back.F) == 0,
                "ok=" + ok + " reason=" + P.Reason(rep) + " bits=" + BitConverter.SingleToUInt32Bits(back.F).ToString("x8"));
        }
        {
            var w = new Rcv.Var(); w.F = -0.0f;
            var back = new Rcv.Var(); var rep = new Rcv.TableReport();
            bool ok = Rcv.Schema.VarLoad(back, P.SaveVar(w), rep);
            bad += P.Row("negzero_variable_read_lands_positive", ok && BitConverter.SingleToUInt32Bits(back.F) == 0,
                "ok=" + ok + " reason=" + P.Reason(rep) + " bits=" + BitConverter.SingleToUInt32Bits(back.F).ToString("x8"));
        }
        {
            var neg = new Rcv.Root(); neg.F = -0.0f;
            var pos = new Rcv.Root(); pos.F = 0.0f;
            byte[] a = P.SaveFixed(neg), b = P.SaveFixed(pos);
            bad += P.Row("negzero_fixed_write_stores_positive", a.Length > 0 && a.AsSpan().SequenceEqual(b), "lengths " + a.Length + "/" + b.Length);
        }
        {
            // v rides alone: [slot][offset: 3 bits][end]; offset 7 over base
            // 250 spells 257, which the announced uint8 cannot hold
            var s = new Snd.Root(); s.V = 251;
            byte[] bytes = P.Send(s);
            if (bytes.Length > 0) { P.SetBits(bytes, 16 + refBits, 3, 7); }
            var r = new Rcv.Root(); var rep = new Rcv.TableReport();
            var v = P.Receive(bytes, P.FromSnd(), r, rep);
            bad += P.Row("message_ranged_held_to_announced_kind", v != Ok, "verdict=" + v + " landed v=" + r.V);
        }
        {
            // u rides alone: [slot][arm][end]; a union FIELD at None is elided
            // at every build, so arm 0 stored for it is refused even skipped
            var s = new Snd.Root(); s.U.Type = Snd.UType.On;
            byte[] bytes = P.Send(s);
            if (bytes.Length > 0) { P.SetBits(bytes, 16 + refBits, refBits, 0); }
            var r = new Rcv.Root(); var rep = new Rcv.TableReport();
            var v = P.Receive(bytes, P.FromSnd(), r, rep);
            bad += P.Row("message_skipped_union_field_arm0_refused", v != Ok, "verdict=" + v);
        }
        Console.WriteLine(bad == 0 ? "PROBE OK" : "FAILED " + bad);
        return bad == 0 ? 0 : 1;
    }
}
`

var (
	csReview1121Once sync.Once
	csReview1121Out  []byte
	csReview1121Err  error
)

func csReview1121Run(t *testing.T) ([]byte, error) {
	t.Helper()
	csReview1121Once.Do(func() { csReview1121Out, csReview1121Err = csReview1121Build(t) })
	return csReview1121Out, csReview1121Err
}

func csReview1121Build(t *testing.T) ([]byte, error) {
	slowtest.Gate(t, "the .NET SDK (it compiles and runs the generated C# reader)")
	t.Helper()
	dir := t.TempDir()
	var sources []string
	for i, schema := range []string{csReview1121Rcv, csReview1121Snd} {
		u := unitFrom(t, schema)
		files, err := csharp.Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		tables, err := Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		maps.Copy(files, tables)
		unit := filepath.Join(dir, "gen", fmt.Sprintf("u%02d", i))
		if err := os.MkdirAll(unit, 0o700); err != nil {
			t.Fatal(err)
		}
		for name, data := range files {
			if !strings.HasSuffix(name, ".cs") {
				continue
			}
			path := filepath.Join(unit, name)
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			sources = append(sources, path)
		}
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
			t.Fatalf("the C# runtime checkout: %v", err)
		}
		sources = append(sources, path)
	}
	main := filepath.Join(dir, "Review1121.cs")
	if err := os.WriteFile(main, []byte(csReview1121Main), 0o600); err != nil {
		t.Fatal(err)
	}
	csc, refs, runtime := csDotnetToolchain(t)
	dotnet := csDotnet()
	app := filepath.Join(dir, "csreview1121.dll")
	args := []string{"exec", csc, "-nologo", "-noconfig", "-target:exe", "-optimize+", "-unsafe",
		"-nullable:annotations", "-langversion:latest", "-nostdlib+", "-nowarn:CS8500", "-out:" + app}
	for _, ref := range refs {
		args = append(args, "-r:"+ref)
	}
	args = append(args, sources...)
	args = append(args, main)
	compile := testproc.Command(t, dotnet, args...)
	compile.Env, compile.Dir = csDotnetEnv(dir, dir), dir
	compileOut, compileErr := compile.CombinedOutput()
	if compileErr != nil {
		return compileOut, fmt.Errorf("compile: %w", compileErr)
	}
	cfg := fmt.Sprintf("{\"runtimeOptions\":{\"tfm\":\"net10.0\",\"framework\":{\"name\":\"Microsoft.NETCore.App\",\"version\":%q}}}\n", runtime)
	if err := os.WriteFile(filepath.Join(dir, "csreview1121.runtimeconfig.json"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	run := testproc.Command(t, dotnet, "exec", app)
	run.Env, run.Dir = csDotnetEnv(dir, dir), dir
	runOut, runErr := run.CombinedOutput()
	return append(compileOut, runOut...), runErr
}
