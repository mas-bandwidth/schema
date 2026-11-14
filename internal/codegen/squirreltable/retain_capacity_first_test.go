package squirreltable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestSquirrelRetainChecksCapacityFirst(t *testing.T) {
	t.Parallel()

	old := `package probe
table Root { marker int32 }
`
	newer := `package probe
table Root { marker int32
 future [..512]string }
`

	// Instrument only this generated runtime copy. The count is the number of
	// payload bytes the resolver actually copies, not elapsed time.
	tables, err := squirreltable.Generate(retainUnitFrom(t, old))
	if err != nil {
		t.Fatal(err)
	}
	const runtimeName = "schema_table_retain.nut"
	runtime, ok := tables[runtimeName]
	if !ok {
		t.Fatalf("generated runtime lacks %s", runtimeName)
	}
	runtimeText := string(runtime)
	needle := "local _M = vargv[0];"
	if !strings.Contains(runtimeText, needle) {
		t.Fatal("retain runtime instrumentation anchor missing")
	}
	runtimeText = strings.Replace(runtimeText, needle, needle+"\n::retainResolveBytes <- 0;", 1)
	needle = "for (local i = 0; i < n; i++) out.append(b[at + i] & 0xff);"
	replacement := "for (local i = 0; i < n; i++) { ::retainResolveBytes++; out.append(b[at + i] & 0xff); }"
	if strings.Contains(runtimeText, needle) {
		runtimeText = strings.Replace(runtimeText, needle, replacement, 1)
	} else {
		needle = "for (local i = 0; i < n && !out.failed; i++) outAppend(out, b[at + i] & 0xff);"
		replacement = "for (local i = 0; i < n && !out.failed; i++) { ::retainResolveBytes++; outAppend(out, b[at + i] & 0xff); }"
		if !strings.Contains(runtimeText, needle) {
			t.Fatal("retain raw-copy instrumentation anchor missing")
		}
		runtimeText = strings.Replace(runtimeText, needle, replacement, 1)
	}
	tables[runtimeName] = []byte(runtimeText)

	files, err := squirrel.Generate(retainUnitFrom(t, old))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	outDir := filepath.Join(dir, "probe")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		writeFile(t, filepath.Join(outDir, name), data)
	}
	for name, data := range tables {
		writeFile(t, filepath.Join(outDir, name), data)
	}

	inputs := make(map[string][]byte)
	wantByCapacity := make(map[int][]byte)
	for _, capacity := range []int{64, 128} {
		items := capacity * 4
		var json strings.Builder
		json.WriteString(`{"marker":7,"future":[`)
		for i := 0; i < items; i++ {
			if i != 0 {
				json.WriteByte(',')
			}
			json.WriteString(`"xy"`)
		}
		json.WriteString(`] }`)

		newModel := tabletext.NewModel(retainUnitFrom(t, newer))
		newValue := newModel.New(newModel.Lookup("Root"))
		var newReport tabletext.Report
		if !newModel.Read(newValue, []byte(json.String()), &newReport) {
			t.Fatalf("Go writer rejected the capacity-%d input: %+v", capacity, newReport)
		}
		wire, err := tablewire.Encode(newModel, newValue)
		if err != nil {
			t.Fatalf("Go writer encode at capacity %d: %v", capacity, err)
		}
		inputs[fmt.Sprintf("wire%d", capacity)] = wire

		// The Go engine confirms that this same oversized unknown field is
		// dropped as one record at each caller capacity.
		oldModel := tabletext.NewModel(retainUnitFrom(t, old))
		oldValue := oldModel.New(oldModel.Lookup("Root"))
		store := &tablewire.Retain{Capacity: capacity, IdCapacity: 32}
		var report tabletext.Report
		if _, err := tablewire.DecodeRetain(oldModel, oldValue, wire, store, &report); err != nil {
			t.Fatalf("Go retain read at capacity %d: %v", capacity, err)
		}
		if report.Retained != 0 || report.RetainLost != 1 {
			t.Fatalf("Go retain at capacity %d kept %d and lost %d, want 0 and 1", capacity, report.Retained, report.RetainLost)
		}
		want, err := tablewire.EncodeRetain(oldModel, oldValue, store, &report)
		if err != nil {
			t.Fatalf("Go retain write at capacity %d: %v", capacity, err)
		}
		wantByCapacity[capacity] = want
	}

	main := `local I = dofile("inputs.nut", true);
local schema = dofile("probe/schema_module.nut", true);
local T = schema.require("ProbeTables");
local R = schema.require("schema_table_retain");
local rep = function() { return { unknown=0, mismatch=0, widened=0, oor=0, repeated=0, bodyDamaged=false, refused=false }; };
foreach (capacity in [64, 128]) {
  ::retainResolveBytes = 0;
  local wire = capacity == 64 ? I.wire64 : I.wire128;
  local report = rep();
  local store = { capacity = capacity, idCapacity = 32 };
  local v = R.LoadRetain(T, "Root", wire, store, report);
  if (v == null) throw "retain load refused";
  local n = R.MeasureRetain(T, "Root", v, store);
  local out = blob(n);
  local saved = R.SaveRetain(T, "Root", v, store, out, report);
  print("case " + capacity + " " + report.retained + " " + report.retainLost + " " + ::retainResolveBytes + " " + saved + "\n");
  ::hexline(out);
}
`
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(main))
	stdout := string(sqrun.Run(t, dir, "main.nut", inputs))
	lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("capacity probe printed %d lines, want 4:\n%s", len(lines), stdout)
	}
	for i, capacity := range []int{64, 128} {
		var gotCapacity, retained, lost, resolved, saved int
		if _, err := fmt.Sscanf(lines[2*i], "case %d %d %d %d %d", &gotCapacity, &retained, &lost, &resolved, &saved); err != nil {
			t.Fatalf("probe line %q: %v", lines[2*i], err)
		}
		if gotCapacity != capacity || retained != 0 || lost != 1 {
			t.Errorf("Squirrel retain at capacity %d: got capacity=%d retained=%d lost=%d, want %d, 0, 1", capacity, gotCapacity, retained, lost, capacity)
		}
		if resolved > capacity+3 {
			t.Errorf("Squirrel retain at capacity %d resolved %d payload bytes before dropping; want at most capacity plus one 3-byte entry", capacity, resolved)
		}
		want := fmt.Sprintf("%x", wantByCapacity[capacity])
		if lines[2*i+1] != want || saved != len(wantByCapacity[capacity]) {
			t.Errorf("Squirrel retain at capacity %d saved %d bytes %s, Go saved %d bytes %s", capacity, saved, lines[2*i+1], len(wantByCapacity[capacity]), want)
		}
	}
}
