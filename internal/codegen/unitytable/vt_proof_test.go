package unitytable_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func mustRunUnityVTProbe(t *testing.T, files map[string][]byte, probeCS string) string {
	t.Helper()
	out, code, _, err := runUnityProbe(t, files, probeCS, nil)
	if err != nil {
		t.Fatalf("probe build/exec error: %v\n%s", err, out)
	}
	if code != 0 {
		t.Fatalf("probe exited %d:\n%s", code, out)
	}
	return out
}

func mustFailUnityVTProbe(t *testing.T, files map[string][]byte, mutFile, oldStr, newStr, probeCS, wantSubstr string) {
	t.Helper()
	sabotaged := make(map[string][]byte, len(files))
	for k, v := range files {
		cp := make([]byte, len(v))
		copy(cp, v)
		sabotaged[k] = cp
	}
	orig := string(sabotaged[mutFile])
	if !strings.Contains(orig, oldStr) {
		t.Fatalf("negative control target %q not found in %s", oldStr, mutFile)
	}
	sabotaged[mutFile] = []byte(strings.Replace(orig, oldStr, newStr, 1))
	out, code, _, err := runUnityProbe(t, sabotaged, probeCS, nil)
	if err != nil {
		t.Fatalf("negative control failed to compile/exec cleanly: %v\n%s", err, out)
	}
	if code == 0 {
		t.Fatalf("NEGATIVE CONTROL FAILED: sabotaging %q -> %q in %s still exited 0:\n%s", oldStr, newStr, mutFile, out)
	}
	if wantSubstr != "" && !strings.Contains(out, wantSubstr) {
		t.Fatalf("NEGATIVE CONTROL FAILED for wrong reason (want %q, exit %d):\n%s", wantSubstr, code, out)
	}
}

