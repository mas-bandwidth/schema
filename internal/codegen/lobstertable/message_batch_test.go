package lobstertable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobstertable"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// THE MESSAGE BATCH, HELD TO THE GO ENGINE (docs/SPEC-TABLES.md §3.3, §6.5).
// TestLobsterMessageForm compares every manifest message row at the full batch
// capacity; these tests take the same accepted rows and vary what the read is
// given: a batch capacity one short of the count, exact and one over (the
// caller's capacity bound and its batch_too_large count), and damaged bytes —
// every truncation of the wire and a trailing byte past the padding (the batch
// is accepted whole or refused, with a count of zero). Each case's verdict and
// count is tablewire.DecodeMessages's for the same bytes and capacity. Read
// diagnostics stay local: only the verdict and the count are compared.

// batchCase is one read of a manifest row's bytes at one capacity.
type batchCase struct {
	row      messageRow
	label    string
	data     []byte
	capacity int
}

const messageBatchProgram = `def mbatch(root:string, ann:string, file:string, capacity:int):
    let v = schema_rt.new_vocabulary(schema_rt.message_entries_here())
    let announcement = read_file(ann)
    assert announcement
    assert schema_rt.announce_read(v, announcement, schema_rt.LoadReport {})
    let data = read_file(file)
    assert data != nil
    let rep = schema_rt.LoadReport {}
    let recs, count, ok = schema_rt.load_messages(root, v, data, rep, capacity)
    if ok:
        assert length(recs) == count
    else:
        assert length(recs) == 0
    print((if ok: "read" else: "refused") + " {count}")
`

// goBatch is the reference engine's verdict and count for one case.
func goBatch(t *testing.T, g *loadUnit, c batchCase) string {
	t.Helper()
	m := tabletext.NewModel(g.u)
	st := m.Lookup(c.row.root)
	announcement, err := os.ReadFile(filepath.Join(root, c.row.announcement))
	if err != nil {
		t.Fatal(err)
	}
	v := &tablewire.Vocabulary{}
	var arep tabletext.Report
	if err := v.AnnounceRead(announcement, &arep); err != nil || !v.Announced() {
		t.Fatalf("%s: the reference refuses the announcement: %v", c.row.name, err)
	}
	insts := make([]*tabletext.Instance, c.capacity)
	for i := range insts {
		insts[i] = m.New(st)
	}
	var rep tabletext.Report
	count, ok, derr := tablewire.DecodeMessages(m, insts, c.data, v, &rep)
	return fmt.Sprintf("%s %d", messageReadVerdict(ok && derr == nil), count)
}

// batchCases builds the capacity and damage cases of every accepted manifest
// row whose root carries the message form.
func batchCases(t *testing.T, g *loadUnit, rows []messageRow) []batchCase {
	t.Helper()
	m := tabletext.NewModel(g.u)
	var out []batchCase
	for _, r := range rows {
		st := m.Lookup(r.root)
		if st == nil || !lobstertable.MessageRoot(st) {
			continue
		}
		wire, err := os.ReadFile(filepath.Join(root, r.wire))
		if err != nil {
			t.Fatal(err)
		}
		full := batchCase{row: r, label: "full", data: wire, capacity: tablewire.TableMessageBatchMax}
		var count int
		var verdict string
		if _, err := fmt.Sscanf(goBatch(t, g, full), "%s %d", &verdict, &count); err != nil {
			t.Fatal(err)
		}
		if verdict != "read" {
			continue // a refused row has no capacity to vary
		}
		for _, capacity := range []int{count - 1, count, count + 1} {
			if capacity >= 0 {
				out = append(out, batchCase{row: r, label: fmt.Sprintf("capacity %d", capacity), data: wire, capacity: capacity})
			}
		}
		for n := 0; n < len(wire); n++ {
			out = append(out, batchCase{row: r, label: fmt.Sprintf("truncated to %d", n), data: wire[:n], capacity: tablewire.TableMessageBatchMax})
		}
		for _, b := range []byte{0x00, 0xff} {
			tail := append(append([]byte{}, wire...), b)
			out = append(out, batchCase{row: r, label: fmt.Sprintf("trailing %#02x", b), data: tail, capacity: tablewire.TableMessageBatchMax})
		}
	}
	return out
}

