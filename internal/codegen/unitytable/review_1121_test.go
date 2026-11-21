package unitytable_test

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestUnityReview1121Reads holds the C# (unity) share of the 2026-11-21 review:
// UNITY-1 (the JSON timestamp read takes the whole int64 nanosecond range,
// its own writer's output included) and the cross-leg value rules (a float32
// range compared at float32, -0 landing and stored as +0, a counted array
// below its minimum refused on the message, fixed and JSON reads (the writers
// debug-assert the count, so no variable image below it is made here),
// message-form ranged values held to
// the announced kind, a union field at arm 0 refused on the skip path). Two
// units share one assembly so one can announce to the other.
func TestUnityReview1121Reads(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dotnet Roslyn csc Unity dialect review 2026-11-21 reads")

	files := map[string][]byte{}
	for prefix, src := range map[string]string{"rcv_": unityReview1121Rcv, "snd_": unityReview1121Snd} {
		got, err := unityGenerateFiles(fixedproof.ParseCheckUnit(t, src), nil)
		if err != nil {
			t.Fatal(err)
		}
		for name, data := range got {
			files[prefix+name] = data
		}
	}
	out, code, _, err := runUnityProbe(t, files, unityReview1121Main, nil)
	if err != nil {
		t.Fatalf("probe build/exec error: %v\n%s", err, out)
	}
	for _, row := range []string{
		"unity1_timestamp_min_round_trips",
		"unity1_timestamp_max_round_trips",
		"json_float32_range_at_float32",
		"negzero_json_read_lands_positive",
		"negzero_variable_read_lands_positive",
		"negzero_fixed_write_stores_positive",
		"message_ranged_held_to_announced_kind",
		"message_skipped_union_field_arm0_refused",
		"message_count_below_min_refused",
		"fixed_count_below_min_refused",
		"json_count_below_min_refused",
		"born_at_min",
	} {
		if !strings.Contains(out, "ROW "+row+": ok") {
			t.Errorf("row %s is not held:\n%s", row, out)
		}
	}
	if code != 0 && !t.Failed() {
		t.Fatalf("probe exited %d:\n%s", code, out)
	}
}

const unityReview1121Rcv = `package rcv

fixed table Root
{
    born [2..4]int32
    v uint16
    f float32 = 0 | min = 0, max = 0.1, resolution = 0.01
}

table Var
{
    born [2..4]int32
    f float32 = 0 | min = 0, max = 0.1, resolution = 0.01
    t timestamp
}
`

const unityReview1121Snd = `package snd

union U
{
    on
    n int32
}

fixed table Root
{
    born [0..4]int32
    v uint8 = 250 | min = 250, max = 255
    u U
}
`

