package unitytable_test

import (
	"encoding/binary"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/unitytable"
	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
)

// TestUnityFixedR14GrownUnionTag is the unity (C#) leg's R14: when a union's
// tag widens between the writer's lock layout and this reader (1 byte to 8),
// the writer's raw tag is read at the WRITER's tag width, so the pair compiles
// WHOLE and a 5-byte writer record is never read past
// (docs/FIXED-FORM-ALGORITHM.md §5.2 EMIT kind 15, ruled 2026-11-25). Reading
// at the reader's width would pull the arm's payload into the tag.
//
// The unity leg compiles a lock entry's plan in the GENERATOR
// (internal/codegen/unitytable/fixedform.go compileFixedPlan, then
// olderWriterLane, which lineageLane runs for every lock entry), so the row
// drives that path on the R14 vector and then runs the lane the generator
// lays through the leg's real runtime: TableWire.FixedOpen (header, layout,
// the writer's bounds pass) and TableWire.FixedApply, compiled into a
// generated unit. The unity None const reads no record byte (it lands a
// constant); the writer's tag is read by the bounds pass's range check and by
// every arm's guard, each at the writer's width.
//
// THE VECTOR: a layout block is a u32LE entry count, then 17-byte entries: id
// u64LE, kind u8, size u32LE, children u32LE (13 a table, 15 a union, 8 a u32).
// THEIRS: [1,13,5,1], [7,15,5,1], [9,8,4,0]; MINE(tag): the same ids at
// tag + 4. The reader's image holds the tag at 0 (tag bytes), the u32 at tag.
func TestUnityFixedR14GrownUnionTag(t *testing.T) {
	t.Parallel()
	theirs := r14Block(1)
	const hash = 0x3333333344444444
	const theirRecord = 5

	// THE TWINS: the tag kept at 1 and grown to 2 compile whole too.
	for _, tag := range []int64{1, 2, 8} {
		lane := unitytable.R14GrownTagLane(theirs, hash, 1, tag)
		if lane.Why != "" {
			t.Fatalf("R14: THEIRS vs MINE(%d) is refused %s; the grown tag must compile whole", tag, lane.Why)
		}
		r14Reach(t, tag, lane, theirRecord)
	}

	lane := unitytable.R14GrownTagLane(theirs, hash, 1, 8)
	var none, rangeCheck, armCopy *unitytable.R14Op
	for i := range lane.Ops {
		o := &lane.Ops[i]
		switch {
		case o.Code == unitytable.R14OpConst && len(o.Guards) == 0 && o.Lo == 0:
			none = o
		case o.Code == unitytable.R14OpCopy:
			armCopy = o
		}
	}
	for i := range lane.Checks {
		o := &lane.Checks[i]
		if o.Code == unitytable.R14OpRangeU && len(o.Guards) == 0 && o.Src == 0 {
			rangeCheck = o
		}
	}
	if none == nil {
		t.Fatalf("R14: the union's None const is not in the plan: %+v", lane.Ops)
	}
	if none.Dst != 0 || none.Size != 8 || none.ReadsSourceBytes {
		t.Fatalf("R14: the None const must land MY 8-byte tag at 0 and read no record byte: %+v", *none)
	}
	if rangeCheck == nil || rangeCheck.Size != 1 || rangeCheck.Hi != 1 {
		t.Fatalf("R14: the writer's raw tag must be read at the WRITER's 1-byte width and held to its 1 arm: %+v", lane.Checks)
	}
	if armCopy == nil || armCopy.Src != 1 || armCopy.Size != 4 || armCopy.Dst != 8 ||
		len(armCopy.Guards) != 1 || armCopy.Guards[0].At != 0 || armCopy.Guards[0].Width != 1 || armCopy.Guards[0].Value != 1 {
		t.Fatalf("R14: the u32 arm must copy 4 bytes from src 1 (past the WRITER's 1-byte tag) to my 8, under the writer's tag at width 1: %+v", lane.Ops)
	}

	// RUN the lane through the leg's real runtime.
	probeUnit := fixedproof.ParseCheckUnit(t, "package probe\nfixed table C {\n    lead uint32 = 1\n}\n")
	files, err := unityGenerateFiles(probeUnit, nil)
	if err != nil {
		t.Fatal(err)
	}
	files["R14Lane.cs"] = []byte(fmt.Sprintf(r14LaneCS, lane.CS))
	inputs := map[string][]byte{
		"arm.bin":    r14File(theirs, hash, []byte{1, 0x44, 0x33, 0x22, 0x11}),
		"none.bin":   r14File(theirs, hash, []byte{0, 0, 0, 0, 0}),
		"forged.bin": r14File(theirs, hash, []byte{2, 0, 0, 0, 0}),
	}
	out, code, _, err := runUnityProbe(t, files, r14Probe, inputs)
	if err != nil {
		t.Fatalf("R14: probe build/exec error: %v\n%s", err, out)
	}
	if code != 0 || !strings.Contains(out, "ROW R14: ok") {
		t.Fatalf("R14 (exit %d):\n%s", code, out)
	}
}

