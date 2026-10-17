package gotable

// FULL-WIDTH ENUM AND UNION ORDINALS ON THE GO LEG (docs/roadmap.sexp node
// `fixed-tables`, leaf go/R31; docs/FIXED-FORM-ALGORITHM.md §4.1, §5.2, §5.8
// rows 5, 7 and 14). One fixture per the card: an enum with an ordinal past
// 65535 and a union with more than 255 arms, written by a writer with more
// variants than the reader.
//
// The writer declares 300 enum variants and 300 union arms; the reader keeps
// three variants and two arms, so the writer's last variant (ordinal 300)
// must remap through a table whose length word is the WRITER's count. The
// 8-byte Wide enum (max past 2^32) carries forged ordinals past 65535 and
// past MaxUint32: read through a 64-bit temporary they land None, while a
// 32-bit temporary would truncate 0x1_0000_0001 to variant 1. The reader
// finding on this card pins the lanes unconditionally: Arg/Arg2 are u64, so
// the generated probe below also stamps 0x1_0000_0001 on both guard lanes.
//
// Assertions on the emitted plan that read Go values in-process are unit
// tests. The generated-unit probe is a functional test: it opens with
// slowtest.Gate and compiles the generated unit with the Go toolchain.

import (
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// t16Names builds n schema names with a prefix: v0..v299, a0..a299.
func t16Names(prefix string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("%s%d", prefix, i)
	}
	return out
}

// t16Fixture is the one schema shape every leg of this card declares. many /
// pick / outer select the variant and arm lists, so one builder spells the
// writer (300 of each) and the reader (a subset) with identical ids.
func t16Fixture(many, pick, outer []string) string {
	var b strings.Builder
	b.WriteString("package probe\n\ntype Cell { n int32 }\n\nenum Many { ")
	b.WriteString(strings.Join(many, ", "))
	b.WriteString(" }\n\nunion Pick {\n")
	for _, a := range pick {
		fmt.Fprintf(&b, "    %s Cell\n", a)
	}
	b.WriteString("}\n\ntype Holder { inner Inner }\n\nunion Inner {\n    x Cell\n    y Cell\n}\n\nunion Outer {\n")
	for _, a := range outer {
		if a == "a299" {
			fmt.Fprintf(&b, "    %s Holder\n", a)
		} else {
			fmt.Fprintf(&b, "    %s Cell\n", a)
		}
	}
	b.WriteString("}\n\nfixed table Root {\n    many Many\n    pick Pick\n    top Outer\n    seq int32\n}\n")
	return b.String()
}

func t16WriterSchema() string {
	return t16Fixture(t16Names("v", 300), t16Names("a", 300), t16Names("a", 300))
}

func t16ReaderSchema() string {
	return t16Fixture([]string{"v0", "v1", "v299"}, []string{"a0", "a299"}, []string{"a0", "a299"})
}

// t16OverflowSchema is a writer whose enum alone passes the remap length
// word: 65536 variants, one past the uint16 bound (§5.8 row 14).
func t16OverflowSchema() string {
	var b strings.Builder
	b.WriteString("package probe\n\nenum Many { ")
	b.WriteString(strings.Join(t16Names("v", 65536), ", "))
	b.WriteString(" }\n\nfixed table Root {\n    many Many\n    seq int32\n}\n")
	return b.String()
}

func t16LayoutB64(t *testing.T, schema string) string {
	t.Helper()
	u := unitFrom(t, schema)
	st := u.Tables["Root"]
	if st == nil {
		t.Fatal("the fixture declares no fixed table Root")
	}
	return base64.StdEncoding.EncodeToString(ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(st)))
}

func TestFixedRoadmapGoT16OrdinalLanes(t *testing.T) {
	t.Parallel()
	t.Run("go/R31", func(t *testing.T) {
		t.Parallel()
		t16GoR31(t)
	})
}

