package pythontable

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// These actual manifest wires owe strict named refusal, not a generated-tree
// absence. Equal contents in distinct BlobRefs remain lawful (SPEC §2.5).
func blobWireDriverFiles(t *testing.T) map[string][]byte {
	t.Helper()
	repo := filepath.Join("..", "..", "..")
	files := map[string][]byte{}
	for key, path := range map[string]string{"blobdemo": "tables/blobs", "mapdemo": "tables/maps"} {
		generated, err := Generate(loadUnitPaths(t, filepath.Join(repo, path)))
		if err != nil {
			t.Fatal(err)
		}
		for name, body := range generated {
			files["generated/"+key+"/"+name] = body
		}
	}
	for _, name := range []string{"wire.py", "_common.py"} {
		body, err := os.ReadFile(filepath.Join(repo, "test/conformance/python/surfaces", name))
		if err != nil {
			t.Fatal(err)
		}
		files["surfaces/"+name] = body
	}
	for _, name := range []string{"blob_shared", "blob_shared_string", "blob_equal_bytes"} {
		body, err := os.ReadFile(filepath.Join(repo, "testdata/wire/tables", name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		files[name+".bin"] = body
	}
	files["manifest"] = []byte("instance blob_shared blobdemo Catalog blob_shared.bin no-text\ninstance blob_shared_string mapdemo Docs blob_shared_string.bin no-text\ninstance blob_equal_bytes blobdemo Catalog blob_equal_bytes.bin no-text\n")
	return files
}

const blobWireDriverProbe = `import os, sys
from pathlib import Path
os.environ['PYTHON_CONFORMANCE_GENERATED'] = 'generated'
sys.path.insert(0, 'surfaces')
import wire
from _common import root_class, unit_package
Path('out').mkdir()
wire.run('manifest', 'out')
for case in ('blob_shared', 'blob_shared_string'):
    assert Path('out', case + '.absent').exists(), 'missing observed blob_shared refusal'
    assert Path('out', case + '.absent').read_bytes() == b'blob_shared', 'driver dropped a public refusal'
    assert not Path('out', case).exists()
assert Path('out/blob_equal_bytes').read_bytes() == Path('blob_equal_bytes.bin').read_bytes()
Catalog = root_class('blobdemo', 'Catalog')
Docs = root_class('mapdemo', 'Docs')
blobs = unit_package('blobdemo')
maps = unit_package('mapdemo')

def writer_refuses(value, error, label):
    for operation in (value.measure, value.save):
        try: operation()
        except error as err:
            assert err.rule == 'blob_shared', (label, err.rule)
        else: raise AssertionError('shared ' + label + ' writer accepted')

r = Catalog(); shared = blobs.BlobRef(b'same')
r.thumb = shared; r.alias = shared
assert r.thumb is r.alias
writer_refuses(r, blobs.InvalidValue, 'bytes')
r.thumb = blobs.BlobRef(b'same'); r.alias = blobs.BlobRef(b'same')
assert r.thumb is not r.alias
saved = r.save()
assert r.measure() == len(saved) and Catalog.load(saved).save() == saved
r.thumb = b'same'; r.alias = b'same'
assert r.save() == saved

d = Docs(); shared_text = maps.BlobRef('same')
d.pages = {'a': shared_text, 'b': shared_text}
assert d.pages['a'] is d.pages['b']
writer_refuses(d, maps.InvalidValue, 'string')
d.pages = {'a': maps.BlobRef('same'), 'b': maps.BlobRef('same')}
assert d.pages['a'] is not d.pages['b']
saved_text = d.save()
assert d.measure() == len(saved_text) and Docs.load(saved_text).save() == saved_text

import _common
original = Catalog.load
Path('wrong-manifest').write_text('instance wrong blobdemo Catalog blob_shared.bin\n')
# ANY public refusal is the leg's answer, whatever its reason: the harness
# compares the verdict (docs/rulings.json refusal-verdict-only)
def damaged_load(data): raise blobs.Refused('wire_damaged')
Catalog.load = staticmethod(damaged_load)
Path('damaged-out').mkdir()
wire.run('wrong-manifest', 'damaged-out')
assert Path('damaged-out/wrong.absent').read_bytes() == b'wire_damaged', 'driver dropped a public refusal'
assert not Path('damaged-out/wrong').exists() and not _common.DRIVER_ERRORS
# a failure that is not the codec's public refusal is a DRIVER ERROR: no
# answer, the case named, and the surface red once its cases have run
def broken_load(data): raise ValueError('blob_shared')
Catalog.load = staticmethod(broken_load)
Path('broken-out').mkdir()
wire.run('wrong-manifest', 'broken-out')
assert _common.DRIVER_ERRORS == ['wrong'], 'driver turned a non-public failure into an answer'
assert not Path('broken-out/wrong.absent').exists() and not Path('broken-out/wrong').exists()
del _common.DRIVER_ERRORS[:]
Catalog.load = original
# NO CODEC is an EMPTY .absent: never an abort, never a refusal
Path('missing-manifest').write_text('instance missing missing Root blob_shared.bin\n')
wire.run('missing-manifest', 'out')
assert Path('out/missing.absent').exists() and Path('out/missing.absent').read_bytes() == b'', 'driver treated a missing root as a refusal or an error'
assert not Path('out/missing').exists() and not _common.DRIVER_ERRORS
print('blob wire contract passed')
`

func TestPythonWireBlobDriver(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters and actual manifest blob wires")
	files := blobWireDriverFiles(t)
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		dir := t.TempDir()
		writeTree(t, dir, files)
		out, stderr, err := pyrun.Try(t, pin, dir, "probe.py", map[string][]byte{"probe.py": []byte(pyrun.ImportPrelude + blobWireDriverProbe)})
		if err != nil || strings.TrimSpace(out) != "blob wire contract passed" {
			t.Fatalf("%s: %v\n%s\n%s", pin, err, out, stderr)
		}
	}
}

func TestPythonWireBlobDriverNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters and planted read/write/driver faults")
	original := blobWireDriverFiles(t)
	for _, tc := range []struct{ name, path, before, after, diagnostic string }{
		{"reader", "generated/blobdemo/blobdemo/_schema/load.py", "if blob and names[k + 1] > 1:", "if False:", "missing observed blob_shared refusal"},
		{"writer", "generated/blobdemo/blobdemo/_schema/save.py", "if identity in ctx.blob_refs:", "if False:", "shared bytes writer accepted"},
		{"driver", "surfaces/wire.py", "absent(outdir, case, err.reason)", "no_codec(outdir, case)", "driver dropped a public refusal"},
		{"driver-missing", "surfaces/wire.py", "        no_codec(outdir, case)\n        return", "        raise AssertionError(case)", "driver treated a missing root as a refusal or an error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := maps.Clone(original)
			source := string(files[tc.path])
			count := strings.Count(source, tc.before)
			want := 1
			if tc.name == "writer" {
				want = 2
			}
			if count != want {
				t.Fatalf("%s control has %d targets, want %d", tc.name, count, want)
			}
			files[tc.path] = []byte(strings.ReplaceAll(source, tc.before, tc.after))
			for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
				dir := t.TempDir()
				writeTree(t, dir, files)
				_, stderr, err := pyrun.Try(t, pin, dir, "probe.py", map[string][]byte{"probe.py": []byte(pyrun.ImportPrelude + blobWireDriverProbe)})
				if err == nil || !strings.Contains(stderr, tc.diagnostic) {
					t.Fatalf("%s control did not fail by name: %v\n%s", pin, err, stderr)
				}
			}
		})
	}
}
