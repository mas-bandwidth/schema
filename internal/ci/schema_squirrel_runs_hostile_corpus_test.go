// The Squirrel port runs the shared hostile-input corpus
// (schema-shared-hostile-corpus) in its unit tier.
//
// One corpus, two readers: internal/ci/hostilecorpus/corpus holds one raw file
// per input and a line manifest whose verdicts are the GO ENGINE's
// (internal/tablewire, internal/tabletext), never a generated reader's. This
// test walks every manifest case through the Squirrel port's generated table
// module: the text carrier through FromJson/ToJson, the wire carrier through
// ReadStrict, the announcement carriers through AnnounceRead, and the message
// carriers through LoadMessages. An accept must carry the manifest's decoded
// value digest (SHA-256 of the canonical text); a refuse must name the
// manifest's reason. A disagreement is a finding, not a reason to edit the
// corpus (docs/HOSTILE-CORPUS.md).
package ci

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type hostileCase struct {
	name, carrier, schema, table, input, verdict, reason, digest string
}

type hostileResult struct {
	verdict string
	reason  string
	text    []byte
}

// squirrelHostileProbeSchema is the one manifest schema that is not in the
// checkout: the engine uses it for the builder-path count refusal
// (docs/HOSTILE-CORPUS.md, "Carriers").
const squirrelHostileProbeSchema = "// probe.schema — the hostile corpus's count-refusal root.\npackage probe\n\ntable Root\n{\n    entries map[int32]int32\n}\n"

func squirrelHostileCorpusDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(repoRoot(t), "internal", "ci", "hostilecorpus", "corpus")
}

// squirrelHostileCases reads the line-oriented manifest: name, carrier,
// schema, table, input, provenance, verdict, reason, digest.
func squirrelHostileCases(t *testing.T) []hostileCase {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(squirrelHostileCorpusDir(t), "manifest.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	var out []hostileCase
	for i, line := range strings.Split(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 9 {
			t.Fatalf("manifest line %d: %d fields, want 9", i+1, len(f))
		}
		out = append(out, hostileCase{
			name: f[0], carrier: f[1], schema: f[2], table: f[3], input: f[4],
			verdict: f[6], reason: f[7], digest: f[8],
		})
	}
	return out
}

func squirrelHostileUnit(t *testing.T, root, tmp, schema string) (*ir.Unit, error) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(schema))
	if schema == "schemas/probe.schema" {
		path = filepath.Join(tmp, "probe.schema")
		if err := os.WriteFile(path, []byte(squirrelHostileProbeSchema), 0o644); err != nil {
			return nil, err
		}
	}
	paths, err := compiler.GatherPaths([]string{path})
	if err != nil {
		return nil, err
	}
	return compiler.New().Load(paths)
}

func squirrelHostileKey(schema string) string {
	return strings.NewReplacer("/", "_", ".", "_").Replace(schema)
}

func squirrelHostileHex(b []byte) string {
	if b == nil {
		return "-"
	}
	return hex.EncodeToString(b)
}

func squirrelHostileFindings(c hostileCase, got hostileResult) []string {
	if got.verdict != c.verdict {
		return []string{fmt.Sprintf("%s: verdict %q, manifest %q", c.name, got.verdict, c.verdict)}
	}
	switch c.verdict {
	case "refuse":
		if got.reason != c.reason {
			return []string{fmt.Sprintf("%s: refusal %q, manifest %q", c.name, got.reason, c.reason)}
		}
	case "accept":
		if c.digest == "-" {
			return nil
		}
		// The corpus digest is SHA-256 of tabletext.Model.Write, the VALUE
		// text; the generated backend's ToJson adds the file form's one
		// trailing newline, which is not part of the value.
		sum := sha256.Sum256([]byte(strings.TrimSuffix(string(got.text), "\n")))
		if hex.EncodeToString(sum[:]) != c.digest {
			return []string{fmt.Sprintf("%s: decoded value digest %s, manifest %s", c.name, hex.EncodeToString(sum[:]), c.digest)}
		}
	default:
		return []string{fmt.Sprintf("%s: manifest verdict %q is neither accept nor refuse", c.name, c.verdict)}
	}
	return nil
}

