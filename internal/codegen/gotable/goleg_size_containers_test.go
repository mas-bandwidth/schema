package gotable

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

var goLegSizeContainersFiles = []string{
	"lists.go", "maps.go", "unions.go", "wide.go", "integrity.go",
	"refusal.go", "source.go", "surface.go", "testgen.go",
}

const (
	goLegSizeContainersBaseLines   = 1558
	goLegSizeContainersBudgetLines = 1546
)

func TestGoLegSizeContainers(t *testing.T) {
	t.Parallel()
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate source files")
	}
	total := 0
	for _, name := range goLegSizeContainersFiles {
		data, err := os.ReadFile(filepath.Join(filepath.Dir(testFile), name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		total += bytes.Count(data, []byte("\n"))
		if len(data) > 0 && data[len(data)-1] != '\n' {
			total++
		}
	}
	if total > goLegSizeContainersBudgetLines {
		t.Fatalf("containers emitter has %d lines, budget is %d (BASE %d)", total, goLegSizeContainersBudgetLines, goLegSizeContainersBaseLines)
	}
}
