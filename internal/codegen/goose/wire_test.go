package goose

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
)

// TestGooseVendoredSerializeIsThePin holds runtime/serialize*.goose to
// runtime/SERIALIZE_PIN: every listed file present with its sha256, and no
// serialize*.goose the pin does not list (docs/PORT-GOOSE.md, THE RUNTIME
// LAYOUT). A runtime gap is fixed in serialize.goose and re-vendored, never
// patched here.
func TestGooseVendoredSerializeIsThePin(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join("runtime", "SERIALIZE_PIN"))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	var repo, tag, commit string
	for n, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		f := strings.Fields(line)
		switch {
		case line == "" || strings.HasPrefix(line, "#"):
		case f[0] == "repository" && len(f) == 2:
			repo = f[1]
		case f[0] == "tag" && len(f) == 2:
			tag = f[1]
		case f[0] == "commit" && len(f) == 2:
			commit = f[1]
		case f[0] == "sha256" && len(f) == 3:
			files[f[2]] = f[1]
		default:
			t.Fatalf("SERIALIZE_PIN line %d: unknown directive %q", n+1, line)
		}
	}
	if repo != "https://github.com/mas-bandwidth/serialize.goose" || tag != "v2.1.1" || len(commit) != 40 || len(files) == 0 {
		t.Fatalf("SERIALIZE_PIN names repository %q tag %q commit %q and %d files; want serialize.goose v2.1.1 at a full commit", repo, tag, commit, len(files))
	}
	for name, want := range files {
		b, err := os.ReadFile(filepath.Join("runtime", name))
		if err != nil {
			t.Errorf("SERIALIZE_PIN lists %s, which is not vendored: %v", name, err)
			continue
		}
		sum := sha256.Sum256(b)
		if got := hex.EncodeToString(sum[:]); got != want {
			t.Errorf("runtime/%s sha256 %s, the pin says %s: re-vendor, never edit", name, got, want)
		}
	}
	vendored, _ := filepath.Glob(filepath.Join("runtime", "serialize*.goose"))
	sort.Strings(vendored)
	for _, p := range vendored {
		if _, ok := files[filepath.Base(p)]; !ok {
			t.Errorf("runtime/%s is vendored but SERIALIZE_PIN does not list it", filepath.Base(p))
		}
	}
}

// runWireLeg generates the schema directory src, puts the hand-written Goose
// test program beside the modules with every testdata/wire golden as an input
// file, and runs it under the pinned compiler in both modes: the release run
// (-O1) and --debug, whose overflow and range checks abort on any signed
// overflow or lossy `as` the generated codec would commit.
func runWireLeg(t *testing.T, src, program string) {
	t.Helper()
	root := filepath.Join("..", "..", "..")
	u := unitFromDir(t, filepath.Join(root, src))
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeAll(t, dir, files)
	main, err := os.ReadFile(filepath.Join(root, program))
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "main.goose", string(main))
	goldens, _ := filepath.Glob(filepath.Join(root, "testdata", "wire", "*.bin"))
	inputs := map[string][]byte{}
	for _, g := range goldens {
		b, err := os.ReadFile(g)
		if err != nil {
			t.Fatal(err)
		}
		inputs[filepath.Base(g)] = b
	}
	if len(inputs) == 0 {
		t.Fatal("no wire goldens under testdata/wire")
	}
	if out := string(gooserun.Run(t, dir, "main.goose", inputs)); out != "OK\n" {
		t.Fatalf("%s printed:\n%s", program, out)
	}
	if out := string(gooserun.RunDebug(t, dir, "main.goose", nil)); out != "OK\n" {
		t.Fatalf("%s under --debug printed:\n%s", program, out)
	}
}

// TestGooseExamplesAgreeWithTheWireGoldens is the cross-language wire gate
// (SPEC §7.2) for examples/: test/goose/main.goose writes the instances the
// C++ test pins and byte-compares each against testdata/wire, round-trips
// them, checks branch zeroing and the refusals (test/go/main.go's twin).
func TestGooseExamplesAgreeWithTheWireGoldens(t *testing.T) {
	t.Parallel()
	runWireLeg(t, "examples", filepath.Join("test", "goose", "main.goose"))
}

// TestGooseLudicrousAgreesWithTheWireGoldens is the same gate for
// examples128/: fixed(I, F), ufixed(I, F), int128 and uint128
// (test/go-ludicrous/main.go's twin).
func TestGooseLudicrousAgreesWithTheWireGoldens(t *testing.T) {
	t.Parallel()
	runWireLeg(t, "examples128", filepath.Join("test", "goose-ludicrous", "main.goose"))
}
