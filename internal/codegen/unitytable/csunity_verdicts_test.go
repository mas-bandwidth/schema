package unitytable_test

// The C# (unity) leg's share of the cs/unity verdict fixes. Each row is red on
// the code as it stood before the fix, except the dep_wire_* and
// dep_json_write_* rows, which hold what that leg already did:
//
//	dep_json_*       a `| deprecated` variant in the text reads as the field's
//	                 declared default (docs/SPEC-TABLES.md §2.15)
//	mapkey_*         a map key is bounded by its own string(N), not the 255-byte name cap
//	mapdup_*         a repeated binary map key is damage, not a named refusal
//	b64_*            a non-canonical base64 value is a named refusal, not damage
//	messages_*       a refused message batch hands back reset values (§3.3)

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const unityVerdictsVdt = `package vdt

enum Kind
{
    Alpha
    Old   | deprecated
    Beta
}

table Root
{
    k    Kind = Beta
    ks   [..3]Kind
}

table Maps
{
    entries map[uint32]int32
    names   map[string(300)]int32
}

table Blob
{
    data bytes(8)
}

fixed table Msg
{
    v bool
    w int32 = 5
}
`

// the sender of the message batch: the same Msg with v an int32, which the
// receiver (v a bool) cannot read, so the batch is refused at its first body.
const unityVerdictsSnd = `package snd

fixed table Msg
{
    v int32
    w int32 = 5
}
`

func uvLE64(v uint64) []byte { return binary.LittleEndian.AppendUint64(nil, v) }

func uvWire(body []byte, ids ...uint64) []byte {
	wire := append([]byte{1}, body...)
	for _, id := range ids {
		wire = append(wire, uvLE64(id)...)
	}
	return append(wire, uvLE64(uint64(len(ids)))...)
}

func TestUnityVerdicts(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dotnet Roslyn csc Unity dialect verdicts")

	vdt := fixedproof.ParseCheckUnit(t, unityVerdictsVdt)
	m := tabletext.NewModel(vdt)
	rootK := ir.TableFieldWireId(m.Lookup("Root").Fields[0])
	rootKs := ir.TableFieldWireId(m.Lookup("Root").Fields[1])
	entries := ir.TableFieldWireId(m.Lookup("Maps").Fields[0])
	oldWire := uvWire([]byte{1, 30, 2, 0}, rootK, ir.TableWireId("Old"))
	// an array whose one element is the deprecated variant
	oldElemWire := uvWire([]byte{1, 14, 3, 30, 1, 2, 0}, rootKs, ir.TableWireId("Old"))
	// two map entries under ONE binary key
	entry := []byte{2, 8, 7, 0, 0, 0, 0}
	payload := append([]byte{13, 2, byte(len(entry))}, entry...)
	payload = append(payload, byte(len(entry)))
	payload = append(payload, entry...)
	dupBody := append([]byte{1, 14, byte(len(payload))}, payload...)
	dupBody = append(dupBody, 0)
	dupWire := uvWire(dupBody, entries, ir.MapKeyWireId)

	files := map[string][]byte{}
	for prefix, u := range map[string]*ir.Unit{
		"vdt_": vdt,
		"snd_": fixedproof.ParseCheckUnit(t, unityVerdictsSnd),
	} {
		got, err := unityGenerateFiles(u, nil)
		if err != nil {
			t.Fatal(err)
		}
		for name, data := range got {
			files[prefix+name] = data
		}
	}
	main := fmt.Sprintf(unityVerdictsMain, hex.EncodeToString(oldWire), hex.EncodeToString(dupWire), hex.EncodeToString(oldElemWire))
	out, code, _, err := runUnityProbe(t, files, main, nil)
	if err != nil {
		t.Fatalf("probe build/exec error: %v\n%s", err, out)
	}
	for _, row := range []string{
		"dep_wire_scalar",
		"dep_wire_element",
		"dep_json_scalar",
		"dep_json_element",
		"dep_json_write_refused",
		"dep_json_write_control",
		"mapkey_300_bytes",
		"mapdup_wire_is_damage",
		"b64_control_canonical",
		"b64_pad_bit_is_refused",
		"b64_unpadded_is_refused",
		"messages_refused_batch_resets",
	} {
		if !strings.Contains(out, "ROW "+row+": ok") {
			t.Errorf("row %s is not held:\n%s", row, out)
		}
	}
	if code != 0 && !t.Failed() {
		t.Fatalf("probe exited %d:\n%s", code, out)
	}
}