const unityReview1121Main = `using System;
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
    public static Rcv.Schema.TableVocabulary FromSnd()
    {
        var voc = new Rcv.Schema.TableVocabulary();
        var rep = new Rcv.Schema.TableReport();
        Rcv.Schema.AnnounceRead(voc, Snd.Schema.Announce(), ref rep);
        return voc;
    }
    public static bool Receive(byte[] bytes, Rcv.Schema.TableVocabulary voc, out Rcv.Root into, ref Rcv.Schema.TableReport rep)
    {
        var values = new Rcv.Root[] { Rcv.Schema.NewRoot() };
        bool ok = Rcv.Schema.RootLoadMessages(values, bytes, voc, ref rep, out int count);
        into = values[0];
        return ok && count == 1;
    }
    public static byte[] SaveVar(Rcv.Var v)
    {
        long size = Rcv.Schema.VarMeasure(ref v);
        if (size < 0) { return new byte[0]; }
        byte[] buf = new byte[size];
        long n = Rcv.Schema.VarSave(ref v, buf);
        return buf.AsSpan(0, (int)Math.Max(0, n)).ToArray();
    }
    public static byte[] SaveFixed(Rcv.Root v)
    {
        byte[] buf = new byte[Rcv.Schema.RootFixedMeasure(1)];
        long n = Rcv.Schema.RootFixedSave(new Rcv.Root[] { v }, buf);
        return buf.AsSpan(0, (int)Math.Max(0, n)).ToArray();
    }
    public static byte[] ToJson(Rcv.Var v)
    {
        long size = Rcv.Schema.VarToJsonMeasure(ref v);
        if (size < 0) { return new byte[0]; }
        byte[] buf = new byte[size];
        long n = Rcv.Schema.VarToJson(ref v, buf);
        return buf.AsSpan(0, (int)Math.Max(0, n)).ToArray();
    }
    public static string Reason(Rcv.Schema.TableReport r)
    {
        return !string.IsNullOrEmpty(r.ReasonText) ? r.ReasonText : Rcv.Schema.TableReport.ReasonName(r.Reason);
    }
}

static class ProbeMain
{
    static int Main()
    {
        int bad = 0;
        int refBits = P.FromSnd().RefBits;

        foreach (var (name, t) in new[] { ("unity1_timestamp_min_round_trips", long.MinValue), ("unity1_timestamp_max_round_trips", long.MaxValue) })
        {
            // the whole int64 nanosecond range is a timestamp: the writer's
            // own text for either end reads back to the same instant
            var w = Rcv.Schema.NewVar(); w.T = t;
            byte[] text = P.ToJson(w);
            var back = Rcv.Schema.NewVar(); var rep = new Rcv.Schema.TableReport();
            bool ok = text.Length > 0 && Rcv.Schema.VarFromJson(ref back, text, ref rep);
            bad += P.Row(name, ok && back.T == t,
                "ok=" + ok + " reason=" + P.Reason(rep) + " text=" + Encoding.ASCII.GetString(text) + " back=" + back.T);
        }
        {
            // the top grid step is float32(0.1), above the double 0.1: held at
            // float32, it is in range, and the writer's own text reads back
            var w = Rcv.Schema.NewVar(); w.F = 0.1f;
            byte[] text = P.ToJson(w);
            var back = Rcv.Schema.NewVar(); var rep = new Rcv.Schema.TableReport();
            bool ok = text.Length > 0 && Rcv.Schema.VarFromJson(ref back, text, ref rep);
            bad += P.Row("json_float32_range_at_float32", ok && BitConverter.SingleToUInt32Bits(back.F) == BitConverter.SingleToUInt32Bits(0.1f),
                "ok=" + ok + " reason=" + P.Reason(rep) + " text=" + Encoding.ASCII.GetString(text));
        }
        {
            var back = Rcv.Schema.NewVar(); var rep = new Rcv.Schema.TableReport();
            bool ok = Rcv.Schema.VarFromJson(ref back, Encoding.ASCII.GetBytes("{\"f\":-0.0}"), ref rep);
            bad += P.Row("negzero_json_read_lands_positive", ok && BitConverter.SingleToUInt32Bits(back.F) == 0,
                "ok=" + ok + " reason=" + P.Reason(rep) + " bits=" + BitConverter.SingleToUInt32Bits(back.F).ToString("x8"));
        }
        {
            var w = Rcv.Schema.NewVar(); w.F = -0.0f;
            var back = Rcv.Schema.NewVar(); var rep = new Rcv.Schema.TableReport();
            bool ok = Rcv.Schema.VarLoad(ref back, P.SaveVar(w), ref rep);
            bad += P.Row("negzero_variable_read_lands_positive", ok && BitConverter.SingleToUInt32Bits(back.F) == 0,
                "ok=" + ok + " reason=" + P.Reason(rep) + " bits=" + BitConverter.SingleToUInt32Bits(back.F).ToString("x8"));
        }
        {
            var neg = Rcv.Schema.NewRoot(); neg.F = -0.0f;
            var pos = Rcv.Schema.NewRoot(); pos.F = 0.0f;
            byte[] a = P.SaveFixed(neg), b = P.SaveFixed(pos);
            bad += P.Row("negzero_fixed_write_stores_positive", a.Length > 0 && a.AsSpan().SequenceEqual(b), "lengths " + a.Length + "/" + b.Length);
        }
        {
            // v rides alone: [slot][offset: 3 bits][end]; offset 7 over base
            // 250 spells 257, which the announced uint8 cannot hold
            var s = Snd.Schema.NewRoot(); s.V = 251;
            byte[] bytes = P.Send(s);
            if (bytes.Length > 0) { P.SetBits(bytes, 16 + refBits, 3, 7); }
            var rep = new Rcv.Schema.TableReport();
            bool ok = P.Receive(bytes, P.FromSnd(), out Rcv.Root r, ref rep);
            bad += P.Row("message_ranged_held_to_announced_kind", bytes.Length > 0 && !ok, "ok=" + ok + " landed v=" + r.V);
        }
        {
            // u rides alone: [slot][arm][end]; a union FIELD at None is elided
            // at every build, so arm 0 stored for it is refused even skipped
            var s = Snd.Schema.NewRoot(); s.U.Type = Snd.UType.On;
            byte[] bytes = P.Send(s);
            if (bytes.Length > 0) { P.SetBits(bytes, 16 + refBits, refBits, 0); }
            var rep = new Rcv.Schema.TableReport();
            bool ok = P.Receive(bytes, P.FromSnd(), out Rcv.Root r, ref rep);
            bad += P.Row("message_skipped_union_field_arm0_refused", bytes.Length > 0 && !ok, "ok=" + ok);
        }
        {
            // the sender's [0..4] lets a count of 1 ride; this reader's
            // [2..4] holds it to its minimum
            var s = Snd.Schema.NewRoot(); s.Born[0] = 5; s.BornCount = 1;
            var rep = new Rcv.Schema.TableReport();
            bool ok = P.Receive(P.Send(s), P.FromSnd(), out Rcv.Root r, ref rep);
            bad += P.Row("message_count_below_min_refused", !ok && P.Reason(rep).StartsWith("count_over_bound"),
                "ok=" + ok + " reason=" + P.Reason(rep) + " count=" + r.BornCount);
        }
        {
            // the writer asserts the count, so the image below the minimum is
            // made by finding the stored count (the one byte a count of 2 and
            // a count of 3 over the same zero elements differ in) and storing 1
            var two = Rcv.Schema.NewRoot();
            var three = Rcv.Schema.NewRoot(); three.BornCount = 3;
            byte[] a = P.SaveFixed(two), b = P.SaveFixed(three);
            int at = -1, diffs = 0;
            for (int i = 0; i < Math.Min(a.Length, b.Length); i++) { if (a[i] != b[i]) { at = i; diffs++; } }
            var back = new Rcv.Root[] { Rcv.Schema.NewRoot() }; var rep = new Rcv.Schema.TableReport();
            long n = -2;
            if (a.Length > 0 && a.Length == b.Length && diffs == 1 && a[at] == 2)
            {
                a[at] = 1;
                n = Rcv.Schema.RootFixedLoad(back, a, ref rep);
            }
            bad += P.Row("fixed_count_below_min_refused", n == -1 && rep.Refused && rep.Reason == Rcv.Schema.TableReport.CountOverBound,
                "n=" + n + " diffs=" + diffs + " reason=" + P.Reason(rep) + " count=" + back[0].BornCount);
        }
        {
            var w = Rcv.Schema.NewVar(); var rep = new Rcv.Schema.TableReport();
            bool below = Rcv.Schema.VarFromJson(ref w, Encoding.ASCII.GetBytes("{\"born\":[1]}"), ref rep);
            string why = P.Reason(rep);
            var w2 = Rcv.Schema.NewVar(); var rep2 = new Rcv.Schema.TableReport();
            bool at = Rcv.Schema.VarFromJson(ref w2, Encoding.ASCII.GetBytes("{\"born\":[1,2]}"), ref rep2);
            bad += P.Row("json_count_below_min_refused", !below && rep.Refused && at && w2.BornCount == 2,
                "below=" + below + " reason=" + why + " at=" + at + " reason=" + P.Reason(rep2));
        }
        {
            bool ok = Rcv.Schema.NewVar().BornCount == 2 && Rcv.Schema.NewRoot().BornCount == 2;
            bad += P.Row("born_at_min", ok, "var=" + Rcv.Schema.NewVar().BornCount + " root=" + Rcv.Schema.NewRoot().BornCount);
        }
        Console.WriteLine(bad == 0 ? "PROBE OK" : "FAILED " + bad);
        return bad == 0 ? 0 : 1;
    }
}
`
