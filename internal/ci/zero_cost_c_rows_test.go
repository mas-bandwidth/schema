package ci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEachCZeroCostRowStartsFromAnEmptyTree: the C zero-cost gate's two
// rows, heavy and rest, each judge the whole C tree they leave, so each must
// first delete the whole tree, never only its own part: run after c-rest, a
// heavy row that cleared only its own directories judged the rest's files
// and failed ("did not write vocab9/Vocab9Table.c"). Each row is its own
// schema-test call, in either order.
func TestEachCZeroCostRowStartsFromAnEmptyTree(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	const clear = "\t@rm -rf $(BLOCKFUL_ROOT)/c $(BLOCKLESS_ROOT)/c\n"
	for _, row := range []string{"tables-block-zero-cost-c-heavy", "tables-block-zero-cost-c-rest"} {
		head := row + ": bin/schema $(BLOCKLESS_SCHEMA)\n"
		at := strings.Index(string(data), head)
		if at < 0 {
			t.Errorf("the Makefile has no rule %q", strings.TrimSpace(head))
			continue
		}
		if !strings.HasPrefix(string(data)[at+len(head):], clear) {
			t.Errorf("%s does not start by deleting the whole C tree (%q): it would judge what another row left", row, strings.TrimSpace(clear))
		}
	}
}
