package rusttable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestRustWideBlobFixedLoader pins the Rust leg's fixed loader for a declared
// fixed table past §3.4's ceiling (docs/SPEC-TABLES.md §3.4,
// docs/FIXED-FORM-ALGORITHM.md §5.9 #48): tables/examples' WideBlob, whose
// record body is 280012 bytes. Such a table has no fixed form — no block,
// plan, lineage or record size is emitted for it, and no lineage entry handed
// in at compile is consulted — but it is still owed a fixed loader, and that
// loader is the strict read at its fixed root (one read: the whole value or
// a refusal). It refuses form 3 as layout_record_too_large, refuses the older
// forms by name as at every fixed root, treats an empty file as damage that
// resets the destination, and never selects the variable form. A named
// refusal leaves the caller's values untouched.
func TestRustWideBlobFixedLoader(t *testing.T) {
	t.Parallel()

	// The Go-side surface, before any Rust runs.
	files, err := Generate(unitFrom(t, wideBlobFixedSchema))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	fixed, ok := files["probe_fixed.rs"]
	if !ok {
		t.Fatalf("no probe_fixed.rs: WideBlob has no fixed loader at all (modules %v)", sortedKeys(files))
	}
	text := string(fixed)
	for _, want := range []string{"pub fn wide_blob_fixed_load(", "TableFixedReason::LayoutRecordTooLarge", "TableFixedReason::PreviousForm"} {
		if !strings.Contains(text, want) {
			t.Errorf("probe_fixed.rs does not contain %q", want)
		}
	}
	for _, bad := range []string{"WIDE_BLOB_FIXED_BLOCK", "WIDE_BLOB_FIXED_PLAN", "WIDE_BLOB_FIXED_LINEAGE", "WIDE_BLOB_FIXED_RECORD_BYTES", "pub fn wide_blob_fixed_save("} {
		if strings.Contains(text, bad) {
			t.Errorf("probe_fixed.rs contains %q — a table past the ceiling has no fixed form to emit", bad)
		}
	}
	// A lineage entry handed in for WideBlob is neither an error nor data:
	// GenerateLineage must not fail over it and must not emit it.
	withLineage, err := GenerateLineage(unitFrom(t, wideBlobFixedSchema), map[string][]FixedLineageEntry{
		"WideBlob": {{Wire: 42, Layout: []byte{1, 2, 3}, Record: 280020}},
	})
	if err != nil {
		t.Fatalf("a lineage entry for a table with no form must not fail the build: %v", err)
	}
	if strings.Contains(string(withLineage["probe_fixed.rs"]), "WIDE_BLOB_FIXED_LINEAGE") {
		t.Error("a lineage entry for WideBlob was emitted, though the table has no form")
	}

	out, err := runWideBlobFixedProbe(t, nil)
	if err != nil {
		t.Fatalf("the probe did not run: %v\n%s", err, out)
	}
	okLines, bad := wideBlobLines(out)
	for clause := range bad {
		t.Errorf("the probe printed WIDEBLOB %s BAD", clause)
	}
	for _, clause := range wideBlobClauses {
		if !okLines[clause] {
			t.Errorf("the probe did not print WIDEBLOB %s ok", clause)
		}
	}
}

// TestRustWideBlobFixedLoaderNegativeControl is the rule's reversed witness:
// each refusal half of the generated loader is taken out of the real
// generated Rust in turn, and the probe must go red on exactly that half's
// clause and on nothing else.
func TestRustWideBlobFixedLoaderNegativeControl(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name     string
		sabotage [2]string
		red      string
	}{
		{"ceiling", [2]string{
			"    if data[0] == TABLE_FIXED_FORM {\n        return report.refuse(TableFixedReason::LayoutRecordTooLarge);\n    }",
			"    if false && data[0] == TABLE_FIXED_FORM {\n        return report.refuse(TableFixedReason::LayoutRecordTooLarge);\n    }",
		}, "ceiling-form3"},
		{"previous-form", [2]string{
			"        return report.refuse(TableFixedReason::LayoutRecordTooLarge);\n    }\n    report.refuse(match data[0] {\n        1 => TableFixedReason::PreviousForm,",
			"        return report.refuse(TableFixedReason::LayoutRecordTooLarge);\n    }\n    report.refuse(match data[0] {\n        1 => TableFixedReason::NewerForm,",
		}, "previous-form"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			out, err := runWideBlobFixedProbe(t, func(files map[string][]byte) {
				hit := false
				for name, data := range files {
					if strings.Contains(string(data), c.sabotage[0]) {
						files[name] = []byte(strings.ReplaceAll(string(data), c.sabotage[0], c.sabotage[1]))
						hit = true
					}
				}
				if !hit {
					t.Fatalf("NEGATIVE CONTROL: the sabotage %q did not apply", c.sabotage[0])
				}
			})
			if err != nil {
				t.Fatalf("the sabotaged probe did not run: %v\n%s", err, out)
			}
			okLines, bad := wideBlobLines(out)
			if !bad[c.red] {
				t.Fatalf("NEGATIVE CONTROL FAILED: taking out the %s half left WIDEBLOB %s green\n%s", c.name, c.red, out)
			}
			for clause := range bad {
				if clause != c.red {
					t.Errorf("NEGATIVE CONTROL: taking out the %s half reddened %s outside it", c.name, clause)
				}
			}
			for _, clause := range wideBlobClauses {
				if clause != c.red && !okLines[clause] {
					t.Errorf("NEGATIVE CONTROL: taking out the %s half lost WIDEBLOB %s ok", c.name, clause)
				}
			}
		})
	}
}

