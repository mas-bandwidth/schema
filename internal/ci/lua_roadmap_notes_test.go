package ci

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestLuaRoadmapNotesDescribeThePresent holds the Lua notes to the present
// state: the packet codec, the fixed form and the variable-table runtime are on
// main with no tag, and the tolerant entry points are owed to the cut, not
// offered as features. Every packet, fixed and variable-table note must name
// the cut or the :done bar, and no port/lua note may name the retired
// schema_leg_lua tag.
func TestLuaRoadmapNotesDescribeThePresent(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(filepath.Join(repoRoot(t), "docs", "roadmap.sexp"))
	if err != nil {
		t.Fatal(err)
	}

	nodeRx := regexp.MustCompile(`\(:id "(port/lua/[^"]+)"[^\n]*?:note "([^"]*)"`)
	buildTagRx := regexp.MustCompile(`(?i)build[ -]?tag`)

	var packet, fixed, vt []string
	var badTag []string
	var badBar []string
	var badTolerant []string
	for _, m := range nodeRx.FindAllStringSubmatch(string(raw), -1) {
		id, note := m[1], m[2]
		if strings.Contains(note, "schema_leg_lua") || buildTagRx.MatchString(note) {
			badTag = append(badTag, id)
		}
		switch {
		case id == "port/lua/packet":
			packet = append(packet, id)
		case strings.HasPrefix(id, "port/lua/fixed/"):
			fixed = append(fixed, id)
		case strings.HasPrefix(id, "port/lua/vt/"):
			vt = append(vt, id)
		default:
			continue
		}
		if !strings.Contains(note, ":done") || !strings.Contains(note, "cut") {
			badBar = append(badBar, id)
		}
		if !strings.Contains(note, "LoadTolerant") || !strings.Contains(note, "ReadFixedTolerant") {
			badTolerant = append(badTolerant, id)
		}
	}

	if len(packet) != 1 {
		t.Fatalf("found %d packet tasks; want 1", len(packet))
	}
	if len(fixed) != 56 {
		t.Fatalf("found %d fixed tasks; want 56", len(fixed))
	}
	if len(vt) != 31 {
		t.Fatalf("found %d variable-table tasks; want 31 task nodes for the 32 cells (string-identity shares port/lua/vt/blobs)", len(vt))
	}
	if len(badTag) > 0 {
		t.Fatalf("notes still name the retired tag: %s", strings.Join(badTag, ", "))
	}
	if len(badBar) > 0 {
		t.Fatalf("notes name neither the cut nor the :done bar: %s", strings.Join(badBar, ", "))
	}
	if len(badTolerant) > 0 {
		t.Fatalf("notes do not name LoadTolerant and ReadFixedTolerant as owed to the cut: %s", strings.Join(badTolerant, ", "))
	}
}
