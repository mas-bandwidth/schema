package baseline_test

// THE REFERENT CORPUS (F-004 to F-009, docs/SPEC-TABLES.md §18.3): every way
// a field's referent can change, each pinned against a REAL READ of a REAL OLD
// SAVE. A case is a directory under testdata/referents:
//
//	before.schema  the schema the save was written under
//	after.schema   the edited schema the check judges and the new reader reads
//	old.json       the old author's value, in the text form
//	old.save       the committed table-wire bytes the OLD writer made of it
//	new.json       what the NEW reader makes of old.save (cases that read)
//	case           finding, verdict, read (reads|refuses), unknown, roots, cook,
//	               and `names` lines: words a finding must say about the read
//
// The test holds three things to each other: the old writer still makes
// old.save byte for byte; the new reader does exactly what the case says it
// does; and the baseline's verdict PREDICTS that read — a pass is a read that
// refuses nothing and counts nothing unknown, and a read that refuses or
// loses a stored value as unknown is never a pass: a substitution refuses, and
// an edit in place the read reports warns (docs/SPEC-TABLES.md §18.3). A
// `names` line holds the finding to the words that describe that read. Each
// defect case sits beside a control that differs in the one fact the defect
// turns on. `cook same` holds the cook projection still across the edit: an
// old cook keeps opening.
//
// Regenerate old.save and new.json after a deliberate wire change with
// go test ./internal/baseline -run TestReferentCorpus -update-referents, and
// read the diff.

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/baseline"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

var updateReferents = flag.Bool("update-referents", false, "rewrite old.save and new.json in testdata/referents")

type referentCase struct {
	finding, verdict, read, cook string
	unknown                      int
	roots                        [2]string
	names                        []string
}

func readReferentCase(t *testing.T, dir string) referentCase {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "case"))
	if err != nil {
		t.Fatal(err)
	}
	var c referentCase
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			t.Fatalf("%s: unreadable case line %q", dir, line)
		}
		switch f[0] {
		case "finding":
			c.finding = f[1]
		case "verdict":
			c.verdict = f[1]
		case "read":
			c.read = f[1]
		case "cook":
			c.cook = f[1]
		case "unknown":
			n, err := strconv.Atoi(f[1])
			if err != nil {
				t.Fatalf("%s: unknown %q", dir, f[1])
			}
			c.unknown = n
		case "names":
			c.names = append(c.names, strings.TrimSpace(strings.TrimPrefix(line, "names")))
		case "roots":
			if len(f) != 3 {
				t.Fatalf("%s: roots takes two names", dir)
			}
			c.roots = [2]string{f[1], f[2]}
		default:
			t.Fatalf("%s: unknown case key %q", dir, f[0])
		}
	}
	if c.verdict == "" || c.read == "" || c.roots[0] == "" {
		t.Fatalf("%s: a case names its verdict, its read and its roots", dir)
	}
	return c
}

// strongest is the verdict a check reports: a refusal outranks a warning.
func strongest(fs []baseline.Finding) baseline.Verdict {
	v := baseline.Pass
	for _, f := range fs {
		if f.Verdict > v {
			v = f.Verdict
		}
	}
	return v
}

