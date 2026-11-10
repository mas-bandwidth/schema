package cstable

// The C# variable strict read (docs/SPEC-TABLES.md §4, §6.5): one read returns
// the whole value or refuses the whole load. TestCSharpVariableCellsStrict
// runs that read. TestCSharpVariableCellsStrictNegativeControl plants one
// fault in the emitted runtime the probe actually runs: TableWire.Load still
// returns Ok, but it wipes the value on the success path. The clean row must
// then report that the wire did not read the whole value. Refusal rows stay
// refusals. The unplanted tree is the tree the positive test runs.

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

const vtCellsReaderSchema = `package probe

enum Mood
{
    Calm,
    Angry | was = "Mad"
}

table Spot
{
    x int32 = 0
    y int32 = 7
}

table Leaf
{
    id uint32
    name string(8)
    next *Leaf
}

table Bag | was = "OldBag"
{
    n int32 = 0
}

union Arm
{
    n int32 | was = "num"
    spot Spot
    text string(4)
}

table Root | was = "OldRoot"
{
    n int32 = 0 | min = 0, max = 10
    label string(8)
    mood Mood = Angry
    flag bool = true
    wide uint128
    flux int128 | min = -1000, max = 1000
    score fixed(16, 16) | min = -100, max = 100
    wide32 int64
    blob *bytes
    note *string
    title string(4)
    spots [..3]Spot
    leaves []*Leaf
    head *Leaf
    nums []uint32
    least [..4]uint8
    arm Arm
    arms []Arm
    entries map[uint32]int32
    old int32 = 3 | was = "former"
    bag *Bag
}
`

const vtCellsRangeSchema = `package probe

table Root
{
    n int32 = 0
}
`

const vtCellsUnknownSchema = `package probe

table Root
{
    n int32 = 0
    extra int32 = 0
}
`

const vtCellsWideSchema = `package probe

table Root
{
    wide32 int32 = 0
}
`

const vtCellsRenameSchema = `package probe

enum Mood
{
    Calm,
    Mad
}

table OldBag
{
    n int32 = 0
}

table Spot
{
    x int32 = 0
    y int32 = 0
}

union Arm
{
    n int32 | was = "num"
    spot Spot
    text string(4)
}

table Root
{
    former int32 = 0
    mood Mood = Mad
    bag *OldBag
    arm Arm
}
`

// vtCellsOkAnchor is the one success return of TableWire.Load. The template
// indents that pair by eight spaces; emit writes it inside Schema and then
// inside the namespace, four more spaces each time, so the file has sixteen.
// The plant inserts a reset in front of that return, in the emitted artifact, once.
const vtCellsOkAnchor = "                if (!complete) { type.Reset(value); return Finish(report, Verdict.BodyStopped); }\n                return Finish(report, Verdict.Ok);"

const vtCellsOkPlanted = "                if (!complete) { type.Reset(value); return Finish(report, Verdict.BodyStopped); }\n                type.Reset(value); return Finish(report, Verdict.Ok);"

