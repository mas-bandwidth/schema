// The second half of docs/USAGE.md states what Go carries today: maps,
// unbounded arrays, byte buffers, the cooked and block forms, field walking,
// the view, the text form, text lists, integrity and embedding. Each check
// below holds one section to the rule the Go implementation keeps. A check
// that names a section which has moved fails with the heading, and a check
// whose stale spelling is still present fails with the line to look at, so a
// section cannot drift in either direction.
package compiler

import (
	"os"
	"strings"
	"testing"
)

type usageCheck struct {
	name    string
	section string // the heading line that opens the section
	end     string // the heading line that opens the next one
	want    string // the Go spelling or name the row states
	stale   string // the sentence the row replaces
}

var usageChecks = []usageCheck{
	{
		name:    "maps",
		section: "### Maps: `ships map[string(32)]ShipConfig`",
		end:     "### Unbounded arrays:",
		want:    "IndexFind",
		stale:   "Every other backend\nrefuses a unit that declares one",
	},
	{
		name:    "unbounded arrays",
		section: "### Unbounded arrays:",
		end:     "### Byte buffers:",
		want:    "[]T",
		stale:   "the tool's `cook` and `uncook`\nhalves do not yet",
	},
	{
		name:    "byte buffers",
		section: "### Byte buffers:",
		end:     "### Timestamps:",
		want:    "*string",
		stale:   "C++ and the tool carry the construct today",
	},
	{
		name:    "block form's generated files",
		section: "### The block form:",
		end:     "### The cooked form:",
		want:    "`<Base>Block.go`",
		stale:   "`block_runtime.rs` for Rust — so a project",
	},
	{
		name:    "block form's open reason",
		section: "### The block form:",
		end:     "### The cooked form:",
		want:    "Go's `Open(base, bytes)`",
		stale:   "no backend writes it yet",
	},
	{
		name:    "cooked form's writers",
		section: "### The cooked form:",
		end:     "### The build version:",
		want:    "CookFrom",
		stale:   "And C++ WRITES a cook too",
	},
	{
		name:    "walking fields carries both classes",
		section: "### Walking fields at runtime",
		end:     "### Inspecting a whole build:",
		want:    "Go carry both classes",
		stale:   "C++ and C carry both\nclasses, the other seven",
	},
	{
		name:    "view file",
		section: "### Inspecting a whole build:",
		end:     "### The text form:",
		want:    "`<Package>View.go`",
		stale:   "the other eight targets emit no view file yet",
	},
	{
		name:    "text form's newline",
		section: "### The text form:",
		end:     "### Carrying a table over a text channel",
		want:    "which writes none",
		stale:   "exactly one newline, and every reader",
	},
	{
		name:    "text form's strict FromJson",
		section: "### The text form:",
		end:     "### Carrying a table over a text channel",
		want:    "FromJsonTolerant",
		stale:   "`FromJson` returns false with",
	},
	{
		name:    "Tables from Go's strict LoadBuilder",
		section: "### Tables from Go",
		end:     "### Text lists and array-root tables",
		want:    "LoadBuilderTolerant",
		stale:   "which keeps what it decoded and reads on",
	},
	{
		name:    "text lists' Append",
		section: "### Text lists and array-root tables",
		end:     "### Unbounded text in lists and map keys",
		want:    "ill-formed UTF-8",
		stale:   "appends (false past either bound)",
	},
	{
		name:    "integrity's tolerant reader",
		section: "### A checksum on a saved file:",
		end:     "### Generated tests:",
		want:    "LedgerLoadTolerant",
		stale:   "the tolerant\n`LedgerLoad` sets `malformed`",
	},
	{
		name:    "embedding's options",
		section: "## Embedding the compiler",
		end:     "## Per-language notes",
		want:    "`runtime` and `surface`",
		stale:   "built-in targets read none today",
	},
	{
		name:    "embedding's version surface",
		section: "## Embedding the compiler",
		end:     "## Per-language notes",
		want:    "compiler.UserAgent()",
		stale:   "`compiler.Version()`",
	},
}

func TestSpecGoUsageSecondHalfStaleStatus(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../docs/USAGE.md")
	if err != nil {
		t.Fatal(err)
	}
	page := string(data)
	for _, c := range usageChecks {
		t.Run(c.name, func(t *testing.T) {
			section := usageSection(t, page, c.section, c.end)
			if !strings.Contains(section, c.want) {
				t.Errorf("docs/USAGE.md section %q does not state %q", c.section, c.want)
			}
			if strings.Contains(section, c.stale) {
				t.Errorf("docs/USAGE.md section %q still holds the stale text %q", c.section, c.stale)
			}
		})
	}
}

// usageSection returns the page between one heading and the next, so a check
// is about the section it names and not about the rest of the page.
func usageSection(t *testing.T, page, start, end string) string {
	t.Helper()
	i := strings.Index(page, start)
	if i < 0 {
		t.Fatalf("docs/USAGE.md has no heading %q — this gate names a section that has moved", start)
	}
	rest := page[i:]
	j := strings.Index(rest, end)
	if j < 0 {
		t.Fatalf("docs/USAGE.md has no heading %q after %q — this gate names a section that has moved", end, start)
	}
	return rest[:j]
}
