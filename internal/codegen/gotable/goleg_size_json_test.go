package gotable

import (
	"os"
	"strings"
	"testing"
)

func TestGoLegSizeJSON(t *testing.T) {
	t.Parallel()
	files := []string{"json.go", "jsongraph.go", "jsonwide.go"}
	lines := 0
	for _, name := range files {
		source, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		lines += strings.Count(string(source), "\n")
	}
	const baseLines = 2828
	const fivePercentTarget = baseLines * 95 / 100
	const reachedBudget = 2818
	if lines > reachedBudget {
		t.Fatalf(
			"JSON Go emitter is %d lines, budget %d; BASE was %d, 5%% target was %d",
			lines, reachedBudget, baseLines, fivePercentTarget,
		)
	}
}