func TestCSharpVariableCellsStrict(t *testing.T) {
	t.Run("vt/wire-fuzzer/cs", func(t *testing.T) {
		files := generateFiles(t, "Repeated", csWireFuzzRepeatedIdSchema)
		if err := csWireFuzzRepeatedIdRule(files); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("strict-read", func(t *testing.T) {
		slowtest.Gate(t, "the .NET SDK (it compiles and runs the generated C#)")
		out, err := vtCellsRun(t, false)
		t.Logf("probe:\n%s", out)
		if err != nil || !strings.Contains(out, "VT_CELLS_OK") {
			t.Fatalf("strict read:\n%s\n%v", out, err)
		}
	})
}

func TestCSharpVariableCellsStrictNegativeControl(t *testing.T) {
	slowtest.Gate(t, "the .NET SDK (it compiles and runs the generated C#)")
	clean, err := vtCellsRun(t, false)
	if err != nil || !strings.Contains(clean, "VT_CELLS_OK") || !strings.Contains(clean, "ROW vt/variable-class/cs: ok") {
		t.Fatalf("unplanted tree:\n%s\n%v", clean, err)
	}
	planted, err := vtCellsRun(t, true)
	t.Logf("planted probe:\n%s", planted)
	if err == nil || strings.Contains(planted, "VT_CELLS_OK") || !strings.Contains(planted, "ROW vt/variable-class/cs: clean wire did not read the whole value\n") {
		t.Fatalf("planted success path did not wipe the value:\n%s\n%v", planted, err)
	}
}

func vtCellsRun(t *testing.T, plant bool) (string, error) {
	t.Helper()
	wires := vtCellsWires(t)
	files := generateFiles(t, "Probe", vtCellsReaderSchema)
	if plant {
		if err := vtCellsPlantOkReset(files); err != nil {
			t.Fatal(err)
		}
	} else if err := vtCellsCountOk(files); err != nil {
		t.Fatal(err)
	}
	return vtCellsCompileRun(t, files, vtCellsProbe(wires))
}

func vtCellsCountOk(files map[string][]byte) error {
	n := 0
	near := ""
	for _, data := range files {
		s := string(data)
		n += strings.Count(s, vtCellsOkAnchor)
		if near == "" {
			if i := strings.Index(s, "Verdict.BodyStopped"); i >= 0 {
				a := max(i-40, 0)
				b := min(i+80, len(s))
				near = s[a:b]
			}
		}
	}
	if n != 1 {
		return fmt.Errorf("emitted TableWire.Load success return appears %d times, want 1; near %q", n, near)
	}
	return nil
}

func vtCellsPlantOkReset(files map[string][]byte) error {
	if err := vtCellsCountOk(files); err != nil {
		return err
	}
	planted := 0
	for name, data := range files {
		s := string(data)
		if !strings.Contains(s, vtCellsOkAnchor) {
			continue
		}
		files[name] = []byte(strings.Replace(s, vtCellsOkAnchor, vtCellsOkPlanted, 1))
		planted++
	}
	if planted != 1 {
		return fmt.Errorf("plant touched %d files, want 1", planted)
	}
	return nil
}

type vtCellsWireSet struct {
	dup, trunc, kind, over, unknown, wide, rename string
	framing                                       []vtCellsVec
	blobShared                                    bool
	blob                                          string
}

type vtCellsVec struct {
	name    string
	hex     string
	refused bool
}

func vtCellsWires(t *testing.T) vtCellsWireSet {
	t.Helper()
	reader := tabletext.NewModel(unitFrom(t, vtCellsReaderSchema))
	clean := vtCellsEncode(t, vtCellsReaderSchema, "Root", `{"n":5}`)
	dup := vtCellsDupBody(t, clean)
	trunc := clean[:len(clean)-1]
	kind := vtCellsFlipKind(t, clean)
	over := vtCellsEncode(t, vtCellsRangeSchema, "Root", `{"n":99}`)
	unknown := vtCellsEncode(t, vtCellsUnknownSchema, "Root", `{"n":5,"extra":3}`)
	wide := vtCellsEncode(t, vtCellsWideSchema, "Root", `{"wide32":6}`)
	rename := vtCellsEncode(t, vtCellsRenameSchema, "Root", `{"former":9,"mood":"Mad","bag":{"n":4},"arm":{"n":6}}`)

	vtCellsMustRefuse(t, reader, "duplicate field id", dup)
	vtCellsMustRefuse(t, reader, "truncated wire", trunc)
	vtCellsMustRefuse(t, reader, "kind mismatch", kind)
	vtCellsMustRefuse(t, reader, "out of range", over)

	unkOK, unkRep := vtCellsDecode(reader, unknown)
	if !unkOK || unkRep.Unknown == 0 {
		t.Fatalf("unknown field: ok=%v report=%+v", unkOK, unkRep)
	}
	wideOK, wideRep, wideInst := vtCellsDecodeInst(reader, wide)
	if !wideOK || wideRep.Widened == 0 || vtCellsFieldI(wideInst, "wide32") != 6 {
		t.Fatalf("widen: ok=%v widened=%d value=%d report=%+v", wideOK, wideRep.Widened, vtCellsFieldI(wideInst, "wide32"), wideRep)
	}
	renOK, _, renInst := vtCellsDecodeInst(reader, rename)
	if !renOK || vtCellsFieldI(renInst, "old") != 9 {
		t.Fatalf("rename wire did not land old=9: ok=%v %s", renOK, vtCellsDump(renInst))
	}

	var set vtCellsWireSet
	set.dup = hex.EncodeToString(dup)
	set.trunc = hex.EncodeToString(trunc)
	set.kind = hex.EncodeToString(kind)
	set.over = hex.EncodeToString(over)
	set.unknown = hex.EncodeToString(unknown)
	set.wide = hex.EncodeToString(wide)
	set.rename = hex.EncodeToString(rename)

	for _, name := range []string{
		"frame_repeat_last_wins",
		"frame_unknown_id_length",
		"frame_unknown_id_reference",
		"node_unknown_to_fixed_reader",
		"node_table_twice",
		"node_index_null",
	} {
		path := filepath.Join("..", "..", "..", "testdata", "wire", "tables", "framing", name+".bin")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("framing corpus %s: %v", path, err)
		}
		ok, rep := vtCellsDecode(reader, data)
		t.Logf("framing %s go_ok=%v reason=%q damaged_via_refused=%v", name, ok, rep.Reason, rep.Refused)
		set.framing = append(set.framing, vtCellsVec{name: name, hex: hex.EncodeToString(data), refused: !ok})
	}
	blobPath := filepath.Join("..", "..", "..", "testdata", "wire", "tables", "blob_shared.bin")
	blob, err := os.ReadFile(blobPath)
	if err != nil {
		t.Fatalf("blob corpus: %v", err)
	}
	blobInst := reader.New(reader.Lookup("Root"))
	var blobRep tabletext.Report
	blobOK, blobErr := tablewire.Decode(reader, blobInst, blob, &blobRep)
	set.blob = hex.EncodeToString(blob)
	set.blobShared = !blobOK && tablewire.RefusalReason(blobErr) == tablewire.ReasonBlobShared
	t.Logf("blob_shared go_ok=%v reason=%s report=%+v", blobOK, tablewire.RefusalReason(blobErr), blobRep)
	return set
}

func vtCellsEncode(t *testing.T, src, table, text string) []byte {
	t.Helper()
	m := tabletext.NewModel(unitFrom(t, src))
	inst := m.New(m.Lookup(table))
	var rep tabletext.Report
	if !m.Read(inst, []byte(text), &rep) {
		t.Fatalf("place %s: %s", text, rep.Reason)
	}
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatalf("encode %s: %v", text, err)
	}
	return wire
}

