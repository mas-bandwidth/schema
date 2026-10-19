package ci

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestNightlyFuzzCorpusIsListedAndReal holds the nightly fuzz corpus list to
// the repository: every entry is a directory, the set is not empty, and it
// names the shape corpora the Go generate targets read. A fuzz target over a
// corpus that is not there is a green light with no blade.
func TestNightlyFuzzCorpusIsListedAndReal(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	list := filepath.Join(root, "make", "schema-go-nightly-fuzz-corpus.txt")
	data, err := os.ReadFile(list)
	if err != nil {
		t.Fatalf("read the corpus list: %v", err)
	}
	var dirs []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if info, err := os.Stat(filepath.Join(root, line)); err != nil || !info.IsDir() {
			t.Errorf("corpus entry %q is not a directory under the repository", line)
			continue
		}
		dirs = append(dirs, line)
	}
	if len(dirs) == 0 {
		t.Fatal("the corpus list names no directory")
	}
	want := map[string]bool{
		"tables/examples":  false,
		"tables/pointers":  false,
		"tables/block":     false,
		"tables/blockhome": false,
		"tables/messages":  false,
		"tables/stream":    false,
		"tables/blobs":     false,
		"tables/lists":     false,
		"tables/maps":      false,
		"tables/arms":      false,
		"tables/backend":   false,
		"tables/vocab":     false,
		"tables/vocab9":    false,
		"tables/scalars":   false,
		"examples-wide":    false,
	}
	for _, dir := range dirs {
		if _, ok := want[dir]; ok {
			want[dir] = true
		}
	}
	for dir, seen := range want {
		if !seen {
			t.Errorf("the shape corpus %q is not in the nightly fuzz set", dir)
		}
	}
}

// TestNightlyFuzzRegressionCorpusIsPresent pins the per-push replay corpus. At
// attempt 1's head the directory did not exist and the replay test continued
// past a missing ReadDir, so it passed on zero inputs. An absent kind is now a
// red here and in the generated replay test.
func TestNightlyFuzzRegressionCorpusIsPresent(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	for _, kind := range []string{"Load", "ReadStrict", "TextJson", "FromJson"} {
		dir := filepath.Join(root, "internal", "ci", "testdata", "nightly-fuzz-corpus", kind)
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("the committed %s regression corpus is absent: %v", kind, err)
		}
		inputs := 0
		for _, entry := range entries {
			if !entry.IsDir() {
				inputs++
			}
		}
		if inputs == 0 {
			t.Fatalf("the committed %s regression corpus has no saved input", kind)
		}
	}
}

