package compiler

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// runCppTableProbe writes the emitted sources and one driver beside them,
// compiles at -O1 under ASan/UBSan, and runs the driver under a hard timeout: a
// driver that will not stop is a failure, so the wall clock is part of the
// assertion.
func runCppTableProbe(t *testing.T, files map[string][]byte, source string) {
	t.Helper()
	slowtest.Gate(t, "the C++ compiler")
	cxx, err := exec.LookPath("c++")
	if err != nil {
		t.Skip("generated C++ execution requires c++")
	}
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.cpp"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "probe")
	args := []string{"-std=c++17", "-O1", "-g", "-Wall", "-Wextra", "-Werror", "-fsanitize=address,undefined", "-fno-sanitize-recover=all", filepath.Join(dir, "main.cpp")}
	for name := range files {
		if strings.HasSuffix(name, ".cpp") {
			args = append(args, filepath.Join(dir, name))
		}
	}
	args = append(args, "-o", bin)
	if out, err := exec.Command(cxx, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, out)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, bin).CombinedOutput(); err != nil {
		t.Fatalf("execute: %v\n%s", err, out)
	}
}
