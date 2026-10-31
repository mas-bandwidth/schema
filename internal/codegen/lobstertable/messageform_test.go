package lobstertable_test

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobstertable"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// messageRow is one `message` line of the tables manifest joined to its
// `connection` line (test/conformance/README.md, "`message` IS THE ONE SURFACE
// THAT READS TWO FILES"): the unit and announcement come from the connection.
type messageRow struct {
	name, unit, root, announcement, wire string
}

func messageRows(t *testing.T) []messageRow {
	t.Helper()
	f, err := os.Open(filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	type conn struct{ unit, announcement string }
	conns := map[string]conn{}
	var lines [][]string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		x := strings.Fields(sc.Text())
		if len(x) >= 5 && x[0] == "connection" {
			conns[x[1]] = conn{x[2], x[4]}
		}
		if len(x) >= 6 && x[0] == "message" && x[5] != "-" { // a named refusal row (a variable table, §3.3) carries no message bytes
			lines = append(lines, x)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	var rows []messageRow
	for _, x := range lines {
		c, ok := conns[x[2]]
		if !ok {
			t.Fatalf("message %s names connection %s, which the manifest does not declare", x[1], x[2])
		}
		rows = append(rows, messageRow{name: x[1], unit: c.unit, root: x[3], announcement: c.announcement, wire: x[5]})
	}
	return rows
}

// messageDump is the program's message half: announce_read, the tolerant
// batch read, one value dump a body, and save_messages' bytes.
const messageDump = `def mrun(root:string, ann:string, file:string):
    if not schema_rt.is_message_root(root):
        print("absent")
        return
    let v = schema_rt.new_vocabulary(schema_rt.message_entries_here())
    let arep = schema_rt.LoadReport {}
    let announcement = read_file(ann)
    assert announcement
    let aok = schema_rt.announce_read(v, announcement, arep)
    print(report_line(arep) + " {aok}")
    let data = read_file(file)
    assert data
    let rep = schema_rt.LoadReport {}
    let recs, count, ok = schema_rt.load_messages_tolerant(root, v, data, rep, schema_rt.message_batch_max)
    print(report_line(rep) + " {count} {ok}")
    for(recs) rec:
        labels = []
        push(labels, rec)
        print(drec(rec))
    let out, n = schema_rt.save_messages(recs, schema_rt.LoadReport {})
    print("{n} " + hexs(out))
`

func messageProgram(g *loadUnit, rows []messageRow) string {
	var b strings.Builder
	for _, m := range g.mods {
		fmt.Fprintf(&b, "import .%s\n", m)
	}
	b.WriteString("import .load\nimport .message\n")
	b.WriteString(keyedRegistrations(g.u))
	b.WriteString(dumpLobster)
	b.WriteString(messageDump)
	for i, r := range rows {
		fmt.Fprintf(&b, "mrun(%s, \"a%d\", \"w%d\")\n", lobster.StringLit([]byte(r.root)), i, i)
	}
	return b.String()
}

// runMessageRows runs every row of one unit and returns each row's lines.
func runMessageRows(t *testing.T, g *loadUnit, rows []messageRow) [][]string {
	t.Helper()
	inputs := map[string][]byte{}
	for i, r := range rows {
		for name, path := range map[string]string{fmt.Sprintf("a%d", i): r.announcement, fmt.Sprintf("w%d", i): r.wire} {
			data, err := os.ReadFile(filepath.Join(root, path))
			if err != nil {
				t.Fatalf("%s: %v", r.name, err)
			}
			inputs[name] = data
		}
	}
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(messageProgram(g, rows)), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.Split(strings.TrimRight(string(lobsterrun.Run(t, g.dir, "main.lobster", inputs)), "\n"), "\n")
	var res [][]string
	at := 0
	for _, r := range rows {
		if at >= len(out) {
			t.Fatalf("%s: the program printed too few lines", r.name)
		}
		if out[at] == "absent" {
			res = append(res, []string{"absent"})
			at++
			continue
		}
		var count int
		head := strings.Fields(out[at+1])
		if len(head) != 3 {
			t.Fatalf("%s: not a batch line: %q", r.name, out[at+1])
		}
		if _, err := fmt.Sscan(head[1], &count); err != nil {
			t.Fatalf("%s: not a count: %q", r.name, head[1])
		}
		n := 2 + count + 1
		if at+n > len(out) {
			t.Fatalf("%s: the program printed too few lines", r.name)
		}
		res = append(res, out[at:at+n])
		at += n
	}
	if at != len(out) {
		t.Fatalf("the program printed %d lines, the rows account for %d", len(out), at)
	}
	return res
}

func goReportLine(rep tabletext.Report, refused bool) string {
	verdict := "read"
	if refused {
		verdict = "refused"
	}
	return fmt.Sprintf("%d,%d,%d,%d,%d,%t,%s", rep.Unknown, rep.KindMismatch, rep.Widened, rep.Clamped, rep.Duplicate, rep.Malformed, verdict)
}

// goMessageRow is the reference engine's answer for one row
// (internal/tablewire AnnounceRead, DecodeMessages, EncodeMessages), in the
// lines the program prints, and the decoded instances.
func goMessageRow(t *testing.T, g *loadUnit, r messageRow) ([]string, []*tabletext.Instance) {
	t.Helper()
	m := tabletext.NewModel(g.u)
	st := m.Lookup(r.root)
	if st == nil {
		t.Fatalf("%s: unit %s declares no %s", r.name, r.unit, r.root)
	}
	if !lobstertable.MessageRoot(st) {
		return []string{"absent"}, nil
	}
	announcement, err := os.ReadFile(filepath.Join(root, r.announcement))
	if err != nil {
		t.Fatal(err)
	}
	wire, err := os.ReadFile(filepath.Join(root, r.wire))
	if err != nil {
		t.Fatal(err)
	}
	v := &tablewire.Vocabulary{}
	var arep tabletext.Report
	aerr := v.AnnounceRead(announcement, &arep)
	lines := []string{goReportLine(arep, aerr != nil) + fmt.Sprintf(" %t", aerr == nil && v.Announced())}
	insts := make([]*tabletext.Instance, tablewire.TableMessageBatchMax)
	for i := range insts {
		insts[i] = m.New(st)
	}
	var rep tabletext.Report
	count, ok, derr := tablewire.DecodeMessages(m, insts, wire, v, &rep)
	lines = append(lines, goReportLine(rep, derr != nil)+fmt.Sprintf(" %d %t", count, ok && derr == nil))
	for _, inst := range insts[:count] {
		d := &goDumper{labels: map[*tabletext.Instance]int{inst: 0}, next: 1}
		lines = append(lines, d.rec(inst))
	}
	saved, err := tablewire.EncodeMessages(m, insts[:count])
	if err != nil {
		t.Fatalf("%s: EncodeMessages: %v", r.name, err)
	}
	return append(lines, fmt.Sprintf("%d %s", len(saved), hex.EncodeToString(saved))), insts[:count]
}

// compareMessageRows runs every manifest message row on both sides and
// returns the rows whose lines differ, by name.
func compareMessageRows(t *testing.T, patch func(dir string)) (map[string]bool, map[string][]*tabletext.Instance, int) {
	t.Helper()
	units, _ := readManifest(t)
	rows := messageRows(t)
	if len(rows) == 0 {
		t.Fatal("the manifest names no message rows: the comparison is vacuous")
	}
	byUnit := map[string][]messageRow{}
	var order []string
	for _, r := range rows {
		if _, seen := byUnit[r.unit]; !seen {
			order = append(order, r.unit)
		}
		byUnit[r.unit] = append(byUnit[r.unit], r)
	}
	bad := map[string]bool{}
	decoded := map[string][]*tabletext.Instance{}
	answered := 0
	for _, key := range order {
		paths, err := compiler.GatherPaths(prefix(units[key]))
		if err != nil {
			t.Fatalf("unit %s: %v", key, err)
		}
		g, err := generateLoadUnit(t, paths)
		if err != nil {
			t.Fatalf("unit %s is refused: %v", key, err)
		}
		if patch != nil {
			patch(g.dir)
		}
		rs := byUnit[key]
		got := runMessageRows(t, g, rs)
		for i, r := range rs {
			want, insts := goMessageRow(t, g, r)
			decoded[r.name] = insts
			if strings.Join(got[i], "\n") != strings.Join(want, "\n") {
				bad[r.name] = true
				if patch == nil {
					t.Errorf("%s (%s): not the reference's answer:\nlobster: %q\ngo:      %q", r.name, r.unit, got[i], want)
				}
				continue
			}
			if want[0] == "absent" {
				continue
			}
			golden, err := os.ReadFile(filepath.Join(root, r.wire))
			if err != nil {
				t.Fatal(err)
			}
			if last := got[i][len(got[i])-1]; last != fmt.Sprintf("%d %s", len(golden), hex.EncodeToString(golden)) {
				bad[r.name] = true
				if patch == nil {
					t.Errorf("%s: the saved batch is not the message golden %s", r.name, r.wire)
				}
				continue
			}
			answered++
		}
	}
	return bad, decoded, len(rows)
}

// TestLobsterMessageForm holds the message form to the reference engine over
// every manifest message row (docs/SPEC-TABLES.md §3.3): the announcement's
// report, the batch read's report, count and verdict, every decoded body, and
// the saved batch, which must also be the row's message golden byte for byte.
// A root that is a variable table is absent on both sides (§3.3).
func TestLobsterMessageForm(t *testing.T) {
	t.Parallel()
	bad, _, total := compareMessageRows(t, nil)
	if len(bad) > 0 {
		t.Fatalf("%d of %d message rows differ from the reference", len(bad), total)
	}
	t.Logf("%d message rows equal the reference's", total)
}

// enumRides reports whether an instance carries a non-None enum value
// anywhere a body writes one, which is what the negative control's patch
// changes on the wire.
func enumRides(inst *tabletext.Instance) bool {
	for i := range inst.Fields {
		fv := &inst.Fields[i]
		cells := fv.Elems
		if len(cells) == 0 {
			cells = []tabletext.Cell{fv.Cell}
		}
		for _, c := range cells {
			if tabletext.EnumOf(fv.Def) != nil && c.U != 0 {
				return true
			}
			if c.Tab != nil && enumRides(c.Tab) {
				return true
			}
		}
	}
	return false
}

// TestLobsterMessageFormNegativeControl patches the emitted runtime in memory
// so no enum variant resolves a slot and a body carrying one refuses, matched
// exactly once,
// and requires the rows that differ from the reference to be exactly the rows
// whose decoded bodies carry a non-None enum: no fewer, no more.
func TestLobsterMessageFormNegativeControl(t *testing.T) {
	t.Parallel()
	const anchor = "        let slot = message_name_slot(variant.id)\n"
	patch := func(dir string) {
		path := filepath.Join(dir, "message.lobster")
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if n := strings.Count(string(src), anchor); n != 1 {
			t.Fatalf("the enum-write anchor is in the emitted runtime %d times, want 1", n)
		}
		patched := strings.Replace(string(src), anchor, "        let slot = message_name_slot(variant.id) * 0\n", 1)
		if err := os.WriteFile(path, []byte(patched), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	bad, decoded, _ := compareMessageRows(t, patch)
	want := map[string]bool{}
	for name, insts := range decoded {
		for _, inst := range insts {
			if enumRides(inst) {
				want[name] = true
			}
		}
	}
	if len(want) == 0 {
		t.Fatal("no message row carries an enum: the control is vacuous")
	}
	var got, exp []string
	for n := range bad {
		got = append(got, n)
	}
	for n := range want {
		exp = append(exp, n)
	}
	sort.Strings(got)
	sort.Strings(exp)
	if strings.Join(got, ",") != strings.Join(exp, ",") {
		t.Fatalf("the patch failed rows %v, want exactly the enum rows %v", got, exp)
	}
	t.Logf("the patch failed exactly the enum rows %v", got)
}
