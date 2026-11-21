package pythontable

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v3/internal/tabletext"
	"github.com/mas-bandwidth/schema/v3/internal/tablewire"
	"github.com/mas-bandwidth/schema/v3/ir"
)

// pyNonCanonicalProbe reads one batch through the generated message load
// three times: under the announcement as written, under the same announcement
// with its build version flipped (another build of the same shape), and the
// flipped read's rewrite back under the package's own announcement. A read
// prints the batch save_messages writes for the values; a refusal prints its
// reason.
const pyNonCanonicalProbe = pyrun.ImportPrelude + `from %[1]s import %[2]s as Root, ANNOUNCEMENT, MessageVocabulary, announce_read, load_messages, save_messages, Refused


def data(name):
    with open(name, 'rb') as f:
        return f.read()


def read(label, announcement, wire):
    v = MessageVocabulary()
    announce_read(v, announcement)
    try:
        values = load_messages(Root, v, wire)
    except Refused as e:
        print(label + '=refused:' + e.reason)
        return None
    out = save_messages(Root, values)
    print(label + '=' + out.hex())
    return out


read('written', data('announce'), data('wire'))
rewrite = read('flipped', data('flipped'), data('wire'))
if rewrite is not None:
    read('back', ANNOUNCEMENT, rewrite)
`

// pyMessageCase is one batch held to the engine: the unit the reader is
// generated from, its root, the announcement the batch was written under and
// the batch.
type pyMessageCase struct {
	name         string
	unit         *ir.Unit
	pkg, root    string
	announcement []byte
	wire         []byte
	// written and flipped are the verdicts the contract names, under the
	// announcement as written and under another build version of it; the
	// engine is held to them before the Python leg is held to the engine
	written, flipped string
	// resave is set where the announcement is the reader's own, so a read
	// re-saves to the batch's own bytes
	resave bool
}

// flipBuild is the announcement with its build version's low byte flipped:
// the same shape, announced by another build. The build version is the
// body's first field (reference 1, kind 9, eight bytes), which the id table's
// first entry names.
func flipBuild(t *testing.T, announcement []byte) []byte {
	t.Helper()
	n := len(announcement)
	if n < 19 || announcement[0] != ir.TableWireForm || announcement[1] != 1 || announcement[2] != 9 {
		t.Fatalf("the announcement's build version is not its first field: %x", announcement)
	}
	count := int(binary.LittleEndian.Uint64(announcement[n-8:]))
	first := n - 8 - count*8
	if count == 0 || first < 11 || binary.LittleEndian.Uint64(announcement[first:]) != ir.TableBuildVersionWireId {
		t.Fatalf("the announcement's first id is not the build version: %x", announcement)
	}
	out := bytes.Clone(announcement)
	out[3] ^= 1
	return out
}

// engineMessageVerdict is the Go engine's verdict on one batch.
func engineMessageVerdict(u *ir.Unit, root string, announcement, wire []byte) string {
	m := tabletext.NewModel(u)
	var v tablewire.Vocabulary
	if err := v.AnnounceRead(announcement, &tabletext.Report{}); err != nil {
		return "announcement refused: " + err.Error()
	}
	ok, err := tablewire.DecodeMessage(m, m.New(m.Lookup(root)), wire, &v, &tabletext.Report{})
	if !ok || err != nil {
		return "refused"
	}
	return "read"
}

