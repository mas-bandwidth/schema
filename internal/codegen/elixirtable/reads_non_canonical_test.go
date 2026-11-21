package elixirtable

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
	"github.com/mas-bandwidth/schema/v3/internal/tabletext"
	"github.com/mas-bandwidth/schema/v3/internal/tablewire"
)

// elixirNonCanonicalRow is one row of testdata/wire/non-canonical/MANIFEST.tsv.
type elixirNonCanonicalRow struct {
	file     string
	form     string
	spelling string
	verdict  string
}

func elixirNonCanonicalRows(t *testing.T, vector string) []elixirNonCanonicalRow {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(vector, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []elixirNonCanonicalRow
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 4 {
			t.Fatalf("MANIFEST.tsv:%d: %d fields, want 4", i+1, len(f))
		}
		if f[0] == "file" && f[1] == "form" && f[2] == "case" && f[3] == "verdict" {
			continue
		}
		if f[3] != "read" && f[3] != "refused" {
			t.Fatalf("MANIFEST.tsv:%d: verdict %q, want read or refused", i+1, f[3])
		}
		rows = append(rows, elixirNonCanonicalRow{file: f[0], form: f[1], spelling: f[2], verdict: f[3]})
	}
	if len(rows) == 0 {
		t.Fatal("MANIFEST.tsv names no rows")
	}
	return rows
}

