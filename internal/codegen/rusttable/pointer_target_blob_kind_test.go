package rusttable

// POINTER-TARGET-BLOB-KIND, RUST (docs/SPEC-TABLES.md §3.1, §6.5;
// docs/rulings.json blob-target-outside-closure-refused): the two reserved blob
// ids are types every reader can name, whatever its root reaches. A blob record
// named from a pointer slot this reader DECLARES whose target is not that blob
// kind refuses the whole read, whether or not the root's closure has an edge of
// that kind; a blob record of a kind the root's closure lacks, reached only
// from fields this reader does not declare, is skipped and counted unknown; a
// record no pointer reaches is damage. The vectors are
// testdata/wire/pointer-target-type/blobkind; every row runs through the plain
// and the retaining read, and a read row's report is held to the manifest's
// unknown count.
//
// The negative control plants the old rule — "can name" decided from the root's
// pointer closure for blobs — in a copy of the generated reader, and holds the
// gate to go red by name on text-names-bytes: that wire must refuse, and under
// the old rule its pointer would instead read null as an unknown record.
//
// The writer half holds the leg's own save and retaining save of a Text and a
// Bin value, blob set and null, to the engine's read: each save must be
// accepted with nothing unknown, so no record rides without a pointer.

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
	"github.com/mas-bandwidth/schema/v3/internal/tabletext"
	"github.com/mas-bandwidth/schema/v3/internal/tablewire"
	"github.com/mas-bandwidth/schema/v3/ir"
)

type rustBlobKindCase struct {
	name, unit, root, file, verdict string
	unknown                         int // -1 when the row does not pin one
}

func rustBlobKindCases(t *testing.T, repo string) []rustBlobKindCase {
	t.Helper()
	path := filepath.Join(repo, "testdata", "wire", "pointer-target-type", "blobkind", "MANIFEST.tsv")
	mf, err := os.Open(path)
	if err != nil {
		t.Fatalf("blob-kind manifest: %v", err)
	}
	defer mf.Close()
	var rows []rustBlobKindCase
	sc := bufio.NewScanner(mf)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 6 {
			t.Fatalf("%s: %d fields, want 6", path, len(f))
		}
		row := rustBlobKindCase{name: f[0], unit: f[1], root: f[2], file: f[3], verdict: f[4], unknown: -1}
		if row.verdict != "read" && row.verdict != "refused" {
			t.Fatalf("%s: verdict %q, want read or refused", path, row.verdict)
		}
		if f[5] != "-" {
			n, err := strconv.Atoi(f[5])
			if err != nil {
				t.Fatalf("%s: unknown %q: %v", path, f[5], err)
			}
			row.unknown = n
		}
		rows = append(rows, row)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatalf("%s names no vectors", path)
	}
	return rows
}

// rustBlobKindAnswer is one read entry's answer for one vector.
type rustBlobKindAnswer struct {
	verdict string
	unknown int
	null    bool
}

func rustBlobKindProbeBody(t *testing.T, dir string, rows []rustBlobKindCase) string {
	t.Helper()
	var body strings.Builder
	for i, row := range rows {
		wire, err := os.ReadFile(filepath.Join(dir, row.file))
		if err != nil {
			t.Fatalf("%s: %v", row.file, err)
		}
		fmt.Fprintf(&body, "const WIRE_%d: [u8; %d] = [%s];\n", i, len(wire), rustBytes(wire))
	}
	for i, row := range rows {
		fn := ir.RustSnake(row.root)
		field := rustBlobKindRootField(t, row.root)
		fmt.Fprintf(&body, `
{
    let wire = &WIRE_%d[..];
    let mut report = TableReport::default();
    let v = %s_load(wire, &mut report);
    if v.is_some() && !report.damaged {
        let is_null = v.as_ref().is_none_or(|n| n.%s.is_none());
        println!("%s plain read {} {}", report.unknown, if is_null { "null" } else { "present" });
    } else {
        println!("%s plain refused");
    }
    let mut retain = TableRetain { bytes: Vec::with_capacity(1 << 16), ids: Vec::with_capacity(256), nodes: Default::default() };
    let mut rreport = TableReport::default();
    let rv = %s_load_retain(wire, &mut retain, &mut rreport);
    if rv.is_some() && !rreport.damaged {
        let is_null = rv.as_ref().is_none_or(|n| n.%s.is_none());
        println!("%s retain read {} {}", rreport.unknown, if is_null { "null" } else { "present" });
    } else {
        println!("%s retain refused");
    }
}
`, i, fn, field, row.name, row.name, fn, field, row.name, row.name)
	}
	return body.String()
}

