package gotable

import (
	"fmt"
	"path/filepath"
	"testing"
)

// ---- a count or a length past the PLAN's bound refuses BY NAME --------------
//
// docs/SPEC-TABLES.md §3.4 (THE SLACK RULE) and docs/FIXED-FORM-ALGORITHM.md
// §4.5: a count word or a length word the plan's bound cannot hold — negative
// or past the bound — is a REFUSAL BY NAME, `count_over_bound` and
// `length_over_bound`, checked before any destination byte is written, so
// REFUSE stays total over the batch. Neither op clamps: a clamp changes the
// meaning of a forged record, and with zero slack behind the bound the same
// forged length used to answer two ways (clamped-clean or text_ill_formed,
// whichever the slack bytes said). One forged length now answers one way.
//
// The bound is THE PLAN's — the WRITER's own for that lineage entry (bill
// §12.5), never the reader's — so the compiled-lineage leg forges a count
// that is inside the reader's bound and the distractor peer's bound and past
// the WRITER's, which only a plan carrying the writing peer's bound refuses.
//
// A ranged scalar's clamp (§4.6, the bounds pass) is a different rule and is
// unchanged; slack behind a count or a length stays UNSPECIFIED and never a
// refusal — the refusal is the WORD lying about the wire, not the slack.

func TestFixedFormCountAndLengthOverBoundRefuseByName(t *testing.T) {
	t.Parallel()

	t.Run("identity", func(t *testing.T) {
		t.Parallel()
		runGenerated(t, `package probe
fixed table Root {
    marks [..95]int32
    note  string(41)
}
`, `package probe
import ("encoding/binary"; "testing")

func TestBoundRefusals(t *testing.T) {
	if RootFixedBodyBytes != 429 {
		t.Fatalf("the record body moved: %d bytes, so the offsets below are stale", RootFixedBodyBytes)
	}
	one := Root{}
	RootReset(&one)
	one.MarksCount = 1
	one.Marks[0] = 0x0AAAAAAA
	one.NoteLength = 2
	copy(one.Note[:], "qq")
	clean := make([]byte, RootFixedMeasure(1))
	if n := RootFixedSave([]Root{one}, clean); n != int64(len(clean)) {
		t.Fatalf("save %d", n)
	}
	bodyAt := func(data []byte) []byte { return data[len(data)-RootFixedRecordBytes+8:] }
	// body: count@0, 95 elements @4, length@384, 41 payload bytes @388.
	slackRanges := [][2]int{{8, 384}, {390, 429}}
	withSlack := func() []byte {
		data := append([]byte(nil), clean...)
		body := bodyAt(data)
		for _, r := range slackRanges {
			for i := r[0]; i < r[1]; i++ {
				body[i] = 0xFF
			}
		}
		return data
	}
	forged := func(base []byte, off int, v uint32) []byte {
		data := append([]byte(nil), base...)
		binary.LittleEndian.PutUint32(bodyAt(data)[off:], v)
		return data
	}
	wantRefusal := func(name, want string, data []byte) {
		t.Helper()
		got := make([]Root, 1)
		var r TableReport
		plan := make([]TableFixedEntry, 256)
		n := RootFixedLoad(got, data, plan, &r)
		if n != -1 || r.Reason != want {
			t.Fatalf("%s: n=%d reason=%q, want -1 and %q: %+v", name, n, r.Reason, want, r)
		}
		if r.Malformed || r.Clamped != 0 || r.Unknown != 0 || r.Widened != 0 {
			t.Fatalf("%s: REFUSE is total, no counter moves: %+v", name, r)
		}
		var zero Root
		if got[0] != zero {
			t.Fatalf("%s: not one destination byte is written: %+v", name, got[0])
		}
	}

	// THE SLACK RULE STILL HOLDS: non-zero slack is UNSPECIFIED and never a
	// refusal — the clean record with 0xFF behind its count and its length
	// reads clean and moves no counter.
	{
		got := make([]Root, 1)
		var r TableReport
		plan := make([]TableFixedEntry, 256)
		if n := RootFixedLoad(got, withSlack(), plan, &r); n != 1 {
			t.Fatalf("clean load %d %+v", n, r)
		}
		if got[0].MarksCount != 1 || got[0].Marks[0] != 0x0AAAAAAA || got[0].NoteLength != 2 {
			t.Fatalf("clean load landed %+v", got[0])
		}
		if r.Clamped != 0 || r.Malformed || r.Reason != "" {
			t.Fatalf("non-zero slack moves nothing: %+v", r)
		}
	}

	// AT THE BOUND IS NOT PAST IT: count 95 and length 41 land clean.
	{
		got := make([]Root, 1)
		var r TableReport
		plan := make([]TableFixedEntry, 256)
		at := forged(clean, 0, 95)
		binary.LittleEndian.PutUint32(bodyAt(at)[384:], 41)
		for i := 390; i < 429; i++ {
			bodyAt(at)[i] = 'q'
		}
		if n := RootFixedLoad(got, at, plan, &r); n != 1 {
			t.Fatalf("at-bound load %d %+v", n, r)
		}
		if got[0].MarksCount != 95 || got[0].NoteLength != 41 || r.Clamped != 0 || r.Reason != "" {
			t.Fatalf("at-bound: count=%d len=%d %+v", got[0].MarksCount, got[0].NoteLength, r)
		}
	}

	// A FORGED COUNT OF 200 past the bound of 95 refuses count_over_bound,
	// WITH and WITHOUT 0xFF slack behind it — one answer either way.
	wantRefusal("count200/zeroslack", "count_over_bound", forged(clean, 0, 200))
	wantRefusal("count200/ffslack", "count_over_bound", forged(withSlack(), 0, 200))
	// A NEGATIVE count is a count the bound cannot hold either.
	wantRefusal("countNeg", "count_over_bound", forged(clean, 0, 0xFFFFFFFF))

	// A FORGED LENGTH OF 99 past the bound of 41 refuses length_over_bound —
	// never text_ill_formed and never a clamp — with ZERO slack (the case
	// that used to read clean through the clamp) and with 0xFF slack (the
	// case that used to answer text_ill_formed): ONE forged length, ONE way.
	wantRefusal("len99/zeroslack", "length_over_bound", forged(clean, 384, 99))
	wantRefusal("len99/ffslack", "length_over_bound", forged(withSlack(), 384, 99))
	wantRefusal("lenNeg", "length_over_bound", forged(clean, 384, 0xFFFFFFFF))
}
`)
	})

	t.Run("compiled-lineage", func(t *testing.T) {
		t.Parallel()
		corpus := fixedCorpus(t)
		newer := readSchema(t, "VNEW_array_bounded_grow")
		writer := readSchema(t, "VOLD_array_bounded_grow")
		distractor := readSchema(t, "VMID_array_bounded_grow")
		table := fixedRootName(t, writer)
		src := fmt.Sprintf(`package probe

import ("bytes"; "encoding/binary"; "os"; "testing")

func refuseOverBound(t *testing.T, data []byte, forged uint32, want string) {
	t.Helper()
	needle := []byte{0xAA, 0xAA, 0xAA, 0xAA, 0x04, 0x00, 0x00, 0x00}
	at := bytes.Index(data, needle)
	if at < 0 {
		t.Fatal("bound refusal: the lead 0x0AAAAAAA followed by vals_count 4 is not in the file")
	}
	if bytes.Contains(data[at+1:], needle) {
		t.Fatal("bound refusal: the forged-count needle occurs more than once, so the search is not a locator")
	}
	binary.LittleEndian.PutUint32(data[at+4:], forged)
	back := make([]%[2]s, 8)
	var r TableReport
	plan := make([]TableFixedEntry, 4096)
	n := %[2]sFixedLoad(back, data, plan, &r)
	if n != -1 || r.Reason != want {
		t.Fatalf("bound refusal: n=%%d reason=%%q, want -1 and %%q: %%+v", n, r.Reason, want, r)
	}
	if r.Malformed || r.Clamped != 0 || r.Unknown != 0 || r.KindMismatch != 0 || r.Widened != 0 || r.Duplicate != 0 {
		t.Fatalf("bound refusal: REFUSE is total, no counter moves: %%+v", r)
	}
	var zero %[2]s
	if back[0] != zero {
		t.Fatalf("bound refusal: not one destination byte is written: %%+v", back[0])
	}
}

func TestWriterBoundCountRefuses(t *testing.T) {
	data, err := os.ReadFile(%[1]q)
	if err != nil {
		t.Fatal(err)
	}
	// 5 is past the WRITER's bound of 4 and inside the distractor's 6 and the
	// reader's own 8: only a plan carrying the WRITING peer's bound refuses.
	refuseOverBound(t, append([]byte(nil), data...), 5, "count_over_bound")
	// The old clamp's far end: 9, and the negative end, both refuse by name.
	refuseOverBound(t, append([]byte(nil), data...), 9, "count_over_bound")
	refuseOverBound(t, append([]byte(nil), data...), 0xFFFFFFFF, "count_over_bound")
	// AT the writer's bound is not past it: the clean file lands exactly as
	// before, count 4 and its four live values.
	{
		d := append([]byte(nil), data...)
		back := make([]%[2]s, 8)
		var r TableReport
		plan := make([]TableFixedEntry, 4096)
		n := %[2]sFixedLoad(back, d, plan, &r)
		if n != 1 || back[0].ValsCount != 4 || r.Reason != "" || r.Clamped != 0 {
			t.Fatalf("bound refusal: the clean file at the bound must still land: n=%%d %%+v", n, r)
		}
	}
}
`, filepath.Join(corpus, "old_array_bounded_grow.bin"), table)
		out, err := runVersionProbe(t, newer, []string{writer, distractor}, src)
		if err != nil {
			t.Fatalf("count over bound on a compiled lineage plan: %v\n%s", err, out)
		}
	})
}