// elixirNonCanonicalExpected is the canonical file a read row re-saves to: the
// vector itself, or the canonical rewrite pinned beside it for a lawful
// spelling no writer produces (docs/SPEC-TABLES.md §3).
func elixirNonCanonicalExpected(t *testing.T, vector string, row elixirNonCanonicalRow) []byte {
	t.Helper()
	path := filepath.Join(vector, row.file)
	if row.form == "variable" {
		rewrite := filepath.Join(vector, "canonical", filepath.Base(row.file))
		if _, err := os.Stat(rewrite); err == nil {
			path = rewrite
		} else if row.file == "variable-fields-out-of-order.bin" {
			path = filepath.Join(vector, "variable-canonical.bin")
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// elixirNonCanonicalMeasureHolds says whether load_measure answers this row's
// verdict: it sizes the load from the id table, the node table's framing and
// the root's storage walk, so it refuses the structural spellings it meets.
func elixirNonCanonicalMeasureHolds(row elixirNonCanonicalRow) bool {
	if row.verdict == "read" {
		return true
	}
	switch row.spelling {
	case "empty-node-table", "node-table-out-of-place":
		return true
	}
	return false
}

// TestElixirReadsRefuseNonCanonical holds the Elixir variable-form and
// message-form reads to docs/SPEC-TABLES.md §3, §3.3 and docs/rulings.json
// reads-refuse-non-canonical over every row of
// testdata/wire/non-canonical/MANIFEST.tsv, through every read entry: load/2,
// variable_load/2, load_retain/4, load_measure/2 and the message load. A read
// row re-saves to the canonical bytes; a refused row is refused by every
// entry. A message row is also read under another build version, where only
// the union field written with arm reference 0 stays refused (§3.3 (c)).
func TestElixirReadsRefuseNonCanonical(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Elixir toolchain")

	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	vector := filepath.Join(root, "testdata", "wire", "non-canonical")
	rows := elixirNonCanonicalRows(t, vector)
	for _, row := range rows {
		t.Run(row.file, func(t *testing.T) {
			t.Parallel()
			data, err := os.ReadFile(filepath.Join(vector, row.file))
			if err != nil {
				t.Fatalf("MANIFEST.tsv names %s which is not on disk: %v", row.file, err)
			}
			schema := filepath.Join(root, "test", "non-canonical", "NonCanonical.schema")
			announcement := filepath.Join(vector, "announcement.bin")
			module, table := "Noncanonical", "Message"
			if strings.HasPrefix(row.file, "../message-one-encoding/") {
				schema = filepath.Join(root, "test", "message-one-encoding", "One.schema")
				announcement = filepath.Join(root, "testdata", "wire", "message-one-encoding", "announcement.bin")
				module, table = "Messageone", "One"
			}
			var probe string
			switch row.form {
			case "variable":
				if module != "Noncanonical" {
					t.Fatalf("%s: a variable row outside NonCanonical.schema", row.file)
				}
				probe = elixirNonCanonicalVariableProbe(module, data, elixirNonCanonicalExpected(t, vector, row), row.verdict == "read", elixirNonCanonicalMeasureHolds(row))
			case "message":
				ann, err := os.ReadFile(announcement)
				if err != nil {
					t.Fatal(err)
				}
				probe = elixirNonCanonicalMessageProbe(module, table, ann, data, row.verdict == "read", row.spelling != "union-arm-zero")
			case "announcement":
				if row.verdict != "read" {
					t.Fatalf("%s: an announcement row must read", row.file)
				}
				probe = elixirNonCanonicalAnnouncementProbe(module, data)
			default:
				t.Fatalf("MANIFEST.tsv: %s: unknown form %q", row.file, row.form)
			}
			out, err := runArmProbe(t, elixirBinary(t), generateElixir(t, loadUnit(t, schema)), probe)
			if err != nil || !strings.Contains(out, "ELIXIR_NON_CANONICAL_OK") || strings.Contains(out, "warning:") {
				t.Fatalf("%s (%s, want %s): %v\n%s", row.file, row.spelling, row.verdict, err, out)
			}
		})
	}
	t.Run("union None element", testElixirUnionNoneElementReads)
}

// testElixirUnionNoneElementReads is the control for §3.3 (c): the refusal is
// of a union FIELD written with arm reference 0, never of a None element of an
// array of unions, which is positional. The engine's batch carrying one reads
// under this reader's build version and under another.
func testElixirUnionNoneElementReads(t *testing.T) {
	t.Parallel()
	const schema = `package messagenone

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
`
	u := unitFrom(t, schema)
	m := tabletext.NewModel(u)
	value := m.New(m.Lookup("Root"))
	var report tabletext.Report
	if !m.Read(value, []byte(`{"tick": 1, "choices": [{}, {"item": 3}]}`), &report) {
		t.Fatalf("value: %+v", report)
	}
	batch, err := tablewire.EncodeMessage(m, value)
	if err != nil {
		t.Fatal(err)
	}
	probe := elixirNonCanonicalMessageProbe("Messagenone", "Root", tablewire.Announce(u), batch, true, true)
	out, err := runArmProbe(t, elixirBinary(t), generateElixir(t, u), probe)
	if err != nil || !strings.Contains(out, "ELIXIR_NON_CANONICAL_OK") || strings.Contains(out, "warning:") {
		t.Fatalf("a None element of an array of unions: %v\n%s", err, out)
	}
}

const elixirNonCanonicalCheck = `
check = fn
  true, _ -> :ok
  false, label -> IO.puts(:stderr, "FAIL: " <> label); Process.put(:failed, true)
end
`

const elixirNonCanonicalEnd = `
if Process.get(:failed, false), do: System.halt(1)
IO.puts("ELIXIR_NON_CANONICAL_OK")
`

func elixirNonCanonicalVariableProbe(module string, data, expected []byte, read, measureHolds bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "alias %s.Table, as: T\n", module)
	b.WriteString(elixirNonCanonicalCheck)
	fmt.Fprintf(&b, `
wire = Base.decode16!(%q, case: :lower)
expected = Base.decode16!(%q, case: :lower)
want = %t
measure_holds = %t
measure = T.load_measure("Root", wire)
if want or measure_holds do
  check.(want == (measure >= 0), "load_measure answered #{measure}")
end
for {name, result} <- [{"load", T.load("Root", wire)}, {"variable_load", T.variable_load("Root", wire)}] do
  case result do
    {:ok, v, _report} ->
      check.(want, name <> " read a non-canonical file")
      if want do
        case T.save("Root", v) do
          {:ok, out} -> check.(out == expected, name <> " save is not the canonical bytes")
          other -> check.(false, name <> " save answered " <> inspect(other))
        end
      end
    {:error, reason, _report} when is_atom(reason) ->
      check.(not want, name <> " refused a lawful file: " <> inspect(reason))
    other -> check.(false, name <> " answered " <> inspect(other))
  end
end
zero = %%{unknown: 0, widened: 0, retained: 0, retain_lost: 0, reason: "", missing_required: 0}
store = %%{records: [], used: 0, cap: 4096, ids: [], id_cap: 256}
case T.load_retain("Root", wire, store, zero) do
  {:ok, v, report, retain} ->
    check.(want, "load_retain read a non-canonical file")
    if want do
      case T.save_retain("Root", v, retain, report) do
        {:ok, out, _report, _retain} -> check.(out == expected, "save_retain is not the canonical bytes")
        other -> check.(false, "save_retain answered " <> inspect(other))
      end
    end
  {:error, reason, _report, retain} when is_atom(reason) ->
    check.(not want, "load_retain refused a lawful file: " <> inspect(reason))
    check.(retain.records == [], "a refused load_retain left the store populated")
  other -> check.(false, "load_retain answered " <> inspect(other))
end
`, hex.EncodeToString(data), hex.EncodeToString(expected), read, measureHolds)
	b.WriteString(elixirNonCanonicalEnd)
	return b.String()
}

// elixirNonCanonicalMessageProbe runs one batch through the message load bound
// to root twice: under the announcement as written, where the manifest's
// verdict holds and a read re-saves to the same bytes, and under another build
// version (docs/SPEC-TABLES.md §3.3). There a field at a default and fields out
// of order are lawful evolution and read, re-saving under that build to the
// same bytes, while this build's bound write refuses the same bodies exactly
// where this build's read refuses the batch; a union field written with arm
// reference 0 (§3.3 (c)) is refused at every build. The root-less read
// answers :no_root.
func elixirNonCanonicalMessageProbe(module, root string, announcement, data []byte, read, otherBuildRead bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "import Bitwise\nalias %s.TableMessage, as: M\n", module)
	b.WriteString(elixirNonCanonicalCheck)
	fmt.Fprintf(&b, `
announcement = Base.decode16!(%q, case: :lower)
wire = Base.decode16!(%q, case: :lower)
want = %t
other_want = %t
{:ok, announced} = M.announce_read(announcement)
check.({:error, :no_root} == M.load_messages(announced, wire), "the root-less load_messages read a batch")
{:ok, vocab} = M.for_root(announced, %q)
case M.load_messages(vocab, wire) do
  {:ok, values} ->
    check.(want, "load_messages read a non-canonical batch")
    if want do
      check.(length(values) == 1, "load_messages count")
      check.({:ok, wire} == M.save_messages(vocab, values), "save_messages is not the batch's bytes")
    end
  other ->
    check.(not want, "load_messages refused a lawful batch: " <> inspect(other))
end
other = %%{vocab | build_version: bxor(vocab.build_version, 1)}
case M.load_messages(other, wire) do
  {:ok, values} ->
    check.(other_want, "other build: load_messages read a union field at arm 0")
    if other_want do
      check.({:ok, wire} == M.save_messages(other, values), "other build: save_messages is not the batch's bytes")
      own = M.save_messages(vocab, values)
      if want do
        check.({:ok, wire} == own, "own build: save_messages is not the batch's bytes")
      else
        check.({:error, :invalid_value} == own, "own build: save_messages wrote a batch its read refuses: " <> inspect(own))
      end
    end
  refused ->
    check.(not other_want, "other build: load_messages refused a lawful batch: " <> inspect(refused))
end
`, hex.EncodeToString(announcement), hex.EncodeToString(data), read, otherBuildRead, root)
	b.WriteString(elixirNonCanonicalEnd)
	return b.String()
}

func elixirNonCanonicalAnnouncementProbe(module string, data []byte) string {
	var b strings.Builder
	fmt.Fprintf(&b, "alias %s.TableMessage, as: M\n", module)
	b.WriteString(elixirNonCanonicalCheck)
	fmt.Fprintf(&b, `
announcement = Base.decode16!(%q, case: :lower)
check.(M.announce() == announcement, "announce/0 is not the pinned announcement")
check.(M.announce_measure() == byte_size(announcement), "announce_measure/0 is not its length")
case M.announce_read(announcement) do
  {:ok, vocab} -> check.(vocab.announced, "announce_read did not set announced")
  other -> check.(false, "announce_read answered " <> inspect(other))
end
`, hex.EncodeToString(data))
	b.WriteString(elixirNonCanonicalEnd)
	return b.String()
}
