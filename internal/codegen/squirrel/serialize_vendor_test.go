package squirrel_test

import (
	"crypto/sha256"
	"encoding/hex"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestVendoredSquirrelSerializeIsThePin pins the vendored serialize port
// (schema-squirrel-serialize-vendor; docs/PORT-SQUIRREL.md, THE RUNTIME LAYOUT): the
// type wire's bottom layer, its src files copied verbatim from the repository
// SERIALIZE_PIN names. Every file the pin lists exists in
// internal/codegen/squirrel/serialize with that sha256, and no serialize*.nut file is
// there the pin does not list. Then a generated unit in a t.TempDir() loads the vendored
// entry module (serialize.nut) beside the generated modules under the pinned interpreter
// and round-trips one serializeInt and one serializeFloat through WriteStream and
// ReadStream, printing hex, compared with the bytes of two "writer canonical" records of
// the port's vendored corpus (.ref/serialize.nut/conformance/int.txt and float.txt), pasted
// below as literals naming each record (schema's go.mod has no serialize.go and none is
// added).
func TestVendoredSquirrelSerializeIsThePin(t *testing.T) {
	t.Parallel()

	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	serializeDir := filepath.Join(root, "internal", "codegen", "squirrel", "serialize")
	pin, err := readPin(filepath.Join(serializeDir, "SERIALIZE_PIN"))
	if err != nil {
		t.Fatal(err)
	}
	if pin.repo == "" || pin.commit == "" || len(pin.files) == 0 {
		t.Fatalf("SERIALIZE_PIN is incomplete: repo %q commit %q files %d", pin.repo, pin.commit, len(pin.files))
	}

	// every file the pin lists exists under serialize/ with that sha256
	for _, f := range pin.files {
		data, err := os.ReadFile(filepath.Join(serializeDir, f.name))
		if err != nil {
			t.Errorf("pinned %s is absent: %v", f.name, err)
			continue
		}
		sum := sha256.Sum256(data)
		if got := hex.EncodeToString(sum[:]); got != f.sha256 {
			t.Errorf("%s: sha256 %s, want %s", f.name, got, f.sha256)
		}
	}

	// no serialize*.nut file is there the pin does not list
	listed := map[string]bool{}
	for _, f := range pin.files {
		listed[f.name] = true
	}
	entries, err := os.ReadDir(serializeDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "serialize") || !strings.HasSuffix(e.Name(), ".nut") {
			continue
		}
		if !listed[e.Name()] {
			t.Errorf("%s is present but not listed by the pin", e.Name())
		}
	}

	roundTripSerialize(t, root)
}

// readPin parses SERIALIZE_PIN: `repository <url>`, `commit <sha>`, and one
// `sha256 <hash> <file>` line per vendored file, # comments and blank lines skipped.
func readPin(path string) (struct {
	repo   string
	commit string
	files  []struct {
		name   string
		sha256 string
	}
}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return struct {
			repo   string
			commit string
			files  []struct {
				name   string
				sha256 string
			}
		}{}, err
	}
	var p struct {
		repo   string
		commit string
		files  []struct {
			name   string
			sha256 string
		}
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		switch {
		case strings.HasPrefix(line, "repository "):
			p.repo = strings.TrimPrefix(line, "repository ")
		case strings.HasPrefix(line, "commit "):
			p.commit = strings.TrimPrefix(line, "commit ")
		case strings.HasPrefix(line, "sha256 "):
			parts := strings.Fields(strings.TrimPrefix(line, "sha256 "))
			if len(parts) == 2 {
				p.files = append(p.files, struct {
					name   string
					sha256 string
				}{name: parts[1], sha256: parts[0]})
			}
		}
	}
	return p, nil
}

// roundTripSerialize generates a unit, emits the vendored serialize files beside its
// modules, and runs a probe under the pinned interpreter (sqrun, docs/PORT-SQUIRREL.md,
// THE HARNESS) that writes and reads one serializeInt and one serializeFloat through
// WriteStream and ReadStream, printing the bytes as hex lines. The written bytes are
// compared with two "writer canonical" records of the port's vendored corpus:
// int.txt "int-range-0-7-costs-three-bits" (min 0, max 7, value 5 -> bytes 05) and
// float.txt "float-one" (value 1.0 -> bytes 00 00 80 3F). Each read-back value is
// re-serialized to the same bytes, proving the round trip byte for byte.
func roundTripSerialize(t *testing.T, root string) {
	t.Helper()

	u := &ir.Unit{
		Package:    "hello",
		ProtocolId: 0x11,
		Files:      []*ir.File{{Base: "Hello"}},
	}
	files, err := squirrel.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, squirrel.Serialize())
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	const main = `local _in = dofile("inputs.nut", true); // defines ::hexline (sqrun)
local serialize = dofile("serialize.nut", true);

// serializeInt [0,7] = 5 -> bytes 05 (conformance/int.txt, "int-range-0-7-costs-three-bits")
local wi = serialize.WriteStream(::blob(8));
if (!wi.serializeInt({ value = 5 }, 0, 7)) throw "int write failed: " + wi.error();
wi.flush();
::hexline(wi.data());

local ri = serialize.ReadStream(wi.data());
local hi = { value = null };
if (!ri.serializeInt(hi, 0, 7)) throw "int read failed: " + ri.error();
local wi2 = serialize.WriteStream(::blob(8));
if (!wi2.serializeInt(hi, 0, 7)) throw "int rewrite failed";
wi2.flush();
::hexline(wi2.data());

// serializeFloat 1.0 -> bytes 0000803f (conformance/float.txt, "float-one")
local wf = serialize.WriteStream(::blob(8));
if (!wf.serializeFloat({ value = 1.0 })) throw "float write failed: " + wf.error();
wf.flush();
::hexline(wf.data());

local rf = serialize.ReadStream(wf.data());
local hf = { value = null };
if (!rf.serializeFloat(hf)) throw "float read failed: " + rf.error();
local wf2 = serialize.WriteStream(::blob(8));
if (!wf2.serializeFloat(hf)) throw "float rewrite failed";
wf2.flush();
::hexline(wf2.data());
`
	if err := os.WriteFile(filepath.Join(dir, "main.nut"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	lines, err := sqrun.HexLines(sqrun.Run(t, dir, "main.nut", nil))
	if err != nil {
		t.Fatal(err)
	}

	wantInt := []byte{0x05}                     // int.txt "int-range-0-7-costs-three-bits"
	wantFloat := []byte{0x00, 0x00, 0x80, 0x3F} // float.txt "float-one"
	want := [][]byte{wantInt, wantInt, wantFloat, wantFloat}
	if len(lines) != len(want) {
		t.Fatalf("probe printed %d hex lines, want %d: %x", len(lines), len(want), lines)
	}
	for i := range want {
		if string(lines[i]) != string(want[i]) {
			t.Errorf("line %d: got %x, want %x (the %s record)", i, lines[i], want[i], recordName(i))
		}
	}
}

func recordName(i int) string {
	switch i {
	case 0, 1:
		return "int #int-range-0-7-costs-three-bits"
	default:
		return "float float-one"
	}
}
