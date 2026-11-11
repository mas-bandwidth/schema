package squirreltable_test

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/ci/hostilecorpus"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// wideBitsPrelude is the strict-load probe over the wide corpus: the plain
// <Root>Load is the strict read (§6.5), so a null answer is a refusal and a
// value prints the field the case names.
const wideBitsPrelude = `local inputs = dofile("inputs.nut", true);
local run = function(unit, mod, root, key, get) {
  local schema = dofile(unit + "/schema_module.nut", true);
  local T = schema.require(mod);
  local report = {};
  local v = T[root + "Load"](inputs[key], report);
  local verdict = v == null ? "refused" : "read";
  local value = "-";
  if (v != null && get != null) value = format("%d", get(v));
  print(key + "\t" + verdict + "\t" + value + "\n");
};
`

// wideBitsCase is one probe line: the strict read's verdict and, for an
// accepted twin, the value it read back.
type wideBitsCase struct {
	verdict string
	value   string
}

// wideBitsField names the bits field a wide case changes: the twin cases are
// named for their table, carrier and field.
func wideBitsField(t *testing.T, u *ir.Unit, c hostilecorpus.Case) *ir.Field {
	t.Helper()
	st, ok := u.Tables[c.Table]
	if !ok {
		t.Fatalf("%s: the schema declares no table %s", c.Name, c.Table)
	}
	for _, f := range st.Fields {
		if f.Type.Kind == ir.TBits && strings.Contains(c.Name, "-"+f.Name+"-") {
			return f
		}
	}
	t.Fatalf("%s: the case names no bits field of %s", c.Name, c.Table)
	return nil
}

// bitsCeiling is 2^N - 1, the largest value a bits(N) holds.
func bitsCeiling(bits int) uint64 {
	if bits >= 64 {
		return ^uint64(0)
	}
	return (uint64(1) << bits) - 1
}

// TestSquirrelReadsWideBitsCorpus runs every wire case of the shared wide
// hostile corpus through the generated Squirrel strict load: the verdict is
// the manifest's (the Go engine's), and each accepted twin reads back the
// exact value its bits(N) ceiling allows. The Go engine's own verdict on a
// refused case and its twin is confirmed beside it.
func TestSquirrelReadsWideBitsCorpus(t *testing.T) {
	t.Parallel()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	corpusDir := filepath.Join(root, "internal", "ci", "hostilecorpus", "wide")
	_, cases, err := hostilecorpus.LoadManifest(corpusDir)
	if err != nil {
		t.Fatal(err)
	}

	var wire []hostilecorpus.Case
	for _, c := range cases {
		if c.Carrier == "wire" {
			wire = append(wire, c)
		}
	}
	if len(wire) == 0 {
		t.Fatal("the wide manifest names no wire case")
	}

	paths, err := compiler.GatherPaths([]string{filepath.Join(root, filepath.FromSlash(wire[0].Schema))})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	mod := emitUnit(t, dir, "wide", u)

	var p strings.Builder
	p.WriteString(wideBitsPrelude)
	inputs := map[string][]byte{}
	for _, c := range wire {
		payload, err := hostilecorpus.Payload(corpusDir, c.Input)
		if err != nil {
			t.Fatal(err)
		}
		inputs[c.Name] = payload
		get := "null"
		if c.Verdict == hostilecorpus.Accept {
			get = fmt.Sprintf("function(x) { return x.%s; }", squirrel.Member(wideBitsField(t, u, c)))
		}
		fmt.Fprintf(&p, "run(%q, %q, %q, %q, %s);\n", "wide", mod, c.Table, c.Name, get)
	}
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(p.String()))

	got := map[string]wideBitsCase{}
	out := sqrun.Run(t, dir, "main.nut", inputs)
	for line := range strings.SplitSeq(strings.TrimRight(string(out), "\n"), "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) != 3 {
			t.Fatalf("probe line %q is not a case, a verdict and a value", line)
		}
		got[fields[0]] = wideBitsCase{verdict: fields[1], value: fields[2]}
	}

	for _, c := range wire {
		row, ok := got[c.Name]
		if !ok {
			t.Fatalf("%s: the probe printed no line", c.Name)
		}
		wantVerdict := "refused"
		if c.Verdict == hostilecorpus.Accept {
			wantVerdict = "read"
		}
		if row.verdict != wantVerdict {
			t.Errorf("%s: the strict read says %q, the manifest %q", c.Name, row.verdict, wantVerdict)
			continue
		}
		if c.Verdict != hostilecorpus.Accept {
			continue
		}
		f := wideBitsField(t, u, c)
		want := bitsCeiling(f.Type.Width)
		value, err := strconv.ParseUint(row.value, 10, 64)
		if err != nil {
			t.Errorf("%s: read back %q, not a value: %v", c.Name, row.value, err)
			continue
		}
		if value != want {
			t.Errorf("%s: read back %d, want the bits(%d) ceiling %d", c.Name, value, f.Type.Width, want)
		}
	}

	t.Run("go-engine", func(t *testing.T) {
		model := tabletext.NewModel(u)
		for _, name := range []string{"wide-wire-variable-b48-bit63", "wide-wire-variable-b48-max"} {
			var c hostilecorpus.Case
			found := false
			for _, k := range cases {
				if k.Name == name {
					c, found = k, true
					break
				}
			}
			if !found {
				t.Fatalf("the wide manifest names no %s", name)
			}
			payload, err := hostilecorpus.Payload(corpusDir, c.Input)
			if err != nil {
				t.Fatal(err)
			}
			verdict, _, _, err := hostilecorpus.EngineVerdict(model, c, payload)
			if err != nil {
				t.Fatalf("%s: the engine named no verdict: %v", name, err)
			}
			if verdict != c.Verdict {
				t.Errorf("%s: the Go engine says %q, the manifest %q", name, verdict, c.Verdict)
			}
		}
	})
}
