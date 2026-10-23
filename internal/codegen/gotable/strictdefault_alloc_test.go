package gotable

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
)

// TestGoPlainReadsAllocateNothing holds that the plain-named strict reads
// (docs/SPEC-TABLES.md §6.5) add zero allocations over their ...Tolerant twins
// on a clean wire and a supplied report.
func TestGoPlainReadsAllocateNothing(t *testing.T) {
	t.Parallel()
	const schema = `package probe
fixed table Fix { n int32 = 1 | min = 0, max = 10
 m int32 = 2 }
table Var { n int32 = 1 | min = 0, max = 10
 req int32 | required }
`
	const probe = `package probe
import (
	"fmt"
	"testing"
	"unsafe"
)
func varFile(fields ...[]byte) []byte {
	var ids TableIds
	buf := make([]byte, 128)
	w := TableWriter{Buffer: buf, Ids: &ids}
	w.Put8(1)
	for _, f := range fields {
		w.Id(VarTableFields[f[0]].Id)
		w.Put8(f[1])
		for _, b := range f[2:] {
			w.Put8(b)
		}
	}
	w.Put8(0)
	w.Trailer()
	return buf[:w.Offset]
}
func region(n int64) []byte {
	raw := make([]byte, n+64)
	off := int((64 - uintptr(unsafe.Pointer(&raw[0]))%64) % 64)
	return raw[off : off+int(n)]
}
func vocab(t *testing.T) *TableVocabulary {
	v := new(TableVocabulary)
	v.Init(make([]TableMessageEntry, TableMessageEntriesHere))
	a := make([]byte, AnnounceMeasure())
	if Announce(a) != int64(len(a)) {
		t.Fatal("announce")
	}
	var r TableReport
	if !AnnounceRead(v, a, &r) {
		t.Fatalf("announce read %+v", r)
	}
	return v
}
func TestPlainReadsAlloc(t *testing.T) {
	varClean := varFile([]byte{0, 4, 3, 0, 0, 0}, []byte{1, 4, 4, 0, 0, 0})
	varReg := region(VarLoadMeasure(varClean))
	varJSON := []byte("{\"n\":3,\"req\":4}")
	var vb VarBuilder
	if !vb.Init() {
		t.Fatal("init")
	}
	defer vb.Shutdown()

	fixVal := Fix{N: 3, M: 4}
	fixClean := make([]byte, FixMeasure(&fixVal))
	if FixSave(&fixVal, fixClean) != int64(len(fixClean)) {
		t.Fatal("fix save")
	}
	fixJSON := []byte("{\"n\":3,\"m\":4}")
	fixForm3 := make([]byte, FixFixedMeasure(1))
	if FixFixedSave([]Fix{fixVal}, fixForm3) != int64(len(fixForm3)) {
		t.Fatal("fixed save")
	}
	fixPlan := make([]TableFixedEntry, 256)
	fixGot := make([]Fix, 1)
	voc := vocab(t)
	fixMsg := make([]byte, FixMeasureMessages([]*Fix{&fixVal}, nil))
	if FixSaveMessages([]*Fix{&fixVal}, fixMsg, nil) != int64(len(fixMsg)) {
		t.Fatal("save messages")
	}

	var r TableReport
	var fv Fix

	measure := func(name string, plain, tolerant func()) {
		pa := testing.AllocsPerRun(100, plain)
		ta := testing.AllocsPerRun(100, tolerant)
		fmt.Printf("ALLOC\t%s\t%d\t%d\n", name, int(pa), int(ta))
	}

	measure("variable Load", func() {
		if VarLoad(varReg, varClean, &r) == nil {
			t.Fatal("variable Load")
		}
	}, func() {
		if VarLoadTolerant(varReg, varClean, &r) == nil {
			t.Fatal("variable LoadTolerant")
		}
	})
	measure("variable LoadBuilder", func() {
		if !VarLoadBuilder(&vb, varClean, &r) {
			t.Fatal("variable LoadBuilder")
		}
	}, func() {
		if !VarLoadBuilderTolerant(&vb, varClean, &r) {
			t.Fatal("variable LoadBuilderTolerant")
		}
	})
	measure("variable FromJson", func() {
		if !VarFromJson(&vb, varJSON, &r) {
			t.Fatal("variable FromJson")
		}
	}, func() {
		if !VarFromJsonTolerant(&vb, varJSON, &r) {
			t.Fatal("variable FromJsonTolerant")
		}
	})
	measure("fixed Load", func() {
		if !FixLoad(&fv, fixClean, &r) {
			t.Fatal("fixed Load")
		}
	}, func() {
		if !FixLoadTolerant(&fv, fixClean, &r) {
			t.Fatal("fixed LoadTolerant")
		}
	})
	measure("fixed FromJson", func() {
		if !FixFromJson(&fv, fixJSON, &r) {
			t.Fatal("fixed FromJson")
		}
	}, func() {
		if !FixFromJsonTolerant(&fv, fixJSON, &r) {
			t.Fatal("fixed FromJsonTolerant")
		}
	})
	measure("fixed FixedLoad", func() {
		if FixFixedLoad(fixGot, fixForm3, fixPlan, &r) != 1 {
			t.Fatal("fixed FixedLoad")
		}
	}, func() {
		if FixFixedLoadTolerant(fixGot, fixForm3, fixPlan, &r) != 1 {
			t.Fatal("fixed FixedLoadTolerant")
		}
	})
	measure("fixed LoadMessages", func() {
		if n, ok := FixLoadMessages(fixGot, voc, fixMsg, &r); !ok || n != 1 {
			t.Fatal("fixed LoadMessages")
		}
	}, func() {
		if n, ok := FixLoadMessagesTolerant(fixGot, voc, fixMsg, &r); !ok || n != 1 {
			t.Fatal("fixed LoadMessagesTolerant")
		}
	})
}
`
	out, err := runGeneratedResult(t, schema, probe, "-v", "-run", "TestPlainReadsAlloc")
	if err != nil {
		t.Fatalf("generated runtime: %v\n%s", err, out)
	}
	type pair struct{ plain, tolerant int }
	got := map[string]pair{}
	for line := range strings.SplitSeq(string(out), "\n") {
		fields := strings.Split(strings.TrimSpace(line), "\t")
		if len(fields) == 4 && fields[0] == "ALLOC" {
			pa, _ := strconv.Atoi(fields[2])
			ta, _ := strconv.Atoi(fields[3])
			got[fields[1]] = pair{plain: pa, tolerant: ta}
		}
	}
	cases := []struct {
		name string
		want int
	}{
		{"variable Load", 0},
		{"variable LoadBuilder", 1},
		{"variable FromJson", 3},
		{"fixed Load", 0},
		{"fixed FromJson", 0},
		{"fixed FixedLoad", 0},
		{"fixed LoadMessages", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, ok := got[c.name]
			if !ok {
				t.Fatalf("missing measurement for %s in:\n%s", c.name, out)
			}
			if p.plain != c.want || p.plain != p.tolerant {
				t.Fatalf("%s: AllocsPerRun = %d (tolerant = %d), want %d", c.name, p.plain, p.tolerant, c.want)
			}
		})
	}
	_ = fmt.Sprint
}
