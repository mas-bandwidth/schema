package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestParseManifest pins what the table leg reads: unit lines by key, and the
// instance lines that carry text, in order.
func TestParseManifest(t *testing.T) {
	t.Parallel()
	m, err := ParseManifest("# a comment\nunit a  tables/a\nunit b x/B1.schema x/B2.schema\n" +
		"instance one a Root testdata/wire/tables/one.bin\n" +
		"instance two b Other testdata/wire/tables/two.bin no-text\n" +
		"instance three b Other testdata/wire/tables/three.bin go-refusal=x\n" +
		"report r a Root w.bin\n")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(m.Units["b"], []string{"x/B1.schema", "x/B2.schema"}) || !slices.Equal(m.Keys, []string{"a", "b"}) {
		t.Errorf("units %v keys %v", m.Units, m.Keys)
	}
	want := []Instance{{"one", "a", "Root"}, {"three", "b", "Other"}}
	if !slices.Equal(m.Instances, want) {
		t.Errorf("instances %v; want %v (no-text left out)", m.Instances, want)
	}
	for _, bad := range []string{
		"unit a\n",
		"unit a x\nunit a y\ninstance i a R w\n",
		"unit a x\ninstance i b R w\n",
		"unit a x\ninstance i a R w no-text\n",
	} {
		if _, err := ParseManifest(bad); err == nil {
			t.Errorf("ParseManifest(%q) accepted it", bad)
		}
	}
}

// TestThisRepositorysCorpus holds the tool's two corpus readings to the tree:
// the manifest parses with instances to encode, and every packet driver is the
// Makefile's own, sources and units both.
func TestThisRepositorysCorpus(t *testing.T) {
	t.Parallel()
	text, err := os.ReadFile(filepath.Join("../..", ManifestPath))
	if err != nil {
		t.Fatal(err)
	}
	m, err := ParseManifest(string(text))
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range m.Instances {
		if _, err := os.Stat(filepath.Join("../../testdata/conformance/tables/json", in.Name+".json")); err != nil {
			t.Errorf("instance %s carries text and has no JSON: %v", in.Name, err)
		}
	}
	mk, err := os.ReadFile("../../Makefile")
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range PacketDrivers {
		for _, s := range d.Sources {
			if !strings.Contains(string(mk), s) {
				t.Errorf("packet driver %s: the Makefile builds no %s", d.Name, s)
			}
			if _, err := os.Stat(filepath.Join("../..", s)); err != nil {
				t.Errorf("packet driver %s: %v", d.Name, err)
			}
		}
		for _, u := range d.Units {
			if !strings.Contains(string(mk), "generate --lang cpp --out generated/") || !strings.Contains(string(mk), strings.Join(u, " ")) {
				t.Errorf("packet driver %s: the Makefile generates no %s", d.Name, u)
			}
		}
	}
}

func TestFirstDifference(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		a, b string
		want int
	}{
		{"", "", -1},
		{"abc", "abc", -1},
		{"abc", "abd", 2},
		{"xbc", "abc", 0},
		{"ab", "abc", 2},
		{"abc", "ab", 2},
	} {
		if got := FirstDifference([]byte(c.a), []byte(c.b)); got != c.want {
			t.Errorf("FirstDifference(%q, %q) = %d; want %d", c.a, c.b, got, c.want)
		}
	}
	d := CompareBytes([]byte{1, 2, 3}, []byte{1, 2, 4, 5})
	if d == nil || d.String() != "first differs at byte 2 of 3/4: previous 0x03, new 0x04" {
		t.Errorf("CompareBytes reads %v", d)
	}
}