// TestUnityVTCoreAndVariableGraph proves variable-class & fixed-class storage,
// Ref<T> shared fixed-table pointers, DAG + cyclic pointer graphs, *string /
// *bytes blob identity, TableKeyed<T, E> 1-indexing, unbounded lists & text
// lists (with max_bytes cap), 128-bit & fixed-point scalars, timestamps,
// unions, optionals, defaults, was= renames, | id table wire framing, and
// integer widening — with a planted negative control.
func TestUnityVTCoreAndVariableGraph(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dotnet Roslyn csc Unity dialect variable-table proof")

	const schemaV1 = `package vtgraph

enum Slot { Primary, Secondary }

flags Perks { Fast, Armored }

fixed table Badge
{
    code  uint32 = 7
    title string(16) = "rookie"
}

union Action
{
    idle
    boost  int32
    badge  Badge
    target *Node
}

table Node | id, was = "OldNode"
{
    code_id  int8 | was = "old_id"
    u        uint8
    next     *Node
    peers    []*Node
    shared   *Badge
    pinned   [2]*Badge
    counted  [..3]*Badge
    note     *string
    payload  *bytes
    tags     []string(12)
    scores   [Slot]int32
    opt_tag  ?int32
    opt_arr  ?[..2]int16
    acts     [..3]Action
    wide_i   int128 = -42 | min = -1000000, max = 1000000
    wide_u   uint128 = 99
    fx_s     fixed(100, 28) | min = -1000, max = 1000
    fx_u     ufixed(100, 28) | min = 0, max = 1000
    born_at  timestamp
    perks    Perks = { Fast }
    lookup   map[string(12)]int32
    by_num   map[int16]*Badge
}
`
	const schemaV2 = `package vtgraph

enum Slot { Primary, Secondary }

flags Perks { Fast, Armored }

fixed table Badge
{
    code  uint32 = 7
    title string(16) = "rookie"
}

union Action
{
    idle
    boost  int32
    badge  Badge
    target *Node
}

table Node | id, was = "OldNode"
{
    code_id  int32 | was = "old_id"
    u        uint64
    next     *Node
    peers    []*Node
    shared   *Badge
    pinned   [2]*Badge
    counted  [..3]*Badge
    note     *string
    payload  *bytes
    tags     []string(12)
    scores   [Slot]int32
    opt_tag  ?int32
    opt_arr  ?[..2]int16
    acts     [..3]Action
    wide_i   int128 = -42 | min = -1000000, max = 1000000
    wide_u   uint128 = 99
    fx_s     fixed(100, 28) | min = -1000, max = 1000
    fx_u     ufixed(100, 28) | min = 0, max = 1000
    born_at  timestamp
    perks    Perks = { Fast }
    lookup   map[string(12)]int32
    by_num   map[int16]*Badge
}
`
	u1 := fixedproof.ParseCheckUnit(t, schemaV1)
	u2 := fixedproof.ParseCheckUnit(t, schemaV2)
	files1, err := unityGenerateFiles(u1, nil)
	if err != nil {
		t.Fatalf("generate v1: %v", err)
	}
	files2, err := unityGenerateFiles(u2, nil)
	if err != nil {
		t.Fatalf("generate v2: %v", err)
	}

	probe1 := fmt.Sprintf(`using System;
using System.IO;
using System.Text;
using Serialize;
using Vtgraph;

class Program
{
    static int Main()
    {
        ProbeCommon.InstallAssertTrap();
        ulong bv = Schema.BuildVersion;
        if (bv != 0x%016xUL)
        {
            Console.Error.WriteLine("FAIL: build_version mismatch");
            return 1;
        }

        var root = Schema.NewNode();
        if (root.WideI != new Int128Value(ulong.MaxValue, unchecked((ulong)-42L)) || root.WideU != new UInt128Value(0UL, 99UL) || root.Perks != Perks.Fast)
        {
            Console.Error.WriteLine("FAIL: defaults mismatch");
            return 1;
        }

        // TableKeyed 1-indexing and bounds refusal on None (0) and past Max (3).
        root.Scores[(int)Slot.Primary] = 111;
        root.Scores[(int)Slot.Secondary] = 222;
        bool refusedZero = false, refusedHigh = false;
        try { var _ = root.Scores[0]; } catch (ArgumentOutOfRangeException) { refusedZero = true; }
        try { root.Scores[3] = 9; } catch (ArgumentOutOfRangeException) { refusedHigh = true; }
        if (!refusedZero || !refusedHigh)
        {
            Console.Error.WriteLine("FAIL: TableKeyed bounds not enforced");
            return 1;
        }

        // Shared fixed-table Ref<Badge>, *string / *bytes blobs, cycle & blob_shared refusal, and DAG *Node graph!
        var sharedBadge = new Schema.Ref<Badge>(Schema.NewBadge());
        sharedBadge.Value.Code = 9001;
        ProbeCommon.SetUtf8(sharedBadge.Value.Title.AsSpan(), out sharedBadge.Value.TitleLength, "ace");

        var sharedNote = new Schema.TableBlob(Encoding.UTF8.GetBytes("shared-memo"));
        var sharedBytes = new Schema.TableBlob(new byte[] { 0xde, 0xad, 0xbe, 0xef });

        var child = Schema.NewNode();
        child.CodeId = -7;
        child.U = 200;
        child.Next = root; // cycle: root -> child -> root (must be refused!)
        child.Shared = sharedBadge;
        child.Pinned[0] = sharedBadge;
        child.Pinned[1] = sharedBadge;
        child.Note = sharedNote;
        child.Payload = sharedBytes;

        root.CodeId = -12;
        root.U = 250;
        root.Next = child;
        root.Peers = new Node[] { child, child };
        root.PeersCount = 2;
        root.Shared = sharedBadge;
        root.Pinned[0] = sharedBadge;
        root.Pinned[1] = sharedBadge;
        root.Counted[0] = sharedBadge;
        root.CountedCount = 1;
        root.Note = sharedNote;
        root.Payload = sharedBytes;

        // Unbounded text list with max_bytes = 16
        NodeTagsText t0 = default;
        ProbeCommon.SetUtf8(t0.Value.AsSpan(), out t0.ValueLength, "alpha");
        NodeTagsText t1 = default;
        ProbeCommon.SetUtf8(t1.Value.AsSpan(), out t1.ValueLength, "beta");
        root.Tags = new NodeTagsText[] { t0, t1 };
        root.TagsCount = 2;

        // Optional scalar & optional counted array (present-empty!)
        root.OptTag = 77;
        root.OptTagPresent = true;
        root.OptArrCount = 0;
        root.OptArrPresent = true;

        // Union array arms (void, scalar, *Node pointer)
        root.Acts[0].Type = ActionType.Idle;
        root.Acts[1].Type = ActionType.Boost;
        root.Acts[1].Boost = 42;
        root.Acts[2].Type = ActionType.Target;
        root.Acts[2].Target = child;
        root.ActsCount = 3;

        // 128-bit & fixed-point & timestamp
        root.WideI = new Int128Value(ulong.MaxValue, unchecked((ulong)-999999L));
        root.WideU = new UInt128Value(0x1234UL, 0x56789abcdef0UL);
        root.FxS = new Int128Value(ulong.MaxValue, unchecked((ulong)-256L));
        root.FxU = new UInt128Value(0UL, 512UL);
        root.BornAt = 1700000000123456789L;

        // Maps (sorted string keys and signed int16 keys sharing sharedBadge)
        var e1 = new NodeLookupEntry();
        ProbeCommon.SetUtf8(e1.Key, out e1.KeyLength, "apple");
        e1.Value = 10;
        var e2 = new NodeLookupEntry();
        ProbeCommon.SetUtf8(e2.Key, out e2.KeyLength, "zeta");
        e2.Value = 20;
        root.Lookup = new NodeLookupEntry[] { e1, e2 };
        root.LookupCount = 2;

        var n1 = new NodeByNumEntry { Key = -5, Value = sharedBadge };
        var n2 = new NodeByNumEntry { Key = 3, Value = sharedBadge };
        root.ByNum = new NodeByNumEntry[] { n1, n2 };
        root.ByNumCount = 2;

        // 1) Cyclic graph (root -> child -> root) is refused by Measure, ToJsonMeasure, and CookMeasure!
        if (Schema.NodeMeasure(root) >= 0 || Schema.NodeToJsonMeasure(root) >= 0 || Schema.NodeCookMeasure(root) >= 0)
        {
            Console.Error.WriteLine("FAIL: cyclic graph must be refused");
            return 1;
        }
        child.Next = null;

        // 2) Shared TableBlob across two slots is refused by Form-1 NodeMeasure (blob_shared), while Cook packs it!
        if (Schema.NodeMeasure(root) >= 0 || Schema.NodeCookMeasure(root) <= 0)
        {
            Console.Error.WriteLine("FAIL: blob_shared refusal vs Cook packing mismatch");
            return 1;
        }
        child.Note = new Schema.TableBlob(Encoding.UTF8.GetBytes("child-memo"));
        child.Payload = new Schema.TableBlob(new byte[] { 0xca, 0xfe });

        long wireLen = Schema.NodeMeasure(root);
        if (wireLen <= 0)
        {
            Console.Error.WriteLine("FAIL: NodeMeasure <= 0 on valid DAG graph");
            return 1;
        }
        byte[] wire = new byte[wireLen];
        if (Schema.NodeSave(root, wire) != wireLen)
        {
            Console.Error.WriteLine("FAIL: NodeSave != NodeMeasure");
            return 1;
        }
        if (Schema.NodeLoadMeasure(wire) < 0)
        {
            Console.Error.WriteLine("FAIL: NodeLoadMeasure < 0");
            return 1;
        }

        Node loaded = null;
        Schema.TableReport rep = default;
        if (!Schema.NodeLoad(ref loaded, wire, ref rep) || rep.Refused || rep.Damaged)
        {
            Console.Error.WriteLine("FAIL: NodeLoad failed: " + rep.ReasonText);
            return 1;
        }
        if (!ReferenceEquals(loaded.Next, loaded.Peers[0]) || !ReferenceEquals(loaded.Next, loaded.Peers[1]) || !ReferenceEquals(loaded.Next, loaded.Acts[2].Target))
        {
            Console.Error.WriteLine("FAIL: DAG *Node pointer sharing lost");
            return 1;
        }
        if (!ReferenceEquals(loaded.Shared, loaded.Next.Shared) || !ReferenceEquals(loaded.Shared, loaded.Pinned[0]) || !ReferenceEquals(loaded.Shared, loaded.ByNum[0].Value))
        {
            Console.Error.WriteLine("FAIL: Ref<Badge> pointer sharing lost");
            return 1;
        }
        if (Encoding.UTF8.GetString(loaded.Note.Data) != "shared-memo" || Encoding.UTF8.GetString(loaded.Next.Note.Data) != "child-memo" || loaded.Payload.Data.Length != 4)
        {
            Console.Error.WriteLine("FAIL: string/bytes blob payload mismatch");
            return 1;
        }
        if (!loaded.OptTagPresent || loaded.OptTag != 77 || !loaded.OptArrPresent || loaded.OptArrCount != 0)
        {
            Console.Error.WriteLine("FAIL: optional fields lost");
            return 1;
        }
        if (loaded.BornAt != 1700000000123456789L || loaded.Scores[(int)Slot.Secondary] != 222 || loaded.TagsCount != 2)
        {
            Console.Error.WriteLine("FAIL: timestamp, keyed array, or text list mismatch");
            return 1;
        }

        File.WriteAllBytes("out.bin", wire);
        return 0;
    }
}
`, ir.BuildVersion(u1))

	out1, code, wire1, err := runUnityProbe(t, files1, probe1, nil)
	if err != nil || code != 0 {
		t.Fatalf("v1 probe failed (code=%d, err=%v):\n%s", code, err, out1)
	}

	// Verify widening (int8 -> int32, uint8 -> uint64) in v2 reading v1's wire!
	const probe2 = `using System;
using System.IO;
using Vtgraph;

class Program
{
    static int Main()
    {
        byte[] wire = File.ReadAllBytes("in.bin");
        Node loaded = null;
        Schema.TableReport rep = default;
        if (!Schema.NodeLoad(ref loaded, wire, ref rep) || rep.Refused || rep.Damaged)
        {
            Console.Error.WriteLine("FAIL: v2 NodeLoad failed: " + rep.ReasonText);
            return 1;
        }
        if (rep.Widened < 2 || loaded.CodeId != -12 || loaded.U != 250UL || loaded.Next.CodeId != -7 || loaded.Next.U != 200UL)
        {
            Console.Error.WriteLine($"FAIL: widening mismatch: Widened={rep.Widened} CodeId={loaded.CodeId} U={loaded.U}");
            return 1;
        }
        return 0;
    }
}
`
	out2, code2, _, err := runUnityProbe(t, files2, probe2, map[string][]byte{"in.bin": wire1})
	if err != nil || code2 != 0 {
		t.Fatalf("v2 widening probe failed (code=%d, err=%v):\n%s", code2, err, out2)
	}

	// Negative control: sabotaging BuildVersion constant turns probe1 RED by name.
	oldBV := fmt.Sprintf("public const ulong BuildVersion = 0x%016xUL;", ir.BuildVersion(u1))
	newBV := fmt.Sprintf("public const ulong BuildVersion = 0x%016xUL;", ir.BuildVersion(u1)^1)
	mustFailUnityVTProbe(t, files1, "Tables.Runtime.cs", oldBV, newBV, probe1, "FAIL: build_version mismatch")
}