// compareBatches runs every case of every unit on both sides and returns the
// labels of the cases whose answers differ, and the number compared.
func compareBatches(t *testing.T, patch func(dir string)) (map[string]bool, int) {
	t.Helper()
	units, _ := readManifest(t)
	rows := messageRows(t)
	byUnit := map[string][]messageRow{}
	var order []string
	for _, r := range rows {
		if _, seen := byUnit[r.unit]; !seen {
			order = append(order, r.unit)
		}
		byUnit[r.unit] = append(byUnit[r.unit], r)
	}
	bad := map[string]bool{}
	compared := 0
	for _, key := range order {
		paths, err := compiler.GatherPaths(prefix(units[key]))
		if err != nil {
			t.Fatalf("unit %s: %v", key, err)
		}
		g, err := generateLoadUnit(t, paths)
		if err != nil {
			t.Fatalf("unit %s is refused: %v", key, err)
		}
		cases := batchCases(t, g, byUnit[key])
		if len(cases) == 0 {
			continue
		}
		if patch != nil {
			patch(g.dir)
		}
		inputs := map[string][]byte{}
		var b strings.Builder
		for _, m := range g.mods {
			fmt.Fprintf(&b, "import .%s\n", m)
		}
		b.WriteString("import .load\nimport .message\n")
		b.WriteString(keyedRegistrations(g.u))
		b.WriteString(messageBatchProgram)
		for i, c := range cases {
			ann, err := os.ReadFile(filepath.Join(root, c.row.announcement))
			if err != nil {
				t.Fatal(err)
			}
			inputs[fmt.Sprintf("a%d", i)] = ann
			inputs[fmt.Sprintf("w%d", i)] = c.data
			fmt.Fprintf(&b, "mbatch(%s, \"a%d\", \"w%d\", %d)\n", lobster.StringLit([]byte(c.row.root)), i, i, c.capacity)
		}
		if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		got := strings.Split(strings.TrimRight(string(lobsterrun.Run(t, g.dir, "main.lobster", inputs)), "\n"), "\n")
		if len(got) != len(cases) {
			t.Fatalf("unit %s: the program printed %d lines for %d cases", key, len(got), len(cases))
		}
		for i, c := range cases {
			name := c.row.name + " " + c.label
			if want := goBatch(t, g, c); got[i] != want {
				bad[name] = true
				if patch == nil {
					t.Errorf("%s (%s): lobster %q, the reference %q", name, key, got[i], want)
				}
			}
			compared++
		}
	}
	if compared == 0 {
		t.Fatal("no message batch case was compared: the comparison is vacuous")
	}
	return bad, compared
}

func TestLobsterMessageBatchMatchesTheGoEngine(t *testing.T) {
	t.Parallel()
	bad, compared := compareBatches(t, nil)
	if len(bad) > 0 {
		t.Fatalf("%d of %d batch cases differ from the reference", len(bad), compared)
	}
	t.Logf("%d batch cases (capacity, truncation, trailing byte) equal the reference's", compared)
}

// patchMessage replaces one anchor, present exactly once, in the emitted
// message runtime.
func patchMessage(t *testing.T, dir, anchor, with string) {
	t.Helper()
	path := filepath.Join(dir, "message.lobster")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(src), anchor); n != 1 {
		t.Fatalf("the anchor %q is in the emitted message runtime %d times, want 1", anchor, n)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(src), anchor, with, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestLobsterMessageBatchNegativeControl plants two bugs, one at a time, and
// requires exactly the cases each one reaches red: a capacity bound one too
// loose reds every one-short capacity case and nothing else, and a read that
// stops checking the bytes after the padding reds trailing-byte cases and no
// capacity case.
func TestLobsterMessageBatchNegativeControl(t *testing.T) {
	t.Parallel()
	loose, _ := compareBatches(t, func(dir string) {
		patchMessage(t, dir, "    if count > capacity:\n", "    if count > capacity + 1:\n")
	})
	if len(loose) == 0 {
		t.Fatal("a capacity bound one too loose left every batch case green")
	}
	for name := range loose {
		if !strings.Contains(name, " capacity ") {
			t.Errorf("the loose capacity bound turned %s red, not a capacity case", name)
		}
	}
	tail, _ := compareBatches(t, func(dir string) {
		patchMessage(t, dir, "    if not mb_align(bits) or bits.off != bits.n:\n", "    if not mb_align(bits):\n")
	})
	trailing := 0
	for name := range tail {
		if strings.Contains(name, " capacity ") {
			t.Errorf("the unchecked tail turned the capacity case %s red", name)
		}
		if strings.Contains(name, " trailing ") {
			trailing++
		}
	}
	if trailing == 0 {
		t.Fatal("a read that ignores the bytes after the padding left every trailing-byte case green")
	}
}
