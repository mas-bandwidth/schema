package compiler

import (
	"strings"
	"testing"
)

func TestDartCookTextRuntimeNameCollision(t *testing.T) {
	errs := checkErrors(t, "package probe\nenum TableCookTextStart { A, B }\ntable Holder { value TableCookTextStart }\n")
	for _, err := range errs {
		if strings.Contains(err.Error(), "TABLE-wire runtime") && strings.Contains(err.Error(), "TableCookTextStart") {
			return
		}
	}
	t.Fatalf("cook text runtime name collision was not refused by name: %v", errs)
}
