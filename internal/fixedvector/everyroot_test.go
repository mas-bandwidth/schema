package fixedvector

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// fakeTB records what EveryRoot says when its test ends.
type fakeTB struct {
	name     string
	cleanups []func()
	errors   []string
	fatal    string
}

func (f *fakeTB) Helper()           {}
func (f *fakeTB) Name() string      { return f.name }
func (f *fakeTB) Cleanup(fn func()) { f.cleanups = append(f.cleanups, fn) }
func (f *fakeTB) Errorf(format string, a ...any) {
	f.errors = append(f.errors, fmt.Sprintf(format, a...))
}
func (f *fakeTB) Fatalf(format string, a ...any) { f.fatal = fmt.Sprintf(format, a...) }
func (f *fakeTB) Logf(string, ...any)            {}
func (f *fakeTB) end() {
	for i := len(f.cleanups) - 1; i >= 0; i-- {
		f.cleanups[i]()
	}
}

func writeManifest(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "manifest.txt"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestManifestReadsEveryRow(t *testing.T) {
	dir := writeManifest(t, "file=a.bin row=a side=none root=A records=2 values=r0.x=1,r0.s=\"a\\,b\"\nfile=b.bin row=b side=old root=B records=1 refuse=out_of_range values=r0.y=2\n")
	rows, err := Manifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []ManifestRow{
		{File: "a.bin", Row: "a", Side: "none", Root: "A", Records: 2, Landed: []Value{{"r0.x", "1"}, {"r0.s", `"a\,b"`}}},
		{File: "b.bin", Row: "b", Side: "old", Root: "B", Records: 1, Refuse: "out_of_range", Landed: []Value{{"r0.y", "2"}}},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("rows %+v, want %+v", rows, want)
	}
}

// TestManifestLaysForgedAndExpectOverValues: what a read lands is the stored
// values with the forged cell laid over them where the forge reached a stored
// value, and the reader's expect= over that, a float keeping its spelling.
func TestManifestLaysForgedAndExpectOverValues(t *testing.T) {
	dir := writeManifest(t, ""+
		"file=h.bin row=h side=hostile root=H records=1 forged=r0.aim@104=1.5|0x3FC00000 values=r0.lead=1,r0.aim=0.5|0x3F000000\n"+
		"file=d.bin row=d side=none root=D records=1 forged=r0.opt.n@543=0 expect=r0.arr_count=3,clamped=0 values=r0.opt_present=0,r0.arr_count=3\n"+
		"file=z.bin row=z side=none root=Z records=1 expect=r0.value=0 values=r0.lead=1,r0.value=-0|0x80000000\n"+
		"file=t.bin row=t side=none root=D records=1 forged=r0.text@620=0 expect=r0.text_length=2 values=r0.text=\"hi\",r0.text_length=2\n")
	rows, err := Manifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := [][]Value{
		{{"r0.lead", "1"}, {"r0.aim", "1.5|0x3FC00000"}},
		{{"r0.opt_present", "0"}, {"r0.arr_count", "3"}},
		{{"r0.lead", "1"}, {"r0.value", "0|0x00000000"}},
		// a forged BYTE of a dead span is no value of the field
		{{"r0.text", `"hi"`}, {"r0.text_length", "2"}},
	}
	for i, r := range rows {
		if !reflect.DeepEqual(r.Landed, want[i]) {
			t.Errorf("%s landed %+v, want %+v", r.File, r.Landed, want[i])
		}
	}
}

func TestManifestRefusesByName(t *testing.T) {
	for _, c := range []struct{ name, body, want string }{
		{"missing", "", "not readable"},
		{"empty", "# nothing\n", "names no file"},
		{"twice", "file=a.bin records=1 values=\nfile=a.bin records=1 values=\n", "names a.bin twice"},
		{"no count", "file=a.bin root=A values=\n", "no records= count"},
		{"no values", "file=a.bin root=A records=1\n", "carries no values="},
		{"value twice", "file=a.bin records=1 values=r0.x=1,r0.x=2\n", "names r0.x twice"},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			if c.name != "missing" {
				dir = writeManifest(t, c.body)
			}
			_, err := Manifest(dir)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err %v, want it to say %q", err, c.want)
			}
		})
	}
}

