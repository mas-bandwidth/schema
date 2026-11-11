// TestPortSquirrelPageCurrent pins docs/PORT-SQUIRREL.md to the tree it
// describes: both surfaces carry their codecs, a map has table storage, and
// the table wire carries the 128-bit kinds on its variable and fixed forms.
// The float-width check the page still owes stays stated. Each corrected
// statement has a want, and each removed phrase has a stale the page must not
// carry.
package compiler

import (
	"os"
	"strings"
	"testing"
)

// portSquirrelSection returns the page from start up to (not including) end,
// so a literal is found where the page states it.
func portSquirrelSection(t *testing.T, page, start, end string) string {
	t.Helper()
	from := strings.Index(page, start)
	if from < 0 {
		t.Fatalf("docs/PORT-SQUIRREL.md has no heading %q", start)
	}
	to := strings.Index(page[from:], end)
	if to < 0 {
		t.Fatalf("docs/PORT-SQUIRREL.md has no heading %q after %q", end, start)
	}
	return page[from : from+to]
}

func TestPortSquirrelPageCurrent(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../docs/PORT-SQUIRREL.md")
	if err != nil {
		t.Fatal(err)
	}
	page := string(data)

	rows := []struct {
		row    string
		text   string
		wants  []string
		stales []string
	}{
		{
			row:  "the surfaces carry their codecs",
			text: portSquirrelSection(t, page, "# Porting schema to Squirrel", "## THE PINNED INTERPRETER"),
			wants: []string{
				"Both surfaces carry their codecs",
				"the packet wire's read and write",
				"variable, fixed, message and text forms",
				"THE CARD MAP below names each",
			},
			stales: []string{
				"Neither surface carries a codec",
				"the cards below add them without editing the skeleton's files",
			},
		},
		{
			row:  "the map row",
			text: portSquirrelSection(t, page, "### The table surface", "## 64-BIT AND 128-BIT INTEGERS"),
			wants: []string{
				"an array of the generated `{ key, value }` entry",
				"ascending key order, born empty",
			},
			stales: []string{
				"not mapped: refused by name",
			},
		},
		{
			row:  "the 128-bit table kinds",
			text: portSquirrelSection(t, page, "## 64-BIT AND 128-BIT INTEGERS", "## BYTE BUFFERS"),
			wants: []string{
				"The table wire carries the 128-bit",
				"kinds on its variable and fixed forms",
			},
			stales: []string{
				"The table wire's 128-bit kinds are",
				"refused today (`ir.RefuseWideTableKinds`)",
			},
		},
		{
			row:  "the pinned-VM check is held",
			text: portSquirrelSection(t, page, "## THE PINNED INTERPRETER", "## THE TYPE MAPPING"),
			wants: []string{
				"`_intsize_ == 8 && _floatsize_ == 8 && _charsize_ == 1`",
				"requires `schema_table_descriptor` before any other table runtime",
			},
			stales: []string{
				"that check is owed to the first codec card",
			},
		},
	}
	for _, r := range rows {
		for _, want := range r.wants {
			if !strings.Contains(r.text, want) {
				t.Errorf("%s: the page no longer states %q", r.row, want)
			}
		}
		for _, stale := range r.stales {
			if strings.Contains(r.text, stale) {
				t.Errorf("%s: the page still states the stale %q", r.row, stale)
			}
		}
	}
}
