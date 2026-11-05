// THE STRICT-ONLY CARRY: ONE READ, AND A FIXED ROOT THAT REFUSES FORM `1`.
//
// This card carries two pull requests into one branch. #2790
// (rowan/strict-only-spec) states the one-read rule in the spec pages and
// lists it in docs/rulings.json as `strict-only-read` and
// `refusal-verdict-only`, with the work list in docs/plans/strict-only.md.
// #2789 (rowan/fixed-root-refuses-form1) states that a reader that expects a
// fixed table refuses every other form, and lists it as
// `fixed-root-refuses-form1`.
//
// The rule in one line: every read returns the whole value or refuses the
// whole load; the VERDICT is the only cross-language contract; reasons and
// counters are per-language diagnostics and are never compared. So this pin
// compares no cross-language reason or counter: it reads the pages and holds six
// things — the three rulings are in docs/rulings.json, the plan is there, the
// spec states the fixed-root refusal by name, SPEC-TABLES.md names no
// `LoadTolerant` entry point as a live read, the forged writer-range rows
// refuse the whole load rather than clamp, and a declared fixed root past the
// ceiling has NO FILE FORM in the page and the engine. The behavioral pin
// checks this engine's own diagnostic names.
package compiler

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// ruling is the shape of the docs/rulings.json entry this pin reads: the id
// and the words the ruling rests on. The `applies` cells are not read; this
// pin is about the rulings themselves.
type ruling struct {
	ID     string   `json:"id"`
	Date   string   `json:"date"`
	Quotes []string `json:"quotes"`
	Source string   `json:"source"`
}