func TestSameMatchesTheManifestSpellings(t *testing.T) {
	for _, c := range []struct {
		want, got string
		same      bool
	}{
		{"42", "42", true},
		{"42", "43", false},
		{"0.5|0x3F000000", "0x3F000000", true},
		{"0.5|0x3F000000", "0x3FE0000000000000", true}, // float32 widened exactly
		{"0.5|0x3F000000", "0x3FE0000000000001", false},
		{"-0|0x80000000", "0x0000000000000000", false}, // -0 is not +0 by bits
		{"1|0x3FF0000000000000", "0x3FF0000000000000", true},
		{"1|0x3FF0000000000000", "0x3F800000", false},
		{"0.5|0x3F000000", "0.5", false},
		{"62.5|raw=1000", "1000", true},
		{"62.5|raw=1000", "raw=1000", true},
		{"62.5|raw=1000", "62.5", false},
		{`"a\,b"`, Text("a,b"), true},
		{`"\u00FF"`, Text("\xff"), true},
		{`"ab"`, Text("ac"), false},
	} {
		if got := Same(c.want, c.got); got != c.same {
			t.Errorf("Same(%q, %q) = %v, want %v", c.want, c.got, got, c.same)
		}
	}
}

// TestCheckJudgesEveryAnswer: the plants each answer is checked for.
func TestCheckJudgesEveryAnswer(t *testing.T) {
	read := ManifestRow{File: "r.bin", Records: 2, Landed: []Value{{"r0.v", "7"}, {"r0.f", "0.5|0x3F000000"}, {"r1.v", "-8"}}}
	refused := ManifestRow{File: "x.bin", Records: 1, Refuse: "out_of_range", Landed: []Value{{"r0.v", "100"}}}
	right := Answer{Records: 2, Values: map[string]string{"r0.v": "7", "r0.f": "0x3FE0000000000000", "r1.v": "-8"}}
	for _, c := range []struct {
		name string
		row  ManifestRow
		got  Answer
		want []string
	}{
		{"read right", read, right, nil},
		{"refusal right", refused, Answer{Refused: "out_of_range"}, nil},
		// THE SYMMETRIC PLANT: a read and a write wrong the same way round-trip
		// byte for byte; only the landed values tell
		{"value off by one", read, Answer{Records: 2, Values: map[string]string{"r0.v": "8", "r0.f": "0x3FE0000000000000", "r1.v": "-7"}},
			[]string{"landed r0.v=8; the reference wrote 7", "landed r1.v=-7; the reference wrote -8"}},
		{"value missing", read, Answer{Records: 2, Values: map[string]string{"r0.v": "7", "r1.v": "-8"}},
			[]string{"landed nothing at r0.f; the reference wrote 0.5|0x3F000000"}},
		{"records", read, Answer{Records: 1, Values: right.Values}, []string{"landed 1 records; the reference wrote 2"}},
		{"refused a read", read, Answer{Refused: "wire_damaged"}, []string{"refused wire_damaged; the reference reads it, 2 records"}},
		{"wrong refusal", refused, Answer{Refused: "wire_damaged"}, []string{"refused wire_damaged; the reference refuses the whole load out_of_range"}},
		{"landed a refusal", refused, Answer{Records: 1, Values: map[string]string{"r0.v": "100"}}, []string{"landed 1 records; the reference refuses the whole load out_of_range"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			a := &Answers{}
			got := a.Check(c.row, c.got)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("Check = %q, want %q", got, c.want)
			}
			if a.Count() != 1 {
				t.Fatalf("the answer was not recorded: %d answered", a.Count())
			}
		})
	}
}