// rustBlobKindRootField is the one pointer field each root of the corpus
// declares, the slot whose nullity the read answer reports.
func rustBlobKindRootField(t *testing.T, root string) string {
	t.Helper()
	switch root {
	case "Text":
		return "body"
	case "Bin":
		return "data"
	}
	t.Fatalf("the blob-kind corpus has no pointer field for root %q", root)
	return ""
}

func rustBlobKindAnswers(t *testing.T, out string) map[string]rustBlobKindAnswer {
	t.Helper()
	got := map[string]rustBlobKindAnswer{}
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		f := strings.Fields(line)
		switch {
		case len(f) == 3 && f[2] == "refused":
			got[f[0]+" "+f[1]] = rustBlobKindAnswer{verdict: "refused"}
		case len(f) == 5 && f[2] == "read":
			n, err := strconv.Atoi(f[3])
			if err != nil {
				t.Fatalf("%s: %q is not a count", line, f[3])
			}
			got[f[0]+" "+f[1]] = rustBlobKindAnswer{verdict: "read", unknown: n, null: f[4] == "null"}
		default:
			t.Fatalf("the probe printed %q", line)
		}
	}
	return got
}

func rustBlobKindProblems(rows []rustBlobKindCase, got map[string]rustBlobKindAnswer) []string {
	var problems []string
	for _, row := range rows {
		for _, entry := range []string{"plain", "retain"} {
			answer, ok := got[row.name+" "+entry]
			if !ok {
				problems = append(problems, fmt.Sprintf("%s %s: the probe printed no answer", row.name, entry))
				continue
			}
			if answer.verdict != row.verdict {
				problems = append(problems, fmt.Sprintf("%s %s: verdict %q, the manifest says %q", row.name, entry, answer.verdict, row.verdict))
				continue
			}
			if row.verdict != "read" {
				continue
			}
			if row.unknown >= 0 && answer.unknown != row.unknown {
				problems = append(problems, fmt.Sprintf("%s %s: unknown %d, the manifest says %d", row.name, entry, answer.unknown, row.unknown))
			}
			if row.name == "text-unknown-field-bytes" && !answer.null {
				problems = append(problems, fmt.Sprintf("%s %s: the pointer did not read null", row.name, entry))
			}
		}
	}
	return problems
}

// rustBlobKindRootClosureRule plants the old rule in a copy of the generated
// runtime: a blob record outside the root's pointer closure is not a type the
// reader can name, so a declared slot naming the other blob kind reads null as
// an unknown record instead of refusing.
func rustBlobKindRootClosureRule(files map[string][]byte) (map[string][]byte, bool) {
	out := maps.Clone(files)
	name := VariableRuntimeModule + ".rs"
	s := string(out[name])
	old := "if declared.contains(&tid) || tid == BYTES_TYPE_ID {"
	if !strings.Contains(s, old) {
		return out, false
	}
	out[name] = []byte(strings.Replace(s, old, "if declared.contains(&tid) {", 1))
	return out, true
}

