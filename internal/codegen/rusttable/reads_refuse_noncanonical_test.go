package rusttable

// THE SHARED NON-CANONICAL VECTORS, RUST (docs/SPEC-TABLES.md §3, §3.1, §3.3,
// §4; docs/rulings.json reads-refuse-non-canonical): every row of
// testdata/wire/non-canonical/MANIFEST.tsv is generated for this leg and run
// through the read entries the leg has for its form: the plain load, the
// retaining load and the load measure for the variable form, the message batch
// load for the message form, and announce_read for the announcement. A read
// row re-saves through each entry's writer to the canonical bytes (the vector
// itself, or the canonical rewrite pinned beside it); a refused row is refused
// by every entry. A message row is also read under another build version,
// where only a union field written with arm reference 0 stays refused
// (§3.3 (c)). The verdict is the contract, so no row compares a reason.

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
	"github.com/mas-bandwidth/schema/v3/internal/tabletext"
	"github.com/mas-bandwidth/schema/v3/internal/tablewire"
	"github.com/mas-bandwidth/schema/v3/ir"
)

type rustNonCanonicalRow struct {
	file, form, spelling, verdict string
	unitPath, unitDir, root       string
}

func rustNonCanonicalToolchain(t *testing.T) {
	t.Helper()
	slowtest.Gate(t, "rustc (it builds and runs the generated Rust)")
	if _, err := exec.LookPath("rustc"); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and rustc is not on PATH: %v", err)
		}
		t.Skipf("rustc is not on PATH: %v", err)
	}
}

func TestRustReadsRefuseNonCanonical(t *testing.T) {
	t.Parallel()
	rustNonCanonicalToolchain(t)
	repo, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	vectors := filepath.Join(repo, "testdata", "wire", "non-canonical")
	rows := rustNonCanonicalRows(t, repo, vectors)
	if len(rows) == 0 {
		t.Fatal("MANIFEST.tsv names no vectors")
	}

	byUnit := map[string][]rustNonCanonicalRow{}
	for _, row := range rows {
		byUnit[row.unitPath] = append(byUnit[row.unitPath], row)
	}
	for unitPath, unitRows := range byUnit {
		files, err := Generate(generateUnit(t, repo, []string{unitPath}))
		if err != nil {
			t.Fatalf("%s: generate: %v", unitPath, err)
		}
		// Every message row is read under its own unit's announcement, the
		// announcement.bin beside its vectors.
		ann, err := os.ReadFile(filepath.Join(unitRows[0].unitDir, "announcement.bin"))
		if err != nil {
			t.Fatalf("%s: %v", unitPath, err)
		}
		var body strings.Builder
		fmt.Fprintf(&body, "const ANN: [u8; %d] = [%s];\n", len(ann), rustBytes(ann))
		for i, row := range unitRows {
			wire, err := os.ReadFile(filepath.Join(vectors, row.file))
			if err != nil {
				t.Fatalf("%s: %v", row.file, err)
			}
			exp := rustNonCanonicalExpected(t, vectors, row, wire)
			fmt.Fprintf(&body, "const WIRE_%d: [u8; %d] = [%s];\n", i, len(wire), rustBytes(wire))
			fmt.Fprintf(&body, "const EXP_%d: [u8; %d] = [%s];\n", i, len(exp), rustBytes(exp))
		}
		for i, row := range unitRows {
			var probe string
			switch row.form {
			case "variable":
				probe = rustNonCanonicalVariableProbe
			case "message":
				probe = rustNonCanonicalMessageProbe
			case "announcement":
				probe = rustNonCanonicalAnnounceProbe
			default:
				t.Fatalf("MANIFEST.tsv: form %q", row.form)
			}
			probe = strings.ReplaceAll(probe, "ROW", fmt.Sprint(i))
			probe = strings.ReplaceAll(probe, "root_", ir.RustSnake(row.root)+"_")
			body.WriteString(probe)
		}
		out, err := runRustGeneratedFilesProbe(t, files, body.String())
		if err != nil {
			t.Fatalf("%s: probe failed: %v\n%s", unitPath, err, out)
		}
		got := map[string]string{}
		for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
			f := strings.Fields(line)
			if len(f) != 3 {
				t.Fatalf("%s: the probe printed %q", unitPath, line)
			}
			got[f[0]+" "+f[1]] = f[2]
		}
		for i, row := range unitRows {
			want := map[string]string{}
			switch row.form {
			case "variable":
				if row.verdict == "read" {
					want["plain"], want["retain"], want["measure"] = "same", "same", "read"
				} else {
					want["plain"], want["retain"] = "refused", "refused"
					// The measure sizes the load from the id table and the node
					// table's framing (§4.2, §6.5): it answers the structural
					// spellings it meets, and is a bound, not a verdict, for
					// the rest.
					if row.spelling == "empty-node-table" || row.spelling == "node-table-out-of-place" {
						want["measure"] = "refused"
					}
				}
			case "message":
				if row.verdict == "read" {
					want["message"] = "same"
				} else {
					want["message"] = "refused"
				}
				// Under another build version order and the default are not
				// judged (§3.3 (a), (b)); a union field at arm reference 0 is
				// refused at every build (§3.3 (c)).
				if row.spelling == "union-arm-zero" {
					want["other"] = "refused"
				} else {
					want["other"] = "canonical"
				}
			case "announcement":
				want["announce"] = "read"
			}
			for entry, w := range want {
				answer, ok := got[fmt.Sprint(i)+" "+entry]
				if !ok {
					t.Errorf("%s %s: the probe printed no answer", row.file, entry)
					continue
				}
				if answer != w {
					t.Errorf("%s %s (%s): the read answered %q, want %q (manifest: %s)", row.file, entry, row.spelling, answer, w, row.verdict)
				}
			}
		}
	}
}