func TestEveryRootHoldsTheAnswersToTheManifest(t *testing.T) {
	corpus := writeManifest(t, "file=a.bin records=1 values=r0.x=1\nfile=b.bin records=1 refuse=out_of_range values=r0.x=2\nfile=c.bin records=1 values=r0.x=3\n")
	answer := func(a *Answers, rows []ManifestRow, skip string) {
		for _, r := range rows {
			if r.File == skip {
				continue
			}
			if r.Refuse != "" {
				a.Check(r, Answer{Refused: r.Refuse})
				continue
			}
			a.Check(r, Answer{Records: 1, Values: map[string]string{"r0.x": r.Landed[0].Text}})
		}
	}
	t.Run("every root", func(t *testing.T) {
		f := &fakeTB{name: "TestEveryRootControl"}
		a, rows := EveryRoot(f, "every-root-control", corpus)
		answer(a, rows, "")
		f.end()
		if len(f.errors) != 0 || f.fatal != "" {
			t.Fatalf("errors %q %q", f.errors, f.fatal)
		}
		recs := recorded("every-root-control")
		if len(recs) != 1 || recs[0] != (Record{Leg: "every-root-control", Test: "TestEveryRootControl", Answered: 3, Manifest: 3, Reads: 2, Refusals: 1, Compared: 2}) {
			t.Fatalf("record %+v", recs)
		}
	})
	// THE PLANT: one root dropped from the answers is red by its name, and
	// the walk leaves no record
	t.Run("dropped root", func(t *testing.T) {
		f := &fakeTB{name: "TestDropped"}
		a, rows := EveryRoot(f, "every-root-dropped", corpus)
		answer(a, rows, "b.bin")
		f.end()
		if len(f.errors) != 2 || !strings.Contains(f.errors[0], "corpus root b.bin") || !strings.Contains(f.errors[1], "answered 2 of the manifest's 3") {
			t.Fatalf("errors %q", f.errors)
		}
		if recs := recorded("every-root-dropped"); len(recs) != 0 {
			t.Fatalf("an incomplete walk left a record: %+v", recs)
		}
	})
	t.Run("answer the manifest lacks", func(t *testing.T) {
		f := &fakeTB{name: "TestUnknown"}
		a, rows := EveryRoot(f, "every-root-unknown", corpus)
		answer(a, rows, "")
		a.Check(ManifestRow{File: "z.bin", Refuse: "x"}, Answer{Refused: "x"})
		f.end()
		if len(f.errors) != 2 || !strings.Contains(f.errors[0], "z.bin, which the corpus manifest does not carry") {
			t.Fatalf("errors %q", f.errors)
		}
	})
	t.Run("no manifest", func(t *testing.T) {
		f := &fakeTB{name: "TestNoManifest"}
		EveryRoot(f, "every-root-none", t.TempDir())
		if !strings.Contains(f.fatal, "manifest") {
			t.Fatalf("fatal %q", f.fatal)
		}
	})
}

// TestMainRequiresTheRecordOfThisRun is Main's decision: a selected test, with
// the slow half on, that left no complete record is red; the record must be
// the named test's own.
func TestMainRequiresTheRecordOfThisRun(t *testing.T) {
	full := Record{Leg: "lua", Test: "TestWalk", Answered: 105, Manifest: 105, Reads: 82, Refusals: 23, Compared: 676}
	for _, c := range []struct {
		name           string
		selected, slow bool
		recs           []Record
		fail           bool
		says           string
	}{
		{"recorded", true, true, []Record{full}, false, "answered 105 of the manifest's 105"},
		// THE HOLLOW PLANT: the test ran and never reached EveryRoot
		{"never called", true, true, nil, true, "left no every-root record"},
		{"another test's record", true, true, []Record{{Leg: "lua", Test: "TestOther", Answered: 105, Manifest: 105}}, true, "left no every-root record"},
		{"short walk", true, true, []Record{{Leg: "lua", Test: "TestWalk", Answered: 104, Manifest: 105}}, true, "left no every-root record"},
		{"not selected", false, true, nil, false, "is not selected in this run"},
		{"slow gate off", true, false, nil, false, "SCHEMA_SLOW is not set"},
	} {
		t.Run(c.name, func(t *testing.T) {
			says, fail := judge("lua", "TestWalk", c.selected, c.slow, c.recs)
			if fail != c.fail || !strings.Contains(says, c.says) {
				t.Fatalf("judge = %q, %v; want fail=%v saying %q", says, fail, c.fail, c.says)
			}
		})
	}
}

func TestSelectsMatchesGoTestSelection(t *testing.T) {
	for _, c := range []struct {
		run, skip string
		want      bool
	}{
		{"", "", true},
		{"^TestWalk$", "", true},
		{"^TestOther$", "", false},
		{"Walk/sub", "", true},
		{"", "^TestStrict$|^TestJson$", true},
		{"", "^TestWalk$", false},
		{"", "TestWalk/fx1", true}, // skips a subtest, the test still runs
	} {
		if got := selects(c.run, c.skip, "TestWalk"); got != c.want {
			t.Errorf("selects(%q, %q) = %v, want %v", c.run, c.skip, got, c.want)
		}
	}
}