// TestUnityVTTextFormAndJsonStrict proves strict JSON text form on fixed &
// variable roots, | json = "array" roots, RFC 3339 timestamps, 128-bit &
// fixed-point decimal strings, base64 bytes, and hostile JSON refusals — with
// a planted negative control.
func TestUnityVTTextFormAndJsonStrict(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dotnet Roslyn csc Unity dialect JSON proof")

	const schema = `package vtjson

table FloatList | json = "array"
{
    values []float32
}

fixed table Vec3
{
    x float32
    y float32
    z float32
}

table EventLog
{
    at      timestamp
    amount  fixed(100, 28) | min = -1000, max = 1000
    raw     bytes(8)
    origin  Vec3
    items   []string(16)
}
`
	u := fixedproof.ParseCheckUnit(t, schema)
	files, err := unityGenerateFiles(u, nil)
	if err != nil {
		t.Fatal(err)
	}

	const probe = `using System;
using System.Text;
using Vtjson;

class Program
{
    static int Main()
    {
        Schema.TableReport rep = default;
        FloatList fl = null;
        byte[] arrJson = Encoding.UTF8.GetBytes("[1.5,-2,3.25]");
        if (!Schema.FloatListFromJson(ref fl, arrJson, ref rep) || fl.ValuesCount != 3 || fl.Values[0] != 1.5f || fl.Values[1] != -2f || fl.Values[2] != 3.25f)
        {
            Console.Error.WriteLine("FAIL: [json = array] FromJson failed");
            return 1;
        }
        long vm = Schema.FloatListToJsonMeasure(fl);
        byte[] vout = new byte[vm];
        if (Schema.FloatListToJson(fl, vout) != vm || !Encoding.UTF8.GetString(vout).StartsWith("[", StringComparison.Ordinal))
        {
            Console.Error.WriteLine("FAIL: [json = array] ToJson failed");
            return 1;
        }

        // Object syntax must be refused for json = "array" root!
        rep = default;
        if (Schema.FloatListFromJson(ref fl, Encoding.UTF8.GetBytes("{\"values\":[1]}"), ref rep) || !rep.Refused)
        {
            Console.Error.WriteLine("FAIL: object accepted for json=array root");
            return 1;
        }

        string input = "{\"at\":\"2026-11-18T12:34:56.789Z\",\"amount\":\"-12.5\",\"raw\":\"AQID\",\"origin\":{\"x\":1,\"y\":2,\"z\":3},\"items\":[\"one\",\"two\"]}";
        EventLog log = null;
        rep = default;
        if (!Schema.EventLogFromJson(ref log, Encoding.UTF8.GetBytes(input), ref rep) || rep.Refused)
        {
            Console.Error.WriteLine("FAIL: EventLogFromJson failed: " + rep.ReasonText);
            return 1;
        }
        if (log.ItemsCount != 2 || ProbeCommon.GetUtf8(log.Items[1].Value.AsSpan(), log.Items[1].ValueLength) != "two" || log.RawLength != 3 || log.Raw[2] != 3)
        {
            Console.Error.WriteLine("FAIL: EventLog fields mismatch");
            return 1;
        }
        long jm = Schema.EventLogToJsonMeasure(log);
        byte[] jbuf = new byte[jm];
        if (Schema.EventLogToJson(log, jbuf) != jm)
        {
            Console.Error.WriteLine("FAIL: EventLogToJson measure mismatch");
            return 1;
        }
        string round = Encoding.UTF8.GetString(jbuf);
        if (!round.Contains("\"2026-11-18T12:34:56.789Z\"", StringComparison.Ordinal) || !round.Contains("\"-12.5\"", StringComparison.Ordinal))
        {
            Console.Error.WriteLine("FAIL: EventLogToJson text mismatch: " + round);
            return 1;
        }

        // Duplicate key and unknown key must be refused in strict JSON!
        rep = default;
        if (Schema.EventLogFromJson(ref log, Encoding.UTF8.GetBytes("{\"raw\":\"AQID\",\"raw\":\"AQID\"}"), ref rep) || !rep.Refused)
        {
            Console.Error.WriteLine("FAIL: duplicate JSON key accepted");
            return 1;
        }
        // Unknown key is skipped and counted in Unknown; trailing garbage is refused!
        rep = default;
        if (!Schema.EventLogFromJson(ref log, Encoding.UTF8.GetBytes("{\"unknown_field\":1}"), ref rep) || rep.Refused || rep.Unknown != 1)
        {
            Console.Error.WriteLine("FAIL: unknown JSON key handling mismatch");
            return 1;
        }
        rep = default;
        if (Schema.EventLogFromJson(ref log, Encoding.UTF8.GetBytes("{\"raw\":\"AQID\"} trailing"), ref rep) || !rep.Refused)
        {
            Console.Error.WriteLine("FAIL: trailing JSON garbage accepted");
            return 1;
        }
        return 0;
    }
}
`
	mustRunUnityVTProbe(t, files, probe)
	mustFailUnityVTProbe(t, files, "probe.Tables.cs", "info.JsonArray = true;", "info.JsonArray = false;", probe, "FAIL: [json = array] FromJson failed")
}

