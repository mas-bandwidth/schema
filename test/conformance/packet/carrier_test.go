package packet

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
)

func seedShape(t *testing.T, s Seed) *Shape {
	t.Helper()
	schemas, err := filepath.Glob(filepath.Join(s.Dir, "*.schema"))
	if err != nil || len(schemas) == 0 {
		t.Fatalf("seed %s: no schema under %s (%v)", s.Key, s.Dir, err)
	}
	u, err := compiler.New().Load(schemas)
	if err != nil {
		t.Fatal(err)
	}
	root, ok := u.Structs[s.Root]
	if !ok {
		t.Fatalf("seed %s: no type %s", s.Key, s.Root)
	}
	sh, err := ShapeOf(root)
	if err != nil {
		t.Fatal(err)
	}
	return sh
}

// Every seed is carried: its root projects onto the carried constructs.
func TestSeedsAreCarried(t *testing.T) {
	for _, s := range Seeds {
		seedShape(t, s)
	}
}

// The generator is a function of its seed, respects every range, reaches
// every union arm (None included) and both ends of every range, and its flat
// form parses back to the same canonical JSON.
func TestUnionBeforeBytesInstances(t *testing.T) {
	sh := seedShape(t, Seeds[0])
	a := sh.Instances(7, 256)
	b := sh.Instances(7, 256)
	tags := map[int]bool{}
	small := map[int64]bool{}
	for i := range a {
		if a[i].JSON() != b[i].JSON() {
			t.Fatalf("instance %d differs between two draws of one seed: %s vs %s", i, a[i].JSON(), b[i].JSON())
		}
		u := a[i].Fields[0]
		tags[u.Tag] = true
		if u.Tag == 1 {
			v := u.Payload.Fields[0].Int.Int64()
			if v < 0 || v > 127 {
				t.Fatalf("instance %d: Small.v = %d outside [0, 127]", i, v)
			}
			small[v] = true
		}
		for _, e := range a[i].Fields[1].Elems {
			if e.Int.Sign() < 0 || e.Int.Int64() > 255 {
				t.Fatalf("instance %d: a data byte %s outside uint8", i, e.Int)
			}
		}
		back, err := sh.Parse(a[i].Flat())
		if err != nil {
			t.Fatalf("instance %d: %v", i, err)
		}
		if back.JSON() != a[i].JSON() {
			t.Fatalf("instance %d: flat round trip %s -> %s", i, a[i].JSON(), back.JSON())
		}
	}
	if !tags[0] || !tags[1] {
		t.Fatalf("union arms reached: %v; want None and a", tags)
	}
	if !small[0] || !small[127] {
		t.Fatalf("Small.v never reached a bound: 0 %v, 127 %v", small[0], small[127])
	}
	if sh.Instances(8, 1)[0].JSON() == a[0].JSON() && sh.Instances(9, 1)[0].JSON() == a[0].JSON() {
		t.Fatal("the seed does not reach the generator")
	}
}

// The canonical JSON and flat forms are pinned for one hand-built value of
// each union arm.
func TestUnionBeforeBytesForms(t *testing.T) {
	sh := seedShape(t, Seeds[0])
	for _, tc := range []struct{ flat, json string }{
		{"0 1 2 3 4", `{"u":{},"data":[1,2,3,4]}`},
		{"1 127 0 255 9 8", `{"u":{"a":{"v":127}},"data":[0,255,9,8]}`},
	} {
		v, err := sh.Parse(tc.flat)
		if err != nil {
			t.Fatal(err)
		}
		if v.JSON() != tc.json || v.Flat() != tc.flat {
			t.Fatalf("%q: json %s flat %q; want %s", tc.flat, v.JSON(), v.Flat(), tc.json)
		}
	}
	for _, bad := range []string{"", "2 1 2 3 4", "0 1 2 3", "0 1 2 3 4 5", "1 5 1 2 3", "0 x 2 3 4"} {
		if _, err := sh.Parse(bad); err == nil {
			t.Fatalf("%q parsed; want a structural refusal", bad)
		}
	}
}

func TestBitFlips(t *testing.T) {
	got := BitFlips([][]byte{{0x00}, {0x01}})
	// {00}'s flips are 01..80; {01}'s are 00, 03, 05, ..., 81: sixteen, all distinct
	if len(got) != 16 {
		t.Fatalf("%d mutants, want 16: %x", len(got), got)
	}
	for i := 1; i < len(got); i++ {
		if bytes.Compare(got[i-1], got[i]) >= 0 {
			t.Fatalf("mutants not sorted and distinct: %x", got)
		}
	}
}

// Each comparison goes red on exactly the disagreement planted in it.
func TestComparisonsGoRed(t *testing.T) {
	sh := seedShape(t, Seeds[0])
	inst := sh.Instances(1, 2)
	if bad := CompareWrites(inst, map[string][]string{"a": {"01", "02"}, "b": {"01", "02"}}); len(bad) != 0 {
		t.Fatalf("agreeing writers reported: %v", bad)
	}
	bad := CompareWrites(inst, map[string][]string{"a": {"01", "02"}, "b": {"01", "03"}})
	if len(bad) != 1 || !strings.Contains(bad[0], "instance 1") || !strings.Contains(bad[0], "b=03") {
		t.Fatalf("a planted write disagreement: %v", bad)
	}

	good := []Read{{JSON: inst[0].JSON()}, {JSON: inst[1].JSON()}}
	if bad := CompareRoundTrips(inst, map[string][]Read{"a<-b": good}); len(bad) != 0 {
		t.Fatalf("faithful round trips reported: %v", bad)
	}
	bad = CompareRoundTrips(inst, map[string][]Read{"a<-b": {good[0], {Refused: true}}, "b<-a": {good[1], good[1]}})
	if len(bad) != 2 || !strings.Contains(bad[0], "a<-b read back refused") || !strings.Contains(bad[1], "b<-a") {
		t.Fatalf("a planted refusal and a wrong value: %v", bad)
	}

	mutants := [][]byte{{1}, {2}, {3}}
	same := []Read{{Refused: true}, {JSON: "{}"}, {JSON: "[]"}}
	if bad := CompareSweep(mutants, map[string][]Read{"a": same, "b": same}); len(bad) != 0 {
		t.Fatalf("agreeing readers reported: %v", bad)
	}
	bad = CompareSweep(mutants, map[string][]Read{"a": same, "b": {{Refused: true}, {Refused: true}, {JSON: "[1]"}}})
	if len(bad) != 2 || !strings.Contains(bad[0], "mutant 02") || !strings.Contains(bad[1], "accepted [1]") {
		t.Fatalf("a planted verdict and value disagreement: %v", bad)
	}
}

func TestParseReads(t *testing.T) {
	sh := seedShape(t, Seeds[0])
	reads, err := sh.ParseReads([]string{"refused", "ok 0 1 2 3 4"})
	if err != nil {
		t.Fatal(err)
	}
	if !reads[0].Refused || reads[1].JSON != `{"u":{},"data":[1,2,3,4]}` {
		t.Fatalf("%v", reads)
	}
	for _, bad := range []string{"", "accepted", "ok 0 1"} {
		if _, err := sh.ParseReads([]string{bad}); err == nil {
			t.Fatalf("%q parsed", bad)
		}
	}
}
