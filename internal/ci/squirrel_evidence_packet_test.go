package ci

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var (
	squirrelOverlayIDRx    = regexp.MustCompile(`^\(:id\s+"([^"]+)"`)
	squirrelOverlayStateRx = regexp.MustCompile(`:state\s+:([A-Za-z0-9_-]+)`)
	squirrelOverlayRevRx   = regexp.MustCompile(`:tested-revision\s+"([^"]*)"`)
	squirrelOverlayNoteRx  = regexp.MustCompile(`:note\s+"([^"]*)"`)
	squirrelOverlayStrRx   = regexp.MustCompile(`"([^"]*)"`)
	squirrelGoTestFuncRx   = regexp.MustCompile(`(?m)^func\s+(Test[A-Za-z0-9_]+)\s*\(`)
)

// TestSquirrelEvidencePacketAndDocTagsRows verifies docs/roadmap-evidence/squirrel.sexp
// for port/squirrel/packet and port/squirrel/vt/doc-tags, asserts that every
// cited Go test exists in the named file and every cited make target exists in
// a makefile.
func TestSquirrelEvidencePacketAndDocTagsRows(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	overlayPath := filepath.Join(root, "docs", "roadmap-evidence", "squirrel.sexp")
	overlayBytes, err := os.ReadFile(overlayPath)
	if err != nil {
		t.Fatalf("read docs/roadmap-evidence/squirrel.sexp: %v", err)
	}

	if err := validateSquirrelEvidenceOverlay(root, string(overlayBytes)); err != nil {
		t.Fatal(err)
	}

	// Negative controls: a row citing a missing Go test or make target must be refused.
	badGoTest := strings.Replace(
		string(overlayBytes),
		"TestSquirrelTypeWireReadsTheGoldens",
		"TestSquirrelNonexistentGoTest",
		1,
	)
	if err := validateSquirrelEvidenceOverlay(root, badGoTest); err == nil {
		t.Fatal("negative control: validator accepted a nonexistent Go test in :evidence")
	}

	badMakeTarget := strings.Replace(
		string(overlayBytes),
		"packet-utf8-squirrel",
		"packet-nonexistent-squirrel-target",
		1,
	)
	if err := validateSquirrelEvidenceOverlay(root, badMakeTarget); err == nil {
		t.Fatal("negative control: validator accepted a nonexistent make target in :evidence")
	}

}

func validateSquirrelEvidenceOverlay(root, overlayText string) error {
	rows := map[string]string{}
	for line := range strings.SplitSeq(overlayText, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, ";") {
			continue
		}
		m := squirrelOverlayIDRx.FindStringSubmatch(trimmed)
		if m == nil {
			return fmt.Errorf("malformed overlay line: %s", trimmed)
		}
		id := m[1]
		if _, dup := rows[id]; dup {
			return fmt.Errorf("duplicate overlay row %s", id)
		}
		rows[id] = trimmed

		stateMatch := squirrelOverlayStateRx.FindStringSubmatch(trimmed)
		if stateMatch == nil {
			return fmt.Errorf("%s: missing :state", id)
		}
		state := stateMatch[1]
		switch state {
		case "done":
			revMatch := squirrelOverlayRevRx.FindStringSubmatch(trimmed)
			if revMatch == nil || !isFortyHexSHA(revMatch[1]) {
				return fmt.Errorf("%s: :done row must carry a 40-hex :tested-revision", id)
			}
			evList := extractSquirrelEvidenceStrings(trimmed)
			if len(evList) == 0 {
				return fmt.Errorf("%s: :done row has no :evidence strings", id)
			}
			for _, ev := range evList {
				if err := checkSquirrelEvidenceCitation(root, id, ev); err != nil {
					return err
				}
			}
		case "todo":
			noteMatch := squirrelOverlayNoteRx.FindStringSubmatch(trimmed)
			if noteMatch == nil || strings.TrimSpace(noteMatch[1]) == "" {
				return fmt.Errorf("%s: :todo row must carry a non-empty :note explaining why", id)
			}
		default:
			return fmt.Errorf("%s: unexpected :state :%s", id, state)
		}
	}

	requiredDone := []string{
		"port/squirrel/packet",
		"port/squirrel/vt/doc-tags",
	}
	for _, id := range requiredDone {
		line, ok := rows[id]
		if !ok {
			return fmt.Errorf("missing required row %s in docs/roadmap-evidence/squirrel.sexp", id)
		}
		stateMatch := squirrelOverlayStateRx.FindStringSubmatch(line)
		if stateMatch == nil || stateMatch[1] != "done" {
			return fmt.Errorf("%s: expected :state :done, got %v", id, stateMatch)
		}
	}

	packetLine := rows["port/squirrel/packet"]
	for _, wantSub := range []string{
		"TestSquirrelTypeWireReadsTheGoldens",
		"TestSquirrelTypeWireReadNegativeControl",
		"TestSquirrelTypeWireWritesTheGoldens",
		"TestSquirrelTypeWireWriteNegativeControl",
		"TestVendoredSquirrelSerializeIsThePin",
		"TestSquirrelPacketFloatsAreFinite",
		"TestSquirrelPacketFiniteNegativeControl",
		"packet-utf8-squirrel",
		"packet-utf8-squirrel-negative-control",
		"packet-wide-squirrel",
		"packet-wide-squirrel-negative-control",
		"packet-defaults-squirrel",
		"packet-defaults-squirrel-negative-control",
		"packet-finite-squirrel-negative-control",
	} {
		if !strings.Contains(packetLine, wantSub) {
			return fmt.Errorf("port/squirrel/packet evidence missing %q", wantSub)
		}
	}

	docTagsLine := rows["port/squirrel/vt/doc-tags"]
	for _, wantSub := range []string{
		"TestSquirrelViewDocAndTags",
		"TestSquirrelViewDocAndTagsNegativeControl",
	} {
		if !strings.Contains(docTagsLine, wantSub) {
			return fmt.Errorf("port/squirrel/vt/doc-tags evidence missing %q", wantSub)
		}
	}

	return nil
}