func vtCellsDecode(m *tabletext.Model, wire []byte) (bool, tabletext.Report) {
	ok, rep, _ := vtCellsDecodeInst(m, wire)
	return ok, rep
}

func vtCellsDecodeInst(m *tabletext.Model, wire []byte) (bool, tabletext.Report, *tabletext.Instance) {
	inst := m.New(m.Lookup("Root"))
	var rep tabletext.Report
	ok, _ := tablewire.Decode(m, inst, wire, &rep)
	return ok, rep, inst
}

func vtCellsMustRefuse(t *testing.T, m *tabletext.Model, what string, wire []byte) {
	t.Helper()
	ok, rep := vtCellsDecode(m, wire)
	if ok {
		t.Fatalf("%s: Go accepted a wire the strict read must refuse (%+v)", what, rep)
	}
}

func vtCellsDupBody(t *testing.T, wire []byte) []byte {
	t.Helper()
	if len(wire) < 9 {
		t.Fatal("wire too short to duplicate")
	}
	count := binary.LittleEndian.Uint64(wire[len(wire)-8:])
	table := len(wire) - int(count)*8 - 8
	if table < 2 || table > len(wire) || wire[table-1] != 0 {
		t.Fatalf("wire body does not end on its zero reference (table=%d len=%d)", table, len(wire))
	}
	fields := append([]byte(nil), wire[1:table-1]...)
	if len(fields) == 0 {
		t.Fatal("n=5 produced an empty body")
	}
	out := append([]byte{wire[0]}, fields...)
	out = append(out, fields...)
	out = append(out, 0)
	out = append(out, wire[table:]...)
	return out
}

func vtCellsFlipKind(t *testing.T, wire []byte) []byte {
	t.Helper()
	if len(wire) < 4 || wire[0] != 1 {
		t.Fatalf("not a form-1 wire: %x", wire)
	}
	i := 1
	for i < len(wire) && wire[i]&0x80 != 0 {
		i++
	}
	i++ // the kind byte sits after the field reference
	if i >= len(wire) {
		t.Fatal("no kind byte")
	}
	out := append([]byte(nil), wire...)
	out[i] = 1 // bool, where the writer placed an int32
	return out
}