// TestNightlyFuzzGeneratorEmitsJSONAndFailFastOracle pins the two harness
// defects at attempt 1's head: text/JSON parse only ever reached LoadText (a
// base64 wire decode) and oracle violations were log lines, not failures.
func TestNightlyFuzzGeneratorEmitsJSONAndFailFastOracle(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	gen := filepath.Join(root, "internal", "ci", "nightlyfuzzgen.go")

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module nightlyfuzz\n\ngo 1.23\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pkg := filepath.Join(dir, "cells")
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		t.Fatal(err)
	}
	fake := `package cells

type TableReport struct{}

func (r *TableReport) Err() error { return nil }

type Cells struct{ Value uint32 }

func CellsReset(value *Cells) {}

func CellsLoad(value *Cells, data []byte, report *TableReport) bool { return false }

func CellsReadStrict(wire []byte) (*Cells, error) { return nil, nil }
`
	if err := os.WriteFile(filepath.Join(pkg, "cells.go"), []byte(fake), 0o644); err != nil {
		t.Fatal(err)
	}
	list := filepath.Join(dir, "corpus.txt")
	if err := os.WriteFile(list, []byte(pkg+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "run", gen, list, dir)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=readonly")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate the scratch harness: %v\n%s", err, out)
	}
	src, err := os.ReadFile(filepath.Join(dir, "nightly_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"func FuzzFromJson(",
		".json(data[1:])",
		"CellsFromJson(&v, data, report)",
		"errors.As(err, &oracle)",
		"checkRefusal(t, err)",
		"func decodeSaved(",
	} {
		if !bytes.Contains(src, []byte(want)) {
			t.Errorf("the generated harness is missing %q", want)
		}
	}
	if bytes.Contains(src, []byte(`t.Logf("typed refusal`)) {
		t.Error("the generated harness still logs an oracle error instead of failing")
	}
}

// TestNightlyFuzzPlantedReaderIsCaught is the planted-pattern proof for the
// nightly harness. It builds a scratch copy of the REAL generated examples
// reader, drops the Room(n) check from TableReader.Body in the copy, and runs
// the real fuzzer: FuzzLoad must find it inside the nightly budget, the saved
// input must fail replay with the plant in and pass with it out, and the same
// generated reader without the plant must go green. It is the functional tier:
// test/slowgate/proof sets SCHEMA_SLOW and requires the --- PASS line.
func TestNightlyFuzzPlantedReaderIsCaught(t *testing.T) {
	t.Parallel()
	if os.Getenv("SCHEMA_SLOW") == "" {
		t.Skip("functional tier: run through make schema-go-nightly-fuzz-negative-control")
	}
	root := repoRoot(t)
	gen := filepath.Join(root, "internal", "ci", "nightlyfuzzgen.go")
	filter := filepath.Join(root, "internal", "codegen", "gotable", "fuzzunit", "filter_generate.go")

	runFuzz := func(t *testing.T, dir, target, fuzztime string) (string, error) {
		t.Helper()
		cmd := exec.Command("go", "test", "-run", "^$", "-fuzz", "^"+target+"$", "-fuzztime="+fuzztime, "-parallel", "2", "-timeout", "600s")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "SCHEMA_GO_FUZZ_REGRESSIONS=")
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	runReplay := func(t *testing.T, dir string) (string, error) {
		t.Helper()
		cmd := exec.Command("go", "test", "-run", "^TestSavedInputsReplay$", "-count=1", "-timeout", "600s")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"SCHEMA_GO_FUZZ_SEEDS=",
			"SCHEMA_GO_FUZZ_REGRESSIONS="+filepath.Join(dir, "regressions"))
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	// scratch emits the named corpus through the real standalone emitter into a
	// fresh module. manifest, when non-nil, writes a scratch MANIFEST that names
	// the emitted package, so the generator seeds the harness from the real
	// instance files for the reader's own table.
	scratch := func(t *testing.T, corpus, plant string, manifest func(pkg string) string) string {
		t.Helper()
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module nightlyfuzz\n\ngo 1.23\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		pkg := filepath.Join(dir, "generated")
		if err := os.MkdirAll(pkg, 0o755); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("go", "run", "./cmd/schema", "generate", "--lang", "go", "--runtime", "standalone", "--out", pkg, corpus)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("generate the real %s reader: %v\n%s", corpus, err, out)
		}
		cmd = exec.Command("go", "run", filter, pkg)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("filter the generated reader: %v\n%s", err, out)
		}
		switch plant {
		case "room":
			patchGeneratedReader(t, pkg)
		case "surrogate":
			patchSurrogateLookahead(t, pkg)
		case "resolve":
			patchResolveBound(t, pkg)
		}
		list := filepath.Join(dir, "corpus.txt")
		if err := os.WriteFile(list, []byte(pkg+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		args := []string{"run", gen, list, dir}
		if manifest != nil {
			path := filepath.Join(dir, "MANIFEST.txt")
			if err := os.WriteFile(path, []byte(manifest(pkg)), 0o644); err != nil {
				t.Fatal(err)
			}
			args = append(args, path)
		}
		cmd = exec.Command("go", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("generate the scratch harness: %v\n%s", err, out)
		}
		for _, kind := range []string{"Load", "ReadStrict", "TextJson", "FromJson"} {
			if err := os.MkdirAll(filepath.Join(dir, "regressions", kind), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "regressions", kind, "refusal-ff"), []byte("go test fuzz v1\n[]byte(\"\\x00\\xff\")\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return dir
	}
	saved := func(t *testing.T, dir, target string) (string, []byte) {
		t.Helper()
		entries, err := filepath.Glob(filepath.Join(dir, "testdata", "fuzz", target, "*"))
		if err != nil || len(entries) == 0 {
			t.Fatalf("%s failed but saved no minimized input", target)
		}
		data, err := os.ReadFile(entries[0])
		if err != nil {
			t.Fatal(err)
		}
		return filepath.Base(entries[0]), data
	}
	var loadName string
	var loadData []byte
	var jsonName string
	var jsonData []byte
	var resolveName string
	var resolveData []byte

	t.Run("room plant in", func(t *testing.T) {
		dir := scratch(t, "tables/examples", "room", nil)
		out, err := runFuzz(t, dir, "FuzzLoad", "8s")
		if err == nil {
			t.Fatalf("the fuzzer stayed green with Room(n) dropped from TableReader.Body:\n%s", out)
		}
		loadName, loadData = saved(t, dir, "FuzzLoad")
		if err := os.WriteFile(filepath.Join(dir, "regressions", "Load", loadName), loadData, 0o644); err != nil {
			t.Fatal(err)
		}
		if replayOut, replayErr := runReplay(t, dir); replayErr == nil {
			t.Fatalf("the saved input replayed green with the plant in:\n%s", replayOut)
		}
	})
	t.Run("room plant out", func(t *testing.T) {
		if len(loadData) == 0 {
			t.Fatal("no saved input from the room plant-in run")
		}
		dir := scratch(t, "tables/examples", "", nil)
		if err := os.WriteFile(filepath.Join(dir, "regressions", "Load", loadName), loadData, 0o644); err != nil {
			t.Fatal(err)
		}
		if replayOut, replayErr := runReplay(t, dir); replayErr != nil {
			t.Fatalf("the saved input failed replay with the plant out:\n%s", replayOut)
		}
		if out, err := runFuzz(t, dir, "FuzzLoad", "8s"); err != nil {
			t.Fatalf("the fuzzer went red with the plant out:\n%s", out)
		}
	})

	t.Run("surrogate plant in", func(t *testing.T) {
		dir := scratch(t, "tables/examples", "surrogate", nil)
		out, err := runFuzz(t, dir, "FuzzFromJson", "12s")
		if err == nil {
			t.Fatalf("FuzzFromJson stayed green with the surrogate lookahead changed to in.pos+1:\n%s", out)
		}
		jsonName, jsonData = saved(t, dir, "FuzzFromJson")
		if err := os.WriteFile(filepath.Join(dir, "regressions", "FromJson", jsonName), jsonData, 0o644); err != nil {
			t.Fatal(err)
		}
		if replayOut, replayErr := runReplay(t, dir); replayErr == nil {
			t.Fatalf("the saved JSON input replayed green with the plant in:\n%s", replayOut)
		}
	})
	t.Run("surrogate plant out", func(t *testing.T) {
		if len(jsonData) == 0 {
			t.Fatal("no saved input from the surrogate plant-in run")
		}
		dir := scratch(t, "tables/examples", "", nil)
		if err := os.WriteFile(filepath.Join(dir, "regressions", "FromJson", jsonName), jsonData, 0o644); err != nil {
			t.Fatal(err)
		}
		if replayOut, replayErr := runReplay(t, dir); replayErr != nil {
			t.Fatalf("the saved JSON input failed replay with the plant out:\n%s", replayOut)
		}
		if out, err := runFuzz(t, dir, "FuzzFromJson", "12s"); err != nil {
			t.Fatalf("FuzzFromJson went red with the plant out:\n%s", out)
		}
	})

	// The pointers walk's Resolve bound (GraphdemoTable.go:1225): the plant lets
	// index len(Ids)/8+1 through, so the id read starts past the trailer. The
	// proof first pins a REPLAY input that reaches it (graph_tree's first node
	// reference changed to n+1), then asks the bounded fuzzer to find the same
	// out-of-range reference without being handed that input.
	resolveManifest := func(pkg string) string {
		return fmt.Sprintf("unit graphdemo %s\ninstance graph_tree graphdemo Scene %s\n", pkg, filepath.Join(root, "testdata", "wire", "tables", "graph_tree.bin"))
	}
	t.Run("resolve plant in", func(t *testing.T) {
		dir := scratch(t, "tables/pointers", "resolve", resolveManifest)
		wire, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "tables", "graph_tree.bin"))
		if err != nil {
			t.Fatal(err)
		}
		if len(wire) < 10 {
			t.Fatal("graph_tree.bin is too short to carry a node trailer")
		}
		n := binary.LittleEndian.Uint64(wire[len(wire)-8:])
		if n == 0 || n > 254 {
			t.Fatalf("graph_tree.bin names %d nodes; the replay input needs a one-byte reference", n)
		}
		selector, err := tableSelector(t, dir, "Scene")
		if err != nil {
			t.Fatal(err)
		}
		reach := append([]byte(nil), wire...)
		reach[1] = byte(n + 1)
		reach = append([]byte{byte(selector)}, reach...)
		reachPath := filepath.Join(dir, "regressions", "Load", "resolve-bound")
		if err := os.WriteFile(reachPath, reach, 0o644); err != nil {
			t.Fatal(err)
		}
		if replayOut, replayErr := runReplay(t, dir); replayErr == nil {
			t.Fatalf("the out-of-range reference replayed green with the Resolve bound loosened:\n%s", replayOut)
		}
		if err := os.Remove(reachPath); err != nil {
			t.Fatal(err)
		}
		out, err := runFuzz(t, dir, "FuzzLoad", "10s")
		if err == nil {
			t.Fatalf("FuzzLoad stayed green with the Resolve bound loosened:\n%s", out)
		}
		resolveName, resolveData = saved(t, dir, "FuzzLoad")
		if err := os.WriteFile(filepath.Join(dir, "regressions", "Load", resolveName), resolveData, 0o644); err != nil {
			t.Fatal(err)
		}
		if replayOut, replayErr := runReplay(t, dir); replayErr == nil {
			t.Fatalf("the fuzzer's saved input replayed green with the plant in:\n%s", replayOut)
		}
	})
	t.Run("resolve plant out", func(t *testing.T) {
		if len(resolveData) == 0 {
			t.Fatal("no saved input from the resolve plant-in run")
		}
		dir := scratch(t, "tables/pointers", "", resolveManifest)
		if err := os.WriteFile(filepath.Join(dir, "regressions", "Load", resolveName), resolveData, 0o644); err != nil {
			t.Fatal(err)
		}
		if replayOut, replayErr := runReplay(t, dir); replayErr != nil {
			t.Fatalf("the saved input failed replay with the plant out:\n%s", replayOut)
		}
		if out, err := runFuzz(t, dir, "FuzzLoad", "10s"); err != nil {
			t.Fatalf("FuzzLoad went red with the plant out:\n%s", out)
		}
	})
}

// tableSelector returns the first byte a generated harness gives a table name.
func tableSelector(t *testing.T, dir, name string) (int, error) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "nightly_test.go"))
	if err != nil {
		return 0, err
	}
	selector := 0
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "name: \"") {
			if strings.HasPrefix(trimmed, "name: \""+name+"\"") {
				return selector, nil
			}
			selector++
		}
	}
	return 0, fmt.Errorf("the generated harness has no table %q", name)
}