const unityVerdictsMain = `using System;
using System.Text;

static class P
{
    public static int Row(string name, bool ok, string why)
    {
        Console.WriteLine("ROW " + name + ": " + (ok ? "ok" : "FAILED " + why));
        return ok ? 0 : 1;
    }
    public static byte[] J(string s) { return Encoding.UTF8.GetBytes(s); }
    public static string Reason(Vdt.Schema.TableReport r)
    {
        return !string.IsNullOrEmpty(r.ReasonText) ? r.ReasonText : Vdt.Schema.TableReport.ReasonName(r.Reason);
    }
}

static class ProbeMain
{
    static int Main()
    {
        int bad = 0;
        byte[] oldWire = Convert.FromHexString("%s");
        byte[] dupWire = Convert.FromHexString("%s");
        byte[] oldElemWire = Convert.FromHexString("%s");
        {
            var v = Vdt.Schema.NewRoot(); var r = new Vdt.Schema.TableReport();
            bool ok = Vdt.Schema.RootLoad(ref v, oldWire, ref r);
            bad += P.Row("dep_wire_scalar", ok && v.K == Vdt.Kind.Beta && r.Unknown == 0, "ok=" + ok + " k=" + v.K + " unknown=" + r.Unknown);
        }
        {
            var v = Vdt.Schema.NewRoot(); var r = new Vdt.Schema.TableReport();
            bool ok = Vdt.Schema.RootLoad(ref v, oldElemWire, ref r);
            bad += P.Row("dep_wire_element", ok && v.KsCount == 1 && v.Ks[0] == Vdt.Kind.None && r.Unknown == 0, "ok=" + ok + " count=" + v.KsCount + " k0=" + v.Ks[0] + " unknown=" + r.Unknown);
        }
        {
            var v = Vdt.Schema.NewRoot(); var r = new Vdt.Schema.TableReport();
            bool ok = Vdt.Schema.RootFromJson(ref v, P.J("{\"k\":\"Old\"}"), ref r);
            bad += P.Row("dep_json_scalar", ok && v.K == Vdt.Kind.Beta && r.Unknown == 0, "ok=" + ok + " k=" + v.K + " unknown=" + r.Unknown);
            v = Vdt.Schema.NewRoot(); r = new Vdt.Schema.TableReport();
            ok = Vdt.Schema.RootFromJson(ref v, P.J("{\"k\":\"Alpha\",\"ks\":[\"Alpha\",\"Old\"]}"), ref r);
            bad += P.Row("dep_json_element", ok && v.K == Vdt.Kind.Alpha && v.KsCount == 2 && v.Ks[0] == Vdt.Kind.Alpha && v.Ks[1] == Vdt.Kind.None && r.Unknown == 0,
                "ok=" + ok + " count=" + v.KsCount + " ks=" + v.Ks[0] + "," + v.Ks[1] + " unknown=" + r.Unknown);
        }
        {
            var v = Vdt.Schema.NewRoot(); v.K = Vdt.Kind.Old;
            long refused = Vdt.Schema.RootToJsonMeasure(ref v);
            bad += P.Row("dep_json_write_refused", refused < 0, "a deprecated variant measured " + refused);
            v = Vdt.Schema.NewRoot(); v.Ks[0] = Vdt.Kind.Old; v.KsCount = 1;
            long refusedElem = Vdt.Schema.RootToJsonMeasure(ref v);
            bad += P.Row("dep_json_write_element_refused", refusedElem < 0, "a deprecated element measured " + refusedElem);
            v = Vdt.Schema.NewRoot(); v.K = Vdt.Kind.Alpha;
            long fine = Vdt.Schema.RootToJsonMeasure(ref v);
            bad += P.Row("dep_json_write_control", fine > 0, "an ordinary variant measured " + fine);
        }
        {
            var v = new Vdt.Maps(); var r = new Vdt.Schema.TableReport();
            bool ok = Vdt.Schema.MapsFromJson(ref v, P.J("{\"names\":{\"" + new string('a', 300) + "\":1}}"), ref r);
            bad += P.Row("mapkey_300_bytes", ok && v.NamesCount == 1 && v.Names[0].KeyLength == 300 && v.Names[0].Value == 1,
                "ok=" + ok + " reason=" + P.Reason(r));
        }
        {
            var v = new Vdt.Maps(); var r = new Vdt.Schema.TableReport();
            bool ok = Vdt.Schema.MapsLoad(ref v, dupWire, ref r);
            bad += P.Row("mapdup_wire_is_damage", !ok && r.Damaged && P.Reason(r) != "duplicate", "ok=" + ok + " damaged=" + r.Damaged + " reason=" + P.Reason(r));
        }
        {
            var v = new Vdt.Blob(); var r = new Vdt.Schema.TableReport();
            bool ok = Vdt.Schema.BlobFromJson(ref v, P.J("{\"data\":\"QUI=\"}"), ref r);
            bad += P.Row("b64_control_canonical", ok && v.DataLength == 2, "ok=" + ok + " reason=" + P.Reason(r));
            v = new Vdt.Blob(); r = new Vdt.Schema.TableReport();
            ok = Vdt.Schema.BlobFromJson(ref v, P.J("{\"data\":\"QUJ=\"}"), ref r);
            bad += P.Row("b64_pad_bit_is_refused", !ok && r.Refused && !r.Damaged && P.Reason(r).StartsWith("base64_not_canonical"),
                "ok=" + ok + " refused=" + r.Refused + " damaged=" + r.Damaged + " reason=" + P.Reason(r));
            v = new Vdt.Blob(); r = new Vdt.Schema.TableReport();
            ok = Vdt.Schema.BlobFromJson(ref v, P.J("{\"data\":\"QUI\"}"), ref r);
            bad += P.Row("b64_unpadded_is_refused", !ok && r.Refused && !r.Damaged && P.Reason(r).StartsWith("base64_not_canonical"),
                "ok=" + ok + " refused=" + r.Refused + " damaged=" + r.Damaged + " reason=" + P.Reason(r));
        }
        {
            // a batch the receiver refuses at its first body: the caller's values
            // are handed back reset (the touched one), never half-read or stale
            var a = new Snd.Msg { V = 1, W = 5 }; var b = new Snd.Msg { V = 2, W = 5 };
            byte[] buf = new byte[4096];
            long n = Snd.Schema.MsgSaveMessages(new Snd.Msg[] { a, b }, buf);
            byte[] bytes = buf.AsSpan(0, (int)Math.Max(0, n)).ToArray();
            var voc = new Vdt.Schema.TableVocabulary(); var arep = new Vdt.Schema.TableReport();
            Vdt.Schema.AnnounceRead(voc, Snd.Schema.Announce(), ref arep);
            var values = new Vdt.Msg[2];
            values[0].W = 77; values[1].W = 77;
            var rep = new Vdt.Schema.TableReport();
            bool ok = Vdt.Schema.MsgLoadMessages(values, bytes, voc, ref rep, out int count);
            bad += P.Row("messages_refused_batch_resets", n > 0 && !ok && count == 0 && P.Reason(rep) == "kind_mismatch" && values[0].W == 5,
                "n=" + n + " ok=" + ok + " count=" + count + " reason=" + P.Reason(rep) + " w0=" + values[0].W + " w1=" + values[1].W);
        }
        Console.WriteLine(bad == 0 ? "PROBE OK" : "FAILED " + bad);
        return bad == 0 ? 0 : 1;
    }
}
`