// TestUnityVTBlockAndCook proves <Root>Block Begin/Open zero-copy row framing
// and <Root>Cook CookMeasure/Cook/Open (both Little and Big endian, binary
// search Find over integer and string map keys, shared blobs, unbounded lists,
// and non-finite float guard) — with a planted negative control.
func TestUnityVTBlockAndCook(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dotnet Roslyn csc Unity dialect Block and Cook proof")

	const schema = `package vtbc

fixed table Sample
{
    tick  uint32
    gain  float32 = 1.0
    label string(12)
}

fixed table SampleFrame
{
    stamp uint64
    rows  [..4]Sample
}

table Catalog
{
    by_code map[int16]Sample
    by_name map[string(16)]*Sample
    nums    []int32
    blob    *bytes
}
`
	u := fixedproof.ParseCheckUnit(t, schema)
	files, err := unityGenerateFiles(u, nil)
	if err != nil {
		t.Fatal(err)
	}

	const probe = `using System;
using System.Buffers.Binary;
using System.Text;
using Vtbc;

class Program
{
    static int Main()
    {
        // 1. Block Begin + Open + refusal on count overflow & corrupted magic
        byte[] blockBuf = new byte[SampleFrameBlock.BlockMaxBytes];
        var bref = new Schema.TableBlockRefusal();
        if (SampleFrameBlock.Begin(blockBuf, 0, 5, bref) != null || bref.Array != "rows" || bref.Maximum != 4)
        {
            Console.Error.WriteLine("FAIL: count overflow not refused by SampleFrameBlock.Begin");
            return 1;
        }

        SampleFrameBlock writer = SampleFrameBlock.Begin(blockBuf, 0, 2, bref);
        if (writer == null || BinaryPrimitives.ReadUInt64LittleEndian(blockBuf.AsSpan(0, 8)) != 0x4B4C42414D484353UL)
        {
            Console.Error.WriteLine("FAIL: SampleFrameBlock.Begin(2) header mismatch");
            return 1;
        }
        BinaryPrimitives.WriteUInt64LittleEndian(blockBuf.AsSpan(24, 8), 999UL);
        var rowCur = writer.RowsCursor();
        writer.RowsAt(0, rowCur);
        BinaryPrimitives.WriteUInt32LittleEndian(blockBuf.AsSpan(rowCur.At + 0, 4), 10u);
        BinaryPrimitives.WriteUInt32LittleEndian(blockBuf.AsSpan(rowCur.At + 4, 4), Schema.TableFloatToBits(2.5f));
        writer.RowsAt(1, rowCur);
        BinaryPrimitives.WriteUInt32LittleEndian(blockBuf.AsSpan(rowCur.At + 0, 4), 20u);
        BinaryPrimitives.WriteUInt32LittleEndian(blockBuf.AsSpan(rowCur.At + 4, 4), Schema.TableFloatToBits(-4.0f));

        if (!SampleFrameBlock.Open(out SampleFrameBlock reader, blockBuf, 0, writer.Length, out Schema.TableRefuseReason bReason) || reader.RowsCount != 2 || reader.Stamp != 999UL)
        {
            Console.Error.WriteLine("FAIL: SampleFrameBlock.Open failed: " + bReason);
            return 1;
        }
        var rRow = reader.RowsCursor();
        reader.RowsAt(0, rRow);
        if (rRow.Tick != 10u || rRow.Gain != 2.5f)
        {
            Console.Error.WriteLine("FAIL: SampleFrameBlock row 0 mismatch");
            return 1;
        }
        reader.RowsAt(1, rRow);
        if (rRow.Tick != 20u || rRow.Gain != -4.0f)
        {
            Console.Error.WriteLine("FAIL: SampleFrameBlock row 1 mismatch");
            return 1;
        }

        byte[] badBlock = (byte[])blockBuf.Clone();
        badBlock[0] ^= 0xff;
        if (SampleFrameBlock.Open(out _, badBlock, 0, writer.Length, out Schema.TableRefuseReason badRef) || badRef != Schema.TableRefuseReason.not_a_cook)
        {
            Console.Error.WriteLine("FAIL: corrupted block magic not refused: " + badRef);
            return 1;
        }

        // 2. Cook Little + Big endian, O(1) cursor reads, map Find binary search, non-finite float guard
        Sample s0 = Schema.NewSample();
        s0.Tick = 10;
        s0.Gain = 2.5f;
        ProbeCommon.SetUtf8(s0.Label.AsSpan(), out s0.LabelLength, "first");
        Sample s1 = Schema.NewSample();
        s1.Tick = 20;
        s1.Gain = -4.0f;
        ProbeCommon.SetUtf8(s1.Label.AsSpan(), out s1.LabelLength, "second");

        var cat = Schema.NewCatalog();
        var sharedRef = new Schema.Ref<Sample>(s1);
        cat.ByCode = new CatalogByCodeEntry[]
        {
            new CatalogByCodeEntry { Key = -10, Value = s0 },
            new CatalogByCodeEntry { Key = 42, Value = s1 },
        };
        cat.ByCodeCount = 2;

        var ne0 = new CatalogByNameEntry { Value = sharedRef };
        ProbeCommon.SetUtf8(ne0.Key, out ne0.KeyLength, "alpha");
        var ne1 = new CatalogByNameEntry { Value = sharedRef };
        ProbeCommon.SetUtf8(ne1.Key, out ne1.KeyLength, "omega");
        cat.ByName = new CatalogByNameEntry[] { ne0, ne1 };
        cat.ByNameCount = 2;

        cat.Nums = new int[] { 100, 200, 300 };
        cat.NumsCount = 3;
        cat.Blob = new Schema.TableBlob(new byte[] { 9, 8, 7 });

        long csz = Schema.CatalogCookMeasure(cat);
        if (csz <= 0)
        {
            Console.Error.WriteLine("FAIL: CatalogCookMeasure <= 0");
            return 1;
        }
        byte[] leBuf = new byte[csz];
        byte[] beBuf = new byte[csz];
        if (!Schema.CatalogCook(cat, leBuf, Schema.TableByteOrder.Little) || !Schema.CatalogCook(cat, beBuf, Schema.TableByteOrder.Big))
        {
            Console.Error.WriteLine("FAIL: CatalogCook Little/Big failed");
            return 1;
        }

        // Big-endian image on a little-endian host is identified by foreign_order (not_a_cook on swapped headerorder)
        if (CatalogCook.Open(out _, beBuf, 0, beBuf.Length, out _))
        {
            Console.Error.WriteLine("FAIL: Big-endian cook opened on little-endian host");
            return 1;
        }

        if (!CatalogCook.Open(out CatalogCook cook, leBuf, 0, leBuf.Length, out Schema.TableRefuseReason cReason))
        {
            Console.Error.WriteLine("FAIL: CatalogCook.Open failed: " + cReason);
            return 1;
        }
        CatalogRow view = cook.Cursor();
        int regEnd = cook.Region + cook.Length;
        var ce = new CatalogByCodeEntryRow(leBuf, 0, regEnd);
        var sr = new SampleRow(leBuf, 0, regEnd);
        if (view.ByCodeFind(-10, ce) == null)
        {
            Console.Error.WriteLine("FAIL: ByCodeFind(-10) null");
            return 1;
        }
        ce.Value(sr);
        if (sr.Tick != 10u || Encoding.UTF8.GetString(sr.LabelBytes) != "first")
        {
            Console.Error.WriteLine("FAIL: ByCodeFind(-10) value mismatch");
            return 1;
        }
        if (view.ByCodeFind(42, ce) == null || view.ByCodeFind(99, ce) != null)
        {
            Console.Error.WriteLine("FAIL: ByCodeFind(42/99) mismatch");
            return 1;
        }
        var ne = new CatalogByNameEntryRow(leBuf, 0, regEnd);
        if (view.ByNameFind(Encoding.UTF8.GetBytes("omega"), ne) == null || view.ByNameFind(Encoding.UTF8.GetBytes("missing"), ne) != null)
        {
            Console.Error.WriteLine("FAIL: ByNameFind mismatch");
            return 1;
        }
        if (view.NumsCount != 3 || view.NumsAt(2) != 300)
        {
            Console.Error.WriteLine("FAIL: Nums list cursor mismatch");
            return 1;
        }

        // Non-finite float planted into a cooked Sample image sets NonFiniteFloat on SampleRow!
        long sCookSz = Schema.SampleCookMeasure(ref s0);
        byte[] sCookBuf = new byte[sCookSz];
        Schema.SampleCook(ref s0, sCookBuf, Schema.TableByteOrder.Little);
        BinaryPrimitives.WriteUInt32LittleEndian(sCookBuf.AsSpan(64 + 4, 4), 0x7fc00000u); // NaN at gain offset 4
        if (!SampleCook.Open(out SampleCook sc, sCookBuf, 0, sCookBuf.Length, out _))
        {
            Console.Error.WriteLine("FAIL: SampleCook.Open failed");
            return 1;
        }
        SampleRow nanRow = sc.Cursor();
        float ignoredGain = nanRow.Gain;
        if (ignoredGain != 0.0f || nanRow.NonFiniteFloat != "Sample.gain")
        {
            Console.Error.WriteLine("FAIL: cooked NaN did not record Sample.gain: " + nanRow.NonFiniteFloat);
            return 1;
        }
        return 0;
    }
}
`
	mustRunUnityVTProbe(t, files, probe)
	mustFailUnityVTProbe(t, files, "Tables.Runtime.cs", "public const ulong TableBlockMagic = 0x4B4C42414D484353UL;", "public const ulong TableBlockMagic = 0x4B434C424D484353UL;", probe, "FAIL: SampleFrameBlock.Begin(2) header mismatch")
}

