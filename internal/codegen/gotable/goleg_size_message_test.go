package gotable

import (
	"bytes"
	"os"
	"testing"
)

// TestGoLegSizeMessage pins the message group's hand-written emitter size.
// At schema-go's 22e1b4f3 the group was 1176 lines; the budget is 5% under,
// 1117. A later card that grows the group must say so here and in its report.
func TestGoLegSizeMessage(t *testing.T) {
	t.Parallel()
	const base, budget = 1176, 1117
	files := []string{
		"message.go",
		"messagecontainers.go",
		"messageread.go",
		"messageregion.go",
		"messagewrite.go",
		"retain.go",
		"retainmessage.go",
	}
	total := 0
	for _, name := range files {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		total += bytes.Count(data, []byte("\n"))
	}
	if total > budget {
		t.Fatalf("message group is %d lines, above the %d-line budget (base %d)", total, budget, base)
	}
	t.Logf("message group is %d lines (base %d, budget %d)", total, base, budget)
}