// patchSurrogateLookahead changes the JSON string-escape surrogate lookahead
// from in.pos+2 (a partner needs both its backslash and its u) to in.pos+1, an
// off-by-one that lets a high surrogate at the end of the input read one byte
// past it. It is the JSON leg's proof of the same bug class Room(n) proves for
// the wire: a bound read from the input and then trusted past it.
func patchSurrogateLookahead(t *testing.T, pkg string) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(pkg, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	patched := 0
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		const check = "in.pos+2 <= len(in.text)"
		if !bytes.Contains(data, []byte(check)) {
			continue
		}
		data = bytes.Replace(data, []byte(check), []byte("in.pos+1 <= len(in.text)"), 1)
		if err := os.WriteFile(file, data, 0o644); err != nil {
			t.Fatal(err)
		}
		patched++
	}
	if patched == 0 {
		t.Fatal("the generated reader has no surrogate in.pos+2 lookahead to change")
	}
}

// patchResolveBound loosens the node-table Resolve bound by one, so index
// len(Ids)/8+1 reads the id at the end of the trailer instead of refusing.
func patchResolveBound(t *testing.T, pkg string) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(pkg, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	patched := 0
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		const check = "if ref == 0 || ref > uint64(len(r.Ids)/8) {"
		if !bytes.Contains(data, []byte(check)) {
			continue
		}
		data = bytes.Replace(data, []byte(check), []byte("if ref == 0 || ref > uint64(len(r.Ids)/8)+1 {"), 1)
		if err := os.WriteFile(file, data, 0o644); err != nil {
			t.Fatal(err)
		}
		patched++
	}
	if patched == 0 {
		t.Fatal("the generated reader has no Resolve bound to loosen")
	}
}