// rustNonCanonicalExpected is the canonical file a read row re-saves to: the
// vector itself, or the canonical rewrite the manifest's generator pins beside
// it for a lawful spelling no writer produces (docs/SPEC-TABLES.md §3).
func rustNonCanonicalExpected(t *testing.T, vectors string, row rustNonCanonicalRow, wire []byte) []byte {
	t.Helper()
	if row.form != "variable" || row.verdict != "read" {
		return wire
	}
	path := filepath.Join(vectors, "canonical", filepath.Base(row.file))
	if _, err := os.Stat(path); err != nil {
		if row.file != "variable-fields-out-of-order.bin" {
			return wire
		}
		path = filepath.Join(vectors, "variable-canonical.bin")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// The probes print "<row> <entry> <answer>": refused, or for a read whether
// the read value re-saves to the canonical bytes (same or diff).
const rustNonCanonicalVariableProbe = `
{
    let wire = &WIRE_ROW[..];
    let mut report = TableReport::default();
    match root_load(wire, &mut report) {
        Some(v) if !report.damaged => {
            let saved = root_save(&v);
            println!("ROW plain {}", if saved == EXP_ROW { "same" } else { "diff" });
        }
        _ => println!("ROW plain refused"),
    }
    let mut retain = TableRetain { bytes: Vec::with_capacity(1 << 16), ids: Vec::with_capacity(256), nodes: Default::default() };
    let mut rreport = TableReport::default();
    match root_load_retain(wire, &mut retain, &mut rreport) {
        Some(v) if !rreport.damaged => {
            let saved = root_save_retain(&v, &mut retain, &mut TableReport::default());
            println!("ROW retain {}", if saved == EXP_ROW { "same" } else { "diff" });
        }
        _ => println!("ROW retain refused"),
    }
    println!("ROW measure {}", if root_load_measure(wire) > 0 { "read" } else { "refused" });
}
`

const rustNonCanonicalMessageProbe = `
{
    let wire = &WIRE_ROW[..];
    let mut storage = vec![TableMessageEntry::default(); 64];
    let mut vocab = TableVocabulary::new(&mut storage);
    let mut arep = TableReport::default();
    let announced = announce_read(&mut vocab, &ANN, &mut arep);
    if !announced || arep.damaged {
        println!("ROW message refused");
        println!("ROW other refused");
    } else {
        let mut values = vec![root_message_default(); 4];
        let mut report = TableReport::default();
        let (count, ok) = root_load_messages(&mut values, &vocab, wire, &mut report);
        if ok && !report.damaged {
            let need = root_measure_messages(&values[..count], &mut TableReport::default());
            let mut buf = vec![0u8; need.max(0) as usize];
            let wrote = root_save_messages(&values[..count], &mut buf, &mut TableReport::default());
            println!("ROW message {}", if wrote >= 0 && &buf[..wrote as usize] == wire { "same" } else { "diff" });
        } else {
            println!("ROW message refused");
        }
        // ANOTHER BUILD VERSION: the batch reads, and its rewrite is a batch
        // this build reads and re-saves unchanged.
        vocab.build_version ^= 1;
        let mut values = vec![root_message_default(); 4];
        let mut report = TableReport::default();
        let (count, ok) = root_load_messages(&mut values, &vocab, wire, &mut report);
        vocab.build_version ^= 1;
        if ok && !report.damaged {
            let need = root_measure_messages(&values[..count], &mut TableReport::default());
            let mut buf = vec![0u8; need.max(0) as usize];
            let wrote = root_save_messages(&values[..count], &mut buf, &mut TableReport::default());
            let mut again = vec![root_message_default(); 4];
            let mut areport = TableReport::default();
            let (acount, aok) = if wrote > 0 { root_load_messages(&mut again, &vocab, &buf[..wrote as usize], &mut areport) } else { (0, false) };
            let mut twice = vec![0u8; buf.len()];
            let rewrote = if aok && !areport.damaged && acount == count { root_save_messages(&again[..acount], &mut twice, &mut TableReport::default()) } else { -1 };
            println!("ROW other {}", if wrote > 0 && rewrote == wrote && twice[..wrote as usize] == buf[..wrote as usize] { "canonical" } else { "noncanonical" });
        } else {
            println!("ROW other refused");
        }
    }
}
`

const rustNonCanonicalAnnounceProbe = `
{
    let mut storage = vec![TableMessageEntry::default(); 64];
    let mut vocab = TableVocabulary::new(&mut storage);
    let mut report = TableReport::default();
    let ok = announce_read(&mut vocab, &WIRE_ROW, &mut report);
    println!("ROW announce {}", if ok && !report.damaged { "read" } else { "refused" });
}
`

// TestRustMessageUnionNoneElementReads is the control for §3.3 (c): the
// refusal is of a union FIELD written with arm reference 0, never of a None
// element of an array of unions, which is positional. The engine's batch
// carrying one reads under this reader's build version and under another.
func TestRustMessageUnionNoneElementReads(t *testing.T) {
	t.Parallel()
	rustNonCanonicalToolchain(t)
	u := unitFrom(t, `package messagenone

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
	m := tabletext.NewModel(u)
	value := m.New(m.Lookup("Root"))
	var rep tabletext.Report
	if !m.Read(value, []byte(`{"tick": 1, "choices": [{}, {"item": 3}]}`), &rep) {
		t.Fatalf("value: %+v", rep)
	}
	wire, err := tablewire.EncodeMessages(m, []*tabletext.Instance{value})
	if err != nil {
		t.Fatal(err)
	}
	ann := tablewire.Announce(u)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`
const ANN: [u8; %d] = [%s];
const WIRE: [u8; %d] = [%s];
{
    let mut storage = vec![TableMessageEntry::default(); 64];
    let mut vocab = TableVocabulary::new(&mut storage);
    let mut arep = TableReport::default();
    if !announce_read(&mut vocab, &ANN, &mut arep) || arep.damaged { println!("announce refused"); }
    for other in 0..2 {
        let mut values = vec![root_message_default(); 1];
        let mut report = TableReport::default();
        let (count, ok) = root_load_messages(&mut values, &vocab, &WIRE, &mut report);
        let v = &values[0];
        println!("build{} {}", other, if ok && !report.damaged && count == 1 && v.tick == 1 && v.choices_count == 2 && v.choices[0].tag() == 0 && v.choices[1].tag() != 0 && v.choice.tag() == 0 { "read" } else { "refused" });
        vocab.build_version ^= 1;
    }
}
`, len(ann), rustBytes(ann), len(wire), rustBytes(wire))
	out, err := runRustGeneratedFilesProbe(t, files, body)
	if err != nil {
		t.Fatalf("probe failed: %v\n%s", err, out)
	}
	if strings.TrimSpace(out) != "build0 read\nbuild1 read" {
		t.Errorf("a None element of an array of unions must read at every build:\n%s", out)
	}
}

// TestRustMessageUndeclaredFieldAtOwnBuild holds §3.3 (b): under the reader's
// own build version every field the sender's record declares is one this
// record declares, so a field it does not declare is refused; under another
// build version it is a newer writer's field, skipped and counted unknown.
func TestRustMessageUndeclaredFieldAtOwnBuild(t *testing.T) {
	t.Parallel()
	rustNonCanonicalToolchain(t)
	const reader = `package messageone

fixed table One
{
    mode  uint8 = 7
    count uint8
}
`
	writer := unitFrom(t, `package messageone

fixed table One
{
    mode  uint8 = 7
    count uint8
    extra uint8
}
`)
	m := tabletext.NewModel(writer)
	value := m.New(m.Lookup("One"))
	var rep tabletext.Report
	if !m.Read(value, []byte(`{"count": 1, "extra": 5}`), &rep) {
		t.Fatalf("value: %+v", rep)
	}
	wire, err := tablewire.EncodeMessages(m, []*tabletext.Instance{value})
	if err != nil {
		t.Fatal(err)
	}
	ann := tablewire.Announce(writer)
	files, err := Generate(unitFrom(t, reader))
	if err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`
const ANN: [u8; %d] = [%s];
const WIRE: [u8; %d] = [%s];
{
    let mut storage = vec![TableMessageEntry::default(); 64];
    let mut vocab = TableVocabulary::new(&mut storage);
    let mut arep = TableReport::default();
    if !announce_read(&mut vocab, &ANN, &mut arep) || arep.damaged { println!("announce refused"); }
    for (name, build) in [("own", BUILD_VERSION), ("other", BUILD_VERSION ^ 1)] {
        vocab.build_version = build;
        let mut values = vec![one_message_default(); 1];
        let mut report = TableReport::default();
        let (count, ok) = one_load_messages(&mut values, &vocab, &WIRE, &mut report);
        println!("{} {}", name, if ok && !report.damaged && count == 1 && values[0].count == 1 && report.unknown == 1 { "read" } else { "refused" });
    }
}
`, len(ann), rustBytes(ann), len(wire), rustBytes(wire))
	out, err := runRustGeneratedFilesProbe(t, files, body)
	if err != nil {
		t.Fatalf("probe failed: %v\n%s", err, out)
	}
	if strings.TrimSpace(out) != "own refused\nother read" {
		t.Errorf("an undeclared field is refused at the reader's own build and read at another:\n%s", out)
	}
}

func rustNonCanonicalRows(t *testing.T, repo, vectors string) []rustNonCanonicalRow {
	t.Helper()
	f, err := os.Open(filepath.Join(vectors, "MANIFEST.tsv"))
	if err != nil {
		t.Fatalf("MANIFEST.tsv: %v", err)
	}
	defer f.Close()
	var rows []rustNonCanonicalRow
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cells := strings.Split(line, "\t")
		if len(cells) != 4 {
			t.Fatalf("MANIFEST.tsv: %d fields, want file, form, case, verdict: %q", len(cells), line)
		}
		if cells[0] == "file" && cells[1] == "form" && cells[2] == "case" && cells[3] == "verdict" {
			continue
		}
		row := rustNonCanonicalRow{file: cells[0], form: cells[1], spelling: cells[2], verdict: cells[3]}
		if row.verdict != "read" && row.verdict != "refused" {
			t.Fatalf("MANIFEST.tsv: verdict %q, want read or refused", row.verdict)
		}
		switch row.form {
		case "variable", "message", "announcement":
		default:
			t.Fatalf("MANIFEST.tsv: form %q, want variable, message or announcement", row.form)
		}
		if row.form == "announcement" && row.verdict != "read" {
			t.Fatalf("MANIFEST.tsv: %s: an announcement row must read", row.file)
		}
		if strings.HasPrefix(row.file, "../message-one-encoding/") {
			row.unitPath = "test/message-one-encoding/One.schema"
			row.unitDir = filepath.Join(repo, "testdata", "wire", "message-one-encoding")
			row.root = "One"
		} else {
			row.unitPath = "test/non-canonical/NonCanonical.schema"
			row.unitDir = vectors
			if row.form == "variable" {
				row.root = "Root"
			} else {
				row.root = "Message"
			}
		}
		if _, err := os.Stat(filepath.Join(vectors, row.file)); err != nil {
			t.Fatalf("MANIFEST.tsv names %s which is not on disk: %v", row.file, err)
		}
		rows = append(rows, row)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return rows
}