func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestCompareTreesBothWays pins the packet comparison: a byte names the file
// and the first byte; a file on one side only is a difference when both runs
// were complete and is passed over when one stopped early.
func TestCompareTreesBothWays(t *testing.T) {
	t.Parallel()
	base := map[string]string{"a.bin": "\x01\x02", "tables/b.bin": "\x03"}
	same := writeTree(t, base)
	if d, n, err := CompareTrees(same, writeTree(t, base), true); err != nil || d != nil || n != 2 {
		t.Fatalf("identical trees: %v, %d, %v", d, n, err)
	}
	d, _, _ := CompareTrees(same, writeTree(t, map[string]string{"a.bin": "\x01\x09", "tables/b.bin": "\x03"}), true)
	if d == nil || d.File != "a.bin" || d.Offset != 1 || d.Old != 2 || d.New != 9 {
		t.Errorf("one changed byte: %+v", d)
	}
	short := writeTree(t, map[string]string{"a.bin": "\x01\x02"})
	if d, _, _ := CompareTrees(same, short, true); d == nil || d.File != "tables/b.bin" || d.Offset != -1 {
		t.Errorf("a pin the new side stopped writing: %+v", d)
	}
	if d, n, _ := CompareTrees(same, short, false); d != nil || n != 1 {
		t.Errorf("one side stopped early: %+v, %d compared; want the common file alone, equal", d, n)
	}
}

// fake is a compiler from tables.
type fake struct {
	name   string
	ids    map[string]string // unit -> id; absent refuses
	packet map[string]string // driver -> its one pin's bytes; absent does not build
	pack   map[string]string // "instance/form" -> bytes; absent refuses
}

func (f fake) Name() string { return f.name }
func (f fake) ID(u []string) (string, error) {
	if id, ok := f.ids[strings.Join(u, " ")]; ok {
		return id, nil
	}
	return "", errors.New("schema: 1 error(s)")
}
func (f fake) Packet(d PacketDriver, tree, out string) (bool, error) {
	body, ok := f.packet[d.Name]
	if !ok {
		return false, errors.New("error: no member named SelectSecond")
	}
	p := filepath.Join(out, "testdata", "wire", d.Name+".bin")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return false, err
	}
	return true, os.WriteFile(p, []byte(body), 0o644)
}
func (f fake) Pack(u []string, root, json string, message bool) ([]byte, error) {
	form := "v"
	if message {
		form = "m"
	}
	body, ok := f.pack[strings.TrimSuffix(filepath.Base(json), ".json")+"/"+form]
	if !ok {
		return nil, errors.New("schema: unknown flag --message")
	}
	return []byte(body), nil
}