// checkPyMessageCase runs one case through both pinned interpreters with the
// generated files given and returns each way the Python reads depart from the
// engine, named by the case. The files are passed in so a negative control can
// hand it a planted runtime.
func checkPyMessageCase(t *testing.T, files map[string][]byte, c pyMessageCase) []string {
	t.Helper()
	flipped := flipBuild(t, c.announcement)
	var problems []string
	for label, got := range map[string]string{
		"written": engineMessageVerdict(c.unit, c.root, c.announcement, c.wire),
		"flipped": engineMessageVerdict(c.unit, c.root, flipped, c.wire),
	} {
		want := c.written
		if label == "flipped" {
			want = c.flipped
		}
		if got != want {
			t.Fatalf("%s: the engine's %s verdict is %s, the contract's %s", c.name, label, got, want)
		}
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	inputs := map[string][]byte{
		"main.py":  fmt.Appendf(nil, pyNonCanonicalProbe, c.pkg, c.root),
		"announce": c.announcement,
		"flipped":  flipped,
		"wire":     c.wire,
	}
	outs := map[string]string{}
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", inputs)
		if err != nil {
			return append(problems, fmt.Sprintf("%s: the probe failed under %s: %v\n%s\n%s", c.name, pin, err, out, stderr))
		}
		outs[pin] = out
	}
	var first string
	for _, out := range outs {
		if first != "" && out != first {
			return append(problems, fmt.Sprintf("%s: the two pins disagree:\n%s\n%s", c.name, first, out))
		}
		first = out
	}
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(first), "\n") {
		if k, v, ok := strings.Cut(strings.TrimSpace(line), "="); ok {
			got[k] = v
		}
	}
	verdict := func(s string) string {
		if strings.HasPrefix(s, "refused") {
			return "refused"
		}
		if s == "" {
			return "absent"
		}
		return "read"
	}
	if v := verdict(got["written"]); v != c.written {
		problems = append(problems, fmt.Sprintf("%s: as written the Python load %s (%s), the engine %s", c.name, v, got["written"], c.written))
	} else if v == "read" && c.resave && got["written"] != hex.EncodeToString(c.wire) {
		problems = append(problems, fmt.Sprintf("%s: as written the batch read re-saves to %s, not its own %x", c.name, got["written"], c.wire))
	}
	if v := verdict(got["flipped"]); v != c.flipped {
		problems = append(problems, fmt.Sprintf("%s: under another build the Python load %s (%s), the engine %s", c.name, v, got["flipped"], c.flipped))
	} else if v == "read" && got["back"] != got["flipped"] {
		problems = append(problems, fmt.Sprintf("%s: under another build the rewrite %s reads back as %s", c.name, got["flipped"], got["back"]))
	}
	return problems
}