// patchGeneratedReader drops the Room(n) bounds check from TableReader.Body in
// the scratch copy of the generated reader. That is the exact class the nightly
// oracle exists to catch: a length that is read from the wire and then trusted
// past the input.
func patchGeneratedReader(t *testing.T, pkg string) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(pkg, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	patched := 0
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		const check = "if !ok || !r.Room(n) {"
		if !bytes.Contains(data, []byte(check)) {
			continue
		}
		data = bytes.Replace(data, []byte(check), []byte("if !ok {"), 1)
		if err := os.WriteFile(file, data, 0o644); err != nil {
			t.Fatal(err)
		}
		patched++
	}
	if patched == 0 {
		t.Fatal("the generated reader has no Room(n) check in TableReader.Body to drop")
	}
}

// TestNightlyFuzzManifestSeedsEachTablesOwnInstances pins the MANIFEST seed
// contract: an instance seeds its own root's selector, never every table's, the
// wire form may be a .bin, and the JSON lexer tokens are wrapped in
// table-shaped JSON as the value of a real field. It scans the real MANIFEST
// against scratch packages named for the three populated shapes the task names:
// a pointer table (Scene), a list table (Save) and a map table (Fleet).
func TestNightlyFuzzManifestSeedsEachTablesOwnInstances(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	gen := filepath.Join(root, "internal", "ci", "nightlyfuzzgen.go")
	out := t.TempDir()
	packages := map[string][]string{
		"pointers": {"Aux", "Scene"},
		"lists":    {"Holder", "Save"},
		"maps":     {"Cells", "Fleet"},
	}
	for dir, names := range packages {
		pkgDir := filepath.Join(out, dir)
		if err := os.MkdirAll(pkgDir, 0o755); err != nil {
			t.Fatal(err)
		}
		var b strings.Builder
		b.WriteString("package " + dir + "\n\ntype TableReport struct{}\n\nfunc (r *TableReport) Err() error { return nil }\n\n")
		for _, name := range names {
			fmt.Fprintf(&b, "var %sTableFields = []TableFieldInfo{\n\t{Name: \"label\", Json: \"label\", TypeName: \"string\"},\n}\n\n", name)
			fmt.Fprintf(&b, "type %s struct{}\n\n", name)
			fmt.Fprintf(&b, "func %sReset(*%s) {}\n", name, name)
			fmt.Fprintf(&b, "func %sLoad(*%s, []byte, *TableReport) bool { return false }\n", name, name)
			fmt.Fprintf(&b, "func %sReadStrict(wire []byte) (*%s, error) { return nil, nil }\n", name, name)
			fmt.Fprintf(&b, "func %sLoadText(string, *TableReport) (*%s, error) { return nil, nil }\n", name, name)
			fmt.Fprintf(&b, "func %sFromJson(*%s, []byte, *TableReport) bool { return false }\n", name, name)
			fmt.Fprintf(&b, "func %sMeasure(*%s) int64 { return 0 }\n", name, name)
			fmt.Fprintf(&b, "func %sSave(*%s, []byte) int64 { return 0 }\n", name, name)
			fmt.Fprintf(&b, "func %sSaveText(*%s) (string, error) { return \"\", nil }\n", name, name)
			fmt.Fprintf(&b, "func %sToJsonMeasure(*%s) int64 { return 0 }\n", name, name)
			fmt.Fprintf(&b, "func %sToJson(*%s, []byte) int64 { return 0 }\n", name, name)
		}
		if err := os.WriteFile(filepath.Join(pkgDir, "tables.go"), []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	list := filepath.Join(out, "corpus.txt")
	if err := os.WriteFile(list, []byte("tables/pointers\ntables/lists\ntables/maps\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "run", gen, list, out, filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=readonly")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate the manifest-seeded harness: %v\n%s", err, output)
	}
	data, err := os.ReadFile(filepath.Join(out, "nightly_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)
	wireBlock := between(src, "var manifestWireSeeds = []manifestSeed{", "\n}\n")
	jsonBlock := between(src, "var manifestJSONSeeds = []manifestSeed{", "\n}\n")
	for _, want := range []string{"graph_tree.bin", "list_scalars.bin", "map_full.bin"} {
		if !strings.Contains(wireBlock, want) {
			t.Errorf("the wire seeds do not carry the %s instance for its own table", want)
		}
	}
	for _, want := range []string{"graph_tree.json", "list_scalars.json", "map_full.json"} {
		if !strings.Contains(jsonBlock, want) {
			t.Errorf("the JSON seeds do not carry the %s instance for its own table", want)
		}
	}
	for _, want := range []string{
		"jsonField: \"label\"",
		"ext != \".wire\" && ext != \".bin\"",
		"doc := \"{\\\"\" + target.jsonField + \"\\\":\" + token + \"}\"",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("the generated harness does not carry %q", want)
		}
	}
	// Scene, Save and Fleet are each the SECOND table in their package, so a
	// selector of 1 proves an instance got its own table's byte, not byte 0.
	for _, block := range []string{wireBlock, jsonBlock} {
		for _, name := range []string{"graph_tree", "list_scalars", "map_full"} {
			if !strings.Contains(block, "{selector: 1, path:") {
				t.Errorf("the %s seed does not carry its own table's selector: %s", name, block)
			}
		}
	}
}

func between(text, start, end string) string {
	i := strings.Index(text, start)
	if i < 0 {
		return ""
	}
	text = text[i+len(start):]
	j := strings.Index(text, end)
	if j < 0 {
		return text
	}
	return text[:j]
}

// TestNightlyFuzzRulesLiveInOwnMakeFragment pins the card's file layout: the
// campaign's rules are in tools/schema-go-nightly-fuzz/nightly.mk, included by
// path from make/go.mk. It is NOT under make/: $(wildcard make/*.mk) is the
// live-port registry (tables-ports-refuse-wide-scalars probes every name as a
// --lang), so a fragment there would be refused unless the root Makefile were
// edited. The regression fixtures live under internal/ci/testdata, not beside
// the test.
func TestNightlyFuzzRulesLiveInOwnMakeFragment(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	fragmentPath := filepath.Join(root, "tools", "schema-go-nightly-fuzz", "nightly.mk")
	fragment, err := os.ReadFile(fragmentPath)
	if err != nil {
		t.Fatalf("the campaign's make fragment is not at %s: %v", fragmentPath, err)
	}
	for _, want := range []string{
		"schema-go-nightly-fuzz:",
		"schema-go-nightly-fuzz-generate:",
		"schema-go-nightly-fuzz-replay:",
		"SCHEMA_GO_NIGHTLY_FUZZ_FUZZTIME ?= 10m",
		"internal/ci/testdata/nightly-fuzz-corpus",
		"tool missing: go",
	} {
		if !strings.Contains(string(fragment), want) {
			t.Errorf("tools/schema-go-nightly-fuzz/nightly.mk does not carry %q", want)
		}
	}
	goMake, err := os.ReadFile(filepath.Join(root, "make", "go.mk"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(goMake), "include tools/schema-go-nightly-fuzz/nightly.mk") {
		t.Error("make/go.mk does not include tools/schema-go-nightly-fuzz/nightly.mk by path")
	}
	if !strings.Contains(string(goMake), "schema-go-nightly-fuzz-negative-control:") {
		t.Error("make/go.mk does not define the negative control tools/negativecontrols enumerates")
	}
	if _, err := os.Stat(filepath.Join(root, "make", "schema-go-nightly-fuzz.mk")); !os.IsNotExist(err) {
		t.Errorf("make/schema-go-nightly-fuzz.mk exists: the live-port registry would probe it as a --lang")
	}
}
