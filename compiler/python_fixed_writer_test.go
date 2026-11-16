package compiler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// Functional: the public compiler generation route reads the real versioned
// lock after a lawful widening, then both pins enforce the older writer bounds.
func TestPythonFixedWriterLock(t *testing.T) {
	t.Parallel()
	pythonFixedWriterLock(t, false)
}

func TestPythonFixedWriterLockNegativeControl(t *testing.T) {
	t.Parallel()
	pythonFixedWriterLock(t, true)
}

func pythonFixedWriterLock(t *testing.T, sabotage bool) {
	t.Helper()
	slowtest.Gate(t, "the pinned Python interpreters")
	dir := t.TempDir()
	path := filepath.Join(dir, "Probe.schema")
	old := `package probe
fixed table T { x int32 | min = -5, max = 5
 s int128 | min = -500, max = 500 }
`
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	c := New()
	u, err := c.Load([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := UpdateSchemaLock(u, []string{path}); err != nil {
		t.Fatal(err)
	}
	oldFiles, err := c.Generate(u, "python", Options{})
	if err != nil {
		t.Fatal(err)
	}
	newer := strings.ReplaceAll(strings.ReplaceAll(old, "-5, max = 5", "-10, max = 10"), "-500, max = 500", "-1000, max = 1000")
	if err := os.WriteFile(path, []byte(newer), 0o600); err != nil {
		t.Fatal(err)
	}
	u, err = c.Load([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := UpdateSchemaLock(u, []string{path}); err != nil {
		t.Fatal(err)
	}
	files, err := c.Generate(u, "python", Options{})
	if err != nil {
		t.Fatal(err)
	}

	if sabotage {
		name := "probe/_fixed.py"
		const anchor = `"hi":5,`
		source := string(files[name])
		if strings.Count(source, anchor) != 1 {
			t.Fatal("historical-bound control lost its generated anchor")
		}
		files[name] = []byte(strings.Replace(source, anchor, `"hi":10,`, 1))
	}
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		olddir := t.TempDir()
		pythonWriterTree(t, olddir, oldFiles)
		script := pyrun.ImportPrelude + `from probe import T
v=T();v.x=5;v.s=500
with open('oldwire','wb') as f:f.write(v.save())
`
		_, stderr, err := pyrun.Try(t, pin, olddir, "main.py", map[string][]byte{"main.py": []byte(script)})
		if err != nil {
			t.Fatalf("old writer: %v\n%s", err, stderr)
		}
		wire, err := os.ReadFile(filepath.Join(olddir, "oldwire"))
		if err != nil {
			t.Fatal(err)
		}
		newdir := t.TempDir()
		pythonWriterTree(t, newdir, files)
		script = pyrun.ImportPrelude + `from probe import T, Refused, FixedLoadReport
with open('oldwire','rb') as f:wire=f.read()
v=T.load(wire);assert v.x==5 and v.s==500
start=20+int.from_bytes(wire[16:20],'little')+8
for offset,size,value in [(0,4,6),(0,4,-6),(4,16,501),(4,16,-501)]:
 bad=bytearray(wire);bad[start+offset:start+offset+size]=value.to_bytes(size,'little',signed=True)
 report=FixedLoadReport();report.layout_hash=123;report.widened=456
 try:T.load(bad,report)
 except Refused:pass
 else:raise AssertionError('historical writer bounds lost')
 assert report.layout_hash==123 and report.widened==456
`
		_, stderr, err = pyrun.Try(t, pin, newdir, "main.py", map[string][]byte{"main.py": []byte(script), "oldwire": wire})
		if sabotage {
			if err == nil || !strings.Contains(stderr, "historical writer bounds lost") {
				t.Fatalf("writer-bound sabotage did not fail by name: %v\n%s", err, stderr)
			}
		} else if err != nil {
			t.Fatalf("locked reader: %v\n%s", err, stderr)
		}
	}
}

func pythonWriterTree(t *testing.T, dir string, files map[string][]byte) {
	t.Helper()
	for name, data := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
