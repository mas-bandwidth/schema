package main

import (
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// scripted is a compiler read from a scenario file: it writes exactly the
// encodings the file lists and refuses every other one.
type scripted struct{ fake }

func (s scripted) Pack(u []string, root, json string, message bool) ([]byte, error) {
	form := "v"
	if message {
		form = "m"
	}
	body, ok := s.pack[strings.TrimSuffix(filepath.Base(json), ".json")+"/"+form]
	if !ok {
		return nil, errors.New("schema: 1 error(s)")
	}
	return []byte(body), nil
}

// scenarioLines is a scenario file's rows: tab-separated, `#` comments and
// blank lines left out.
func scenarioLines(t *testing.T, path string) [][]string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out [][]string
	for line := range strings.SplitSeq(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, strings.Split(line, "\t"))
	}
	return out
}

func scriptedCompiler(t *testing.T, path string) scripted {
	t.Helper()
	c := scripted{fake{ids: map[string]string{}, packet: map[string]string{}, pack: map[string]string{}}}
	unhex := func(s string) string {
		b, err := hex.DecodeString(s)
		if err != nil {
			t.Fatalf("%s: %q: %v", path, s, err)
		}
		return string(b)
	}
	for _, f := range scenarioLines(t, path) {
		switch {
		case f[0] == "name" && len(f) == 2:
			c.name = f[1]
		case f[0] == "id" && len(f) == 3:
			c.ids[f[1]] = f[2]
		case f[0] == "packet" && len(f) == 3:
			c.packet[f[1]] = unhex(f[2])
		case f[0] == "pack" && len(f) == 4 && (f[2] == "variable" || f[2] == "message"):
			c.pack[f[1]+"/"+f[2][:1]] = unhex(f[3])
		default:
			t.Fatalf("%s: unreadable row %q", path, f)
		}
	}
	return c
}

// TestJudgeScenarioCorpus drives the real Judge from the committed
// release-comparison scenarios under testdata/scenarios: each is a manifest,
// what the previous release and the new compiler write for its instances, and
// the report the gate owes. writer-regression holds an instance the previous
// release encodes and the new compiler refuses, in each table form, beside
// the reverse (new reach) and an unchanged instance; new-reach-only is the
// same shape with no regression, and must not refuse.
func TestJudgeScenarioCorpus(t *testing.T) {
	t.Parallel()
	root := filepath.Join("testdata", "scenarios")
	dirs, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	verdicts := map[bool]int{}
	for _, d := range dirs {
		dir := filepath.Join(root, d.Name())
		text, err := os.ReadFile(filepath.Join(dir, "MANIFEST.txt"))
		if err != nil {
			t.Fatal(err)
		}
		m, err := ParseManifest(string(text))
		if err != nil {
			t.Fatal(err)
		}
		old, nw := scriptedCompiler(t, filepath.Join(dir, "previous.tsv")), scriptedCompiler(t, filepath.Join(dir, "new.tsv"))
		var refused bool
		var differences, uncompared []string
		for _, f := range scenarioLines(t, filepath.Join(dir, "expect.tsv")) {
			switch {
			case f[0] == "refused" && len(f) == 2 && (f[1] == "true" || f[1] == "false"):
				refused = f[1] == "true"
			case f[0] == "difference" && len(f) == 2:
				differences = append(differences, f[1])
			case f[0] == "uncompared" && len(f) == 2:
				uncompared = append(uncompared, f[1])
			default:
				t.Fatalf("%s/expect.tsv: unreadable row %q", dir, f)
			}
		}
		verdicts[refused]++
		t.Run(d.Name(), func(t *testing.T) {
			t.Parallel()
			tree := writeTree(t, map[string]string{"test/main.cpp": ""})
			r, err := Judge(old, nw, []string{tree}, m, "json", t.TempDir(), 2)
			if err != nil {
				t.Fatal(err)
			}
			got := slices.Clone(r.Differences)
			slices.Sort(got)
			slices.Sort(differences)
			if !slices.Equal(got, differences) {
				t.Errorf("differences %q; want %q", got, differences)
			}
			if r.Refused() != refused {
				t.Errorf("refused = %v, want %v (nothing compared: %q)", r.Refused(), refused, r.Nothing)
			}
			for _, want := range uncompared {
				if !slices.ContainsFunc(r.Uncompared, func(s string) bool { return strings.HasPrefix(s, want) }) {
					t.Errorf("the report does not list %q as uncompared: %q", want, r.Uncompared)
				}
			}
		})
	}
	if verdicts[true] == 0 || verdicts[false] == 0 {
		t.Errorf("the scenario corpus holds %d refusing and %d passing scenarios: a refusal is only proven beside a passing control", verdicts[true], verdicts[false])
	}
}
