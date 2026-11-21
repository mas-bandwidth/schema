// The tutorial's second language is Go and NOT C (schema#553). Parts 12 and 13
// cross from the C++ reference to exactly one other language, and the owner's
// rule is that the pair must be two DIFFERENT languages for structs in memory:
// "C and C++ is weak sauce ... anybody reading that would be 'Why?!'". Go is
// the one that carries the packet wire, the cook open and the block read the
// crossings need, so the page names Go and drops C.
//
// This gate pulls the fenced blocks out of the two parts and holds the rule
// mechanically rather than by eye: no C block survives in either part, each
// part shows Go, and every Go block PARSES as Go — a snippet that stops being
// Go fails here, and so does a C block that creeps back. A section heading or
// the closing summary that moves fails with the page and the line to look at.
package compiler

import (
	goparser "go/parser"
	"go/token"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// tutorialHeading locates one `## Part N:` section in the page.
const (
	tutorialPart12 = "## Part 12:"
	tutorialPart13 = "## Part 13:"
)

// tutorialPart returns the lines of one `## Part N:` section, up to the next
// `## Part` heading or the end of the page.
func tutorialPart(t *testing.T, page, heading string) []string {
	t.Helper()
	data, err := os.ReadFile(page)
	if err != nil {
		t.Fatal(err)
	}
	var body []string
	inside := false
	for line := range strings.SplitSeq(string(data), "\n") {
		if strings.HasPrefix(line, "## Part ") {
			if inside {
				break
			}
			if strings.HasPrefix(line, heading) {
				inside = true
			}
		}
		if inside {
			body = append(body, line)
		}
	}
	if len(body) == 0 {
		t.Fatalf("%s has no section %q — this gate names a part that has moved", page, heading)
	}
	return body
}

// tutorialFences returns (lang, body) for every fenced block in the lines.
// lang is the info string on the opening ``` line, which is empty for an
// untagged block (a command transcript or a go.mod).
func tutorialFences(lines []string) [][2]string {
	var out [][2]string
	inside := false
	var lang string
	var body []string
	for _, line := range lines {
		if after, ok := strings.CutPrefix(line, "```"); ok {
			if inside {
				out = append(out, [2]string{lang, strings.Join(body, "\n")})
				body = nil
			} else {
				lang = strings.TrimSpace(after)
			}
			inside = !inside
			continue
		}
		if inside {
			body = append(body, line)
		}
	}
	return out
}

// tutorialSchemaBlocks returns the bodies of a part's untagged fences that are
// schema source of the starlight unit: a `package bad` block is a refusal
// demonstration and a `$` block is a command transcript, so neither is.
func tutorialSchemaBlocks(lines []string) []string {
	var out []string
	for _, fence := range tutorialFences(lines) {
		lang, body := fence[0], strings.TrimSpace(fence[1])
		if lang != "" || body == "" {
			continue
		}
		if strings.HasPrefix(body, "package bad") || strings.HasPrefix(body, "$") {
			continue
		}
		out = append(out, body)
	}
	return out
}

// tutorialStarlightPart6 assembles the tutorial's starlight unit as it stands
// once Part 6 adds its first table, from the page itself: Part 4's `Game.schema`
// shown entire with Part 5's constants block in place of its constants, Part
// 4's `Vectors.schema`, Part 5's `Net.schema` (every schema block of the part
// after the constants), and Part 6's `Config.schema`. A block that moves fails
// here with the part to look at.
func tutorialStarlightPart6(t *testing.T, page string) map[string]string {
	t.Helper()
	var game, vectors string
	for _, body := range tutorialSchemaBlocks(tutorialPart(t, page, "## Part 4:")) {
		if !strings.HasPrefix(body, "package starlight") {
			continue
		}
		switch {
		case strings.Contains(body, "type ShipState"):
			game = body
		case strings.Contains(body, "type Vector2"):
			vectors = body
		}
	}
	if game == "" || vectors == "" {
		t.Fatalf("Part 4 no longer shows Game.schema entire and Vectors.schema (game %t, vectors %t)", game != "", vectors != "")
	}
	part5 := tutorialSchemaBlocks(tutorialPart(t, page, "## Part 5:"))
	if len(part5) < 2 || !strings.HasPrefix(part5[0], "const ") {
		t.Fatalf("Part 5 no longer opens with the constants block that joins Game.schema")
	}
	var kept []string
	for line := range strings.SplitSeq(game, "\n") {
		if strings.HasPrefix(line, "const ") {
			continue
		}
		kept = append(kept, line)
		if strings.HasPrefix(line, "package ") {
			kept = append(kept, "", part5[0])
		}
	}
	game = strings.Join(kept, "\n")
	net := "package starlight\n\n" + strings.Join(part5[1:], "\n\n")
	if !strings.Contains(net, "const(0xC7, 8)") || !strings.Contains(net, "reserved(5)") || !strings.Contains(net, "align") {
		t.Fatalf("Part 5's Net.schema no longer carries the const, reserved and align framing this gate holds:\n%s", net)
	}
	var config string
	for _, body := range tutorialSchemaBlocks(tutorialPart(t, page, "## Part 6:")) {
		if strings.HasPrefix(body, "package starlight") && strings.Contains(body, "table ShipConfig") {
			config = body
			break
		}
	}
	if config == "" {
		t.Fatal("Part 6 no longer shows Config.schema with table ShipConfig")
	}
	return map[string]string{
		"Game":    game + "\n",
		"Vectors": vectors + "\n",
		"Net":     net + "\n",
		"Config":  config + "\n",
	}
}

// unitFromTutorialFiles checks a multi-file unit from memory, files in base
// order, the way the driver loads a directory.
func unitFromTutorialFiles(t *testing.T, files map[string]string) *ir.Unit {
	t.Helper()
	bases := make([]string, 0, len(files))
	for base := range files {
		bases = append(bases, base)
	}
	slices.Sort(bases)
	var srcs []check.SourceFile
	for _, base := range bases {
		name := base + ".schema"
		f, perrs := parser.Parse(name, []byte(files[base]))
		if len(perrs) > 0 {
			t.Fatalf("parse %s: %v\n%s", name, perrs[0], files[base])
		}
		srcs = append(srcs, check.SourceFile{Path: name, Name: name, Base: base, Bytes: []byte(files[base]), AST: f})
	}
	u, cerrs := check.Unit(srcs)
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	return u
}

// TestTutorialStarlightGeneratesForPython holds the tutorial's own unit to the
// Python target: once Part 6 adds a table the unit takes the table frame, which
// hosts Part 5's PacketHeader with its const, reserved and align framing. That
// was refused ("the Python packet wire does not carry yet") while the roadmap
// ticked all three. The hosted class must write the const and zero the
// reserved bits, and a read must refuse any other value (SPEC §4.3).
func TestTutorialStarlightGeneratesForPython(t *testing.T) {
	u := unitFromTutorialFiles(t, tutorialStarlightPart6(t, "../docs/TUTORIAL.md"))
	if len(u.Tables) == 0 {
		t.Fatal("the assembled starlight unit has no table; Part 6's table frame is what this gate holds")
	}
	out, err := New().Generate(u, "python", nil)
	if err != nil {
		t.Fatalf("python refuses the tutorial's starlight unit: %v", err)
	}
	var net string
	for path, src := range out {
		if strings.HasSuffix(path, "/Net.py") {
			net = string(src)
		}
	}
	if net == "" {
		t.Fatal("python emitted no Net.py for the starlight unit")
	}
	for _, want := range []string{
		"class PacketHeader:",
		"_s_ri.value = 199\n        _s_serialize_bits(_s_ri, 8)",
		"if _s_ri.value != 199:\n            return False",
		"_s_ri.value = 0\n        _s_serialize_bits(_s_ri, 5)",
		"if _s_ri.value != 0:\n            return False",
		"_s_serialize_align()",
	} {
		if !strings.Contains(net, want) {
			t.Errorf("starlight Net.py lacks %q", want)
		}
	}
}

// TestTutorialSecondLanguageIsGo is the gate (schema#553).
func TestTutorialSecondLanguageIsGo(t *testing.T) {
	const page = "../docs/TUTORIAL.md"
	for _, heading := range []string{tutorialPart12, tutorialPart13} {
		lines := tutorialPart(t, page, heading)
		fences := tutorialFences(lines)

		goBlocks, cBlocks := 0, 0
		for _, fence := range fences {
			lang, body := fence[0], fence[1]
			switch lang {
			case "c":
				cBlocks++
				t.Errorf("%s still shows a C block; the second language is Go, not C (schema#553):\n%s",
					heading, body)
			case "go":
				goBlocks++
				if _, err := goparser.ParseFile(token.NewFileSet(), "tutorial.go", body, goparser.AllErrors); err != nil {
					t.Errorf("%s: a Go block does not parse as Go: %v\n%s", heading, err, body)
				}
			}
		}
		if cBlocks == 0 && goBlocks == 0 {
			t.Errorf("%s crosses to no language at all: no Go block and no C block", heading)
		}
		if goBlocks == 0 {
			t.Errorf("%s shows no Go block; the second language is Go (schema#553)", heading)
		}
	}

	// The part's own title and its closing sentence name the rule, so a silent
	// revert of the prose fails here too.
	part13 := strings.Join(tutorialPart(t, page, tutorialPart13), "\n")
	if !strings.Contains(part13, "### Three wires cross to Go") {
		t.Error("Part 13 no longer says its three wires cross to Go (schema#553)")
	}
	if prose := strings.Join(strings.Fields(part13), " "); !strings.Contains(prose, "Go tools reading its packets") {
		t.Error("Part 13's closing summary no longer names Go tools (schema#553)")
	}
}
