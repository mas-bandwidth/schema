package compiler

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestSpecPacketGuaranteesNameEveryTarget(t *testing.T) {
	t.Parallel()
	read := func(path string) string {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	section := func(page, start, end string) string {
		t.Helper()
		a := strings.Index(page, start)
		if a < 0 {
			t.Fatalf("missing section %q", start)
		}
		b := strings.Index(page[a+len(start):], end)
		if b < 0 {
			t.Fatalf("missing section %q", end)
		}
		return page[a : a+len(start)+b]
	}
	spec := read("../docs/SPEC.md")
	algorithm := read("../docs/PACKET-WIRE-ALGORITHM.md")
	scope := section(spec, "### 4.6 ", "## 7.") +
		section(algorithm, "## 3. ", "## 4. ") +
		section(algorithm, "## 5. ", "## 6. ") +
		section(algorithm, "## 7. ", "## 8. ")
	stale := regexp.MustCompile(`(?i)\b(?:all nine|nine targets)\b`)
	if found := stale.FindAllString(scope, -1); len(found) != 0 {
		t.Errorf("packet scope still limits guarantees to nine: %v", found)
	}
	for _, phrase := range []string{
		"every packet target, by the leg's own mechanism (§5.1)",
		"in every packet target, in every build mode",
	} {
		if !strings.Contains(scope, phrase) {
			t.Errorf("packet scope missing %q", phrase)
		}
	}
	stringsSection := section(spec, "### 4.7 ", "### 4.8 ")
	if strings.Contains(stringsSection, "none\nemits a runtime string call") ||
		strings.Contains(stringsSection, "none emits a runtime string call") {
		t.Error("§4.7 says no backend emits a runtime string call")
	}
	matrix := section(algorithm, "## 7. ", "## 8. ")
	if !strings.Contains(matrix, "| construct | fixture | c | cpp | cs | rust | java | js | dart | elixir | go | lua | luau | squirrel | moo | goose | lobster |") {
		t.Error("§7 matrix header must name all fifteen packet targets in §5.1 order")
	}
}
