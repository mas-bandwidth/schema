package rusttable

// THE SHARED NON-CANONICAL VECTORS, RUST (docs/SPEC-TABLES.md §3, §3.1, §3.3,
// §4; docs/rulings.json reads-refuse-non-canonical): a read of the variable
// form or the message form refuses a file or batch that decodes to a good
// value but that no conforming writer of a lawful lineage version produces.
// Every row of testdata/wire/non-canonical/MANIFEST.tsv is generated for this
// leg and run through the read entries the leg has for its form: the plain
// load, the retaining load, the load measure on a canonical row (a measure is
// a framing bound, not a verdict, §4.2), the message batch load, and
// AnnounceRead for the announcement. A canonical row
// reads and re-saves to the same bytes; every other row is refused. The
// verdict is the contract, so no row compares a refusal's reason.

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type rustNonCanonicalRow struct {
	file, form, spelling, verdict string
	unitPath, unitDir, root       string
}


func TestRustReadsRefuseNonCanonical(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "rustc (it builds and runs the generated Rust)")
	if _, err := exec.LookPath("rustc"); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and rustc is not on PATH: %v", err)
		}
		t.Skipf("rustc is not on PATH: %v", err)
	}
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
			fmt.Fprintf(&body, "const WIRE_%d: [u8; %d] = [%s];\n", i, len(wire), rustBytes(wire))
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
			// The measure is a framing bound and not a verdict
			// (docs/SPEC-TABLES.md §4.2, §6.5): a measure refusal implies a
			// Load refusal, so it is held on the canonical rows, where the
			// Load reads and the measure must answer a size.
			var entries []string
			switch row.form {
			case "variable":
				entries = []string{"plain", "retain"}
				if row.verdict == "read" {
					entries = append(entries, "measure")
				}
			case "message":
				entries = []string{"message"}
			case "announcement":
				entries = []string{"announce"}
			}
			for _, entry := range entries {
				answer, ok := got[fmt.Sprint(i)+" "+entry]
				if !ok {
					t.Errorf("%s %s: the probe printed no answer", row.file, entry)
					continue
				}
				switch {
				case row.verdict == "refused" && answer != "refused":
					t.Errorf("%s %s (%s): the read answered %q, the manifest says refused", row.file, entry, row.spelling, answer)
				case row.verdict == "read" && answer == "refused":
					t.Errorf("%s %s (%s): the read refused, the manifest says read", row.file, entry, row.spelling)
				case row.verdict == "read" && entry != "measure" && entry != "announce" && answer != "same":
					t.Errorf("%s %s: the canonical read did not re-save the same bytes (%q)", row.file, entry, answer)
				}
			}
		}
	}
}

// The probes print "<row> <entry> <answer>": refused, or read with same or
// diff for whether the read value re-saves to the input bytes.
const rustNonCanonicalVariableProbe = `
{
    let wire = &WIRE_ROW[..];
    let mut report = TableReport::default();
    match root_load(wire, &mut report) {
        Some(v) if !report.damaged => {
            let saved = root_save(&v);
            println!("ROW plain {}", if saved == wire { "same" } else { "diff" });
        }
        _ => println!("ROW plain refused"),
    }
    let mut retain = TableRetain { bytes: Vec::with_capacity(1 << 16), ids: Vec::with_capacity(256), nodes: Default::default() };
    let mut rreport = TableReport::default();
    match root_load_retain(wire, &mut retain, &mut rreport) {
        Some(v) if !rreport.damaged => {
            let saved = root_save_retain(&v, &mut retain, &mut TableReport::default());
            println!("ROW retain {}", if saved == wire { "same" } else { "diff" });
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
    let mut values = vec![root_message_default(); 4];
    let mut report = TableReport::default();
    if !announced || arep.damaged {
        println!("ROW message refused");
    } else {
        let (count, ok) = root_load_messages(&mut values, &vocab, wire, &mut report);
        if ok && !report.damaged {
            let need = root_measure_messages(&values[..count], &mut TableReport::default());
            let mut buf = vec![0u8; need.max(0) as usize];
            let wrote = root_save_messages(&values[..count], &mut buf, &mut TableReport::default());
            println!("ROW message {}", if wrote >= 0 && &buf[..wrote as usize] == wire { "same" } else { "diff" });
        } else {
            println!("ROW message refused");
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