// TestJudgeComparesWireBytesUnderAnEqualId pins the decisions on both legs:
// an equal id compares, and a changed byte refuses naming the wire, the unit,
// the instance and the byte; a moved id, a refused unit, a driver that does
// not build on one side and an instance one side refuses are listed and never
// refuse.
func TestJudgeComparesWireBytesUnderAnEqualId(t *testing.T) {
	t.Parallel()
	m := &Manifest{
		Units: map[string][]string{"same": {"tables/same"}, "moved": {"tables/moved"}},
		Keys:  []string{"same", "moved"},
		Instances: []Instance{
			{"s1", "same", "Root"}, {"s2", "same", "Root"}, {"mv", "moved", "Root"},
		},
	}
	old := fake{
		name:   "v1.0.0",
		ids:    map[string]string{"examples": "0x1", "examples128": "0x2", "tables/same": "0x3", "tables/moved": "0x4"},
		packet: map[string]string{"examples": "\x01\x02", "examples128": "\x07"},
		pack:   map[string]string{"s1/v": "\x01", "s2/v": "\x02", "mv/v": "\x05"},
	}
	nw := fake{
		name:   "HEAD",
		ids:    map[string]string{"examples": "0x1", "examples128": "0x2", "tables/same": "0x3", "tables/moved": "0x5", "bench/corpus/Bench.schema": "0x6", "bench/corpus/RealWorld.schema": "0x7"},
		packet: map[string]string{"examples": "\x01\x02"},
		pack:   map[string]string{"s1/v": "\x01", "s1/m": "\x09", "s2/v": "\x02", "mv/v": "\x06"},
	}
	tree := writeTree(t, map[string]string{"test/main.cpp": "", "test/ludicrous_main.cpp": "", "test/bench/main.cpp": ""})
	r, err := Judge(old, nw, []string{tree}, m, "json", t.TempDir(), 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Differences) != 0 {
		t.Errorf("no byte differs under an equal id, and the gate refused: %q", r.Differences)
	}
	all := strings.Join(slices.Concat(r.Compared, r.Moved, r.Unreadable, r.Uncompared), "\n")
	for _, want := range []string{
		"packet wire, driver examples",
		"table forms: 2 variable-form and 0 message-form encodings",
		"tables/moved: 0x4 -> 0x5",
		"bench/corpus/Bench.schema: v1.0.0 refuses it",
		"packet examples128: no driver source builds on both sides",
		"table message form, unit same, instance s1: v1.0.0 refuses it",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("the report does not say %q:\n%s", want, all)
		}
	}

	// THE REFUSALS: one byte on each wire under an equal id.
	nw.packet["examples"] = "\x01\x03"
	nw.pack["s2/v"] = "\x08"
	r, err = Judge(old, nw, []string{tree}, m, "json", t.TempDir(), 3)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"packet wire, unit examples, instance examples.bin first differs at byte 1 of 2/2: previous 0x02, new 0x03",
		"table variable form, unit same, instance s2: first differs at byte 0 of 1/1: previous 0x02, new 0x08",
	}
	if !slices.Equal(r.Differences, want) {
		t.Errorf("differences %q; want %q", r.Differences, want)
	}
	// a driver that passes its own checks on one side only
	failing := failRun{nw}
	r2, err := Judge(old, failing, []string{tree}, m, "json", t.TempDir(), 3)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(r2.Differences, func(d string) bool {
		return strings.HasPrefix(d, "packet wire, unit examples: the driver completes against v1.0.0 and fails its own check against HEAD (FAILED: buffer[0] == 0x51")
	}) {
		t.Errorf("a one-sided driver failure is not a difference: %q", r2.Differences)
	}

	var out bytes.Buffer
	r.Print(&out, "v1.0.0", "HEAD")
	if !strings.Contains(out.String(), "  note         "+TableFormsNote+"\n") {
		t.Errorf("printed report does not carry the table-forms note:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "DIFFERENCE under an equal protocol id: "+want[0]) || !strings.Contains(out.String(), "REFUSED, 2 encoding(s)") {
		t.Errorf("printed report does not refuse by name:\n%s", out.String())
	}
}

func TestNewestRelease(t *testing.T) {
	t.Parallel()
	got, ok := NewestRelease([]string{"v2.5.0", "v2.10.0", "v2.9.9", "main-0123456789ab", "v3.0.0-rc1"})
	if !ok || got != "v2.10.0" {
		t.Errorf("NewestRelease = %q, %v; want v2.10.0", got, ok)
	}
	if _, ok := NewestRelease([]string{"main-0123456789ab"}); ok {
		t.Error("NewestRelease found a release among promotion tags only")
	}
}

// failRun is a compiler whose packet driver writes its pins and then fails
// one of its own checks.
type failRun struct{ fake }

func (f failRun) Packet(d PacketDriver, tree, out string) (bool, error) {
	ran, err := f.fake.Packet(d, tree, out)
	if !ran || err != nil {
		return ran, err
	}
	return true, errors.New("FAILED: buffer[0] == 0x51 (test/main.cpp:194)")
}