func vtCellsFieldI(inst *tabletext.Instance, name string) int64 {
	if inst == nil {
		return -1
	}
	for i := range inst.Fields {
		if inst.Fields[i].Def.Name == name {
			return inst.Fields[i].Cell.I
		}
	}
	return -1
}

func vtCellsDump(inst *tabletext.Instance) string {
	if inst == nil {
		return "nil"
	}
	var b strings.Builder
	for i := range inst.Fields {
		f := &inst.Fields[i]
		fmt.Fprintf(&b, "%s I=%d U=%d B=%v C=%d; ", f.Def.Name, f.Cell.I, f.Cell.U, f.Cell.B, f.Count)
	}
	return b.String()
}

func vtCellsProbe(w vtCellsWireSet) string {
	var framing strings.Builder
	for _, v := range w.framing {
		flag := "false"
		if v.refused {
			flag = "true"
		}
		fmt.Fprintf(&framing, "    Agree(%q, %q, %s);\n", v.name, v.hex, flag)
	}
	blobFlag := "false"
	if w.blobShared {
		blobFlag = "true"
	}
	return fmt.Sprintf(vtCellsProbeSrc, w.dup, w.trunc, w.kind, w.over, w.unknown, w.wide, w.rename, w.blob, blobFlag, framing.String())
}