func checkSquirrelEvidenceCitation(root, id, ev string) error {
	if rest, ok := strings.CutPrefix(ev, "make "); ok {
		var mkFile string
		if openIdx := strings.Index(rest, "("); openIdx >= 0 && strings.HasSuffix(rest, ")") {
			mkFile = strings.TrimSpace(rest[openIdx+1 : len(rest)-1])
			rest = strings.TrimSpace(rest[:openIdx])
		}
		targets := strings.SplitSeq(rest, ",")
		for rawTarget := range targets {
			target := strings.TrimSpace(rawTarget)
			if target == "" {
				return fmt.Errorf("%s: empty make target in %q", id, ev)
			}
			if !makeTargetExists(root, mkFile, target) {
				return fmt.Errorf("%s: make target %q not found (file=%q)", id, target, mkFile)
			}
		}
		return nil
	}

	filePath, testsPart, ok := strings.Cut(ev, ": ")
	if !ok {
		return fmt.Errorf("%s: evidence %q is neither a 'make ...' citation nor '<file>: <TestName>'", id, ev)
	}
	src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(filePath)))
	if err != nil {
		return fmt.Errorf("%s: read cited file %s: %w", id, filePath, err)
	}
	defined := map[string]bool{}
	for _, m := range squirrelGoTestFuncRx.FindAllStringSubmatch(string(src), -1) {
		defined[m[1]] = true
	}
	for token := range strings.SplitSeq(testsPart, ",") {
		name := strings.TrimSpace(token)
		if slash := strings.Index(name, "/"); slash >= 0 {
			name = name[:slash]
		}
		if name == "" {
			return fmt.Errorf("%s: empty Go test name in %q", id, ev)
		}
		if !defined[name] {
			return fmt.Errorf("%s: Go test %s not found in %s", id, name, filePath)
		}
	}
	return nil
}

func makeTargetExists(root, mkFile, target string) bool {
	targetRuleRx := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(target) + `\s*:`)
	var files []string
	if mkFile != "" {
		files = []string{filepath.Join(root, filepath.FromSlash(mkFile))}
	} else {
		files = append(files, filepath.Join(root, "Makefile"))
		matches, _ := filepath.Glob(filepath.Join(root, "make", "*.mk"))
		files = append(files, matches...)
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		if targetRuleRx.Match(b) {
			return true
		}
	}
	return false
}

func extractSquirrelEvidenceStrings(line string) []string {
	at := strings.Index(line, ":evidence")
	if at < 0 {
		return nil
	}
	rest := line[at:]
	for _, endKey := range []string{":tested-revision", ":note"} {
		if end := strings.Index(rest, endKey); end >= 0 {
			rest = rest[:end]
		}
	}
	var out []string
	for _, m := range squirrelOverlayStrRx.FindAllStringSubmatch(rest, -1) {
		out = append(out, m[1])
	}
	return out
}

func isFortyHexSHA(s string) bool {
	if len(s) != 40 {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}