// TestJudgeRefusesARunThatComparedNothing: a run that compared no encoding of
// a wire proves nothing about it, and must not read as a clean pass (the
// finding CI-TOOLS-4). The packet wire is owed against every previous
// release; the table forms against 3.0.0 and later.
func TestJudgeRefusesARunThatComparedNothing(t *testing.T) {
	t.Parallel()
	m := &Manifest{
		Units:     map[string][]string{"same": {"tables/same"}},
		Keys:      []string{"same"},
		Instances: []Instance{{"s1", "same", "Root"}},
	}
	ids := map[string]string{"examples": "0x1", "examples128": "0x2", "bench/corpus/Bench.schema": "0x6", "bench/corpus/RealWorld.schema": "0x7", "tables/same": "0x3"}
	both := fake{
		ids:    ids,
		packet: map[string]string{"examples": "\x01", "examples128": "\x02", "bench": "\x03"},
		pack:   map[string]string{"s1/v": "\x01", "s1/m": "\x02"},
	}
	tree := writeTree(t, map[string]string{"test/main.cpp": "", "test/ludicrous_main.cpp": "", "test/bench/main.cpp": ""})
	for _, c := range []struct {
		name     string
		old, new fake
		refused  bool
		nothing  []string // a prefix of each r.Nothing line, in order
	}{
		{
			name:    "both wires compared",
			old:     fake{name: "v3.0.1", ids: both.ids, packet: both.packet, pack: both.pack},
			new:     fake{name: "HEAD", ids: both.ids, packet: both.packet, pack: both.pack},
			refused: false,
		},
		{
			name:    "the new compiler reads no unit's id",
			old:     fake{name: "v3.0.1", ids: both.ids, packet: both.packet, pack: both.pack},
			new:     fake{name: "HEAD", packet: both.packet, pack: both.pack},
			refused: true,
			nothing: []string{"table variable form:", "table message form:", "packet wire:"},
		},
		{
			name:    "no packet driver builds against the new C++",
			old:     fake{name: "v3.0.1", ids: both.ids, packet: both.packet, pack: both.pack},
			new:     fake{name: "HEAD", ids: both.ids, pack: both.pack},
			refused: true,
			nothing: []string{"packet wire:"},
		},
		{
			name:    "the new compiler packs no table instance",
			old:     fake{name: "v3.0.1", ids: both.ids, packet: both.packet, pack: both.pack},
			new:     fake{name: "HEAD", ids: both.ids, packet: both.packet},
			refused: true,
			nothing: []string{"table variable form:", "table message form:"},
		},
		{
			// Stella's control on #4462: every message-form output gone while
			// the variable form and the packets stay
			name:    "the new compiler writes no message form",
			old:     fake{name: "v3.0.1", ids: both.ids, packet: both.packet, pack: both.pack},
			new:     fake{name: "HEAD", ids: both.ids, packet: both.packet, pack: map[string]string{"s1/v": "\x01"}},
			refused: true,
			nothing: []string{"table message form:"},
		},
		{
			name:    "the new compiler writes no variable form",
			old:     fake{name: "v3.0.1", ids: both.ids, packet: both.packet, pack: both.pack},
			new:     fake{name: "HEAD", ids: both.ids, packet: both.packet, pack: map[string]string{"s1/m": "\x02"}},
			refused: true,
			nothing: []string{"table variable form:"},
		},
		{
			name:    "no table forms against a 2.x release, which owes none",
			old:     fake{name: "v2.9.0", ids: both.ids, packet: both.packet},
			new:     fake{name: "HEAD", ids: both.ids, packet: both.packet, pack: both.pack},
			refused: false,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			r, err := Judge(c.old, c.new, []string{tree}, m, "json", t.TempDir(), 3)
			if err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			r.Print(&out, c.old.name, c.new.name)
			printedRefusal := strings.Contains(out.String(), "release-differential: REFUSED")
			if printedRefusal != c.refused || r.Refused() != c.refused {
				t.Fatalf("refused=%v printed refusal=%v, want %v:\n%s", r.Refused(), printedRefusal, c.refused, out.String())
			}
			if len(r.Nothing) != len(c.nothing) {
				t.Fatalf("nothing compared %q, want lines starting %q", r.Nothing, c.nothing)
			}
			for i, p := range c.nothing {
				if !strings.HasPrefix(r.Nothing[i], p) || !strings.Contains(out.String(), "NOTHING COMPARED: "+r.Nothing[i]) {
					t.Errorf("nothing line %d is %q, want it to start %q and be printed:\n%s", i, r.Nothing[i], p, out.String())
				}
			}
		})
	}
	for prev, want := range map[string]bool{"v2.10.0": false, "v3.0.0": true, "v3.0.2": true, "v10.1.0": true, "clean": true} {
		if got := TablesOwed(prev); got != want {
			t.Errorf("TablesOwed(%q) = %v, want %v", prev, got, want)
		}
	}
}