var wideBlobClauses = []string{"previous-form", "message-form", "newer-form", "ceiling-form3", "empty-damage", "small-clean"}

const wideBlobFixedSchema = `package probe

fixed table WideBlob
{
    label   string(70000)
    payload bytes(70000)
    samples [..70000]uint16
}

fixed table Small
{
    keep uint32
}
`

func sortedKeys(files map[string][]byte) []string {
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// wideBlobLines splits probe output into passing clause ids and failing ids.
func wideBlobLines(out string) (ok map[string]bool, bad map[string]bool) {
	ok, bad = map[string]bool{}, map[string]bool{}
	for line := range strings.SplitSeq(out, "\n") {
		f := strings.Fields(line)
		if len(f) != 3 || f[0] != "WIDEBLOB" {
			continue
		}
		if f[2] == "ok" {
			ok[f[1]] = true
		} else {
			bad[f[1]] = true
		}
	}
	return ok, bad
}

func runWideBlobFixedProbe(t *testing.T, edit func(map[string][]byte)) (string, error) {
	t.Helper()
	slowtest.Gate(t, "rustc (it builds and runs the generated WideBlob fixed loader)")
	if _, err := exec.LookPath("rustc"); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and rustc is not on PATH: %v", err)
		}
		t.Skipf("rustc is not on PATH: %v", err)
	}
	files, err := Generate(unitFrom(t, wideBlobFixedSchema))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if edit != nil {
		edit(files)
	}
	dir := t.TempDir()
	var mods []string
	for name, data := range files {
		mod := strings.TrimSuffix(name, ".rs")
		if strings.Contains(name, "/") || !strings.HasSuffix(name, ".rs") || mod == "lib" || mod == "probe" {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		mods = append(mods, mod)
	}
	sort.Strings(mods)
	var main strings.Builder
	main.WriteString("#![allow(dead_code, unused_imports)]\n")
	for _, m := range mods {
		fmt.Fprintf(&main, "mod %s;\npub use %s::*;\n", m, m)
	}
	main.WriteString(wideBlobProbe)
	if err := os.WriteFile(filepath.Join(dir, "main.rs"), []byte(main.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "wideblob")
	build := exec.Command("rustc", "--edition", "2024", "-o", bin, "main.rs")
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("the probe did not build: %v\n%s", err, out)
	}
	run := exec.Command(bin)
	run.Dir = dir
	out, err := run.CombinedOutput()
	return string(out), err
}

const wideBlobProbe = `
fn clause(name: &str, cond: bool) {
    println!("WIDEBLOB {} {}", name, if cond { "ok" } else { "BAD" });
}

fn main() {
    let mut plan = vec![TableFixedEntry::default(); 64];
    let mut remap = vec![0u16; 64];

    // The older and neighbouring forms are refused by name, and a named
    // refusal leaves the caller's values untouched.
    for (name, form, reason) in [
        ("previous-form", 1u8, TableFixedReason::PreviousForm),
        ("message-form", 2u8, TableFixedReason::MessageFormAsFile),
        ("newer-form", 6u8, TableFixedReason::NewerForm),
    ] {
        let mut values = vec![WideBlobRow::default()];
        values[0].label[0] = 0xAB;
        let mut report = TableFixedReport::default();
        let got = wide_blob_fixed_load(&mut values, &[form], &mut plan, &mut remap, &mut report);
        clause(name, got.is_none() && report.refused && !report.damaged
            && report.reason == reason && values[0].label[0] == 0xAB);
    }

    // Form 3 itself: the record is past the ceiling, refused by that name.
    {
        let mut values = vec![WideBlobRow::default()];
        values[0].label[0] = 0xAB;
        let mut report = TableFixedReport::default();
        let got = wide_blob_fixed_load(&mut values, &[3u8], &mut plan, &mut remap, &mut report);
        clause("ceiling-form3", got.is_none() && report.refused && !report.damaged
            && report.reason == TableFixedReason::LayoutRecordTooLarge && values[0].label[0] == 0xAB);
    }

    // An empty file is damage, and damage resets every destination row.
    {
        let mut values = vec![WideBlobRow::default()];
        values[0].label[0] = 0xAB;
        let mut report = TableFixedReport::default();
        let got = wide_blob_fixed_load(&mut values, &[], &mut plan, &mut remap, &mut report);
        clause("empty-damage", got.is_none() && report.damaged && !report.refused
            && report.reason == TableFixedReason::None && values[0].label[0] == 0);
    }

    // The unit's real fixed root still reads clean, so the fixture is live.
    {
        let rows = [SmallRow { keep: 42 }];
        let mut buf = vec![0u8; small_fixed_measure(1)];
        let saved = small_fixed_save(&rows, &mut buf);
        let mut values = [SmallRow::default()];
        let mut report = TableFixedReport::default();
        let got = small_fixed_load(&mut values, &buf, &mut plan, &mut remap, &mut report);
        clause("small-clean", saved.is_some() && got == Some(1) && values[0].keep == 42
            && !report.refused && !report.damaged);
    }
}
`
