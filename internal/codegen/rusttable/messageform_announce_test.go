package rusttable

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
)

// TestMessageFormAnnounceRust holds the MESSAGE FORM's announcement surface
// (docs/SPEC-TABLES.md §3.3) to the Rust leg, whose bytes are the C++
// reference's, pinned by testdata/conformance/tables/MANIFEST.txt. One subtest
// per clause of the card; the manifest is read here rather than copied, so a
// new connection row is a new subtest with no edit.
func TestMessageFormAnnounceRust(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt")
	conns := manifestConnections(t, manifest)
	// Every unit a connection names, parsed once before the parallel subtests.
	units := map[string][]string{}
	for _, line := range readManifest(t, manifest) {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[0] == "unit" {
			units[fields[1]] = fields[2:]
		}
	}
	loaded := map[string]*unitOut{}
	for _, name := range []string{"backenddemo", "vocabdemo", "vocab9demo"} {
		paths, ok := units[name]
		if !ok {
			t.Fatalf("manifest names no unit %q", name)
		}
		loaded[name] = generateUnit(t, root, paths)
	}

	t.Run("2.1_announcement_bytes", func(t *testing.T) {
		t.Parallel()
		for _, conn := range conns {
			conn := conn
			if conn.unit != "backenddemo" && conn.unit != "vocabdemo" && conn.unit != "vocab9demo" {
				continue
			}
			t.Run(conn.unit, func(t *testing.T) {
				t.Parallel()
				out := loaded[conn.unit]
				module, ok := out.files[AnnounceModule+".rs"]
				if !ok {
					t.Fatalf("no %s.rs; the announcement surface is absent", AnnounceModule)
				}
				got := announcementBytes(t, string(module))
				want, err := os.ReadFile(filepath.Join(root, conn.path))
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(got, want) {
					t.Fatalf("the announcement bytes differ from the reference: %d bytes vs %d", len(got), len(want))
				}
			})
		}
	})

	t.Run("2.2_announce_read_fills_vocabulary", func(t *testing.T) {
		t.Parallel()
		module := string(loaded["backenddemo"].files[AnnounceModule+".rs"])
		for _, want := range []string{"pub struct TableVocabulary", "pub fn announce_read("} {
			if !strings.Contains(module, want) {
				t.Errorf("the announcement surface is missing %q; AnnounceRead is not landed", want)
			}
		}
	})

	t.Run("2.3_announce_read_refusals", func(t *testing.T) {
		t.Parallel()
		module := string(loaded["backenddemo"].files[AnnounceModule+".rs"])
		for _, want := range []string{"second_announcement", "vocabulary_too_large", "malformed"} {
			if !strings.Contains(module, want) {
				t.Errorf("the announcement refusals are missing %q", want)
			}
		}
	})
}

type manifestConnection struct {
	unit, path string
}

// manifestConnections reads only the `connection` rows this card's surface
// answers: the unit whose vocabulary the announcement carries and the file
// that pins its bytes.
func manifestConnections(t *testing.T, path string) []manifestConnection {
	t.Helper()
	var out []manifestConnection
	for _, line := range readManifest(t, path) {
		fields := strings.Fields(line)
		if len(fields) >= 5 && fields[0] == "connection" {
			out = append(out, manifestConnection{unit: fields[2], path: fields[4]})
		}
	}
	if len(out) == 0 {
		t.Fatalf("%s carries no connection rows", path)
	}
	return out
}

func readManifest(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(string(data), "\n")
}

type unitOut struct {
	files map[string][]byte
}

// generateUnit parses one unit's source paths and runs the Rust emitter.
func generateUnit(t *testing.T, root string, paths []string) *unitOut {
	t.Helper()
	var files []check.SourceFile
	for _, p := range paths {
		abs := filepath.Join(root, p)
		info, err := os.Stat(abs)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		if info.IsDir() {
			entries, err := os.ReadDir(abs)
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range entries {
				if !e.IsDir() && strings.HasSuffix(e.Name(), ".schema") && len(e.Name()) > len(".schema") {
					names = append(names, filepath.Join(abs, e.Name()))
				}
			}
		} else {
			names = append(names, abs)
		}
		for _, name := range names {
			data, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			f, perrs := parser.Parse(name, data)
			if len(perrs) > 0 {
				t.Fatalf("parse %s: %v", name, perrs[0])
			}
			files = append(files, check.SourceFile{
				Path: name, Name: filepath.Base(name), Base: strings.TrimSuffix(filepath.Base(name), ".schema"),
				Bytes: data, AST: f,
			})
		}
	}
	u, cerrs := check.Unit(files)
	if len(cerrs) > 0 {
		t.Fatalf("check %v: %v", paths, cerrs[0])
	}
	out, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	return &unitOut{files: out}
}

var announceArray = regexp.MustCompile(`TABLE_ANNOUNCEMENT: \[u8; (\d+)\] = \[([^\]]*)\]`)

// announcementBytes reads the emitted constant back, so the comparison is
// against the bytes the compiler would place in the crate, not a copy.
func announcementBytes(t *testing.T, module string) []byte {
	t.Helper()
	m := announceArray.FindStringSubmatch(module)
	if m == nil {
		t.Fatal("no TABLE_ANNOUNCEMENT array in the emitted module")
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		t.Fatal(err)
	}
	var out []byte
	for _, tok := range strings.Split(m[2], ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		v, err := strconv.ParseUint(strings.TrimPrefix(tok, "0x"), 16, 8)
		if err != nil {
			t.Fatalf("bad byte literal %q: %v", tok, err)
		}
		out = append(out, byte(v))
	}
	if len(out) != n {
		t.Fatalf("the array length says %d, the literals number %d", n, len(out))
	}
	return out
}