func canonicalJSON(t *testing.T, data []byte) string {
	t.Helper()
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, data)
	}
	out, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestReferentCorpus(t *testing.T) {
	root := filepath.Join("testdata", "referents")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("the referent corpus is empty")
	}
	findings := map[string][2]int{} // per finding: defect cases, control cases
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		t.Run(e.Name(), func(t *testing.T) {
			c := readReferentCase(t, dir)
			read := func(name string) []byte {
				t.Helper()
				data, err := os.ReadFile(filepath.Join(dir, name))
				if err != nil {
					t.Fatal(err)
				}
				return data
			}
			beforeSrc, afterSrc := string(read("before.schema")), string(read("after.schema"))
			before, after := unit(t, beforeSrc), unit(t, afterSrc)
			oldRoot, newRoot := before.Tables[c.roots[0]], after.Tables[c.roots[1]]
			if oldRoot == nil || newRoot == nil {
				t.Fatalf("roots %v are not tables of the pair", c.roots)
			}

			// THE OLD SAVE: the old writer makes exactly the committed bytes,
			// and the old reader reads them back clean — the control that the
			// save is a legitimate file and not damage
			om := tabletext.NewModel(before)
			oi := om.New(oldRoot)
			var placed tabletext.Report
			if !om.Read(oi, read("old.json"), &placed) {
				t.Fatalf("the old schema does not take old.json: %+v", placed)
			}
			saved, err := tablewire.Encode(om, oi)
			if err != nil {
				t.Fatalf("the old writer: %v", err)
			}
			if *updateReferents {
				if err := os.WriteFile(filepath.Join(dir, "old.save"), saved, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if committed := read("old.save"); !bytes.Equal(committed, saved) {
				t.Fatalf("the old writer made %x, the committed old.save is %x", saved, committed)
			}
			back := om.New(oldRoot)
			var oldReport tabletext.Report
			if ok, err := tablewire.Decode(om, back, saved, &oldReport); !ok || err != nil || oldReport.Unknown != 0 {
				t.Fatalf("the old reader does not read its own save clean: ok=%v err=%v %+v", ok, err, oldReport)
			}

			// THE NEW READ of the old save
			nm := tabletext.NewModel(after)
			ni := nm.New(newRoot)
			var report tabletext.Report
			ok, err := tablewire.Decode(nm, ni, saved, &report)
			refused := !ok || err != nil
			switch c.read {
			case "refuses":
				if !refused {
					out, _ := nm.Write(ni)
					t.Fatalf("the case says the new read refuses; it read %s (%+v)", out, report)
				}
			case "reads":
				if refused {
					t.Fatalf("the case says the new read reads; it refused: %v", err)
				}
				if report.Unknown != c.unknown {
					t.Errorf("the new read counted %d unknown, the case says %d", report.Unknown, c.unknown)
				}
				got, err := nm.Write(ni)
				if err != nil {
					t.Fatal(err)
				}
				if *updateReferents {
					if err := os.WriteFile(filepath.Join(dir, "new.json"), append(got, '\n'), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				if want := read("new.json"); canonicalJSON(t, got) != canonicalJSON(t, want) {
					t.Errorf("the new read made %s, new.json says %s", got, want)
				}
			default:
				t.Fatalf("read %q: a case reads or refuses", c.read)
			}

			// THE CHECK'S VERDICT, and that it predicts the read: a pass is a
			// read that loses nothing, and nothing that loses passes
			got := baseline.Diff(committed(t, beforeSrc), baseline.Render(after), baseline.DefaultTokenPolicy)
			want := map[string]baseline.Verdict{"pass": baseline.Pass, "warn": baseline.Warn, "refuse": baseline.Refuse}[c.verdict]
			if v := strongest(got); v != want {
				t.Errorf("the check says %s, the case says %s:%s", v, c.verdict, summary(got))
			}
			loses := refused || report.Unknown > 0
			if loses != (want != baseline.Pass) {
				t.Errorf("the verdict %s does not predict the read (refused=%v, unknown=%d):%s", c.verdict, refused, report.Unknown, summary(got))
			}
			for _, words := range c.names {
				if !find(got, want, "", words) {
					t.Errorf("no %s finding says %q about the read:%s", c.verdict, words, summary(got))
				}
			}

			// THE COOK: an edit that moves no byte moves no cook
			switch c.cook {
			case "same":
				if b, a := ir.BuildVersion(before), ir.BuildVersion(after); b != a {
					t.Errorf("the build version moved %016x -> %016x: an old cook no longer opens\n--- before ---\n%s\n--- after ---\n%s",
						b, a, ir.CookProjection(before), ir.CookProjection(after))
				}
			case "":
			default:
				t.Fatalf("cook %q: only `cook same` is a claim", c.cook)
			}

			n := findings[c.finding]
			if want == baseline.Pass {
				n[1]++
			} else {
				n[0]++
			}
			findings[c.finding] = n
		})
	}
	// every finding's defect sits beside a control
	for id, n := range findings {
		if id == "F-004" || id == "F-005" {
			continue // the defect there IS a false refusal: every case is a pass
		}
		if n[0] == 0 || n[1] == 0 {
			t.Errorf("%s has %d defect cases and %d controls: each needs both", id, n[0], n[1])
		}
	}
}
