package compiler

import (
	"strings"
	"testing"
)

func TestCRetainNoneRuntimeNameCollision(t *testing.T) {
	errs := checkErrors(t, "package probe\ntable table_retain_none { value int32 }\n")
	for _, err := range errs {
		if strings.Contains(err.Error(), "TABLE-wire runtime") && strings.Contains(err.Error(), "table_retain_none") {
			return
		}
	}
	t.Fatalf("C retention runtime name collision was not refused by name: %v", errs)
}