const vtCellsProbeSrc = `using System;
using System.Text;
using Probe;

public static class VtCellsProgram
{
    static int bad;

    static void Row(string cell, bool ok, string why)
    {
        if (ok) { Console.WriteLine("ROW " + cell + ": ok"); return; }
        bad++;
        Console.WriteLine("ROW " + cell + ": " + why);
    }

    static bool TextIs(byte[] buf, int n, string s)
    {
        byte[] want = Encoding.UTF8.GetBytes(s);
        if (buf == null || n != want.Length) { return false; }
        for (int i = 0; i < n; i++) { if (buf[i] != want[i]) { return false; } }
        return true;
    }

    static bool TitleIs(Root v, string s)
    {
        if (v.TitleLength != s.Length) { return false; }
        for (int i = 0; i < s.Length; i++) { if (v.Title[i] != (byte)s[i]) { return false; } }
        return true;
    }

    static bool Defaults(Root v)
    {
        return v.N == 0 && v.Flag && v.Mood == Mood.Angry && v.Old == 3 && v.Head == null
            && v.Blob == null && v.Note == null && v.Bag == null && v.Wide32 == 0
            && v.Wide == default && v.Flux == default && v.Score == 0
            && v.LabelLength == 0 && v.TitleLength == 0
            && v.NumsCount == 0 && v.SpotsCount == 0 && v.LeavesCount == 0
            && v.ArmsCount == 0 && v.EntriesCount == 0 && v.LeastCount == 0
            && v.Arm != null && v.Arm.Type == ArmType.None;
    }

    static bool SampleMatches(Root v)
    {
        if (v.N != 5 || v.Flag || v.Mood != Mood.Calm || v.Old != 9) { return false; }
        if (!TextIs(v.Label, v.LabelLength, "hi")) { return false; }
        if (v.Wide != new System.UInt128(0, 99) || v.Flux != (System.Int128)(-7) || v.Score != 65536 || v.Wide32 != 6) { return false; }
        if (v.Blob == null || v.Blob.Data == null || v.Blob.Data.Length != 3 || v.Blob.Data[0] != 1 || v.Blob.Data[1] != 2 || v.Blob.Data[2] != 3) { return false; }
        if (v.Note == null || !TextIs(v.Note.Data, v.Note.Data.Length, "yo")) { return false; }
        if (!TitleIs(v, "ab")) { return false; }
        if (v.SpotsCount != 1 || v.Spots[0] == null || v.Spots[0].X != 4 || v.Spots[0].Y != 5) { return false; }
        if (v.LeavesCount != 1 || v.Leaves[0] == null || v.Leaves[0].Id != 1) { return false; }
        if (v.Head == null || v.Head.Id != 7 || !TextIs(v.Head.Name, v.Head.NameLength, "leaf")) { return false; }
        if (v.NumsCount != 2 || v.Nums[0] != 9 || v.Nums[1] != 8) { return false; }
        if (v.LeastCount != 2 || v.Least[0] != 1 || v.Least[1] != 2) { return false; }
        if (v.Arm == null || v.Arm.Type != ArmType.Spot || v.Arm.Spot == null || v.Arm.Spot.X != 3 || v.Arm.Spot.Y != 4) { return false; }
        if (v.ArmsCount != 2 || v.Arms[0] == null || v.Arms[0].Type != ArmType.N || v.Arms[0].N != 6) { return false; }
        if (v.Arms[1] == null || v.Arms[1].Type != ArmType.Text || !TextIs(v.Arms[1].Text, v.Arms[1].TextLength, "z")) { return false; }
        if (v.EntriesCount != 1 || v.Entries[0] == null || v.Entries[0].Key != 1 || v.Entries[0].Value != 2) { return false; }
        if (v.Bag == null || v.Bag.N != 4) { return false; }
        return true;
    }

    static Root Sample()
    {
        Root r = new Root();
        r.N = 5;
        byte[] hi = Encoding.UTF8.GetBytes("hi");
        hi.CopyTo(r.Label, 0);
        r.LabelLength = hi.Length;
        r.Mood = Mood.Calm;
        r.Flag = false;
        r.Wide = new System.UInt128(0, 99);
        r.Flux = (System.Int128)(-7);
        r.Score = 65536;
        r.Wide32 = 6;
        r.Blob = new TableBlob(new byte[] { 1, 2, 3 });
        r.Note = new TableBlob(Encoding.UTF8.GetBytes("yo"));
        r.Title[0] = (byte)'a';
        r.Title[1] = (byte)'b';
        r.TitleLength = 2;
        r.Spots[0].X = 4;
        r.Spots[0].Y = 5;
        r.SpotsCount = 1;
        Leaf leaf = new Leaf();
        leaf.Id = 7;
        byte[] name = Encoding.UTF8.GetBytes("leaf");
        name.CopyTo(leaf.Name, 0);
        leaf.NameLength = name.Length;
        r.Head = leaf;
        Leaf other = new Leaf();
        other.Id = 1;
        r.Leaves = new Leaf[] { other };
        r.LeavesCount = 1;
        r.Nums = new uint[] { 9, 8 };
        r.NumsCount = 2;
        r.Least[0] = 1;
        r.Least[1] = 2;
        r.LeastCount = 2;
        r.Arm.Type = ArmType.Spot;
        r.Arm.Spot.X = 3;
        r.Arm.Spot.Y = 4;
        Arm a0 = new Arm();
        a0.Type = ArmType.N;
        a0.N = 6;
        Arm a1 = new Arm();
        a1.Type = ArmType.Text;
        byte[] z = Encoding.UTF8.GetBytes("z");
        z.CopyTo(a1.Text, 0);
        a1.TextLength = 1;
        r.Arms = new Arm[] { a0, a1 };
        r.ArmsCount = 2;
        RootEntriesEntry e = new RootEntriesEntry();
        e.Key = 1;
        e.Value = 2;
        r.Entries = new RootEntriesEntry[] { e };
        r.EntriesCount = 1;
        r.Old = 9;
        r.Bag = new Bag();
        r.Bag.N = 4;
        return r;
    }

    static Root Poison()
    {
        Root v = new Root();
        v.N = 99;
        v.Flag = false;
        v.Old = 0;
        v.Mood = Mood.Calm;
        return v;
    }

    static void RoundTrip()
    {
        Root src = Sample();
        long n = Schema.RootMeasure(src);
        if (n < 0)
        {
            Row("vt/variable-class/cs", false, "measure refused the sample");
            return;
        }
        byte[] wire = new byte[n];
        if (Schema.RootSave(src, wire) != n)
        {
            Row("vt/variable-class/cs", false, "save refused the sample");
            return;
        }
        Root dst = Poison();
        TableReport report = new TableReport();
        bool ok = Schema.RootLoad(dst, wire, report);
        if (!ok)
        {
            Row("vt/variable-class/cs", false, "clean wire refused");
            MarkRound("clean wire refused");
            return;
        }
        if (!SampleMatches(dst))
        {
            if (Defaults(dst))
            {
                Row("vt/variable-class/cs", false, "clean wire did not read the whole value");
            }
            else
            {
                Row("vt/variable-class/cs", false, "mismatch N=" + dst.N + " flag=" + dst.Flag + " mood=" + dst.Mood + " old=" + dst.Old + " wide32=" + dst.Wide32 + " score=" + dst.Score + " spots=" + dst.SpotsCount + " least=" + dst.LeastCount + " nums=" + dst.NumsCount + " arms=" + dst.ArmsCount + " entries=" + dst.EntriesCount + " head=" + (dst.Head == null ? "null" : dst.Head.Id.ToString()) + " bag=" + (dst.Bag == null ? "null" : dst.Bag.N.ToString()) + " label=" + dst.LabelLength + " title=" + dst.TitleLength);
            }
            MarkRound("clean wire was not whole");
            return;
        }
        Row("vt/variable-class/cs", true, "");
        Row("vt/wide-scalars/cs", true, "");
        Row("vt/union-table-arms/cs", dst.Arm.Type == ArmType.Spot && dst.Arm.Spot.X == 3, "table arm");
        Row("vt/pointer-arrays/cs", dst.LeavesCount == 1 && dst.Leaves[0].Id == 1 && dst.Head.Id == 7, "pointers");
        Row("vt/union-arrays/cs", dst.ArmsCount == 2 && dst.Arms[0].N == 6 && dst.Arms[1].TextLength == 1, "union array");
        Row("vt/blobs/cs", dst.Blob.Data.Length == 3 && TextIs(dst.Note.Data, dst.Note.Data.Length, "yo"), "blob roundtrip");
        Row("vt/maps/cs", dst.EntriesCount == 1 && dst.Entries[0].Key == 1 && dst.Entries[0].Value == 2, "map roundtrip");
        Row("vt/union-arms-any/cs", true, "");
        Row("vt/wstring/cs", TitleIs(dst, "ab"), "wstring");
        Row("vt/unbounded-arrays/cs", dst.NumsCount == 2 && dst.Nums[0] == 9, "nums");
        Row("vt/fixed-class/cs", dst.SpotsCount == 1 && dst.LeastCount == 2, "counted");
    }

    static void MarkRound(string why)
    {
        Row("vt/wide-scalars/cs", false, why);
        Row("vt/union-table-arms/cs", false, why);
        Row("vt/pointer-arrays/cs", false, why);
        Row("vt/union-arrays/cs", false, why);
        Row("vt/blobs/cs", false, why);
        Row("vt/maps/cs", false, why);
        Row("vt/union-arms-any/cs", false, why);
        Row("vt/wstring/cs", false, why);
        Row("vt/unbounded-arrays/cs", false, why);
        Row("vt/fixed-class/cs", false, why);
    }

    static void JsonRows()
    {
        Root whole = Poison();
        TableReport wr = new TableReport();
        bool wok = Schema.RootFromJson(whole, Encoding.UTF8.GetBytes("{\"n\":5,\"label\":\"hi\"}"), wr);
        Row("vt/text-form/cs", wok && whole.N == 5 && TextIs(whole.Label, whole.LabelLength, "hi") && whole.Flag && whole.Mood == Mood.Angry, wok ? "json did not read the whole value" : "json refused a whole value");

        Root dup = Poison();
        TableReport dr = new TableReport();
        bool dok = Schema.RootFromJson(dup, Encoding.UTF8.GetBytes("{\"n\":1,\"n\":2}"), dr);
        Row("vt/text-form/cs", !dok && Defaults(dup), dok ? "duplicate key accepted" : "duplicate key left a value");

        Root shape = Poison();
        TableReport sr = new TableReport();
        bool sok = Schema.RootFromJson(shape, Encoding.UTF8.GetBytes("{\"head\":{\"id\":7,\"name\":\"leaf\"},\"nums\":[9,8]}"), sr);
        Row("vt/text-form-variable/cs", sok && shape.Head != null && shape.Head.Id == 7 && shape.NumsCount == 2 && shape.Nums[0] == 9, sok ? "variable text was not the whole value" : "variable text refused");

        Root map = Poison();
        TableReport mr = new TableReport();
        bool mok = Schema.RootFromJson(map, Encoding.UTF8.GetBytes("{\"entries\":{\"1\":1,\"1\":2}}"), mr);
        Row("vt/maps/cs", !mok && Defaults(map), mok ? "repeated map key accepted" : "repeated map key left a value");

        Root bare = Poison();
        TableReport br = new TableReport();
        bool bok = Schema.RootFromJson(bare, Encoding.UTF8.GetBytes("{}"), br);
        Row("vt/defaults-and-table-rename/cs", bok && bare.Flag && bare.Mood == Mood.Angry && bare.Old == 3 && bare.N == 0, bok ? "defaults did not land" : "empty text refused");

        Root unk = Poison();
        TableReport ur = new TableReport();
        bool uok = Schema.RootFromJson(unk, Encoding.UTF8.GetBytes("{\"n\":5,\"nope\":1}"), ur);
        Row("vt/retain-unknown/cs", uok && unk.N == 5 && ur.Unknown > 0 && unk.Flag, uok ? "unknown key count=" + ur.Unknown : "unknown key refused the known value");
    }

    static void WireRows()
    {
        Refuse("vt/id-table-wire/cs", "repeated field id", "%s");
        Refuse("vt/fixed-class/cs", "truncated", "%s");
        Refuse("vt/widening/cs", "kind mismatch", "%s");
        Refuse("vt/widening/cs", "out of range", "%s");
        Root unk = Poison();
        TableReport ur = new TableReport();
        byte[] unknown = Convert.FromHexString("%s");
        bool uok = Schema.RootLoad(unk, unknown, ur);
        Row("vt/retain-unknown/cs", uok && unk.N == 5 && ur.Unknown > 0 && DefaultsUnknownKept(unk), uok ? "unknown wire N=" + unk.N + " unknown=" + ur.Unknown : "unknown wire refused");
        Root wide = Poison();
        TableReport wr = new TableReport();
        byte[] widened = Convert.FromHexString("%s");
        bool wok = Schema.RootLoad(wide, widened, wr);
        Row("vt/widening/cs", wok && wide.Wide32 == 6 && wr.Widened > 0, wok ? "wide32=" + wide.Wide32 + " widened=" + wr.Widened : "widen refused");
        Root ren = Poison();
        TableReport rr = new TableReport();
        byte[] renamed = Convert.FromHexString("%s");
        bool rok = Schema.RootLoad(ren, renamed, rr);
        bool renOk = rok && ren.Old == 9 && ren.Mood == Mood.Angry && ren.Bag != null && ren.Bag.N == 4 && ren.Arm.Type == ArmType.N && ren.Arm.N == 6;
        Row("vt/renames/cs", renOk, rok ? "old=" + ren.Old + " mood=" + ren.Mood + " bag=" + (ren.Bag == null ? "null" : ren.Bag.N.ToString()) + " arm=" + ren.Arm.Type : "rename wire refused");
        Row("vt/defaults-and-table-rename/cs", renOk, "table was= did not round-trip the bag node");
    }

    static bool DefaultsUnknownKept(Root v)
    {
        return v.Flag && v.Mood == Mood.Angry && v.Old == 3 && v.Head == null;
    }

    static void Refuse(string cell, string why, string hex)
    {
        Root v = Poison();
        TableReport r = new TableReport();
        bool ok = Schema.RootLoad(v, Convert.FromHexString(hex), r);
        Row(cell, !ok && Defaults(v), ok ? why + " accepted" : why + " left a value N=" + v.N + " flag=" + v.Flag + " reason=" + r.Reason);
    }

    static void Agree(string name, string hex, bool goRefused)
    {
        Root v = Poison();
        TableReport r = new TableReport();
        bool ok = Schema.RootLoad(v, Convert.FromHexString(hex), r);
        if (goRefused)
        {
            Row("vt/fixed-class/cs", !ok && Defaults(v), name + (ok ? " accepted" : " left a value"));
        }
        else
        {
            Row("vt/fixed-class/cs", ok, name + " Go accepted this frame and C# refused it");
        }
    }

    static void BlobShared(string hex, bool wantShared)
    {
        Root v = Poison();
        TableReport r = new TableReport();
        bool ok = Schema.RootLoad(v, Convert.FromHexString(hex), r);
        if (wantShared)
        {
            Row("vt/blobs/cs", !ok && Defaults(v) && r.Reason == "blob_shared", "blob_shared ok=" + ok + " reason=" + r.Reason);
        }
        else
        {
            Row("vt/blobs/cs", !ok && Defaults(v), "shared-blob wire was not a refusal reason=" + r.Reason);
        }
    }

    static unsafe void CookRow()
    {
        Root only = new Root();
        only.N = 5;
        long n = Schema.RootCookMeasure(only);
        if (n <= 0)
        {
            Row("vt/cook-write/cs", false, "cook measure " + n);
            return;
        }
        byte[] buf = new byte[n];
        if (!Schema.RootCook(only, buf))
        {
            Row("vt/cook-write/cs", false, "cook returned false");
            return;
        }
        byte[] tiny = new byte[n > 0 ? n - 1 : 0];
        Array.Fill(tiny, (byte)0xa5);
        bool shortWrote = Schema.RootCook(only, tiny);
        bool intact = true;
        for (int i = 0; i < tiny.Length; i++) { if (tiny[i] != 0xa5) { intact = false; } }
        // Open refuses a base the header's alignment does not divide. A heap
        // byte[] is not aligned to 64, which is the cook alignment ceiling.
        bool opened = false;
        int got = -1;
        TableRefuseReason why = TableRefuseReason.ok;
        int span = (int)n;
        byte[] raw = new byte[span + 64];
        fixed (byte* pin = raw)
        {
            ulong addr = (ulong)pin;
            int off = (int)(((addr + 63UL) & ~63UL) - addr);
            for (int i = 0; i < span; i++) { pin[off + i] = buf[i]; }
            opened = RootCook.Open(out RootCook cook, (IntPtr)(pin + off), span, out why);
            if (opened) { got = cook.Root.N; }
        }
        Row("vt/cook-write/cs", opened && got == 5 && !shortWrote && intact, "open=" + opened + " N=" + got + " why=" + why + " short=" + shortWrote + " intact=" + intact);
    }

    public static unsafe void Main()
    {
        try
        {
            RoundTrip();
            JsonRows();
            WireRows();
            BlobShared("%s", %s);
%s            CookRow();
            if (bad == 0) { Console.WriteLine("VT_CELLS_OK"); }
            else { Environment.Exit(1); }
        }
        catch (Exception ex)
        {
            Console.WriteLine("EX " + ex);
            Environment.Exit(2);
        }
    }
}

namespace Probe
{
    public enum Mood : byte
    {
        None = 0,
        Calm = 1,
        Angry = 2
    }

    public static partial class Schema
    {
        public const Mood MoodMax = (Mood)2;

        public static string EnumNameMood(Mood value)
        {
            switch (value)
            {
                case Mood.None:
                    return "None";
                case Mood.Calm:
                    return "Calm";
                case Mood.Angry:
                    return "Angry";
                default:
                    return "???";
            }
        }
    }
}
`

