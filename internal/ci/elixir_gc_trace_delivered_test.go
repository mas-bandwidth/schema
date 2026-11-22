package ci

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
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
	// The offsets gate is the lang-clean (elixir offsets) row, beside the
	// agreement gate's shard rows (#4530: on shard 1 it took that row past
	// the two-minute cap; #4597). The plan must write the row from its
	// elixir-offsets leg, and the row must run the gate.
	if !strings.Contains(wf, `if ($1 == "elixir-offsets") { printf "%s{\"lang\":\"elixir\",\"shard\":\"offsets\"}"`) {
		t.Error("ci-fast.yml plan no longer writes the lang-clean (elixir offsets) row")
	}
	if !strings.Contains(wf, "                offsets)\n                  set -- tables-elixir-offsets\n") {
		t.Error("ci-fast.yml lang-clean (elixir offsets) does not run tables-elixir-offsets")
	}
	if !strings.Contains(wf, `set -- "tables-elixir-accessor-descriptor-agreement-shard-${SHARD%/*}"`) {
		t.Error("ci-fast.yml lang-clean (elixir K/N) does not run tables-elixir-accessor-descriptor-agreement-shard-K")
	}
}

// The agreement gate's per-commit rows must be exactly shards 1..N of
// make/elixir.mk's ELIXIR_AGREEMENT_SHARDS: a row short and some units are
// never walked per commit; a row over and its make target refuses. The plan
// names the legs elixir-agreement-1..N and writes each as row elixir K/N with
// N its elixir_clean_shards, which must be the same N.
func TestCIFastLangCleanElixirAgreementRowsMatchShards(t *testing.T) {
	root := repoRoot(t)
	mk, err := os.ReadFile(filepath.Join(root, "make", "elixir.mk"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^ELIXIR_AGREEMENT_SHARDS := ([0-9]+)$`).FindSubmatch(mk)
	if m == nil {
		t.Fatal("make/elixir.mk sets no ELIXIR_AGREEMENT_SHARDS")
	}
	n, err := strconv.Atoi(string(m[1]))
	if err != nil || n < 1 {
		t.Fatalf("ELIXIR_AGREEMENT_SHARDS %q is not a positive count", m[1])
	}
	raw, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "ci-fast.yml"))
	if err != nil {
		t.Fatal(err)
	}
	want := "elixir-offsets"
	for k := 1; k <= n; k++ {
		want += fmt.Sprintf(" elixir-agreement-%d", k)
	}
	line := `clean_legs="$clean_legs ` + want + `"`
	if !strings.Contains(string(raw), line) {
		t.Fatalf("ci-fast.yml plan must add the elixir rows as %s (ELIXIR_AGREEMENT_SHARDS=%d)", line, n)
	}
	if shards := fmt.Sprintf("\n          elixir_clean_shards=%d\n", n); !strings.Contains(string(raw), shards) {
		t.Fatalf("ci-fast.yml plan must write the elixir-agreement rows as K/%d: it needs elixir_clean_shards=%d (ELIXIR_AGREEMENT_SHARDS)", n, n)
	}
}
