package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

type provenanceFixture struct {
	dir    string
	assets string
	info   buildInfo
}

func writeProvenanceFile(t *testing.T, dir, name string, data []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
		t.Fatal(err)
	}
}

func writeProvenanceJSON(t *testing.T, dir, name string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	writeProvenanceFile(t, dir, name, data)
}

// These fixtures test integrity, with real temporary artifact files and hashes.
// CSV interpretation and measured-runner correctness have separate gates; these
// sample contents are explicitly synthetic and contain no benchmark timings.
func newProvenanceFixture(t *testing.T, rounds int, sitting string) provenanceFixture {
	t.Helper()
	f := provenanceFixture{dir: t.TempDir(), assets: t.TempDir(), info: buildInfo{Rounds: rounds, Revision: "synthetic-" + sitting}}
	for _, group := range []struct {
		name string
		dest *map[string]string
	}{{"binary", &f.info.Binaries}, {"corpus", &f.info.Corpora}} {
		writeProvenanceFile(t, f.assets, group.name, []byte(group.name+"-"+sitting))
		path := filepath.Join(f.assets, group.name)
		hash, err := hashFile(path)
		if err != nil {
			t.Fatal(err)
		}
		*group.dest = map[string]string{path: hash}
	}
	writeProvenanceJSON(t, f.dir, "build.json", f.info)
	writeProvenanceJSON(t, f.dir, "load.json", []string{"synthetic " + sitting})
	writeTestWindow(t, f.dir, rounds)
	for _, wire := range []string{"packet", "table"} {
		for _, label := range []string{"start", "end"} {
			name := "control-" + label + "-" + wire + ".csv"
			writeProvenanceFile(t, f.dir, name, []byte("synthetic,"+sitting+","+name+"\n"))
		}
		for round := range rounds {
			for _, lang := range []string{"c", "cpp", "cs", "go"} {
				name := fmt.Sprintf("round-%d-%s-%s.csv", round, lang, wire)
				writeProvenanceFile(t, f.dir, name, []byte("synthetic,"+sitting+","+name+"\n"))
			}
		}
	}
	return f
}

func TestCompletedPassIsPortable(t *testing.T) {
	f := newProvenanceFixture(t, 7, "portable")
	if err := sealPass(f.dir); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(f.dir, completionFile))
	if err != nil {
		t.Fatal(err)
	}
	var manifest passManifest
	if err = json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Version != 1 || manifest.Rounds != f.info.Rounds || len(manifest.Files) != 3+4+f.info.Rounds*4*2 {
		t.Fatalf("incomplete manifest: %+v", manifest)
	}
	for _, output := range []string{"README.md", "DETAILS.md", "results.csv"} {
		writeProvenanceFile(t, f.dir, output, []byte("derived output may be regenerated"))
	}
	if err = os.RemoveAll(f.assets); err != nil {
		t.Fatal(err)
	}
	// Move the complete archive as well as removing its original build outputs.
	moved := filepath.Join(t.TempDir(), "archive")
	if err = os.Rename(f.dir, moved); err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	if err = verifyPass(moved); err != nil {
		t.Fatalf("portable verification required original artifacts: %v", err)
	}
	if err = sealPass(moved); err == nil {
		t.Fatal("silently replaced an existing completion manifest")
	}
}

func TestSealResolvesArtifactsFromWorkingDirectory(t *testing.T) {
	f := newProvenanceFixture(t, 7, "relative paths")
	f.info.Binaries = map[string]string{"binary": f.info.Binaries[filepath.Join(f.assets, "binary")]}
	f.info.Corpora = map[string]string{"corpus": f.info.Corpora[filepath.Join(f.assets, "corpus")]}
	writeProvenanceJSON(t, f.dir, "build.json", f.info)
	t.Chdir(f.assets)
	if err := sealPass(f.dir); err != nil {
		t.Fatalf("failed to verify real artifacts from cwd: %v", err)
	}
}

func TestCompletedPassRejectsChangedEvidence(t *testing.T) {
	for _, name := range []string{"build.json", "window.json", "load.json", "control-start-packet.csv", "control-end-table.csv", "round-3-c-table.csv"} {
		t.Run(name, func(t *testing.T) {
			f := newProvenanceFixture(t, 7, "changed")
			if err := sealPass(f.dir); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(f.dir, name))
			if err != nil {
				t.Fatal(err)
			}
			// Trailing whitespace preserves valid JSON while changing exact bytes.
			writeProvenanceFile(t, f.dir, name, append(data, '\n'))
			if err = verifyPass(f.dir); err == nil {
				t.Fatal("accepted changed sealed input")
			}
		})
	}
}