func vtCellsCompileRun(t *testing.T, files map[string][]byte, probe string) (string, error) {
	t.Helper()
	dir := t.TempDir()
	var sources []string
	for name, data := range files {
		if !strings.HasSuffix(name, ".cs") {
			continue
		}
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, p)
	}
	main := filepath.Join(dir, "ProbeMain.cs")
	if err := os.WriteFile(main, []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	sources = append(sources, main)
	csc, refs, runtime := csDotnetToolchain(t)
	app := filepath.Join(dir, "probe.dll")
	args := []string{"exec", csc, "-nologo", "-noconfig", "-target:exe", "-unsafe", "-nowarn:CS8500", "-langversion:latest", "-out:" + app}
	args = append(args, sources...)
	for _, r := range refs {
		args = append(args, "-r:"+r)
	}
	ctx, cancel := slowtest.ProbeContext(t)
	defer cancel()
	cmd := exec.CommandContext(ctx, csDotnet(), args...)
	cmd.Env = csDotnetEnv(dir, dir)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return string(out), fmt.Errorf("csc: %w", err)
	}
	rc := fmt.Sprintf("{\"runtimeOptions\":{\"tfm\":\"net10.0\",\"framework\":{\"name\":\"Microsoft.NETCore.App\",\"version\":%q}}}\n", runtime)
	if err := os.WriteFile(filepath.Join(dir, "probe.runtimeconfig.json"), []byte(rc), 0o600); err != nil {
		t.Fatal(err)
	}
	run := exec.CommandContext(ctx, csDotnet(), "exec", app)
	run.Env = csDotnetEnv(dir, dir)
	run.Dir = dir
	out, err := run.CombinedOutput()
	if err != nil {
		return string(out), err
	}
	return string(out), nil
}
