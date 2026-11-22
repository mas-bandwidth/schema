package compiler

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/viewlisting"
)

var reflectionFieldName = regexp.MustCompile(`(?:^|[[:space:]\(])name="([^"]+)"`)

var reflectionUnsupported = regexp.MustCompile(`(?m)(?:^|[[:space:]])(?:codec|elem_codec|inner)="unsupported"`)

// TestPythonReflectionDriverCorpus requires the public driver to carry every
// corpus unit. A generation refusal or an unsupported live descriptor is a
// named hole, not a skipped comparison.
func TestPythonReflectionDriverCorpus(t *testing.T) {
	t.Parallel()
	root := ".."
	data, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	entries := viewlisting.CorpusEntries(string(data))
	if len(entries) == 0 {
		t.Fatal("empty view corpus")
	}
	units := map[string][]string{}
	for _, e := range entries {
		name, _, ok := strings.Cut(e, ":")
		if !ok {
			t.Fatalf("bad VIEW_CORPUS entry %q", e)
		}
		dir := filepath.Join(root, "tables", name)
		if name == "wide" {
			dir = filepath.Join(root, "examples-wide")
		}
		units["corpus-"+name] = []string{dir}
	}
	manifest, err := os.ReadFile(filepath.Join(root, "testdata/conformance/tables/MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for line := range strings.SplitSeq(string(manifest), "\n") {
		f := strings.Fields(line)
		if len(f) >= 3 && f[0] == "unit" {
			for _, p := range f[2:] {
				units["manifest-"+f[1]] = append(units["manifest-"+f[1]], filepath.Join(root, p))
			}
		}
	}
	names := make([]string, 0, len(units))
	for name := range units {
		names = append(names, name)
	}
	slices.Sort(names)

	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			var paths []string
			for _, p := range units[name] {
				info, err := os.Stat(p)
				if err != nil {
					t.Fatal(err)
				}
				if info.IsDir() {
					rows, err := filepath.Glob(filepath.Join(p, "*.schema"))
					if err != nil {
						t.Fatal(err)
					}
					paths = append(paths, rows...)
				} else {
					paths = append(paths, p)
				}
			}
			if len(paths) == 0 {
				t.Fatal("unit has no schemas")
			}
			c := New()
			u, err := c.Load(paths)
			if err != nil {
				t.Fatal(err)
			}
			files, err := c.Generate(u, "python", nil)
			if err != nil {
				t.Fatalf("unit was not generated: %v", err)
			}
			if _, ok := files[filepath.ToSlash(filepath.Join(u.Package, "_schema/unit_view.py"))]; !ok {
				t.Fatal("unit view missing")
			}
			if len(files) == 0 {
				t.Fatal("files is empty: the loop below would check nothing")
			}
			for path, body := range files {
				if strings.Contains(path, "/_schema/") || !strings.HasSuffix(path, ".py") {
					continue
				}
				for _, loc := range reflectionUnsupported.FindAllIndex(body, -1) {
					line := 1 + strings.Count(string(body[:loc[0]]), "\n")
					field := "unknown"
					names := reflectionFieldName.FindAllSubmatch(body[:loc[0]], -1)
					if len(names) > 0 {
						field = string(names[len(names)-1][1])
					}
					t.Errorf("%s:%d: unsupported field or element %s", path, line, field)
				}
			}
		})
	}
}