// pyNonCanonicalCases is every message and announcement row of
// testdata/wire/non-canonical/MANIFEST.tsv, and the four cases that hold the
// rule's edges: a None element of an array of unions reads, a field the record
// does not declare is refused at the reader's own build version and skipped at
// another, and a union field written None is refused on the skip path too.
func pyNonCanonicalCases(t *testing.T) (cases []pyMessageCase, announcements map[string][2][]byte) {
	t.Helper()
	repo := filepath.Join("..", "..", "..")
	vectors := filepath.Join(repo, "testdata", "wire", "non-canonical")
	oneDir := filepath.Join(repo, "testdata", "wire", "message-one-encoding")
	nc := loadUnitPaths(t, filepath.Join(repo, "test", "non-canonical", "NonCanonical.schema"))
	one := loadUnitPaths(t, filepath.Join(repo, "test", "message-one-encoding", "One.schema"))
	read := func(path string) []byte {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	announcements = map[string][2][]byte{}
	manifest := read(filepath.Join(vectors, "MANIFEST.tsv"))
	for i, line := range strings.Split(string(manifest), "\n") {
		line = strings.TrimRight(line, "\r")
		if i == 0 || line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cols := strings.Split(line, "\t")
		if len(cols) != 4 || (cols[3] != "read" && cols[3] != "refused") {
			t.Fatalf("MANIFEST.tsv:%d: %q", i+1, line)
		}
		if cols[1] == "variable" {
			continue
		}
		unit, pkg, root, announce := nc, "noncanonical", "Message", read(filepath.Join(vectors, "announcement.bin"))
		if strings.HasPrefix(cols[0], "../message-one-encoding/") {
			unit, pkg, root, announce = one, "messageone", "One", read(filepath.Join(oneDir, "announcement.bin"))
		}
		if !bytes.Equal(announce, tablewire.Announce(unit)) {
			t.Fatalf("MANIFEST.tsv:%d: the %s announcement is not the unit's own", i+1, pkg)
		}
		data := read(filepath.Join(vectors, cols[0]))
		switch cols[1] {
		case "announcement":
			if cols[3] != "read" {
				t.Fatalf("MANIFEST.tsv:%d: an announcement row must read", i+1)
			}
			announcements[cols[0]] = [2][]byte{data, tablewire.Announce(unit)}
		case "message":
			// another build judges neither order nor a default; the None
			// reference at a union field is refused at every build (§3.3 (c))
			flipped := "read"
			if cols[2] == "union-arm-zero" {
				flipped = "refused"
			}
			cases = append(cases, pyMessageCase{name: cols[0], unit: unit, pkg: pkg, root: root, announcement: announce, wire: data, written: cols[3], flipped: flipped, resave: true})
		default:
			t.Fatalf("MANIFEST.tsv:%d: unknown form %q", i+1, cols[1])
		}
	}
	if len(cases) == 0 || len(announcements) == 0 {
		t.Fatal("the manifest names no message or announcement row")
	}

	// A None ELEMENT of an array of unions is positional and reads, at this
	// build and another.
	elems := loadUnit(t, `package msgnoneelem
union Choice
{
    item uint8
}
fixed table Root
{
    tick    uint8
    choice  Choice
    choices [..2]Choice
}
`)
	m := tabletext.NewModel(elems)
	value := m.New(m.Lookup("Root"))
	var report tabletext.Report
	if !m.Read(value, []byte(`{"tick": 1, "choices": [{}, {"item": 3}]}`), &report) {
		t.Fatalf("value: %+v", report)
	}
	batch, err := tablewire.EncodeMessage(m, value)
	if err != nil {
		t.Fatal(err)
	}
	cases = append(cases, pyMessageCase{name: "union None element", unit: elems, pkg: "msgnoneelem", root: "Root", announcement: tablewire.Announce(elems), wire: batch, written: "read", flipped: "read", resave: true})

	// A field the record does not declare: refused under the reader's own
	// build version (the sender's announcement carrying it), an unknown
	// field skipped under another.
	sender := loadUnit(t, "package msgundeclared\nfixed table One\n{\n    first  uint8\n    hidden uint8\n    last   uint8\n}\n")
	receiver := loadUnit(t, "package msgundeclared\nfixed table One\n{\n    first uint8\n    last  uint8\n}\n")
	sm := tabletext.NewModel(sender)
	inst := sm.New(sm.Lookup("One"))
	if !sm.Read(inst, []byte(`{"first": 1, "hidden": 2, "last": 3}`), &report) {
		t.Fatalf("value: %+v", report)
	}
	undeclared, err := tablewire.EncodeMessage(sm, inst)
	if err != nil {
		t.Fatal(err)
	}
	forged := bytes.Clone(tablewire.Announce(sender))
	copy(forged[3:11], tablewire.Announce(receiver)[3:11])
	cases = append(cases, pyMessageCase{name: "undeclared field", unit: receiver, pkg: "msgundeclared", root: "One", announcement: forged, wire: undeclared, written: "refused", flipped: "read"})

	// A union field the reader cannot name, written with the None reference:
	// refused on the skip path at every build; the canonical batch reads.
	blind := loadUnit(t, "package messageone\nfixed table One\n{\n    mode  uint8 = 7\n    count uint8\n}\n")
	for _, file := range []string{"canonical.bin", "union-none.bin"} {
		verdict := "read"
		if file == "union-none.bin" {
			verdict = "refused"
		}
		cases = append(cases, pyMessageCase{name: "unknown union field " + file, unit: blind, pkg: "messageone", root: "One", announcement: read(filepath.Join(oneDir, "announcement.bin")), wire: read(filepath.Join(oneDir, file)), written: verdict, flipped: verdict})
	}
	return cases, announcements
}

// TestPythonMessageReadsRefuseNonCanonical holds the Python message load to
// docs/SPEC-TABLES.md §3.3 and docs/rulings.json message-one-encoding and
// reads-refuse-non-canonical, with the Go engine (internal/tablewire) as the
// oracle beside the shared vectors: under the reader's own build version and
// announced shape a field the record does not declare, fields out of the
// writer's declaration order and a field stored at its declared default are
// refused; under another build version they read, and the read's rewrite
// reads back to the same bytes; a union field written with arm reference 0 is
// refused at every build, while a None element of an array of unions reads.
// The refusal's reason is the leg's own diagnostic and is not compared
// (docs/rulings.json refusal-verdict-only). Each announcement row is the
// generated package's own ANNOUNCEMENT and reads.
func TestPythonMessageReadsRefuseNonCanonical(t *testing.T) {
	t.Parallel()
	cases, announcements := pyNonCanonicalCases(t)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			files, err := Generate(c.unit)
			if err != nil {
				t.Fatalf("Generate: %v", err)
			}
			for _, problem := range checkPyMessageCase(t, files, c) {
				t.Error(problem)
			}
		})
	}
	for name, pair := range announcements {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if !bytes.Equal(pair[0], pair[1]) {
				t.Fatalf("%s is not the unit's announcement", name)
			}
		})
	}
}