// TestSquirrelRunsHostileCorpus is the port's runner: every input of the
// committed corpus reaches the manifest's verdict through the generated
// Squirrel reader.
func TestSquirrelRunsHostileCorpus(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	cases := squirrelHostileCases(t)
	tmp, dir := t.TempDir(), t.TempDir()

	type generated struct {
		dir, module string
	}
	units := map[string]generated{}
	unsupported := map[string]string{}
	for _, c := range cases {
		if _, ok := units[c.schema]; ok {
			continue
		}
		if _, ok := unsupported[c.schema]; ok {
			continue
		}
		u, err := squirrelHostileUnit(t, root, tmp, c.schema)
		if err != nil {
			unsupported[c.schema] = err.Error()
			continue
		}
		files, err := squirrel.Generate(u)
		if err != nil {
			unsupported[c.schema] = err.Error()
			continue
		}
		tables, err := squirreltable.Generate(u)
		if err != nil {
			unsupported[c.schema] = err.Error()
			continue
		}
		key := squirrelHostileKey(c.schema)
		out := filepath.Join(dir, key)
		if err := os.MkdirAll(out, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, data := range files {
			if err := os.WriteFile(filepath.Join(out, name), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		for name, data := range tables {
			if err := os.WriteFile(filepath.Join(out, name), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		units[c.schema] = generated{dir: key, module: squirreltable.ModuleName(u)}
	}

	inputs := map[string][]byte{}
	var script strings.Builder
	script.WriteString(squirrelHostilePrelude)
	for _, c := range cases {
		g, ok := units[c.schema]
		if !ok {
			continue
		}
		data, err := os.ReadFile(filepath.Join(squirrelHostileCorpusDir(t), filepath.FromSlash(c.input)))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		inputs[c.name] = data
		ann := "announce:" + g.dir
		if _, ok := inputs[ann]; !ok {
			u, err := squirrelHostileUnit(t, root, tmp, c.schema)
			if err != nil {
				t.Fatalf("%s: %v", c.name, err)
			}
			inputs[ann] = announceFor(u)
		}
		fmt.Fprintf(&script, "run(%q, %q, %q, %q, %q, %q);\n",
			c.name, c.carrier, g.dir, g.module, c.table, ann)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.nut"), []byte(script.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	got := map[string]hostileResult{}
	if len(units) > 0 {
		out := string(sqrun.Run(t, dir, "main.nut", inputs))
		for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
			if line == "" {
				continue
			}
			f := strings.Split(line, "\t")
			if len(f) != 4 {
				t.Fatalf("probe line %q is not name, verdict, reason, hex", line)
			}
			var text []byte
			if f[3] != "-" {
				var err error
				text, err = hex.DecodeString(f[3])
				if err != nil {
					t.Fatalf("%s: decoded text is not hex: %v", f[0], err)
				}
			}
			got[f[0]] = hostileResult{verdict: f[1], reason: f[2], text: text}
		}
	}

	findings := 0
	for _, c := range cases {
		if why, ok := unsupported[c.schema]; ok {
			t.Errorf("FIX-CARD: squirrel-hostile-corpus: %s the port's generator refuses %s: %s", c.name, c.schema, why)
			findings++
			continue
		}
		g, ok := got[c.name]
		if !ok {
			t.Errorf("FIX-CARD: squirrel-hostile-corpus: %s the runner printed no verdict", c.name)
			findings++
			continue
		}
		for _, f := range squirrelHostileFindings(c, g) {
			t.Errorf("FIX-CARD: squirrel-hostile-corpus: %s", f)
			findings++
		}
	}
	if findings > 0 {
		t.Fatalf("%d corpus cases disagree with the manifest", findings)
	}
	t.Logf("%d hostile inputs: the Squirrel reader reaches every manifest verdict", len(cases))
}

// TestSquirrelRunsHostileCorpusPlant is the planted-bad-pattern control: a
// reader that returns one different decoded byte is red, the same reader
// unplanted is green.
func TestSquirrelRunsHostileCorpusPlant(t *testing.T) {
	t.Parallel()
	sum := sha256.Sum256([]byte("{\"value\":1}"))
	c := hostileCase{name: "plant", carrier: "text", verdict: "accept", reason: "-", digest: hex.EncodeToString(sum[:])}
	clean := hostileResult{verdict: "accept", reason: "-", text: []byte("{\"value\":1}")}
	if f := squirrelHostileFindings(c, clean); len(f) != 0 {
		t.Fatalf("the unplanted reader is red: %v", f)
	}
	planted := hostileResult{verdict: "accept", reason: "-", text: []byte("{\"value\":2}")}
	if f := squirrelHostileFindings(c, planted); len(f) == 0 {
		t.Fatal("a reader that decoded a different value was NOT caught")
	}
}

const squirrelHostilePrelude = `local inputs = dofile("inputs.nut", true);
local fresh = function() {
  return { unknown = 0, kindMismatch = 0, widened = 0, clamped = 0, duplicate = 0,
    malformed = false, refused = false };
};
local hexOf = function(s) {
  if (s == null) return "-";
  local out = "";
  for (local i = 0; i < s.len(); i++) out += format("%02x", s[i] & 0xff);
  return out;
};
local emit = function(name, verdict, reason, text) {
  print(name + "\t" + verdict + "\t" + reason + "\t" + hexOf(text) + "\n");
};
local named = function(report, fallback) {
  if (report != null && ("reason" in report) && report.reason != null) return report.reason;
  return fallback;
};
local run = function(name, carrier, dir, mod, root, annkey) {
  local schema = dofile(dir + "/schema_module.nut", true);
  local T = schema.require(mod);
  local J = schema.require("schema_table_json");
  local v = null;
  local r = fresh();
  if (carrier == "text") {
    v = J.FromJson(T, root, inputs[name], r);
    if (v == null || r.malformed) { emit(name, "refuse", "damaged", null); return; }
  } else if (carrier == "wire") {
    local R = schema.require("schema_table_refusal");
    local rr = {};
    v = R.ReadStrict(T, root, inputs[name], rr);
    if (v == null) {
      local why = named(rr, "wire_damaged");
      if (why == "wire_damaged" || why == "malformed") why = "damaged";
      emit(name, "refuse", why, null);
      return;
    }
  } else if (carrier == "announce" || carrier == "announce_second" ||
             carrier == "announce_bytes" || carrier == "announce_entries") {
    local V = {};
    local ar = {};
    local ok = T.AnnounceRead(V, inputs[name], ar);
    if (carrier == "announce_second") {
      if (ok != true) { emit(name, "refuse", named(ar, "damaged"), null); return; }
      ar = {};
      ok = T.AnnounceRead(V, inputs[name], ar);
    }
    if (ok != true) { emit(name, "refuse", named(ar, "damaged"), null); return; }
    emit(name, "accept", "-", null);
    return;
  } else if (carrier == "message" || carrier == "message_novocab") {
    local V = null;
    if (carrier == "message") {
      V = {};
      local ar = {};
      T.AnnounceRead(V, inputs[annkey], ar);
    }
    local mk = T[root];
    local storage = [mk()];
    local rr = {};
    local loadMessages = T[root + "LoadMessages"];
    local n = loadMessages(storage, V, inputs[name], rr);
    if (n < 1) { emit(name, "refuse", named(rr, "damaged"), null); return; }
    v = storage[0];
  } else {
    emit(name, "unknown", carrier, null);
    return;
  }
  local text = J.ToJson(T, root, v);
  if (text == null) { emit(name, "refuse", "damaged", null); return; }
  emit(name, "accept", "-", text);
};
`

func announceFor(u *ir.Unit) []byte {
	return tablewire.Announce(u)
}
