package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repoRoot is where the manifest's paths start: the driver is run from there.
const repoRoot = "../../.."

// TestWriteRefusalNeverNarrowsAnOrdinalOrATag is the reviewer's repro with her
// controls (F-100, F-101). An enum's ordinal and a union's tag are one byte in
// this leg's storage, so 256 and 4294967296 cannot be held: reflect's SetUint
// kept their low byte, which is 0, a lawful member, and the row answered
// `wrote` for a value that was never written. The driver answers
// `unrepresentable`. The controls keep it honest in both directions: 0 and 1
// are written, and 9, which the byte holds and the declaration does not admit,
// is refused by the writer.
func TestWriteRefusalNeverNarrowsAnOrdinalOrATag(t *testing.T) {
	const (
		loadout = "testdata/conformance/tables/wire/loadout_full.bin"
		rootful = "testdata/conformance/tables/wire/root_full.bin"
		store   = "testdata/conformance/tables/wire/store_full.bin"
		evo     = "testdata/conformance/tables/wire/varevo3_msg.bin"
	)
	type target struct{ surface, unit, root, wire, path, kind string }
	targets := []target{
		{"write-refusal", "tabledemo", "LoadoutConfig", loadout, "grade", "ordinal"},
		{"write-refusal", "tabledemo", "RootConfig", rootful, "weapons[0].effect", "tag"},
		{"write-refusal-fixed", "tabledemo", "LoadoutConfig", loadout, "grade", "ordinal"},
		{"write-refusal-fixed", "tabledemo", "RootConfig", rootful, "weapons[0].effect", "tag"},
		{"write-refusal-json", "tabledemo", "LoadoutConfig", loadout, "grade", "ordinal"},
		{"write-refusal-json", "tabledemo", "RootConfig", rootful, "weapons[0].effect", "tag"},
		{"write-refusal-message", "backend_conn", "StorePurchase", store, "currency", "ordinal"},
		{"write-refusal-message", "varevo3_conn", "EvoMsg", evo, "payload", "tag"},
	}
	values := []struct{ text, want string }{
		{"0", "wrote"},
		{"1", "wrote"},
		{"9", "refused"},
		{"256", "unrepresentable"},
		{"4294967296", "unrepresentable"},
		{"-1", "unrepresentable"},
		{"18446744073709551616", "unrepresentable"},
	}
	lines := []line{
		{"connection", "backend_conn", "backenddemo"},
		{"connection", "varevo3_conn", "tblvarevo3"},
	}
	want := map[string]map[string]string{}
	for _, v := range values {
		for i, tg := range targets {
			name := strings.Join([]string{"probe", string(rune('a' + i)), tg.kind, strings.TrimPrefix(v.text, "-")}, "_")
			if strings.HasPrefix(v.text, "-") {
				name += "_negative"
			}
			lines = append(lines, line{tg.surface, name, tg.unit, tg.root, filepath.Join(repoRoot, tg.wire), tg.path, tg.kind, v.text, v.want})
			if want[tg.surface] == nil {
				want[tg.surface] = map[string]string{}
			}
			want[tg.surface][name] = v.want
		}
	}
	for surface, rows := range want {
		out := t.TempDir()
		if err := surfaceWriteRefusal(surface, lines, out); err != nil {
			t.Errorf("%s: %v", surface, err)
		}
		for name, verdict := range rows {
			got, err := os.ReadFile(filepath.Join(out, name))
			if err != nil {
				t.Errorf("%s %s: no answer: %v", surface, name, err)
				continue
			}
			if string(got) != verdict+"\n" {
				t.Errorf("%s %s: answered %q, want %q", surface, name, strings.TrimSpace(string(got)), verdict)
			}
		}
	}
}
