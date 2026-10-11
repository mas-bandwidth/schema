package elixirtable

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestMessageFormAnnounceElixir holds the Elixir leg's ANNOUNCEMENT surface
// (docs/SPEC-TABLES.md §3.3): the compiler-settled form-2 header, to the
// reference's own bytes for the three units of the message corpus.
//
//   - "the ANNOUNCEMENT IS AN ORDINARY FORM-1 FILE ... the generated Announce
//     bytes equal the announcement file" and its length is AnnounceMeasure
//     (§3.3).
func TestMessageFormAnnounceElixir(t *testing.T) {
	t.Parallel()
	root := "../../.."
	units := messageManifestUnits(t, filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	for _, key := range []string{"backenddemo", "vocabdemo", "vocab9demo"} {
		key := key
		t.Run("announce_"+key, func(t *testing.T) {
			t.Parallel()
			u := loadUnit(t, expandSchemaPaths(t, root, units[key].schema)...)
			generated, err := Generate(u)
			if err != nil {
				t.Fatal(err)
			}
			module, ok := generated[MessageFormModule+".ex"]
			if !ok {
				t.Fatalf("the unit grew no %s.ex; files are %v", MessageFormModule, keysOf(generated))
			}
			probe := fmt.Sprintf(`
measure = %s.MessageForm.announce_measure()
IO.write(Integer.to_string(measure) <> "\n")
IO.binwrite(:stdio, %s.MessageForm.announce())
`, ir.GoExportName(u.Package), ir.GoExportName(u.Package))
			out := runElixir(t, module, probe)
			measureLine, bytesOut, ok := bytes.Cut(out, []byte("\n"))
			if !ok {
				t.Fatalf("probe produced no measure line: %q", out)
			}
			manifest, err := os.ReadFile(filepath.Join(root, units[key].announcement))
			if err != nil {
				t.Fatal(err)
			}
			if got, err := strconv.Atoi(string(measureLine)); err != nil || got != len(manifest) {
				t.Fatalf("announce_measure = %q, the announcement file is %d bytes", measureLine, len(manifest))
			}
			if !bytes.Equal(bytesOut, manifest) {
				t.Fatalf("the announcement bytes differ from the manifest's announcement file")
			}
		})
	}
}

type messageManifestUnit struct {
	schema       []string
	announcement string
}

// messageManifestUnits reads the manifest itself, never a copied list: a
// `unit` line gives the source paths and its `connection` line gives the
// announcement file (§3.3).
func messageManifestUnits(t *testing.T, path string) map[string]messageManifestUnit {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	units := map[string]messageManifestUnit{}
	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		switch fields[0] {
		case "unit":
			if len(fields) >= 3 {
				u := units[fields[1]]
				u.schema = fields[2:]
				units[fields[1]] = u
			}
		case "connection":
			if len(fields) >= 5 {
				u := units[fields[2]]
				u.announcement = fields[4]
				units[fields[2]] = u
			}
		}
	}
	return units
}

func absoluteAll(t *testing.T, root string, names []string) []string {
	t.Helper()
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = filepath.Join(root, n)
	}
	return out
}

// expandSchemaPaths resolves a manifest path list the way compiler.GatherPaths
// does: a directory contributes its *.schema files, a named file itself.
func expandSchemaPaths(t *testing.T, root string, names []string) []string {
	t.Helper()
	var out []string
	for _, abs := range absoluteAll(t, root, names) {
		info, err := os.Stat(abs)
		if err != nil {
			t.Fatal(err)
		}
		if !info.IsDir() {
			out = append(out, abs)
			continue
		}
		entries, err := os.ReadDir(abs)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".schema") {
				out = append(out, filepath.Join(abs, e.Name()))
			}
		}
	}
	return out
}

// runElixir compiles one generated module beside a probe script and runs the
// script, answering the script's stdout.
func runElixir(t *testing.T, module []byte, script string) []byte {
	t.Helper()
	dir := t.TempDir()
	probe := filepath.Join(dir, "probe.exs")
	if err := os.WriteFile(probe, append(module, []byte(script)...), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("elixir", probe)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("elixir probe: %v\n%s", err, stderr.String())
	}
	return stdout.Bytes()
}
