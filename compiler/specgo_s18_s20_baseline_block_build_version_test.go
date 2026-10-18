// The standard says what Go already does for the tables baseline, the block
// form and the build version (docs/SPEC-TABLES.md §18, §19, §20). This pin
// holds each landed Go fact to its section and holds the stale sentence it
// replaced out. A row that the standard does not hold is named in the card's
// report rather than asserted here.
package compiler

import (
	"os"
	"strings"
	"testing"
)

const specTablesPage = "../docs/SPEC-TABLES.md"

type specPin struct {
	section string
	holds   string
	stale   string
}

type specRow struct {
	name   string
	checks []specPin
}

func TestSpecGoS18S20BaselineBlockBuildVersion(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(specTablesPage)
	if err != nil {
		t.Fatal(err)
	}
	page := string(raw)

	rows := []specRow{
		{
			name: "§18.2 rename hint rides the warning channel",
			checks: []specPin{{
				section: "18.2", holds: "rides the warning channel",
			}},
		},
		{
			name: "§18.1/§18.2 required=true is recorded and judged",
			checks: []specPin{
				{section: "18.1", holds: "`required=true`"},
				{section: "18.2", holds: "`required=true`"},
			},
		},
		{
			name: "§18.1/§18.2 byte buffer renders type=string or type=bytes",
			checks: []specPin{
				{section: "18.1", holds: "`type=string` or `type=bytes`"},
				{section: "18.2", holds: "`type=string` or `type=bytes`"},
			},
		},
		{
			name: "§18.4 history lists refuse and warn findings",
			checks: []specPin{{
				section: "18.4", holds: "no compatibility-affecting edits",
			}},
		},
		{
			name: "§18.2/§18.4 CLI refuses --reason without --update",
			checks: []specPin{
				{section: "18.2", holds: "`--reason` without `--update` is refused"},
				{section: "18.4", holds: "`--reason` without `--update` is refused"},
			},
		},
		{
			name: "§19.1 Go builder takes *TableBlockRefusal",
			checks: []specPin{{
				section: "19.1", holds: "*TableBlockRefusal",
			}},
		},
		{
			name: "§19.2 a byte_order word that does not match is not_a_cook",
			checks: []specPin{{
				section: "19.2", holds: "where the magic matched and the `byte_order` word does not match",
			}},
		},
		{
			name: "§19.2 offset_of below the projection or misaligned is bad_layout",
			checks: []specPin{{
				section: "19.2",
				holds:   "overlap with the projection IS refused",
				stale:   "OVERLAP is not refused",
			}},
		},
		{
			name: "§19.2 Go refuses truncated before it reads the magic",
			checks: []specPin{{
				section: "19.2",
				holds:   "before it reads the magic",
				stale:   "fewer than its prologue answers `truncated`",
			}},
		},
		{
			name: "§19.2 Go puts <Name>Row in the core Table source",
			checks: []specPin{{
				section: "19.2",
				holds:   "the core Table source",
				stale:   "The GO backend follows both rules exactly",
			}},
		},
		{
			name: "§19.3/§20.3/§20 status Go's layout check is an init() panic",
			checks: []specPin{
				{section: "19.3", holds: "`init()`", stale: "with one honest exception"},
				{section: "20.3", holds: "`init()`"},
				{section: "20 status", holds: "`init()`", stale: "The C# check is a THROW at first use, not a build error."},
			},
		},
		{
			name: "§20 status/§20.7 Go's BuildVersion is in the Table source",
			checks: []specPin{
				{section: "20 status", holds: "carries `Announce`"},
				{section: "20.7", holds: "carries `Announce`"},
			},
		},
		{
			name: "§20.4/§20.8 a same-width flags referent swap MOVES the build version",
			checks: []specPin{
				{section: "20.4", holds: "same-width other MOVES", stale: "swapping one flags declaration for a same-width other"},
				{section: "20.8", holds: "same-width other MOVES", stale: "a flags field's REFERENT swapped for a same-width other moves nothing"},
			},
		},
		{
			name: "§20.8 the flags block already carries the bit positions",
			checks: []specPin{{
				section: "20.8",
				holds:   "held by the bit positions",
				stale:   "which #435 owes",
			}},
		},
		{
			name: "§20.5 the announcement carries the build version",
			checks: []specPin{{
				section: "20.5",
				holds:   "`build_version_mismatch`",
				stale:   "Nothing about a cook crosses a connection",
			}},
		},
	}

	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			for _, pin := range row.checks {
				body := specSection(t, page, pin.section)
				if !strings.Contains(body, pin.holds) {
					t.Errorf("§%s does not hold %q", pin.section, pin.holds)
				}
				if pin.stale != "" && strings.Contains(body, pin.stale) {
					t.Errorf("§%s still holds the stale %q", pin.section, pin.stale)
				}
			}
		})
	}
}

// specSection returns one section's text, from its heading to the next heading
// of the same or a higher level. Whitespace is collapsed so a sentence that
// wraps in the page still matches a one-line literal.
func specSection(t *testing.T, page, section string) string {
	t.Helper()

	starts := map[string]string{
		"18.1":      "### 18.1 The file",
		"18.2":      "### 18.2 The check",
		"18.4":      "### 18.4 Moving it is an explicit act",
		"19.1":      "### 19.1 The builder",
		"19.2":      "### 19.2 The reflective read",
		"19.3":      "### 19.3 The layout contract",
		"20 status": "## 20. The build version",
		"20.3":      "### 20.3 The compiler computes the layout; the backends must assert it",
		"20.4":      "### 20.4 What moves it, and what does not",
		"20.5":      "### 20.5 The connect gate is the protocol id, and nothing else",
		"20.7":      "### 20.7 The surface",
		"20.8":      "### 20.8 Held by test",
	}
	ends := map[string]string{
		"18.1":      "### 18.2",
		"18.2":      "### 18.3",
		"18.4":      "### 18.5",
		"19.1":      "### 19.2",
		"19.2":      "### 19.3",
		"19.3":      "### 19.4",
		"20 status": "### 20.1",
		"20.3":      "### 20.4",
		"20.4":      "### 20.5",
		"20.5":      "### 20.6",
		"20.7":      "### 20.8",
		"20.8":      "## 21.",
	}

	heading, ok := starts[section]
	if !ok {
		t.Fatalf("no start heading for §%s", section)
	}
	end, ok := ends[section]
	if !ok {
		t.Fatalf("no end heading for §%s", section)
	}
	from := strings.Index(page, heading)
	if from < 0 {
		t.Fatalf("§%s start heading %q is gone", section, heading)
	}
	to := strings.Index(page[from+len(heading):], end)
	if to < 0 {
		t.Fatalf("§%s end heading %q is gone", section, end)
	}
	body := page[from : from+len(heading)+to]
	return strings.Join(strings.Fields(body), " ")
}