func TestPythonReflectionDriverPacketOnly(t *testing.T) {
	t.Parallel()
	c := New()
	u, err := c.Load(writeSchemas(t, map[string]string{"Only.schema": `package only
type Child { value int32 }
union Pick { child Child }
/// A packet type.
type Thing {
    value int32
    children [2]Child
    choice Pick
}
`}))
	if err != nil {
		t.Fatal(err)
	}
	files, err := c.Generate(u, "python", nil)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, data := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	probe := pyrun.ImportPrelude + `import only
v = only.UnitView()
assert len(v.types) == 2 and not v.tables
row = next(x for x in v.types if x.name == 'Thing')
child = next(x for x in v.types if x.name == 'Child')
assert row.doc == 'A packet type.'
assert row.type.fields[0].field_id == 0 and row.type.fields[0].json_key is None
field = row.type.fields[1]
assert field.target_desc is child.type
assert field.element is not None and field.element.target_desc is child.type
union_field = row.type.fields[2]
assert union_field.union_desc is not None
assert union_field.union_desc.arms[0].target_desc is child.type
arm = next(x for x in v.unions if x.name == 'Pick').variants[1]
assert arm.id == 0 and arm.payload is child.type and arm.field is None
value = only.Thing()
field.set_element(value, 0, only.Child())
assert field.get_element(value, 0) is value.children[0]
`
	for _, floor := range []bool{false, true} {
		if floor {
			pyrun.RunFloor(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
		} else {
			pyrun.Run(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
		}
	}
	if out, err := pyrun.Analyze(t, dir, "only"); err != nil {
		t.Fatalf("strict mypy:\n%s", out)
	}
}

func TestPythonReflectionDriverNameCollision(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src string }{
		{"declaration", "package collision\ntype UnitView { value int32 }\n"},
		{"module", "package collision\ntype Value { count int32 }\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := New()
			base := "Collision.schema"
			if tc.name == "module" {
				base = "UnitView.schema"
			}
			u, err := c.Load(writeSchemas(t, map[string]string{base: tc.src}))
			if err != nil && tc.name == "declaration" && strings.Contains(err.Error(), "UnitView") {
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err = c.Generate(u, "python", nil); err == nil || !strings.Contains(err.Error(), "UnitView") {
				t.Fatalf("collision not refused by name: %v", err)
			}
		})
	}
}

func TestPythonReflectionDriverAllKinds(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, src string }{
		{"fixed-class", `package reflfixed
        enum Grade { Low, High }
        flags Cap { A, B }
        fixed table Fixed {
            text string(8)
            bytes_value bytes(8)
            f32 float32
            f64 float64
            compressed float32 | min=-5, max=5, resolution=0.01
            bounded int32 | min=-9, max=9
            grade Grade
            cap Cap
            fixed_values [3]int32
            counted_values [..3]int32
        }
        `},
		{"variable-containers", `package reflcontainers
        enum Key { A, B }
        table Node { value int32 }
        union Choice {
            leaf Node
            number int32
        }
        table Root {
            blob *bytes
            text *string
            optional ?int32
            keyed [Key]int32
            nodes []*Node
            bounded [2]*Node
            values []int32
            mapping map[uint32]int32
            choose Choice
        }
        `},
		{"hosted-packet", `package reflpacket
        type Sample {
            a int128 | min=-100, max=100
            b bits(7)
            c float32 | min=-5, max=5, resolution=0.01
            d fixed(16,16) | min=-100, max=100
            e string(8)
            f [3]int32
        }
        table Root { sample Sample }
        `},
		{"scalar-arrays", `package reflarrays
        enum Grade { A, B }
        flags Cap { A, B }
        fixed table Root {
            fixed_float [2]float32
            counted_float [..2]float64
            ranged [..2]int32 | min=-9, max=9
            enums [2]Grade
            caps [2]Cap
            optional ?[2]int32
        }
        `},
		{"pointer-arrays", `package reflpointers
        table Node { value int32 }
        table Root {
            nodes []*Node
            fixed_nodes [2]*Node
            bounded_nodes [..2]*Node
        }
        `},
		{"union-arrays", `package reflunions
        table Node { value int32 }
        union Choice {
            leaf Node
            number int32
        }
        table Root {
            bounded [..2]Choice
            values []Choice
        }
        `},
		{"evolution", `package reflevolution
        enum Grade {
            New | was="Old"
            Other
        }
        union Choice {
            renamed int32 | was="old"
            number int32
        }
        table Root {
            label string(8) = "hello"
            newer int32 | was="older"
            grade Grade
            choice Choice
        }
        `},
		{"text-lists", `package refltextlist
        table Root {
            words []string(8)
            optional_words []?string(8)
        }
        `},
		{"wide-timestamp", `package reflwide
        table Root {
            at timestamp
            large int128 | min=-100, max=100
            scale fixed(112,16) | min=-1000, max=1000
        }
        `},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := New()
			u, err := c.Load(writeSchemas(t, map[string]string{"Kinds.schema": tc.src}))
			if err != nil {
				t.Fatal(err)
			}
			files, err := c.Generate(u, "python", nil)
			if err != nil {
				t.Fatalf("kind hole: %v", err)
			}
			if len(files) == 0 {
				t.Fatal("files is empty: the loop below would check nothing")
			}
			for path, body := range files {
				if strings.Contains(path, "/_schema/") || !strings.HasSuffix(path, ".py") {
					continue
				}
				for _, loc := range reflectionUnsupported.FindAllIndex(body, -1) {
					line := 1 + strings.Count(string(body[:loc[0]]), "\n")
					field := "unknown"
					names := reflectionFieldName.FindAllSubmatch(body[:loc[0]], -1)
					if len(names) > 0 {
						field = string(names[len(names)-1][1])
					}
					t.Errorf("%s:%d: unsupported field or element %s", path, line, field)
				}
			}
		})
	}
}

func TestPythonReflectionDriverNegativeControl(t *testing.T) {
	t.Parallel()
	c := New()
	u, err := c.Load(writeSchemas(t, map[string]string{"Root.schema": "package control\ntable Root { count int32 }\n"}))
	if err != nil {
		t.Fatal(err)
	}
	files, err := c.Generate(u, "python", nil)
	if err != nil {
		t.Fatal(err)
	}
	path := "control/Root.py"
	src := string(files[path])
	if reflectionUnsupported.MatchString(src) {
		t.Fatal("clean descriptor was already unsupported")
	}
	if !strings.Contains(src, `codec="int"`) {
		t.Fatal("control anchor absent")
	}
	broken := strings.Replace(src, `codec="int"`, `codec="unsupported"`, 1)
	if !reflectionUnsupported.MatchString(broken) {
		t.Fatal("unsupported descriptor escaped the driver hole check")
	}
}

func TestPythonReflectionDriverPacketUnionNegativeControl(t *testing.T) {
	t.Parallel()
	c := New()
	u, err := c.Load(writeSchemas(t, map[string]string{"Only.schema": `package only
type Child { value int32 }
union Pick { child Child }
`}))
	if err != nil {
		t.Fatal(err)
	}
	files, err := c.Generate(u, "python", nil)
	if err != nil {
		t.Fatal(err)
	}
	path := "only/_schema/unit_view.py"
	src := string(files[path])
	anchor := `ViewVariant(1, "child", 0, "Child", _descriptors["Child"], None,`
	if !strings.Contains(src, anchor) {
		t.Fatal("packet union payload control anchor absent")
	}
	files[path] = []byte(strings.Replace(src, anchor, `ViewVariant(1, "child", 0, "Child", None, None,`, 1))
	dir := t.TempDir()
	for name, data := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	probe := pyrun.ImportPrelude + `import only
arm = only.UnitView().unions[0].variants[1]
assert arm.payload is not None, 'packet union payload missing'
`
	for _, bin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		_, stderr, err := pyrun.Try(t, bin, dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
		if err == nil || !strings.Contains(stderr, "packet union payload missing") {
			t.Fatalf("payload sabotage escaped: %v\n%s", err, stderr)
		}
	}
}