func t16GoR31(t *testing.T) {
	// THE LANES ARE FULL WIDTH (§4.1: arg : u64, arg2 : u64, "no byte lane
	// anywhere"). Pinned on the emitted runtime text so the width holds
	// unconditionally, not only where a fixture ordinal needs it: a lane
	// stamped above MaxUint32 must survive (the reader finding on this card).
	rt := t02GoNorm(tableFixedRuntime)
	t02GoHas(t, rt, "Arg uint64", "R31 the guard's ordinal lane is full width (FIXED-FORM-ALGORITHM.md §4.1)")
	t02GoHas(t, rt, "Arg2 uint64", "R31 the outer guard's ordinal lane is full width (FIXED-FORM-ALGORITHM.md §4.1)")

	// THE WRITER'S PLAN CARRIES ARM 300 WHOLE, AND THE NESTED ARM ANSWERS TO
	// BOTH TAGS (§4.1, §5.8 rows 5-6): the inner arm's leaf holds the inner
	// ordinal on Arg and the outer arm 300 on Arg2.
	writer := unitFrom(t, t16WriterSchema())
	plan, _ := ir.TableFixedBuildPlan(writer, writer.Tables["Root"])
	var arm300, nested bool
	for _, e := range plan {
		if e.Arg == 300 {
			arm300 = true
		}
		if e.Arg2 == 300 && e.Arg == 2 {
			nested = true
		}
	}
	if !arm300 {
		t.Error("R31: no plan entry carries arm 300's ordinal whole; a narrower lane truncates it")
	}
	if !nested {
		t.Error("R31: no nested entry carries the outer arm 300 on Arg2 with the inner ordinal on Arg")
	}

	// A WRITER PAST THE LENGTH WORD REFUSES AS plan_too_large (§5.2, §5.8
	// row 14), the name the C leg's overflow carries: the remap branch must
	// record the plan-storage refusal, never the record-bound one.
	if i := strings.Index(rt, "if n > tableFixedRemapMax {"); i < 0 || !strings.Contains(rt[i:], "c.overflow = true") && !strings.Contains(rt[i:i+200], "c.overflow = true") {
		t.Error("R31: a variant count past the uint16 length word does not refuse through the plan_too_large overflow (FIXED-FORM-ALGORITHM.md §5.2)")
	}

	// THE FUNCTIONAL HALF: the generated reader compiles the writer's plan
	// and runs it. slowtest.Gate is the toolchain gate; under SCHEMA_SLOW=1
	// this RUNS, and a bare go test skips it by name.
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated unit)")
	reader := unitFrom(t, t16ReaderSchema())
	writerB64 := t16LayoutB64(t, t16WriterSchema())
	overflowB64 := t16LayoutB64(t, t16OverflowSchema())
	probe := strings.Replace(`package probe

import (
	"encoding/base64"
	"encoding/binary"
	"testing"
	"unsafe"
)

const writerLayoutB64 = "@WRITER@"
const overflowLayoutB64 = "@OVERFLOW@"

func t16Body() []byte {
	body := make([]byte, 19)
	binary.LittleEndian.PutUint16(body[0:], 300)  // many: writer variant 300
	binary.LittleEndian.PutUint16(body[2:], 300)  // pick: writer arm 300
	binary.LittleEndian.PutUint32(body[4:], 777)  // pick payload
	binary.LittleEndian.PutUint16(body[8:], 300)  // top: outer arm 300
	body[10] = 2                                  // inner arm 2
	binary.LittleEndian.PutUint32(body[11:], 888) // inner payload
	binary.LittleEndian.PutUint32(body[15:], 9)   // seq
	return body
}

func TestT16OrdinalLanes(t *testing.T) {
	wb, err := base64.StdEncoding.DecodeString(writerLayoutB64)
	if err != nil { t.Fatal(err) }
	parsed, why := tableFixedParseLayout(wb)
	if why != "" { t.Fatalf("the writer layout does not parse: %s", why) }
	plan := make([]TableFixedEntry, 8192)
	var r TableReport
	made := tableFixedCompile(parsed, RootFixedLayout, RootFixedDst, plan, &r)
	if made <= 0 { t.Fatalf("compile made %d (%+v)", made, r) }

	// THE REMAP TABLE'S LENGTH WORD IS THE WRITER'S VARIANT COUNT (§5.2):
	// table[0] == 300, not the reader's 3 and never a capped 255.
	ordinals := 0
	for i := int32(0); i < made; i++ {
		if plan[i].Op != tableFixedOrdinal { continue }
		ordinals++
		p16 := (*uint16)(unsafe.Add(unsafe.Pointer(&plan[0]), uintptr(plan[i].Aux)))
		if ordinals == 1 && *p16 != 300 {
			t.Fatalf("the remap length word is %d, want the writer's 300", *p16)
		}
	}
	if ordinals != 1 { t.Fatalf("the compiled plan carries %d ordinal entries, want many only", ordinals) }

	// THE COMPILED RUN: variant 300 remaps to the reader's v299 (ordinal 3),
	// arm 300 to the reader's second arm, and both payloads land.
	var co Root
	RootReset(&co)
	var rr TableReport
	dst := tableFixedOverlay(unsafe.Pointer(&co), unsafe.Sizeof(co))
	if why := tableFixedRun(plan, made, t16Body(), dst, &rr); why != "" { t.Fatalf("run: %s", why) }
	if co.Many != Manyv299 { t.Fatalf("many landed %d, want the reader ordinal of writer variant 300 (Manyv299)", int(co.Many)) }
	if co.Pick.Type != PickTypeA299 { t.Fatalf("pick tag landed %d, want the reader's arm a299", int(co.Pick.Type)) }
	if co.Pick.A299.N != 777 { t.Fatalf("pick payload landed %d, want 777", co.Pick.A299.N) }
	if co.Top.Type != OuterTypeA299 { t.Fatalf("outer tag landed %d, want the reader's arm a299", int(co.Top.Type)) }
	if co.Top.A299.Inner.Type != InnerTypeY { t.Fatalf("inner tag landed %d, want arm y", int(co.Top.A299.Inner.Type)) }
	if co.Top.A299.Inner.Y.N != 888 { t.Fatalf("inner payload landed %d, want 888", co.Top.A299.Inner.Y.N) }
	if co.Seq != 9 { t.Fatalf("seq landed %d, want 9", co.Seq) }

	// FORGED ORDINALS PAST 65535 AND PAST MaxUint32 LAND None (§4.5: the
	// ordinal reads through a 64-bit temporary). A 32-bit temporary would
	// truncate 0x1_0000_0001 to variant 1 and land it.
	// A FORGED ORDINAL PAST THE WRITER'S SET LANDS None.
	{
		body := t16Body()
		binary.LittleEndian.PutUint16(body[0:], 301)
		var fo Root
		RootReset(&fo)
		var fr TableReport
		fdst := tableFixedOverlay(unsafe.Pointer(&fo), unsafe.Sizeof(fo))
		if why := tableFixedRun(plan, made, body, fdst, &fr); why != "" { t.Fatalf("run: %s", why) }
		if fo.Many != ManyNone { t.Fatalf("forged ordinal 301 landed %d, want None", int(fo.Many)) }
	}

	// THE ORDINAL TEMPORARY IS 64-BIT (§4.5): an 8-byte ordinal of
	// 0x1_0000_0001 read through a 32-bit temporary would land variant 1.
	oc := &tableFixedCompiler{plan: make([]TableFixedEntry, 64), capacity: 64}
	aux := tableFixedLayTable(oc, []uint16{1, 2, 3}, 3)
	oc.plan[0] = TableFixedEntry{Src: 0, Dst: 0, Size: 8, DstSize: 8, Aux: aux, Guard: tableFixedNoGuard, Op: tableFixedOrdinal}
	osrc := make([]byte, 8)
	odst := make([]byte, 8)
	var orr TableReport
	binary.LittleEndian.PutUint64(osrc, 4294967297)
	tableFixedRun(oc.plan, 1, osrc, odst, &orr)
	if v := binary.LittleEndian.Uint64(odst); v != 0 {
		t.Fatalf("the 8-byte ordinal 0x1_0000_0001 landed %d, want None (0): the temporary truncated", v)
	}
	binary.LittleEndian.PutUint64(osrc, 70000)
	odst = make([]byte, 8)
	tableFixedRun(oc.plan, 1, osrc, odst, &orr)
	if v := binary.LittleEndian.Uint64(odst); v != 0 {
		t.Fatalf("the 8-byte ordinal 70000 (past 65535) landed %d, want None (0)", v)
	}
	binary.LittleEndian.PutUint64(osrc, 2)
	odst = make([]byte, 8)
	tableFixedRun(oc.plan, 1, osrc, odst, &orr)
	if v := binary.LittleEndian.Uint64(odst); v != 2 {
		t.Fatalf("the 8-byte ordinal 2 landed %d, want 2", v)
	}

	// BOTH GUARD LANES HOLD AN ORDINAL ABOVE MaxUint32 (§4.1, u64 lanes):
	// the entry fires only when BOTH 8-byte tags equal 0x1_0000_0001.
	src := make([]byte, 24)
	binary.LittleEndian.PutUint64(src[0:], 4294967297)
	binary.LittleEndian.PutUint64(src[8:], 4294967297)
	copy(src[16:], []byte{7, 7, 7, 7})
	entry := TableFixedEntry{Src: 16, Dst: 0, Size: 4, Guard: 0, Arg: 4294967297, ArgW: 8, Guard2: 8, Arg2: 4294967297, ArgW2: 8, Op: tableFixedCopy}
	out := make([]byte, 4)
	var lr TableReport
	tableFixedRun([]TableFixedEntry{entry}, 1, src, out, &lr)
	if binary.LittleEndian.Uint32(out) != 0x07070707 { t.Fatalf("the full-width lanes did not fire together: %x", out) }
	entry.Arg = 1
	out = make([]byte, 4)
	tableFixedRun([]TableFixedEntry{entry}, 1, src, out, &lr)
	if binary.LittleEndian.Uint32(out) != 0 { t.Fatalf("the truncated lane 1 fired on the full-width tag: %x", out) }

	// A WRITER PAST THE LENGTH WORD IS REFUSED AT COMPILE (§5.8 row 14):
	// -1 is the plan-storage refusal the lineage surfaces as plan_too_large.
	ob, err := base64.StdEncoding.DecodeString(overflowLayoutB64)
	if err != nil { t.Fatal(err) }
	oparsed, why := tableFixedParseLayout(ob)
	if why != "" { t.Fatalf("the overflow writer layout does not parse: %s", why) }
	big := make([]TableFixedEntry, 8192)
	var or TableReport
	if got := tableFixedCompile(oparsed, RootFixedLayout, RootFixedDst, big, &or); got != -1 {
		t.Fatalf("a writer past the uint16 length word compiled to %d, want the plan_too_large refusal (-1)", got)
	}
}
`, "@WRITER@", writerB64, 1)
	probe = strings.Replace(probe, "@OVERFLOW@", overflowB64, 1)
	if out, err := runGeneratedUnit(t, reader, probe, nil); err != nil {
		t.Fatalf("generated runtime: %v\n%s", err, out)
	}
}