// TestUnityVTMessageAndRetainUnknown proves Form-2 message batches (including
// cross-build evolution, >256 refusal, variable-table exclusion) and Form-1
// TableRetain unknown preservation, mutation discard, and capacity loss
// accounting — with a planted negative control.
func TestUnityVTMessageAndRetainUnknown(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dotnet Roslyn csc Unity dialect Message and Retain proof")

	const schemaNew = `package vtmsg

enum Mode { Idle, Active, Hyper }

fixed table Command
{
    seq   uint32
    mode  Mode
    extra int32
}

table DocChild
{
    x     int32
    bonus string(16)
}

table Doc
{
    title   string(24)
    child   DocChild
    notes   []int32
    secret  int64
}
`
	const schemaOld = `package vtmsg

enum Mode { Idle, Active }

fixed table Command
{
    seq  uint32
    mode Mode
}

table DocChild
{
    x int32
}

table Doc
{
    title string(24)
    child DocChild
    notes []int32
}
`
	uNew := fixedproof.ParseCheckUnit(t, schemaNew)
	uOld := fixedproof.ParseCheckUnit(t, schemaOld)
	filesNew, err := unityGenerateFiles(uNew, nil)
	if err != nil {
		t.Fatal(err)
	}
	filesOld, err := unityGenerateFiles(uOld, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(filesNew["probe.Tables.cs"]), "DocLoadMessages") {
		t.Fatalf("variable table Doc must not emit LoadMessages")
	}

	const emitHexProbe = `using System;
using Vtmsg;

class Program
{
    static int Main()
    {
        Command c0 = Schema.NewCommand();
        c0.Seq = 1;
        c0.Mode = Mode.Hyper;
        c0.Extra = 999;
        Command c1 = Schema.NewCommand();
        c1.Seq = 2;
        c1.Mode = Mode.Active;
        Command[] batch = new[] { c0, c1 };
        byte[] mbuf = new byte[Schema.CommandMeasureMessages(batch)];
        Schema.CommandSaveMessages(batch, mbuf);

        var doc = Schema.NewDoc();
        ProbeCommon.SetUtf8(doc.Title, out doc.TitleLength, "kept");
        doc.Child.X = 7;
        ProbeCommon.SetUtf8(doc.Child.Bonus.AsSpan(), out doc.Child.BonusLength, "nested-unknown");
        doc.Notes = new int[] { 5 };
        doc.NotesCount = 1;
        doc.Secret = 123456789L;
        byte[] dbuf = new byte[Schema.DocMeasure(doc)];
        Schema.DocSave(doc, dbuf);

        Console.WriteLine(Convert.ToHexString(Schema.Announce()).ToLowerInvariant());
        Console.WriteLine(Convert.ToHexString(mbuf).ToLowerInvariant());
        Console.WriteLine(Convert.ToHexString(dbuf).ToLowerInvariant());
        return 0;
    }
}
`
	hexLines := strings.Split(strings.TrimSpace(mustRunUnityVTProbe(t, filesNew, emitHexProbe)), "\n")
	if len(hexLines) != 3 {
		t.Fatalf("expected 3 hex lines, got %d: %v", len(hexLines), hexLines)
	}

	probeOld := fmt.Sprintf(`using System;
using Vtmsg;

class Program
{
    static int Main()
    {
        byte[] ann = Convert.FromHexString(%q);
        byte[] msg = Convert.FromHexString(%q);
        byte[] docWire = Convert.FromHexString(%q);

        // 1. Message refusal without vocabulary & >256 batch refusal
        Schema.TableReport rep = default;
        var unannounced = new Schema.TableVocabulary();
        Span<Command> dst = new Command[4];
        if (Schema.CommandLoadMessages(dst, msg, unannounced, ref rep, out _) || rep.ReasonText != "no_vocabulary")
        {
            Console.Error.WriteLine("FAIL: unannounced vocabulary not refused");
            return 1;
        }
        Command[] huge = new Command[257];
        rep = default;
        if (Schema.CommandSaveMessages(huge, new byte[4096], ref rep) >= 0 || rep.Reason != Schema.TableReport.BatchTooLarge)
        {
            Console.Error.WriteLine("FAIL: >256 batch not refused");
            return 1;
        }

        // 2. Cross-build message load skips Extra and substitutes unknown Mode.Hyper -> Mode.None
        var vocab = new Schema.TableVocabulary();
        rep = default;
        if (!Schema.AnnounceRead(vocab, ann, ref rep))
        {
            Console.Error.WriteLine("FAIL: AnnounceRead failed");
            return 1;
        }
        rep = default;
        if (!Schema.CommandLoadMessages(dst, msg, vocab, ref rep, out int mcount) || mcount != 2 || rep.Unknown != 2)
        {
            Console.Error.WriteLine($"FAIL: CommandLoadMessages count={mcount} unknown={rep.Unknown}");
            return 1;
        }
        if (dst[0].Seq != 1 || dst[0].Mode != Mode.None || dst[1].Seq != 2 || dst[1].Mode != Mode.Active)
        {
            Console.Error.WriteLine("FAIL: CommandLoadMessages values mismatch");
            return 1;
        }

        // 3. Retain-unknown preserves both root Secret and nested DocChild.Bonus byte-identically!
        var retain = new Schema.TableRetain(4096, 64);
        Doc d = null;
        rep = default;
        if (!Schema.DocLoadRetain(ref d, docWire, retain, ref rep) || rep.Retained != 2 || rep.RetainLost != 0)
        {
            Console.Error.WriteLine($"FAIL: DocLoadRetain Retained={rep.Retained} Lost={rep.RetainLost}");
            return 1;
        }
        long rlen = Schema.DocMeasureRetain(d, retain);
        byte[] rsaved = new byte[rlen];
        rep = default;
        if (Schema.DocSaveRetain(d, retain, rsaved, ref rep) != rlen || rep.RetainLost != 0)
        {
            Console.Error.WriteLine("FAIL: DocSaveRetain failed");
            return 1;
        }
        if (!rsaved.AsSpan().SequenceEqual(docWire))
        {
            Console.Error.WriteLine("FAIL: retained round-trip not byte-identical to writer wire");
            return 1;
        }

        // Capacity-short TableRetain accounts RetainLost cleanly without failing the read!
        var tinyRetain = new Schema.TableRetain(8, 64);
        Doc d2 = null;
        rep = default;
        if (!Schema.DocLoadRetain(ref d2, docWire, tinyRetain, ref rep) || rep.RetainLost != 2 || rep.Retained != 0)
        {
            Console.Error.WriteLine($"FAIL: tiny retain loss accounting: Retained={rep.Retained} Lost={rep.RetainLost}");
            return 1;
        }
        return 0;
    }
}
`, strings.TrimSpace(hexLines[0]), strings.TrimSpace(hexLines[1]), strings.TrimSpace(hexLines[2]))

	mustRunUnityVTProbe(t, filesOld, probeOld)
	mustFailUnityVTProbe(t, filesOld, "Tables.Runtime.cs", "RetainTailSave(ref w, ref ids, value.Path);", "", probeOld, "FAIL: DocSaveRetain failed")
}

