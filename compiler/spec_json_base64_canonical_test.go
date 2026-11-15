// TestSpecJsonBytesAreCanonicalBase64 pins docs/SPEC-TABLES.md §16.2's JSON
// read of a bytes value (docs/rulings.json `json-base64-canonical`): the text
// it accepts is exactly the text the writer writes for those bytes, CANONICAL
// base64 as §16.8 defines it — standard alphabet, padded, pad bits zero. Any
// other spelling that decodes is refused (recommended name
// `base64_not_canonical`); text that does not decode stays refused.
//
// The retired sentence is the unpadded read: "padded and unpadded both read"
// and the unpadded-symbol-count rule after the table. The vector corpus under
// testdata/wire/json-base64-canonical/ is the engine and port cards' input.
package compiler

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSpecJsonBytesAreCanonicalBase64(t *testing.T) {
	t.Parallel()

	t.Run("§16.2 states canonical base64 and retires the unpadded read", func(t *testing.T) {
		t.Parallel()
		page := readSpecTables(t)
		section := specTablesSection(t, page, "### 16.2 ", "### 16.3 ")
		flat := func(s string) string { return strings.Join(strings.Fields(s), " ") }
		text := flat(section)

		for _, want := range []string{
			"CANONICAL base64",
			"pad bits zero",
			"the text the writer writes",
			"base64_not_canonical",
			"§16.8",
			"recommended",
		} {
			if !strings.Contains(text, want) {
				t.Errorf("§16.2 no longer states %q", want)
			}
		}
		for _, stale := range []string{
			"padded and unpadded both read",
			"An unpadded body is base64",
			"symbol count is zero, two or three modulo four",
		} {
			if strings.Contains(text, flat(stale)) {
				t.Errorf("§16.2 still states the retired unpadded read %q", stale)
			}
		}
	})

	t.Run("docs/rulings.json carries json-base64-canonical", func(t *testing.T) {
		t.Parallel()
		data, err := os.ReadFile("../docs/rulings.json")
		if err != nil {
			t.Fatalf("docs/rulings.json: %v", err)
		}
		var file struct {
			Rulings []struct {
				ID         string   `json:"id"`
				Date       string   `json:"date"`
				Quotes     []string `json:"quotes"`
				Source     string   `json:"source"`
				Supersedes []string `json:"supersedes"`
			} `json:"rulings"`
		}
		if err := json.Unmarshal(data, &file); err != nil {
			t.Fatalf("docs/rulings.json: %v", err)
		}
		const rulingText = "ruled 2026-11-10: the JSON read of a bytes value accepts only canonical base64, the text the writer writes; any other spelling is refused"
		found := false
		for _, r := range file.Rulings {
			if r.ID != "json-base64-canonical" {
				continue
			}
			found = true
			if r.Date != "2026-11-10" {
				t.Errorf("json-base64-canonical: date %q, want 2026-11-10", r.Date)
			}
			quotes := strings.Join(r.Quotes, "\n")
			if !strings.Contains(quotes, rulingText) {
				t.Errorf("json-base64-canonical: quotes do not carry the ruling text %q", rulingText)
			}
			if !strings.Contains(r.Source, "strict wins") || !strings.Contains(r.Source, "coordinator") {
				t.Errorf("json-base64-canonical: source does not state the standing rule and the coordinator: %q", r.Source)
			}
			supersedes := strings.Join(r.Supersedes, "\n")
			if !strings.Contains(supersedes, "§16.2") || !strings.Contains(supersedes, "padded and unpadded both read") {
				t.Errorf("json-base64-canonical: supersedes does not name §16.2's unpadded sentence: %q", supersedes)
			}
		}
		if !found {
			t.Error("docs/rulings.json carries no ruling json-base64-canonical")
		}
	})

	t.Run("the vectors exist and the manifest names each with its verdict", func(t *testing.T) {
		t.Parallel()
		dir := filepath.Join("..", "testdata", "wire", "json-base64-canonical")
		data, err := os.ReadFile(filepath.Join(dir, "MANIFEST.tsv"))
		if err != nil {
			t.Fatalf("vectors manifest: %v", err)
		}
		want := map[string]struct {
			json    string
			verdict string
			reason  string
		}{
			"canonical-two-bytes-read": {"canonical-two-bytes-read.json", "read", "-"},
			"padded-bits-set-refused":  {"padded-bits-set-refused.json", "refused", "base64_not_canonical"},
			"unpadded-refused":         {"unpadded-refused.json", "refused", "base64_not_canonical"},
			"not-base64-refused":       {"not-base64-refused.json", "refused", "-"},
		}
		seen := make(map[string]bool)
		for line := range strings.SplitSeq(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			cols := strings.Split(line, "\t")
			if len(cols) < 6 {
				t.Errorf("manifest row %q has %d columns, want name, schema, root, json, verdict, reason", line, len(cols))
				continue
			}
			name, schema, root, jsonFile, verdict, reason := cols[0], cols[1], cols[2], cols[3], cols[4], cols[5]
			expected, ok := want[name]
			if !ok {
				t.Errorf("manifest names an unexpected vector %q", name)
				continue
			}
			seen[name] = true
			if schema != "tables/stream/Stream.schema" {
				t.Errorf("%s: schema %q, want tables/stream/Stream.schema", name, schema)
			}
			if root != "Chunk" {
				t.Errorf("%s: root %q, want Chunk", name, root)
			}
			if jsonFile != expected.json {
				t.Errorf("%s: json file %q, want %q", name, jsonFile, expected.json)
			}
			if verdict != expected.verdict {
				t.Errorf("%s: verdict %q, want %q", name, verdict, expected.verdict)
			}
			if expected.reason != "-" && reason != expected.reason {
				t.Errorf("%s: reason %q, want %q", name, reason, expected.reason)
			}
			if _, err := os.Stat(filepath.Join("..", schema)); err != nil {
				t.Errorf("%s: schema file: %v", name, err)
			}
			body, err := os.ReadFile(filepath.Join(dir, jsonFile))
			if err != nil {
				t.Errorf("%s: json file: %v", name, err)
				continue
			}
			if !strings.Contains(string(body), `"data"`) {
				t.Errorf("%s: JSON text does not carry the bytes(8) field %q", name, "data")
			}
		}
		for name := range want {
			if !seen[name] {
				t.Errorf("manifest omits vector %q", name)
			}
		}

		// The canonical body is exactly the writer's spelling of two bytes, and
		// the two non-canonical bodies decode to the same two bytes or fail to
		// decode at all. These are the rule's edges, not a second grammar.
		canonical, err := os.ReadFile(filepath.Join(dir, "canonical-two-bytes-read.json"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(canonical), `"QUI="`) {
			t.Errorf("canonical vector does not hold QU I=, the canonical spelling of A B: %s", canonical)
		}
		decoded, err := base64.StdEncoding.DecodeString("QUI=")
		if err != nil || string(decoded) != "AB" {
			t.Fatalf("QUI= does not decode to AB: %q %v", decoded, err)
		}
		if !strings.Contains(string(mustReadVector(t, dir, "padded-bits-set-refused.json")), `"QUJ="`) {
			t.Error("the pad-bit vector does not hold QU J=")
		}
		if _, err := base64.StdEncoding.DecodeString("QUJ="); err != nil {
			t.Errorf("QUJ= must decode for the pad-bit case to be a spelling of AB: %v", err)
		}
		if !strings.Contains(string(mustReadVector(t, dir, "unpadded-refused.json")), `"QUI"`) {
			t.Error("the unpadded vector does not hold QU I")
		}
		if _, err := base64.StdEncoding.DecodeString("QUI"); err == nil {
			t.Error("QUI must not decode as padded standard base64")
		}
		if !strings.Contains(string(mustReadVector(t, dir, "not-base64-refused.json")), "$$$$") {
			t.Error("the not-base64 vector does not hold a body outside the base64 alphabet")
		}
	})
}

func mustReadVector(t *testing.T, dir, name string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Errorf("%s: %v", name, err)
	}
	return body
}