// r14Reach holds every entry of the lane inside the writer's record: what an
// entry reads (src + size), and every guard's tag (at + width).
func r14Reach(t *testing.T, tag int64, lane unitytable.R14Lane, record int64) {
	t.Helper()
	for _, set := range [][]unitytable.R14Op{lane.Ops, lane.Checks} {
		for _, o := range set {
			if o.ReadsSourceBytes && o.Src+o.Size > record {
				t.Fatalf("R14: MINE(%d): an entry reads %d + %d past the writer's record of %d: %+v", tag, o.Src, o.Size, record, o)
			}
			for _, g := range o.Guards {
				if g.At+g.Width > record || g.Width != 1 {
					t.Fatalf("R14: MINE(%d): a guard reads the writer's tag at %d + %d, not at the writer's 1-byte width inside %d: %+v", tag, g.At, g.Width, record, o)
				}
			}
		}
	}
}

func r14Block(tag uint32) []byte {
	b := make([]byte, 4+3*17)
	binary.LittleEndian.PutUint32(b, 3)
	put := func(i int, id uint64, kind byte, size, children uint32) {
		at := 4 + i*17
		binary.LittleEndian.PutUint64(b[at:], id)
		b[at+8] = kind
		binary.LittleEndian.PutUint32(b[at+9:], size)
		binary.LittleEndian.PutUint32(b[at+13:], children)
	}
	put(0, 1, 13, tag+4, 1)
	put(1, 7, 15, tag+4, 1)
	put(2, 9, 8, 4, 0)
	return b
}

// r14File is a fixed-form file under the writer's layout holding one record.
func r14File(layout []byte, hash uint64, body []byte) []byte {
	b := make([]byte, 20, 20+len(layout)+8+len(body))
	b[0] = 3
	binary.LittleEndian.PutUint64(b[8:], hash)
	binary.LittleEndian.PutUint32(b[16:], uint32(len(layout)))
	b = append(b, layout...)
	b = binary.LittleEndian.AppendUint64(b, hash)
	return append(b, body...)
}

// r14LaneCS compiles the generator's lane into the generated unit, beside the
// runtime it runs on, and loads a file the way the generated load does:
// FixedOpen, then FixedApply over each record's body.
const r14LaneCS = `using System;

namespace Probe
{
    public static partial class Schema
    {
        private static readonly TableWire.FixedLane[] r14Lanes = new TableWire.FixedLane[]
        {
%s
        };

        public static string R14Load(byte[] data, byte[] image)
        {
            int why = TableWire.FixedOpen(data, r14Lanes, 0, 8, 64, out int pick, out int n, out ulong _);
            if (why != TableReport.Accepted) { return TableReport.ReasonName(why); }
            TableWire.FixedLane lane = r14Lanes[pick];
            ReadOnlySpan<byte> at = new ReadOnlySpan<byte>(data).Slice(data.Length - n * lane.RecordBytes);
            for (int k = 0; k < n; k++)
            {
                TableWire.FixedApply(lane, at.Slice(k * lane.RecordBytes + 8, lane.RecordBytes - 8), image);
            }
            return "accepted";
        }
    }
}
`

const r14Probe = `using System;
using System.Buffers.Binary;
using System.IO;
using Probe;

static class Program
{
    static int failed;

    static void Check(bool ok, string what)
    {
        Console.WriteLine((ok ? "ok   " : "FAIL ") + what);
        if (!ok) { failed++; }
    }

    static byte[] Poisoned()
    {
        byte[] image = new byte[12];
        for (int i = 0; i < image.Length; i++) { image[i] = 0xAB; }
        return image;
    }

    static int Main()
    {
        byte[] image = Poisoned();
        string why = Schema.R14Load(File.ReadAllBytes("arm.bin"), image);
        Check(why == "accepted", "arm 1 reads with no refusal, got " + why);
        ulong tag = BinaryPrimitives.ReadUInt64LittleEndian(image);
        Check(tag == 1UL, "my 8-byte tag lands exactly 1, high bytes zero: got 0x" + tag.ToString("x"));
        uint u = BinaryPrimitives.ReadUInt32LittleEndian(new ReadOnlySpan<byte>(image, 8, 4));
        Check(u == 0x11223344u, "the u32 lands whole past my tag: got 0x" + u.ToString("x"));

        image = Poisoned();
        why = Schema.R14Load(File.ReadAllBytes("none.bin"), image);
        tag = BinaryPrimitives.ReadUInt64LittleEndian(image);
        Check(why == "accepted" && tag == 0UL, "tag 0 reads as None in my 8-byte tag: why=" + why + " tag=0x" + tag.ToString("x"));

        image = Poisoned();
        why = Schema.R14Load(File.ReadAllBytes("forged.bin"), image);
        Check(why == "out_of_range", "a forged writer tag 2 refuses out_of_range (§4.6), got " + why);

        if (failed != 0) { Console.WriteLine("ROW R14: FAILED " + failed); return 1; }
        Console.WriteLine("ROW R14: ok");
        return 0;
    }
}
`
