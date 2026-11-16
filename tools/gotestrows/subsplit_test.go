package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Exercise Go's actual hierarchical -run/-skip matcher. Parent discovery
// repeats, but each leaf (including future on-disk corpus names) runs once.
func TestSubsplitRunsEverySeedExactlyOnce(t *testing.T) {
	t.Parallel()
	tb, err := readTable(filepath.Join("..", "..", tablePath))
	if err != nil {
		t.Fatal(err)
	}
	var subs []subsplit
	for _, sub := range tb.subsplits["./internal/fuzz"] {
		if sub.parent == "^FuzzTableFrontEnd$" {
			subs = append(subs, subsplit{parent: "^FuzzSeeds$", child: sub.child})
		}
	}
	if len(subs) == 0 {
		t.Fatal("the table front-end seed subdivisions are missing")
	}
	rows := plan(table{subsplits: map[string][]subsplit{".": subs}}, []string{"."}, false, 60)
	dir := t.TempDir()
	write := func(name, text string) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module fixture\n\ngo 1.26\n")
	write("seed_test.go", `package fixture
import "testing"
func FuzzSeeds(f *testing.F) {
    for i := 0; i < 20; i++ { f.Add(i) }
    f.Fuzz(func(t *testing.T, n int) {})
}
func FuzzOther(f *testing.F) {
    f.Add(0)
    f.Fuzz(func(t *testing.T, n int) {})
}
func TestNew(t *testing.T) {}
`)
	for _, name := range []string{"named-seed", "0123abcd", "seed#123x"} {
		write("testdata/fuzz/FuzzSeeds/"+name, "go test fuzz v1\nint(123)\n")
	}
	run := func(row Row) map[string]int {
		t.Helper()
		args := []string{"test", "-json", "-count=1", "-timeout=2m"}
		if row.Run != "" {
			args = append(args, "-run", row.Run)
		}
		if row.Skip != "" {
			args = append(args, "-skip", row.Skip)
		}
		args = append(args, ".")
		cmd := exec.Command("go", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOWORK=off", "GOPROXY=off", "GOTOOLCHAIN=local")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("go %q: %v\n%s", args, err, out)
		}
		counts, err := seedExecutions(out)
		if err != nil {
			t.Fatalf("go %q: %v\n%s", args, err, out)
		}
		return counts
	}
	baseline := run(Row{})
	if len(baseline) != 25 { // 20 numbered + 3 custom + sibling fuzz seed + TestNew
		t.Fatalf("fixture ran %d leaves, want 25: %v", len(baseline), baseline)
	}
	var observed []map[string]int
	for _, row := range rows {
		counts := run(row)
		if len(counts) == 0 {
			t.Fatalf("row %+v executes no leaves", row)
		}
		observed = append(observed, counts)
	}
	if err := compareExecutions(baseline, observed); err != nil {
		t.Fatal(err)
	}
	// Dropping the child complement must lose suffix9 and custom/hash seeds.
	broken := append([]map[string]int(nil), observed[:len(observed)-2]...)
	broken = append(broken, observed[len(observed)-1])
	if err := compareExecutions(baseline, broken); err == nil {
		t.Fatal("dropping the child complement escaped the coverage check")
	}
	// An overlapping bucket must be rejected as duplicate execution.
	if err := compareExecutions(baseline, append(observed, observed[0])); err == nil {
		t.Fatal("duplicating a bucket escaped the coverage check")
	}
}

func seedExecutions(out []byte) (map[string]int, error) {
	counts := map[string]int{}
	d := json.NewDecoder(bytes.NewReader(out))
	for {
		var event struct{ Action, Test string }
		if err := d.Decode(&event); err != nil {
			if err == io.EOF {
				return counts, nil
			}
			return nil, err
		}
		if event.Action == "run" && (strings.Contains(event.Test, "/") || strings.HasPrefix(event.Test, "Test")) {
			counts[event.Test]++
		}
	}
}

func compareExecutions(baseline map[string]int, rows []map[string]int) error {
	counts := map[string]int{}
	for _, row := range rows {
		for name, n := range row {
			counts[name] += n
		}
	}
	for name, n := range baseline {
		if n != 1 || counts[name] != 1 {
			return fmt.Errorf("%s: unsharded=%d, rows=%d; want exactly once", name, n, counts[name])
		}
	}
	for name := range counts {
		if _, ok := baseline[name]; !ok {
			return fmt.Errorf("rows ran unexpected seed %s", name)
		}
	}
	return nil
}
