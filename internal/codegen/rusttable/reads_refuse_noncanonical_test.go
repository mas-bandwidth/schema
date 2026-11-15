package rusttable

// THE SHARED NON-CANONICAL VECTORS, RUST (docs/SPEC-TABLES.md §3, §3.1, §3.3,
// §4; docs/rulings.json reads-refuse-non-canonical): a read of the variable
// form or the message form refuses a file or batch that decodes to a good
// value but that no conforming writer of a lawful lineage version produces.
// Every row of testdata/wire/non-canonical/MANIFEST.tsv is generated for this
// leg and run through the read entries the leg has for its form: the plain
// load, the retaining load, the load measure where it answers a verdict, the
// message batch load, and AnnounceRead for the announcement. A canonical row
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
		var body strings.Builder
		for i, row := range unitRows {
			wire, err := os.ReadFile(filepath.Join(vectors, row.file))
			if err != nil {
				t.Fatalf("%s: %v", row.file, err)
			}
			fmt.Fprintf(&body, "const WIRE_%d: [u8; %d] = [%s];\n", i, len(wire), rustBytes(wire))
		}
		var announcements = map[string]int{}
		for i, row := range unitRows {
			if row.form != "announcement" {
				continue
			}
			wire, err := os.ReadFile(filepath.Join(vectors, row.file))
			if err != nil {
				t.Fatalf("%s: %v", row.file, err)
			}
			body.WriteString(fmt.Sprintf("const ANN_%d: [u8; %d] = [%s];\n", i, len(wire), rustBytes(wire)))
			announcements[row.unitDir] = i
		}
		for i, row := range unitRows {
			root := ir.RustSnake(row.root)
			switch row.form {
			case "variable":
				fmt.Fprintf(&body, `
{
    let wire = &WIRE_%d[..];
    let mut report = TableReport::default();
    match %s_load(wire, &mut report) {
        Some(v) if !report.damaged => {
            let saved = %s_save(&v);
            println!("%s plain read %s", if saved == wire { "same" } else { "diff" });
        }
        _ => println!("%s plain refused"),
    }
    let mut retain = TableRetain { bytes: Vec::with_capacity(1 << 16), ids: Vec::with_capacity(256), nodes: Default::default() };
    let mut rreport = TableReport::default();
    match %s_load_retain(wire, &mut retain, &mut rreport) {
        Some(v) if !rreport.damaged => {
            let saved = %s_save_retain(&v, &mut retain, &mut TableReport::default());
            println!("%s retain read %s", if saved == wire { "same" } else { "diff" });
        }
        _ => println!("%s retain refused"),
    }
    println!("%s measure %s", if %s_load_measure(wire) > 0 { "read" } else { "refused" });
}
`, i, root, root, row.spelling, row.spelling, root, root, row.spelling, row.spelling, row.spelling, root)
			case "message":
				ann, ok := announcements[row.unitDir]
				if !ok {
					t.Fatalf("%s: no announcement row for %s", row.file, row.unitDir)
				}
				fmt.Fprintf(&body, `
{
    let wire = &WIRE_%d[..];
    let mut storage = vec![TableMessageEntry::default(); 64];
    let mut vocab = TableVocabulary::new(&mut storage);
    let mut arep = TableReport::default();
    let announced = announce_read(&mut vocab, &ANN_%d, &mut arep);
    let mut values = vec![%s_message_default(); 1];
    let mut report = TableReport::default();
    if !announced || arep.damaged {
        println!("%s message refused");
    } else {
        let (count, ok) = %s_load_messages(&mut values, &vocab, wire, &mut report);
        if ok && !report.damaged {
            let need = %s_measure_messages(&values[..count], &mut TableReport::default());
            let mut buf = vec![0u8; need.max(0) as usize];
            let wrote = %s_save_messages(&values[..count], &mut buf, &mut TableReport::default());
            println!("%s message read %s", if wrote >= 0 && &buf[..wrote as usize] == wire { "same" } else { "diff" });
        } else {
            println!("%s message refused");
        }
    }
}
`, i, ann, root, row.spelling, root, root, root, row.spelling, row.spelling)
			case "announcement":
				fmt.Fprintf(&body, `
{
    let mut storage = vec![TableMessageEntry::default(); 64];
    let mut vocab = TableVocabulary::new(&mut storage);
    let mut report = TableReport::default();
    let ok = announce_read(&mut vocab, &WIRE_%d, &mut report);
    println!("%s announce %s", if ok && !report.damaged { "read" } else { "refused" });
}
`, i, row.spelling)
			default:
				t.Fatalf("MANIFEST.tsv: form %q", row.form)
			}
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
		for _, row := range unitRows {
			var entries []string
			switch row.form {
			case "variable":
				entries = []string{"plain", "retain", "measure"}
			case "message":
				entries = []string{"message"}
			case "announcement":
				entries = []string{"announce"}
			}
			for _, entry := range entries {
				answer, ok := got[row.spelling+" "+entry]
				if !ok {
					t.Errorf("%s %s: the probe printed no answer", row.file, entry)
					continue
				}
				if answer != row.verdict && !(row.verdict == "read" && entry != "measure" && answer == "same") {
					t.Errorf("%s %s: verdict %q, the manifest says %q", row.file, entry, answer, row.verdict)
					continue
				}
				if row.verdict == "read" && (entry == "plain" || entry == "retain" || entry == "message") && answer != "same" {
					t.Errorf("%s %s: canonical read did not re-save the same bytes (%q)", row.file, entry, answer)
				}
			}
		}
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
