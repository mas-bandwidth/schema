package check

import (
	"os"
	"testing"
)

func TestTwoPlusThree(t *testing.T) {
	marker := os.Getenv("MUTATION_CORPUS_PHASE")
	if marker != "" {
		if _, err := os.Stat(marker); err == nil {
			t.Fatal("previous test left a marker")
		}
	}
	if !Five(2, 3) {
		if marker != "" {
			if err := os.WriteFile(marker, []byte("previous run\n"), 0644); err != nil {
				t.Fatal(err)
			}
		}
		t.Fatal("two plus three should be five")
	}
}
