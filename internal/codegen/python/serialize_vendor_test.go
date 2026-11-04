package python

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
)

// stagedSerialize is where serialize.py is staged until its own repository
// exists (third_party/serialize.py, docs/PORT-PYTHON.md, THE BIT LAYER),
// relative to this package.
const stagedSerialize = "../../../third_party/serialize.py/src/serialize"

// checkSerializePin holds a vendored serialize.py directory to its pin: every
// file the pin lists is in dir with that sha256 and the same bytes as the
// staged source file of that name, and no .py file is in dir that the pin
// does not list. It returns every breach.
func checkSerializePin(pin []byte, dir, staged string) []string {
	var bad []string
	listed := map[string]bool{}
	for line := range strings.SplitSeq(string(pin), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		if len(f) != 3 || f[0] != "sha256" {
			bad = append(bad, fmt.Sprintf("SERIALIZE_PIN: unrecognized line %q", line))
			continue
		}
		sum, name := f[1], f[2]
		listed[name] = true
		for _, where := range []string{dir, staged} {
			b, err := os.ReadFile(filepath.Join(where, name))
			if err != nil {
				bad = append(bad, fmt.Sprintf("%s: %v", filepath.Join(where, name), err))
				continue
			}
			got := sha256.Sum256(b)
			if hex.EncodeToString(got[:]) != sum {
				bad = append(bad, fmt.Sprintf("%s has sha256 %s, the pin says %s", filepath.Join(where, name), hex.EncodeToString(got[:]), sum))
			}
		}
	}
	if len(listed) == 0 {
		bad = append(bad, "SERIALIZE_PIN lists no files")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return append(bad, err.Error())
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".py") && !listed[e.Name()] {
			bad = append(bad, fmt.Sprintf("%s is present but the pin does not list it", e.Name()))
		}
	}
	return bad
}

const vendoredProbe = pyrun.ImportPrelude + `import math
from _schema.serialize import ReadStream, Ref, WriteStream

w = WriteStream(8)
assert w.serialize_int(Ref(7), 0, 7)
w.flush()
print(w.data().hex())
r = ReadStream(w.data())
v = Ref(0)
assert r.serialize_int(v, 0, 7), r.refusal()
print(v.value)

w = WriteStream(8)
assert w.serialize_float(Ref(math.pi))
w.flush()
print(w.data().hex())
r = ReadStream(w.data())
f = Ref(0.0)
assert r.serialize_float(f), r.refusal()
print("%.9g" % f.value)
`

// TestVendoredPythonSerializeIsThePin holds runtime/_schema/serialize to
// runtime/SERIALIZE_PIN and to the staged source (make
// vendor-serialize-python writes both), then loads the vendored copy as the
// runtime's subpackage under both pins and round-trips one serialize_int and
// one serialize_float: 7 over [0,7] writes 07, and pi writes db0f4940, the
// bytes of the corpus's canonical records (conformance/int.txt and
// float.txt).
func TestVendoredPythonSerializeIsThePin(t *testing.T) {
	t.Parallel()
	pin, err := os.ReadFile("runtime/SERIALIZE_PIN")
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range checkSerializePin(pin, "runtime/_schema/serialize", stagedSerialize) {
		t.Error(b)
	}
	runtime, err := Runtime()
	if err != nil {
		t.Fatal(err)
	}
	want := "07\n7\ndb0f4940\n3.14159274\n"
	for _, run := range []func(testing.TB, string, string, map[string][]byte) []byte{pyrun.Run, pyrun.RunFloor} {
		dir := t.TempDir()
		writeTree(t, dir, runtime)
		if got := string(run(t, dir, "main.py", map[string][]byte{"main.py": []byte(vendoredProbe)})); got != want {
			t.Errorf("the vendored serialize.py printed\n%s\nwant\n%s", got, want)
		}
	}
}

// TestVendoredPythonSerializePinGoesRed is the pin's negative control: a
// copy of the vendored directory with one byte of _core.py changed, and a
// copy with an unlisted module beside it, each break the pin by name.
func TestVendoredPythonSerializePinGoesRed(t *testing.T) {
	t.Parallel()
	pin, err := os.ReadFile("runtime/SERIALIZE_PIN")
	if err != nil {
		t.Fatal(err)
	}
	copyDir := func(t *testing.T) string {
		dir := t.TempDir()
		entries, err := os.ReadDir("runtime/_schema/serialize")
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			b, err := os.ReadFile(filepath.Join("runtime/_schema/serialize", e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, e.Name()), b, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return dir
	}
	t.Run("a changed byte", func(t *testing.T) {
		dir := copyDir(t)
		p := filepath.Join(dir, "_core.py")
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		b[len(b)-2] ^= 1
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
		bad := checkSerializePin(pin, dir, stagedSerialize)
		if len(bad) != 1 || !strings.Contains(bad[0], "_core.py has sha256") {
			t.Fatalf("a changed _core.py: the pin reported %q, want one breach naming _core.py", bad)
		}
	})
	t.Run("an unlisted module", func(t *testing.T) {
		dir := copyDir(t)
		if err := os.WriteFile(filepath.Join(dir, "_extra.py"), []byte("x = 1\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		bad := checkSerializePin(pin, dir, stagedSerialize)
		if len(bad) != 1 || !strings.Contains(bad[0], "_extra.py is present but the pin does not list it") {
			t.Fatalf("an unlisted module: the pin reported %q, want one breach naming _extra.py", bad)
		}
	})
}
