package ci

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/ci/hostilecorpus"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luatable"
)

const luaHostileCorpusDir = "internal/ci/hostilecorpus/corpus"
const luaHostileCorpusRunner = "internal/ci/schema_lua_runs_hostile_corpus_runner.lua"

type luaHostileAnswer struct {
	verdict string
	reason  string
	text    []byte
}

type luaHostileUnit struct {
	schema string
	module string
	dir    string
	unavailable string
}

func luaHostileCorpusBinary(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("LUA"); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set but LUA=%q does not exist", p)
		}
	}
	if p, err := exec.LookPath("lua"); err == nil {
		return p
	}
	if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
		t.Fatal("SCHEMA_REQUIRE_CORPUS is set and no Lua interpreter resolves; run make dist-lua or set LUA")
	}
	t.Skip("no Lua interpreter resolves; see make/lua.mk")
	return ""
}

// generateLuaHostileReaders writes the schema and table surfaces named by the
// manifest. A table generator refusal is kept as an explicit unavailable
// result so the corpus comparison reports the missing reader as a finding.
func generateLuaHostileReaders(t *testing.T, root, dir string, cases []hostilecorpus.Case) {
	t.Helper()
	seen := map[string]bool{}
	var units []luaHostileUnit
	for _, c := range cases {
		if seen[c.Schema] {
			continue
		}
		seen[c.Schema] = true
		path := filepath.Join(root, filepath.FromSlash(c.Schema))
		if c.Schema == hostilecorpus.ProbeSchemaPath {
			path = filepath.Join(dir, "probe.schema")
			if err := os.WriteFile(path, []byte(hostilecorpus.ProbeSchema), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		paths, err := compiler.GatherPaths([]string{path})
		if err != nil {
			t.Fatalf("%s: gather: %v", c.Schema, err)
		}
		u, err := compiler.New().Load(paths)
		if err != nil {
			t.Fatalf("%s: load: %v", c.Schema, err)
		}
		files, err := lua.Generate(u)
		if err != nil {
			t.Fatalf("%s: generate Lua types: %v", c.Schema, err)
		}
		tables, tableErr := luatable.Generate(u)
		unitDir := filepath.Join(dir, fmt.Sprintf("unit-%d", len(units)))
		if err := os.MkdirAll(unitDir, 0o700); err != nil {
			t.Fatal(err)
		}
		entry := luaHostileUnit{schema: c.Schema, module: luatable.ModuleName(u), dir: unitDir}
		if tableErr != nil {
			entry.unavailable = tableErr.Error()
		} else {
			for name, data := range tables {
				files[name] = data
			}
		}
		for name, data := range files {
			if strings.Contains(name, "/") {
				continue
			}
			if err := os.WriteFile(filepath.Join(unitDir, name), data, 0o600); err != nil {
				t.Fatalf("%s: write %s: %v", c.Schema, name, err)
			}
		}
		units = append(units, entry)
	}
	var index strings.Builder
	for _, unit := range units {
		fmt.Fprintf(&index, "%s\t%s\t%s\t%s\n", unit.schema, unit.dir, unit.module, unit.unavailable)
	}
	if err := os.WriteFile(filepath.Join(dir, "units.tsv"), []byte(index.String()), 0o600); err != nil {
		t.Fatal(err)
	}
}

func runLuaHostileCorpus(t *testing.T, plant bool) (map[string]luaHostileAnswer, []hostilecorpus.Case) {
	t.Helper()
	interpreter := luaHostileCorpusBinary(t)
	root := repoRoot(t)
	corpusDir := filepath.Join(root, luaHostileCorpusDir)
	_, cases, err := hostilecorpus.LoadManifest(corpusDir)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	generateLuaHostileReaders(t, root, dir, cases)
	runner, err := os.ReadFile(filepath.Join(root, luaHostileCorpusRunner))
	if err != nil {
		t.Fatal(err)
	}
	runnerPath := filepath.Join(dir, "runner.lua")
	if err := os.WriteFile(runnerPath, runner, 0o600); err != nil {
		t.Fatal(err)
	}
	outDir := filepath.Join(dir, "answers")
	if err := os.MkdirAll(outDir, 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(interpreter, runnerPath, filepath.Join(corpusDir, hostilecorpus.ManifestName), corpusDir, filepath.Join(dir, "units.tsv"), outDir)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "NOVA_TEST_NO_HOST=1")
	if plant {
		cmd.Env = append(cmd.Env, "HOSTILE_LUA_PLANT=1")
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Lua hostile-corpus runner failed: %v\n%s", err, output)
	}
	answers := map[string]luaHostileAnswer{}
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 4 || fields[0] != "CASE" {
			t.Fatalf("unexpected Lua hostile-corpus output line %q", line)
		}
		answer := luaHostileAnswer{verdict: fields[2], reason: fields[3]}
		if answer.verdict == hostilecorpus.Accept {
			answer.text, err = os.ReadFile(filepath.Join(outDir, fields[1]))
			if err != nil && !os.IsNotExist(err) {
				t.Fatalf("read Lua answer for %s: %v", fields[1], err)
			}
		}
		answers[fields[1]] = answer
	}
	return answers, cases
}

type luaHostileMismatch struct {
	name string
	want string
	got  string
}

func compareLuaHostileCorpus(t *testing.T, cases []hostilecorpus.Case, answers map[string]luaHostileAnswer) []luaHostileMismatch {
	t.Helper()
	var mismatches []luaHostileMismatch
	for _, c := range cases {
		a, ok := answers[c.Name]
		if !ok {
			mismatches = append(mismatches, luaHostileMismatch{c.Name, c.Verdict + "/" + c.Reason, "no answer"})
			continue
		}
		got := a.verdict + "/" + a.reason
		if a.verdict != c.Verdict || a.reason != c.Reason {
			mismatches = append(mismatches, luaHostileMismatch{c.Name, c.Verdict + "/" + c.Reason, got})
			continue
		}
		if c.Verdict == hostilecorpus.Accept && c.Digest != hostilecorpus.NoReason {
			if a.text == nil {
				mismatches = append(mismatches, luaHostileMismatch{c.Name, c.Digest, "no canonical text"})
				continue
			}
			sum := sha256.Sum256(a.text)
			if hex.EncodeToString(sum[:]) != c.Digest {
				mismatches = append(mismatches, luaHostileMismatch{c.Name, c.Digest, hex.EncodeToString(sum[:])})
			}
		}
	}
	return mismatches
}

func TestSchemaLuaRunsHostileCorpus(t *testing.T) {
	t.Parallel()
	got, cases := runLuaHostileCorpus(t, false)
	bad := compareLuaHostileCorpus(t, cases, got)
	if len(bad) != 0 {
		var report strings.Builder
		fmt.Fprintf(&report, "the Lua port disagrees with %d of %d hostile-corpus cases:", len(bad), len(cases))
		for _, mismatch := range bad {
			fmt.Fprintf(&report, "\n  %s: want %s, got %s", mismatch.name, mismatch.want, mismatch.got)
		}
		t.Fatal(report.String())
	}
	fmt.Printf("--- PASS: %d hostile-corpus cases, every verdict, reason and digest\n", len(cases))
}

func TestSchemaLuaHostileCorpusNegativeControl(t *testing.T) {
	t.Parallel()
	clean, cases := runLuaHostileCorpus(t, false)
	planted, _ := runLuaHostileCorpus(t, true)
	want := map[string]hostilecorpus.Case{}
	for _, c := range cases {
		want[c.Name] = c
	}
	const plantedCase = "wire-form-ff"
	wantCase, ok := want[plantedCase]
	if !ok {
		t.Fatalf("the corpus has no %s case to plant", plantedCase)
	}
	cleanAnswer, ok := clean[plantedCase]
	if !ok || cleanAnswer.verdict != wantCase.Verdict || cleanAnswer.reason != wantCase.Reason {
		t.Fatalf("without the plant, %s answered %s/%s; want %s/%s", plantedCase, cleanAnswer.verdict, cleanAnswer.reason, wantCase.Verdict, wantCase.Reason)
	}
	plantedAnswer, ok := planted[plantedCase]
	if !ok || (plantedAnswer.verdict == wantCase.Verdict && plantedAnswer.reason == wantCase.Reason) {
		t.Fatalf("the planted reader bug was not observed for %s", plantedCase)
	}
	fmt.Printf("--- PASS: Lua hostile-corpus plant changed %s from %s/%s to %s/%s\n", plantedCase, cleanAnswer.verdict, cleanAnswer.reason, plantedAnswer.verdict, plantedAnswer.reason)
}
