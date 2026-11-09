package luautable

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var luauTaskLineRx = regexp.MustCompile(`(?m)^\s*\(:id "(port/luau/[^"]+)" :type :task :title "[^"]*" :state :todo :evidence nil :note "([^"]*)"\)\s*$`)

var luauFixedCutRows = map[string]bool{
	"port/luau/fixed/numeric-evolution": true,
	"port/luau/fixed/reports":           true,
	"port/luau/fixed/array-bounds":      true,
	"port/luau/fixed/text":              true,
	"port/luau/fixed/scalar-bounds":     true,
	"port/luau/fixed/ordinals":          true,
	"port/luau/fixed/normalization":     true,
	"port/luau/fixed/hostile-input":     true,
}

var luauVTContractIndependentRows = map[string]bool{
	"port/luau/vt/reflection":    true,
	"port/luau/vt/doc-tags":      true,
	"port/luau/vt/unit-registry": true,
	"port/luau/vt/block-read":    true,
	"port/luau/vt/block-build":   true,
	"port/luau/vt/build-version": true,
	"port/luau/vt/cook-open":     true,
	"port/luau/vt/cook-write":    true,
}

func checkLuauRoadmapNotesAndMake(roadmapText, luauMkText, luauSerializeMkText string) error {
	matches := luauTaskLineRx.FindAllStringSubmatch(roadmapText, -1)
	if len(matches) != 91 {
		return fmt.Errorf("expected 91 port/luau/* task lines with unchanged :state :todo :evidence nil, got %d", len(matches))
	}

	var packetCount, fixedCount, vtCount, portMetaCount int
	for _, m := range matches {
		id, note := m[1], m[2]
		if strings.Contains(note, "Not started") || strings.Contains(note, "no generator, runtime or conformance leg exists") {
			return fmt.Errorf("%s: stale note %q", id, note)
		}

		switch {
		case id == "port/luau/packet":
			packetCount++
			if !strings.Contains(note, "implemented, accepted in docs/roadmap-evidence/luau.sexp") {
				return fmt.Errorf("%s: note missing implemented/accepted statement: %q", id, note)
			}
		case id == "port/luau/generator" || id == "port/luau/runtime" || id == "port/luau/conformance":
			portMetaCount++
			if !strings.Contains(note, "implemented, accepted in docs/roadmap-evidence/luau.sexp") {
				return fmt.Errorf("%s: note missing implemented/accepted statement: %q", id, note)
			}
		case strings.HasPrefix(id, "port/luau/fixed/"):
			fixedCount++
			if !strings.Contains(note, "implemented; per-row proof suite owed (cards schema-luau-fixedproof-*)") {
				return fmt.Errorf("%s: note missing fixedproof statement: %q", id, note)
			}
			hasCut := strings.Contains(note, "rework under strict-only/leg-luau owed")
			if luauFixedCutRows[id] && !hasCut {
				return fmt.Errorf("%s: one-read cut fixed row missing rework under strict-only/leg-luau owed: %q", id, note)
			}
			if !luauFixedCutRows[id] && hasCut {
				return fmt.Errorf("%s: non-cut fixed row unexpectedly claims strict-only/leg-luau rework: %q", id, note)
			}
		case strings.HasPrefix(id, "port/luau/vt/"):
			vtCount++
			if luauVTContractIndependentRows[id] {
				if !strings.Contains(note, "contract-independent") {
					return fmt.Errorf("%s: accelerator/descriptor row missing contract-independent statement: %q", id, note)
				}
			} else {
				if !strings.Contains(note, "wait for the cut") || !strings.Contains(note, "rework under strict-only/leg-luau owed") {
					return fmt.Errorf("%s: variable-table row missing wait-for-the-cut / strict-only/leg-luau statement: %q", id, note)
				}
			}
		default:
			return fmt.Errorf("unexpected port/luau/* task id %q", id)
		}
	}

	if packetCount != 1 || fixedCount != 56 || vtCount != 31 || portMetaCount != 3 {
		return fmt.Errorf("unexpected task counts: packet=%d fixed=%d vt=%d meta=%d", packetCount, fixedCount, vtCount, portMetaCount)
	}

	distIdx := strings.Index(luauMkText, "\ndist-luau:\n")
	if distIdx < 0 {
		return fmt.Errorf("make/luau.mk: dist-luau target not found")
	}
	distRecipe := luauMkText[distIdx:]
	if endIdx := strings.Index(distRecipe[len("\ndist-luau:\n"):], "\n\n"); endIdx >= 0 {
		distRecipe = distRecipe[:len("\ndist-luau:\n")+endIdx]
	}
	sha256Idx := strings.Index(distRecipe, "sha256sum")
	shasumIdx := strings.Index(distRecipe, "shasum -a 256")
	if sha256Idx < 0 || shasumIdx < 0 || sha256Idx > shasumIdx {
		return fmt.Errorf("make/luau.mk: dist-luau recipe must try sha256sum first and fall back to shasum -a 256")
	}

	serSha256Idx := strings.Index(luauSerializeMkText, "sha256sum serialize*.luau")
	serShasumIdx := strings.Index(luauSerializeMkText, "shasum -a 256 serialize*.luau")
	if serSha256Idx < 0 || serShasumIdx < 0 || serSha256Idx > serShasumIdx {
		return fmt.Errorf("make/luau-serialize.mk: vendor-serialize-luau recipe must try sha256sum first and fall back to shasum -a 256")
	}

	return nil
}