// TestJudgeCorpus replays the committed judge corpus (testdata/judge-corpus)
// through the real Judge: the same cases `releasediff -judge-corpus` runs
// before every release differential. F-142 is three of them: two completed
// runs of one driver under an equal id, where the driver's only pin is gone
// from the new side, renamed on it, or new on it, so no file name overlaps.
// CompareTrees reported the listing difference and Judge returned at "no
// encoding in common" before recording it; the release passed on the other
// drivers. The controls beside them are the reviewer's: identical outputs
// pass, an extra pin beside a common one refuses, and two runs that stopped
// early are not held to their listing.
func TestJudgeCorpus(t *testing.T) {
	t.Parallel()
	cases, err := ReadJudgeCorpus(filepath.Join("testdata", "judge-corpus"))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"identical", "pin-removed", "pin-renamed", "pin-new-side-only", "pin-extra", "both-stopped-early"} {
		if !slices.ContainsFunc(cases, func(c JudgeCase) bool { return c.ID == id }) {
			t.Errorf("the corpus has lost the case %s", id)
		}
	}
	for _, c := range cases {
		t.Run(c.ID, func(t *testing.T) {
			t.Parallel()
			if err := RunJudgeCase(filepath.Join("testdata", "judge-corpus"), c, t.TempDir()); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestJudgeCorpusRunnerNeverPassesOnNothing: the runner over an empty corpus,
// a case folder the index does not name, a case with no verdict, and a case
// whose verdict is not the one Judge comes to are each an error.
func TestJudgeCorpusRunnerNeverPassesOnNothing(t *testing.T) {
	t.Parallel()
	plant := func(index string, listings map[string]string) string {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "CASES.tsv"), []byte(index), 0o644); err != nil {
			t.Fatal(err)
		}
		for path, body := range listings {
			p := filepath.Join(dir, filepath.FromSlash(path))
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return dir
	}
	same := "examples\ta.bin\t01\nexamples128\tw.bin\t07\nbench\tb.bin\t03\n"
	whole := map[string]string{"one/previous.tsv": same, "one/new.tsv": same}

	lines, err := RunJudgeCorpus(plant("one\tpass\tcontrol\n", whole), t.TempDir())
	if err != nil || !strings.Contains(strings.Join(lines, "\n"), "all 1 case(s)") {
		t.Fatalf("CONTROL FAILED: a corpus of one passing case: %v\n%s", err, strings.Join(lines, "\n"))
	}
	for _, tc := range []struct {
		name, index string
		listings    map[string]string
		says        string
	}{
		{"an empty corpus", "# no case\n", nil, "names no case"},
		{"no index", "", nil, "names no case"},
		{"a case folder the index does not name", "one\tpass\tcontrol\n", map[string]string{"one/previous.tsv": same, "one/new.tsv": same, "two/new.tsv": same}, "CASES.tsv does not name it: an unread case"},
		{"a case with no verdict", "one\t\tcontrol\n", whole, "a case is id, verdict"},
		{"a refusal that names nothing", "one\trefuse:\tcontrol\n", whole, "a case is id, verdict"},
		{"a case named twice", "one\tpass\ta\none\tpass\tb\n", whole, "named twice"},
		{"a case with no listing", "one\tpass\tcontrol\n", map[string]string{"one/previous.tsv": same}, "new.tsv"},
		{"a listing of no driver", "one\tpass\tcontrol\n", map[string]string{"one/previous.tsv": same, "one/new.tsv": "# nothing\n"}, "lists no driver"},
		{"a verdict Judge does not come to", "one\trefuse:packet wire, unit examples128\tcontrol\n", whole, "owes a refusal"},
		{"a pass Judge refuses", "one\tpass\tcontrol\n", map[string]string{"one/previous.tsv": same, "one/new.tsv": strings.Replace(same, "07", "08", 1)}, "owes a pass and the release was refused"},
	} {
		if _, err := RunJudgeCorpus(plant(tc.index, tc.listings), t.TempDir()); err == nil || !strings.Contains(err.Error(), tc.says) {
			t.Errorf("%s: want an error saying %q, got %v", tc.name, tc.says, err)
		}
	}
}

// TestJudgeRevisionsUnderOneWorkPath (F-216): the committed judge revisions
// (testdata/judge-revisions) are one case id in two successive states, the
// driver's only pin present on both sides and then gone from the new side,
// replayed in order under ONE work path, as `make
// release-differential-judge-corpus` does run after run in build/. Each state
// owes the verdict a fresh scratch gives it. The corpus runner kept a fixed
// folder per case id and the replayed driver only ever added files to it, so
// the pin revision 1 wrote was still there for revision 2: the owed refusal
// failed, and a wrong `pass` verdict passed.
func TestJudgeRevisionsUnderOneWorkPath(t *testing.T) {
	t.Parallel()
	dir := filepath.Join("testdata", "judge-revisions")
	revisions, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(revisions) < 2 {
		t.Fatalf("%s holds %d revision(s): successive states need two", dir, len(revisions))
	}
	// CONTROL: each revision alone, on a fresh scratch, comes to its verdict
	for _, rev := range revisions {
		if _, err := RunJudgeCorpus(filepath.Join(dir, rev.Name()), t.TempDir()); err != nil {
			t.Fatalf("CONTROL FAILED: revision %s on a fresh scratch: %v", rev.Name(), err)
		}
	}
	// THE FINDING: in order, under one work path, each still comes to its own
	work := t.TempDir()
	for _, rev := range revisions {
		if lines, err := RunJudgeCorpus(filepath.Join(dir, rev.Name()), work); err != nil {
			t.Errorf("revision %s after the one before it, under one work path: %v\n%s", rev.Name(), err, strings.Join(lines, "\n"))
		}
	}
	// and a wrong verdict on the last revision is refused there too
	last := revisions[len(revisions)-1].Name()
	wrong := t.TempDir()
	if err := os.CopyFS(wrong, os.DirFS(filepath.Join(dir, last))); err != nil {
		t.Fatal(err)
	}
	index, err := os.ReadFile(filepath.Join(wrong, "CASES.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	flipped := strings.Replace(string(index), "\trefuse:packet wire, unit examples128, instance wide.bin\t", "\tpass\t", 1)
	if flipped == string(index) {
		t.Fatalf("%s/CASES.tsv no longer owes the refusal this test flips", last)
	}
	if err := os.WriteFile(filepath.Join(wrong, "CASES.tsv"), []byte(flipped), 0o644); err != nil {
		t.Fatal(err)
	}
	work = t.TempDir()
	if _, err := RunJudgeCorpus(filepath.Join(dir, revisions[0].Name()), work); err != nil {
		t.Fatal(err)
	}
	if _, err := RunJudgeCorpus(wrong, work); err == nil || !strings.Contains(err.Error(), "owes a pass and the release was refused") {
		t.Errorf("a wrong pass verdict on revision %s, after revision %s under one work path, came out %v; want it refused", last, revisions[0].Name(), err)
	}
}

// TestJudgeRefusesAScratchAnEarlierRunWroteIn (F-216, the class): Judge reads
// every file under each side's packet output as what that side's driver
// wrote, so a work folder that already holds a packet tree would credit a
// side with files an earlier run left. Judge refuses it rather than read it.
func TestJudgeRefusesAScratchAnEarlierRunWroteIn(t *testing.T) {
	t.Parallel()
	m := &Manifest{Units: map[string][]string{}}
	side := fake{
		name:   "v1.0.0",
		ids:    map[string]string{"examples": "0x1"},
		packet: map[string]string{"examples": "\x01"},
	}
	tree := writeTree(t, map[string]string{"test/main.cpp": ""})
	// CONTROL: a fresh work folder judges
	if _, err := Judge(side, side, []string{tree}, m, "json", t.TempDir(), 1); err != nil {
		t.Fatalf("CONTROL FAILED: a fresh work folder: %v", err)
	}
	work := t.TempDir()
	stale := filepath.Join(work, "packet", "examples", "0", "new", "testdata", "wire", "left.bin")
	if err := os.MkdirAll(filepath.Dir(stale), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte{7}, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Judge(side, side, []string{tree}, m, "json", work, 1); err == nil || !strings.Contains(err.Error(), "an earlier run") {
		t.Errorf("Judge over a work folder an earlier run wrote packet output in: %v; want it refused", err)
	}
}