// TestUnityVTReflectionDocTagsAndUnitView proves TableTypeInfo, TableFieldInfo,
// out-of-closure packet type/union descriptors (Id == 0, Json == null),
// TableDocNone reference sharing, /// docs & | tags, and Schema.UnitView() —
// with a planted negative control.
func TestUnityVTReflectionDocTagsAndUnitView(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dotnet Roslyn csc Unity dialect Reflection and UnitView proof")

	const schema = `package vtview

const MAX_HP uint32 = 500

/// Rank doc
enum Rank | tier
{
    /// Rookie doc
    Rookie | starter
    Veteran
}

flags Perks | combat
{
    Shield
}

/// Out-of-closure packet struct
type PacketOnly | net
{
    seq uint16
}

/// Hero table doc
table Hero | entity, synced
{
    /// Name field doc
    name string(24) | indexed
    hp   uint32 = 100
    rank Rank
}
`
	u := fixedproof.ParseCheckUnit(t, schema)
	files, err := unityGenerateFiles(u, nil)
	if err != nil {
		t.Fatal(err)
	}

	probe := fmt.Sprintf(`using System;
using Vtview;

class Program
{
    static int Main()
    {
        var hero = Schema.HeroTableType;
        if (hero.Name != "Hero" || hero.Fixed || hero.Doc != "Hero table doc" || hero.NumTags != 2 || hero.Tags[0] != "entity" || hero.Tags[1] != "synced")
        {
            Console.Error.WriteLine("FAIL: HeroTableType metadata mismatch");
            return 1;
        }
        if (hero.Fields[0].Doc != "Name field doc" || hero.Fields[0].NumTags != 1 || hero.Fields[0].Tags[0] != "indexed")
        {
            Console.Error.WriteLine("FAIL: Hero.name field doc/tags mismatch");
            return 1;
        }
        // Undocumented fields share ReferenceEquals(Doc, Schema.TableDocNone)!
        if (!ReferenceEquals(hero.Fields[1].Doc, Schema.TableDocNone) || !ReferenceEquals(hero.Fields[2].Doc, Schema.TableDocNone))
        {
            Console.Error.WriteLine("FAIL: undocumented fields do not share TableDocNone");
            return 1;
        }

        // Out-of-closure packet type carries Id == 0 and Json == null!
        var pkt = Schema.PacketOnlyTableType;
        if (pkt.Id != 0UL || pkt.Fields[0].Id != 0UL || pkt.Fields[0].Json != null)
        {
            Console.Error.WriteLine("FAIL: out-of-closure PacketOnly must have Id == 0 and Json == null");
            return 1;
        }

        // UnitView registry matches unit metadata!
        var uv = Schema.UnitView();
        if (uv.Package != "vtview" || uv.ProtocolId != 0x%016xUL || uv.Types.Length != 1 || uv.Tables.Length != 1 || uv.Enums.Length != 1 || uv.Flags.Length != 1 || uv.Constants.Length != 1)
        {
            Console.Error.WriteLine("FAIL: UnitView counts or ProtocolId mismatch");
            return 1;
        }
        if (uv.Enums.Span[0].Name != "Rank" || uv.Enums.Span[0].Variants.Span[1].Name != "Rookie" || uv.Enums.Span[0].Variants.Span[1].Tags.Span[0] != "starter")
        {
            Console.Error.WriteLine("FAIL: UnitView enum variant tags mismatch");
            return 1;
        }
        return 0;
    }
}
`, u.ProtocolId)

	mustRunUnityVTProbe(t, files, probe)
	mustFailUnityVTProbe(t, files, "Tables.View.cs", `Package = "vtview"`, `Package = "wrong"`, probe, "FAIL: UnitView counts or ProtocolId mismatch")
}