func TestLuauRoadmapNotesAreCurrent(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	roadmapBytes, err := os.ReadFile(filepath.Join(root, "docs", "roadmap.sexp"))
	if err != nil {
		t.Fatal(err)
	}
	luauMkBytes, err := os.ReadFile(filepath.Join(root, "make", "luau.mk"))
	if err != nil {
		t.Fatal(err)
	}
	luauSerializeMkBytes, err := os.ReadFile(filepath.Join(root, "make", "luau-serialize.mk"))
	if err != nil {
		t.Fatal(err)
	}

	roadmapText := string(roadmapBytes)
	luauMkText := string(luauMkBytes)
	luauSerializeMkText := string(luauSerializeMkBytes)

	if err := checkLuauRoadmapNotesAndMake(roadmapText, luauMkText, luauSerializeMkText); err != nil {
		t.Fatal(err)
	}

	// Negative controls on broken twins of the guarded files.
	staleTwin := strings.Replace(
		roadmapText,
		`:id "port/luau/packet" :type :task :title "Luau (the Roblox dialect of Lua) packet leg: the examples against the shared wire vectors" :state :todo :evidence nil :note "`,
		`:id "port/luau/packet" :type :task :title "Luau (the Roblox dialect of Lua) packet leg: the examples against the shared wire vectors" :state :todo :evidence nil :note "Not started. `,
		1,
	)
	if err := checkLuauRoadmapNotesAndMake(staleTwin, luauMkText, luauSerializeMkText); err == nil {
		t.Fatal("broken twin with Not started in port/luau/packet unexpectedly passed")
	}

	noLegTwin := strings.Replace(
		roadmapText,
		`:id "port/luau/conformance" :type :task :title "Luau (the Roblox dialect of Lua) port: the conformance leg: the shared wire vectors the C++ reference writes, the packet-text, packet-wide and packet-defaults gates, the table goldens, and their negative controls" :state :todo :evidence nil :note "`,
		`:id "port/luau/conformance" :type :task :title "Luau (the Roblox dialect of Lua) port: the conformance leg: the shared wire vectors the C++ reference writes, the packet-text, packet-wide and packet-defaults gates, the table goldens, and their negative controls" :state :todo :evidence nil :note "no generator, runtime or conformance leg exists `,
		1,
	)
	if err := checkLuauRoadmapNotesAndMake(noLegTwin, luauMkText, luauSerializeMkText); err == nil {
		t.Fatal("broken twin with no generator, runtime or conformance leg exists unexpectedly passed")
	}

	noFallbackMkTwin := strings.ReplaceAll(luauMkText, "shasum -a 256", "sha256sum")
	if err := checkLuauRoadmapNotesAndMake(roadmapText, noFallbackMkTwin, luauSerializeMkText); err == nil {
		t.Fatal("broken twin of make/luau.mk without shasum -a 256 fallback unexpectedly passed")
	}

	noFallbackSerializeMkTwin := strings.ReplaceAll(luauSerializeMkText, "shasum -a 256", "sha256sum")
	if err := checkLuauRoadmapNotesAndMake(roadmapText, luauMkText, noFallbackSerializeMkTwin); err == nil {
		t.Fatal("broken twin of make/luau-serialize.mk without shasum -a 256 fallback unexpectedly passed")
	}
}