func TestRustPointerTargetBlobKindRefused(t *testing.T) {
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

	rows := rustBlobKindCases(t, repo)
	byUnit := map[string][]rustBlobKindCase{}
	for _, row := range rows {
		byUnit[row.unit] = append(byUnit[row.unit], row)
	}
	dir := filepath.Join(repo, "testdata", "wire", "pointer-target-type", "blobkind")

	for unit, unitRows := range byUnit {
		u := vcLoadUnit(t, repo, unit)
		files, err := Generate(u)
		if err != nil {
			t.Fatalf("%s: generate: %v", unit, err)
		}
		body := rustBlobKindProbeBody(t, dir, unitRows) + rustBlobKindWriterBody()
		out, err := runRustGeneratedFilesProbe(t, files, body)
		if err != nil {
			t.Fatalf("%s: blob-kind probe failed: %v\n%s", unit, err, out)
		}
		answers := strings.Join(unitReadLines(out), "\n") + "\n"
		for _, p := range rustBlobKindProblems(unitRows, rustBlobKindAnswers(t, answers)) {
			t.Error(p)
		}

		t.Run(unit+" negative control: the root-closure rule", func(t *testing.T) {
			target := ""
			for _, row := range unitRows {
				if row.name == "text-names-bytes" {
					target = row.name
				}
			}
			if target == "" {
				t.Fatalf("%s: no text-names-bytes row to sabotage", unit)
			}
			planted, ok := rustBlobKindRootClosureRule(files)
			if !ok {
				t.Fatal("the sabotage matched nothing in the generated variable runtime")
			}
			patchedBody := rustBlobKindProbeBody(t, dir, []rustBlobKindCase{{
				name: "text-names-bytes", unit: unit, root: "Text",
				file: "text-names-bytes.bin", verdict: "refused", unknown: -1,
			}})
			pout, err := runRustGeneratedFilesProbe(t, planted, patchedBody)
			if err != nil {
				t.Fatalf("the planted reader did not build or run: %v\n%s", err, pout)
			}
			problems := rustBlobKindProblems([]rustBlobKindCase{{
				name: target, unit: unit, root: "Text",
				file: "text-names-bytes.bin", verdict: "refused", unknown: -1,
			}}, rustBlobKindAnswers(t, pout))
			if len(problems) == 0 {
				t.Error("the gate passed a reader that reads a *string slot naming a *bytes record as null")
			}
			for _, p := range problems {
				if !strings.Contains(p, target) {
					t.Errorf("the control went red on %q, not on %s", p, target)
				}
			}
		})

		t.Run(unit+" the writer emits no unreachable blob record", func(t *testing.T) {
			m := tabletext.NewModel(u)
			lines := writerSaveLines(out)
			if len(lines) != 8 {
				t.Fatalf("the probe printed %d save lines, want 8 (Text and Bin, blob set and null, plain and retaining)", len(lines))
			}
			for _, line := range lines {
				f := strings.Fields(line)
				root, entry := f[1], f[2]
				wire, err := hex.DecodeString(f[3])
				if err != nil {
					t.Fatalf("%s: %v", line, err)
				}
				st := m.Lookup(root)
				if st == nil {
					t.Fatalf("%s: no root %q", line, root)
				}
				var report tabletext.Report
				ok, err := tablewire.Decode(m, m.New(st), wire, &report)
				if !ok || err != nil || report.Refused || report.Unknown != 0 {
					t.Errorf("%s %s: the leg's own save does not read back clean: %v %+v", root, entry, err, report)
				}
			}
		})
	}
}

// unitReadLines keeps the read answers, so the writer's save lines are not
// parsed as read answers.
func unitReadLines(out string) []string {
	var lines []string
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		f := strings.Fields(line)
		if len(f) > 0 && (len(f) == 3 || len(f) == 5) && f[0] != "save" {
			lines = append(lines, line)
		}
	}
	return lines
}

func writerSaveLines(out string) []string {
	var lines []string
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		if strings.HasPrefix(line, "save ") {
			lines = append(lines, line)
		}
	}
	return lines
}

// rustBlobKindWriterBody is the writer half: the leg's own save and retaining
// save of a Text and a Bin value, blob set and null, printed as hex for the
// engine's read.
func rustBlobKindWriterBody() string {
	return `
{
    use std::rc::Rc;
    fn hex(b: &[u8]) -> String {
        let mut s = String::new();
        for x in b { s.push_str(&format!("{:02x}", x)); }
        s
    }
    let text_set = Text { body: Some(Rc::from("hello")) };
    let text_null = Text { body: None };
    let bin_set = Bin { data: Some(Rc::from(&b"hello"[..])) };
    let bin_null = Bin { data: None };
    println!("save Text plain {}", hex(&text_save(&text_set)));
    println!("save Text plain {}", hex(&text_save(&text_null)));
    println!("save Bin plain {}", hex(&bin_save(&bin_set)));
    println!("save Bin plain {}", hex(&bin_save(&bin_null)));
    let mut text_retain = TableRetain { bytes: Vec::with_capacity(1 << 16), ids: Vec::with_capacity(256), nodes: Default::default() };
    let mut text_report = TableReport::default();
    println!("save Text retain {}", hex(&text_save_retain(&text_set, &mut text_retain, &mut text_report)));
    let mut text_retain = TableRetain { bytes: Vec::with_capacity(1 << 16), ids: Vec::with_capacity(256), nodes: Default::default() };
    let mut text_report = TableReport::default();
    println!("save Text retain {}", hex(&text_save_retain(&text_null, &mut text_retain, &mut text_report)));
    let mut bin_retain = TableRetain { bytes: Vec::with_capacity(1 << 16), ids: Vec::with_capacity(256), nodes: Default::default() };
    let mut bin_report = TableReport::default();
    println!("save Bin retain {}", hex(&bin_save_retain(&bin_set, &mut bin_retain, &mut bin_report)));
    let mut bin_retain = TableRetain { bytes: Vec::with_capacity(1 << 16), ids: Vec::with_capacity(256), nodes: Default::default() };
    let mut bin_report = TableReport::default();
    println!("save Bin retain {}", hex(&bin_save_retain(&bin_null, &mut bin_retain, &mut bin_report)));
}
`
}
