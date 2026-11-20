package ci

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var gcTraceEnableRE = regexp.MustCompile(`:erlang\.trace\([^\n]*true,\s*\[:garbage_collection\b`)

func checkElixirGCTraceMeter(rel, src string) error {
	locs := gcTraceEnableRE.FindAllStringIndex(src, -1)
	if len(locs) == 0 {
		return nil
	}
	for i, loc := range locs {
		tail := src[loc[1]:]
		if i+1 < len(locs) {
			tail = src[loc[1]:locs[i+1][0]]
		}
		const stopCall = ":erlang.trace(me, false, [:garbage_collection])"
		_, afterStopTail, ok := strings.Cut(tail, stopCall)
		if !ok {
			return fmt.Errorf("%s: :garbage_collection trace #%d has no matching %s", rel, i+1, stopCall)
		}
		between, _, ok := strings.Cut(afterStopTail, "send(tracer, {:reclaimed,")
		if !ok {
			return fmt.Errorf("%s: :garbage_collection trace #%d has no send(tracer, {:reclaimed, ...})", rel, i+1)
		}
		beforeDeliver, afterDeliver, ok := strings.Cut(between, ":erlang.trace_delivered(")
		if !ok {
			return fmt.Errorf("%s: :garbage_collection trace #%d sends {:reclaimed, ...} without flushing :erlang.trace_delivered(me) first", rel, i+1)
		}
		if !strings.Contains(afterDeliver, "{:trace_delivered,") {
			return fmt.Errorf("%s: :garbage_collection trace #%d calls :erlang.trace_delivered without awaiting {:trace_delivered, ...} before send(tracer, {:reclaimed, ...})", rel, i+1)
		}
		if !strings.Contains(beforeDeliver, "used()") {
			return fmt.Errorf("%s: :garbage_collection trace #%d must snapshot live1 = used() after stopping trace and before :erlang.trace_delivered allocates its ref", rel, i+1)
		}
	}
	return nil
}

// TestElixirGCTraceMetersFlushTraceDelivered guards all BEAM :garbage_collection
// trace meters against the cross-scheduler race where send(tracer, {:reclaimed, me})
// overtakes the closing :gc_major_end trace message unless synchronized with
// :erlang.trace_delivered(me). Runs in <1s in ci-policy on every PR.
func TestElixirGCTraceMetersFlushTraceDelivered(t *testing.T) {
	root := repoRoot(t)
	expected := map[string]bool{
		"internal/codegen/elixirtable/offsets_test.go": false,
		"test/elixir-alloc/main.exs":                   false,
		"test/elixir-soak/main.exs":                    false,
	}
	for _, sub := range []string{"internal/codegen", "test", "bench"} {
		err := filepath.WalkDir(filepath.Join(root, sub), func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				return nil
			}
			ext := filepath.Ext(path)
			if ext != ".go" && ext != ".ex" && ext != ".exs" {
				return nil
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			text := string(raw)
			if !gcTraceEnableRE.MatchString(text) {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			expected[rel] = true
			if err := checkElixirGCTraceMeter(rel, text); err != nil {
				t.Error(err)
				return nil
			}
			sabotaged := strings.Replace(text, ":erlang.trace_delivered(", ":erlang.self(", 1)
			if err := checkElixirGCTraceMeter(rel, sabotaged); err == nil {
				t.Errorf("%s: checkElixirGCTraceMeter accepted sabotaged meter without :erlang.trace_delivered", rel)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for rel, seen := range expected {
		if !seen {
			t.Errorf("expected BEAM GC-trace meter %s was not discovered", rel)
		}
	}
}

func TestCIFastLangCleanElixirRunsOffsetsGate(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "ci-fast.yml"))
	if err != nil {
		t.Fatal(err)
	}
	wf := string(raw)
	if !strings.Contains(wf, "tables-elixir-accessor-descriptor-agreement tables-elixir-offsets") {
		t.Fatal("ci-fast.yml lang-clean (elixir) must run tables-elixir-offsets alongside tables-elixir-accessor-descriptor-agreement")
	}
}
