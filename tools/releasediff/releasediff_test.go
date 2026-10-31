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
