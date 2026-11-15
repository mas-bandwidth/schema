package squirreltable_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/ci/hostilecorpus"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

const wideCorpusSchema = "internal/ci/hostilecorpus/wide/wide.schema"

// wideJsonCases reads every text case of the wide corpus through the generated
// <T>FromJson and prints the verdict, with ToJson's hex for an accept.
const wideJsonCases = `
local widecase = function(unit, mod, root, key) {
  local schema = dofile(unit + "/schema_module.nut", true);
  local T = schema.require(mod);
  local J = schema.require("schema_table_json");
  local r = fresh();
  local v = J.FromJson(T, root, inputs[key + ".json"], r);
  if (v == null) { print("wide " + key + " refused -\n"); return; }
  print("wide " + key + " read " + hexOf(J.ToJson(T, root, v)) + "\n");
};
`

// TestSquirrelReadsWideBoundsJson holds the JSON read's integer range check to
// the EXACT declared bound: every text case of the wide corpus gives the
// manifest's verdict (the Go engine's), and an accepted twin writes back the
// engine's canonical text, so its value is the exact one the text spelled.
func TestSquirrelReadsWideBoundsJson(t *testing.T) {
	t.Parallel()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "internal", "ci", "hostilecorpus", "wide")
	_, cases, err := hostilecorpus.LoadManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]hostilecorpus.Case{}
	var texts []hostilecorpus.Case
	for _, c := range cases {
		byName[c.Name] = c
		if c.Carrier == "text" {
			texts = append(texts, c)
		}
	}
	if len(texts) == 0 {
		t.Fatal("the wide corpus names no text case")
	}
	model, err := hostilecorpus.LoadModel(root, t.TempDir(), wideCorpusSchema)
	if err != nil {
		t.Fatal(err)
	}
	read := func(t *testing.T, c hostilecorpus.Case) (string, bool) {
		t.Helper()
		payload, err := hostilecorpus.Payload(dir, c.Input)
		if err != nil {
			t.Fatal(err)
		}
		st := model.Lookup(c.Table)
		if st == nil {
			t.Fatalf("%s: table %q is not in %s", c.Name, c.Table, c.Schema)
		}
		inst := model.New(st)
		var rep tabletext.Report
		if !model.Read(inst, payload, &rep) {
			return "", false
		}
		text, err := model.Write(inst)
		if err != nil {
			t.Fatalf("%s: the engine refused the write: %v", c.Name, err)
		}
		return string(text), true
	}

	// THE GO CONFIRMATION: a refuse case and its accept twin, one past a
	// bound at 2^63 whose double rounds onto the bound, through the engine's
	// own Read.
	t.Run("GoEngine", func(t *testing.T) {
		for _, name := range []string{
			"wide-text-variable-u64-top-past-rounded",
			"wide-text-variable-u64-top-max",
		} {
			c, ok := byName[name]
			if !ok {
				t.Fatalf("the wide corpus is missing %s", name)
			}
			text, read := read(t, c)
			got := hostilecorpus.Refuse
			if read {
				got = hostilecorpus.Accept
			}
			if got != c.Verdict {
				t.Errorf("%s: the engine gave %s, the manifest's verdict is %s", name, got, c.Verdict)
			}
			if read {
				sum := sha256.Sum256([]byte(text))
				if d := hex.EncodeToString(sum[:]); d != c.Digest {
					t.Errorf("%s: the engine's text hashes to %s, the manifest pins %s", name, d, c.Digest)
				}
			}
		}
	})

	unit := loadUnit(t, root, []string{wideCorpusSchema})
	outDir := t.TempDir()
	mod := emitUnit(t, outDir, "wide", unit)
	var probe strings.Builder
	probe.WriteString(jsonPrelude)
	probe.WriteString(wideJsonCases)
	inputs := map[string][]byte{}
	for _, c := range texts {
		payload, err := hostilecorpus.Payload(dir, c.Input)
		if err != nil {
			t.Fatal(err)
		}
		inputs[c.Name+".json"] = payload
		fmt.Fprintf(&probe, "widecase(%q, %q, %q, %q);\n", "wide", mod, c.Table, c.Name)
	}
	writeFile(t, filepath.Join(outDir, "main.nut"), []byte(probe.String()))
	got := map[string][]string{}
	for line := range strings.SplitSeq(strings.TrimRight(string(sqrun.Run(t, outDir, "main.nut", inputs)), "\n"), "\n") {
		f := strings.Fields(line)
		if len(f) != 4 || f[0] != "wide" {
			t.Fatalf("probe line %q is not a wide row", line)
		}
		got[f[1]] = f[2:]
	}

	compared := 0
	for _, c := range texts {
		g, ok := got[c.Name]
		if !ok {
			t.Errorf("%s: the probe printed no row", c.Name)
			continue
		}
		wantVerdict := "refused"
		if c.Verdict == hostilecorpus.Accept {
			wantVerdict = "read"
		}
		if g[0] != wantVerdict {
			t.Errorf("%s: FromJson gave %s, the manifest's verdict is %s", c.Name, g[0], c.Verdict)
			continue
		}
		if c.Verdict == hostilecorpus.Refuse {
			compared++
			continue
		}
		text, read := read(t, c)
		if !read {
			t.Errorf("%s: the engine refused its own accept twin", c.Name)
			continue
		}
		sum := sha256.Sum256([]byte(text))
		if d := hex.EncodeToString(sum[:]); d != c.Digest {
			t.Errorf("%s: the engine's text hashes to %s, the manifest pins %s", c.Name, d, c.Digest)
			continue
		}
		have, ok := unhex(g[1])
		if !ok {
			t.Errorf("%s: ToJson refused the value read", c.Name)
			continue
		}
		// ToJson's canonical text ends with exactly one newline (§16.1);
		// tabletext's Write is the body.
		if string(have) != text+"\n" {
			t.Errorf("%s: ToJson differs from the engine's write\n got %q\nwant %q", c.Name, have, text+"\n")
			continue
		}
		compared++
	}
	if compared != len(texts) {
		t.Errorf("compared %d of %d wide text cases", compared, len(texts))
	}
	t.Logf("compared %d wide text cases: %d accepts wrote the engine's exact value", len(texts), compared)
}