// TestPythonMessageReadsRefuseNonCanonicalNegativeControl plants each
// refusal's removal in a copy of the generated runtime and requires the gate
// above to go red on the case that refusal holds, by the case's name. The
// None-element control plants the opposite fault: a None element judged as a
// union field.
func TestPythonMessageReadsRefuseNonCanonicalNegativeControl(t *testing.T) {
	t.Parallel()
	cases, _ := pyNonCanonicalCases(t)
	byName := map[string]pyMessageCase{}
	for _, c := range cases {
		byName[c.name] = c
	}
	controls := []struct {
		name   string
		plants [][2]string // anchor, replacement in _schema/message.py
		red    string      // the case that must go red
	}{
		{"undeclared field", [][2]string{{"if vocab.build_version == own_version:\n                raise Refused(\"wire_damaged\", \"same-build message names an undeclared field\")", "if False:\n                raise Refused(\"wire_damaged\", \"same-build message names an undeclared field\")"}}, "undeclared field"},
		{"fields out of order", [][2]string{{"if field.ordinal <= last_ordinal:", "if False:"}}, "../message-one-encoding/fields-reordered.bin"},
		{"field at its default", [][2]string{{"if same_shape and stats[2] == substituted and field.is_default(value)", "if False and field.is_default(value)"}}, "../message-one-encoding/explicit-default.bin"},
		{"union field None on the skip path", [][2]string{{"if kind == 15 and in_field:", "if False:"}}, "unknown union field union-none.bin"},
		{"union field None", [][2]string{{"if kind == 15 and in_field:", "if False:"}, {"if in_field:\n                raise Refused(\"wire_damaged\", \"a union field explicitly carries None\")", "if False:\n                raise Refused(\"wire_damaged\", \"a union field explicitly carries None\")"}}, "../message-one-encoding/union-none.bin"},
		{"None element judged as a field", [][2]string{{"_skip_value(reader, inner, vocab, depth + 1, False, pending)\n            if reader.position == start:", "_skip_value(reader, inner, vocab, depth + 1, True, pending)\n            if reader.position == start:"}}, "union None element"},
	}
	for _, control := range controls {
		t.Run(control.name, func(t *testing.T) {
			t.Parallel()
			c, ok := byName[control.red]
			if !ok {
				t.Fatalf("no case named %q", control.red)
			}
			files, err := Generate(c.unit)
			if err != nil {
				t.Fatalf("Generate: %v", err)
			}
			name := c.pkg + "/_schema/message.py"
			src := string(files[name])
			for _, plant := range control.plants {
				if strings.Count(src, plant[0]) != 1 {
					t.Fatalf("the control lost its anchor %q", plant[0])
				}
				src = strings.Replace(src, plant[0], plant[1], 1)
			}
			files[name] = []byte(src)
			problems := checkPyMessageCase(t, files, c)
			if len(problems) == 0 {
				t.Fatalf("the planted fault left %s green", c.name)
			}
			for _, problem := range problems {
				if !strings.HasPrefix(problem, c.name+": ") {
					t.Fatalf("the planted fault went red, but not by the case's name: %s", problem)
				}
				t.Logf("red as planted: %s", problem)
			}
		})
	}
}