// readRulings reads docs/rulings.json and indexes its rulings by id.
func readRulings(t *testing.T) map[string]ruling {
	t.Helper()
	data, err := os.ReadFile("../docs/rulings.json")
	if err != nil {
		t.Fatalf("docs/rulings.json: %v", err)
	}
	var file struct {
		Rulings []ruling `json:"rulings"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatalf("docs/rulings.json: %v", err)
	}
	byID := make(map[string]ruling, len(file.Rulings))
	for _, r := range file.Rulings {
		byID[r.ID] = r
	}
	return byID
}

// readSpecTables reads the tables spec page.
func readSpecTables(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("../docs/SPEC-TABLES.md")
	if err != nil {
		t.Fatalf("docs/SPEC-TABLES.md: %v", err)
	}
	return string(data)
}

// tolerantEntryPoints are the entry points the tolerant read carried. The
// one-read rule retires every one of them: there is one read, and it is the
// plain name.
var tolerantEntryPoints = []string{
	"LoadTolerant",
	"LoadBuilderTolerant",
	"FromJsonTolerant",
	"FromJsonTolerantWith",
	"FixedLoadTolerant",
	"LoadMessagesTolerant",
}

// retirementPhrases mark a paragraph that RETIRES the tolerant read rather
// than naming one of its entry points as a live read.
var retirementPhrases = []string{
	"There is no",
	"are retired",
	"retired",
	"no longer",
	"overturns",
	"superseded",
	"not a dialect",
}

func TestStrictSpecCarriesOneReadAndFixedRoot(t *testing.T) {
	t.Parallel()

	t.Run("the three rulings are in docs/rulings.json", func(t *testing.T) {
		t.Parallel()
		rulings := readRulings(t)
		for _, id := range []string{
			"strict-only-read",
			"refusal-verdict-only",
			"fixed-root-refuses-form1",
		} {
			r, ok := rulings[id]
			if !ok {
				t.Errorf("docs/rulings.json carries no ruling %q", id)
				continue
			}
			if r.Date == "" {
				t.Errorf("the ruling %q carries no date", id)
			}
			if len(r.Quotes) == 0 {
				t.Errorf("the ruling %q quotes nobody", id)
			}
			if r.Source == "" {
				t.Errorf("the ruling %q carries no source", id)
			}
		}
	})

	t.Run("the plan is there", func(t *testing.T) {
		t.Parallel()
		data, err := os.ReadFile("../docs/plans/strict-only.md")
		if err != nil {
			t.Fatalf("docs/plans/strict-only.md: %v", err)
		}
		plan := string(data)
		if strings.TrimSpace(plan) == "" {
			t.Fatal("docs/plans/strict-only.md is empty")
		}
		for _, want := range []string{
			"strict-only-read",
			"refusal-verdict-only",
			"the verdict is the only cross-language contract",
		} {
			if !strings.Contains(plan, want) {
				t.Errorf("docs/plans/strict-only.md no longer states %q", want)
			}
		}
	})

	t.Run("a fixed root refuses every other form by name", func(t *testing.T) {
		t.Parallel()
		page := readSpecTables(t)
		for _, want := range []string{
			// §2: the rule, with its own heading.
			"A READER THAT EXPECTS A FIXED TABLE REFUSES EVERY OTHER FORM, BY NAME",
			// §3.4: the read side, with its own heading.
			"THE READ SIDE REFUSES THE OTHER FORM, BY NAME",
		} {
			if !strings.Contains(page, want) {
				t.Errorf("docs/SPEC-TABLES.md no longer states %q", want)
			}
		}
	})

	t.Run("the forged writer-range rows refuse the whole load", func(t *testing.T) {
		t.Parallel()
		data, err := os.ReadFile("../docs/FIXED-FORM-VERSIONING-TESTS.md")
		if err != nil {
			t.Fatalf("docs/FIXED-FORM-VERSIONING-TESTS.md: %v", err)
		}
		page := string(data)
		for _, stale := range []string{
			"clamped == 1",
			"clamped=1",
			"clamped=<n>",
		} {
			if strings.Contains(page, stale) {
				t.Errorf("docs/FIXED-FORM-VERSIONING-TESTS.md still requires the retired clamp %q; a forged value refuses the whole load", stale)
			}
		}
		// The scalar, array and unsigned lanes all stay; only the verdict changes.
		for _, file := range []string{
			"hostile_range_widen.bin",
			"hostile_range_widen_array.bin",
			"hostile_range_widen_u64.bin",
		} {
			if !strings.Contains(page, file) {
				t.Errorf("docs/FIXED-FORM-VERSIONING-TESTS.md no longer carries %s", file)
			}
		}
		if got := strings.Count(page, "refuses the whole load"); got < 3 {
			t.Errorf("docs/FIXED-FORM-VERSIONING-TESTS.md states the whole-load refusal %d time(s); the three forged range-widen vectors each need it", got)
		}
	})

	t.Run("a declared fixed root past the ceiling has no file form", func(t *testing.T) {
		t.Parallel()
		page := readSpecTables(t)
		// the page wraps at a fixed width, so a sentence is matched with its
		// line breaks folded into single spaces
		flat := strings.Join(strings.Fields(page), " ")
		for _, want := range []string{
			// §3.4's size-bound bullet.
			"A fixed table past it therefore **HAS NO FILE FORM**: its file save refuses (it does not fall back to form `1`), every read at its root refuses form `1` as at every declared fixed root",
			// §3.4's selection bullet.
			"and no file form: a read at their root refuses form `1`, and a file save of one refuses rather than falling back to form `1`",
		} {
			if !strings.Contains(flat, want) {
				t.Errorf("docs/SPEC-TABLES.md no longer states %q", want)
			}
		}
		for _, stale := range []string{
			"A fixed table past it therefore **KEEPS FORM `1`, WHICH IT NEVER LOST**",
			"no form `3` at all, so it keeps form `1`",
		} {
			if strings.Contains(flat, stale) {
				t.Errorf("docs/SPEC-TABLES.md states %q; the carried rule is that such a root has no file form", stale)
			}
		}
	})

	t.Run("fixed root ceiling behavior", func(t *testing.T) {
		t.Parallel()
		for _, body := range []int{65536, 65537} {
			t.Run(fmt.Sprint(body), func(t *testing.T) {
				t.Parallel()
				u, err := unitFromText(fmt.Sprintf("package ceiling\nfixed table Root { payload bytes(%d) }\n", body-4))
				if err != nil {
					t.Fatal(err)
				}
				m := tabletext.NewModel(u)
				st := m.Lookup("Root")
				inst := m.New(st)
				if body == 65536 {
					wire, err := tablewire.Encode(m, inst)
					if err != nil || len(wire) == 0 || wire[0] != 3 {
						t.Fatalf("boundary save: %x %v", wire, err)
					}
					var report tabletext.Report
					if ok, err := tablewire.Decode(m, m.New(st), wire, &report); !ok || err != nil {
						t.Fatalf("boundary read: %v %v", ok, err)
					}
					return
				}
				if ir.TableFixedEmitted(u, st) {
					t.Fatal("oversized fixture still emitted")
				}
				var report tabletext.Report
				ok, err := tablewire.Decode(m, inst, []byte{1, 0, 0, 0, 0, 0, 0, 0, 0, 0}, &report)
				var refusal *tablewire.FixedRefusal
				if ok || !errors.As(err, &refusal) || refusal.Reason != "previous_form" || !report.Refused || report.Reason != "previous_form" {
					t.Errorf("oversized fixed root must refuse form 1 as previous_form: ok=%v err=%v report=%+v", ok, err, report)
				}
				wire, err := tablewire.Encode(m, inst)
				if !errors.As(err, &refusal) || refusal.Reason != "layout_unsupported" || len(wire) != 0 {
					t.Errorf("oversized save must refuse layout_unsupported: bytes=%d err=%v", len(wire), err)
				}
				ok, err = tablewire.Decode(m, inst, []byte{3}, &report)
				if ok || !errors.As(err, &refusal) || refusal.Reason != "layout_unsupported" {
					t.Errorf("oversized form 3: ok=%v err=%v", ok, err)
				}
			})
		}
	})

	t.Run("no LoadTolerant entry point is live", func(t *testing.T) {
		t.Parallel()
		page := readSpecTables(t)
		for i, para := range strings.Split(page, "\n\n") {
			names := []string{}
			for _, name := range tolerantEntryPoints {
				if strings.Contains(para, name) {
					names = append(names, name)
				}
			}
			if len(names) == 0 {
				continue
			}
			retired := false
			for _, phrase := range retirementPhrases {
				if strings.Contains(para, phrase) {
					retired = true
					break
				}
			}
			if !retired {
				t.Errorf("docs/SPEC-TABLES.md paragraph %d names %s as a live entry point; the one read has no tolerant twin",
					i, strings.Join(names, ", "))
			}
		}
	})
}