func TestCompletedPassRejectsMissingExtraOrMixedSamples(t *testing.T) {
	for _, kind := range []string{"missing", "surplus", "renamed", "mixed", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			f := newProvenanceFixture(t, 7, "first")
			if err := sealPass(f.dir); err != nil {
				t.Fatal(err)
			}
			name := "round-2-go-packet.csv"
			path := filepath.Join(f.dir, name)
			switch kind {
			case "missing":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			case "surplus":
				writeProvenanceFile(t, f.dir, "round-7-go-packet.csv", []byte("surplus sample"))
			case "renamed":
				if err := os.Rename(path, filepath.Join(f.dir, "round-2-rust-packet.csv")); err != nil {
					t.Fatal(err)
				}
			case "mixed":
				other := newProvenanceFixture(t, 7, "second")
				data, err := os.ReadFile(filepath.Join(other.dir, name))
				if err != nil {
					t.Fatal(err)
				}
				writeProvenanceFile(t, f.dir, name, data)
			case "symlink":
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				writeProvenanceFile(t, f.assets, "external.csv", data)
				if err = os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err = os.Symlink(filepath.Join(f.assets, "external.csv"), path); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
			}
			if err := verifyPass(f.dir); err == nil {
				t.Fatalf("accepted %s evidence", kind)
			}
		})
	}
}

func TestCompletedPassRejectsTrimmedRounds(t *testing.T) {
	f := newProvenanceFixture(t, 9, "trimmed")
	if err := sealPass(f.dir); err != nil {
		t.Fatal(err)
	}
	f.info.Rounds = 7
	writeProvenanceJSON(t, f.dir, "build.json", f.info)
	for _, round := range []int{7, 8} {
		for _, lang := range []string{"c", "cpp", "cs", "go"} {
			for _, wire := range []string{"packet", "table"} {
				if err := os.Remove(filepath.Join(f.dir, fmt.Sprintf("round-%d-%s-%s.csv", round, lang, wire))); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	if err := verifyPass(f.dir); err == nil {
		t.Fatal("accepted a nine-round sitting trimmed to seven rounds")
	}
}

func TestCompletedPassRequiresExactManifest(t *testing.T) {
	for _, kind := range []string{"absent", "version", "rounds", "missing", "duplicate", "surplus", "digest"} {
		t.Run(kind, func(t *testing.T) {
			f := newProvenanceFixture(t, 7, "manifest")
			if kind != "absent" {
				if err := sealPass(f.dir); err != nil {
					t.Fatal(err)
				}
				data, err := os.ReadFile(filepath.Join(f.dir, completionFile))
				if err != nil {
					t.Fatal(err)
				}
				var manifest passManifest
				if err = json.Unmarshal(data, &manifest); err != nil {
					t.Fatal(err)
				}
				switch kind {
				case "version":
					manifest.Version++
				case "rounds":
					manifest.Rounds++
				case "missing":
					manifest.Files = manifest.Files[1:]
				case "duplicate":
					manifest.Files[1] = manifest.Files[0]
				case "surplus":
					manifest.Files = append(manifest.Files, passFile{Name: "unrecorded.csv"})
				case "digest":
					manifest.Files[0].SHA256 = "not a digest"
				}
				writeProvenanceJSON(t, f.dir, completionFile, manifest)
			}
			if err := verifyPass(f.dir); err == nil {
				t.Fatalf("accepted %s manifest", kind)
			}
		})
	}
}

func TestSealRequiresCurrentArtifactsAndCompleteInputs(t *testing.T) {
	for _, kind := range []string{"binary changed", "corpus changed", "binary missing", "corpus missing", "binary unrecorded", "corpus unrecorded", "missing sample", "surplus sample", "invalid window", "incomplete window"} {
		t.Run(kind, func(t *testing.T) {
			f := newProvenanceFixture(t, 7, "unsealed")
			switch kind {
			case "binary changed", "corpus changed":
				name := "binary"
				if kind == "corpus changed" {
					name = "corpus"
				}
				writeProvenanceFile(t, f.assets, name, []byte("different contents"))
			case "binary missing", "corpus missing":
				name := "binary"
				if kind == "corpus missing" {
					name = "corpus"
				}
				if err := os.Remove(filepath.Join(f.assets, name)); err != nil {
					t.Fatal(err)
				}
			case "binary unrecorded", "corpus unrecorded":
				if kind == "binary unrecorded" {
					f.info.Binaries = nil
				} else {
					f.info.Corpora = nil
				}
				writeProvenanceJSON(t, f.dir, "build.json", f.info)
			case "missing sample":
				if err := os.Remove(filepath.Join(f.dir, "round-6-cs-table.csv")); err != nil {
					t.Fatal(err)
				}
			case "surplus sample":
				writeProvenanceFile(t, f.dir, "unexpected.csv", []byte("surplus"))
			case "invalid window":
				writeProvenanceJSON(t, f.dir, "window.json", map[string]string{"verdict": "INVALID"})
			case "incomplete window":
				writeProvenanceJSON(t, f.dir, "window.json", map[string]string{"verdict": "OK"})
			}
			if err := sealPass(f.dir); err == nil {
				t.Fatalf("sealed pass with %s", kind)
			}
			if _, err := os.Stat(filepath.Join(f.dir, completionFile)); !os.IsNotExist(err) {
				t.Fatalf("failed seal left a completion manifest: %v", err)
			}
		})
	}
}
