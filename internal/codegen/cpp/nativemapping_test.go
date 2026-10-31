// The native type mapping, pinned on the corpus that advertises it (#675,
// tutorial finding G-06, #521). examples/Types.schema maps Vec3 to the hand
// math type VecMath with cpp_native / cpp_include, and SPEC §4.2 says what
// that buys: every generated C++ header that references the mapped type
// includes the mapped header and declares the storage as ::VecMath. The
// mapping is off inside the basis type's own header, so it rides only at a
// reference from ANOTHER file of the unit. For as long as no other example
// file referenced Vec3 the corpus's generated C++ never mentioned VecMath or
// vec_math.h at all: the example advertised a feature its own output did not
// show, and no gate could tell. examples/Degenerate.schema's Vec3Sole is now
// that reference, and the header it produces is pinned by name below.
//
// An external test package: the corpus is loaded through the public compiler
// API, which itself imports this backend.
package cpp_test

import (
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const examplesDir = "../../../examples"

func loadExamples(t *testing.T, c *compiler.Compiler) *ir.Unit {
	t.Helper()
	paths, err := compiler.GatherPaths([]string{examplesDir})
	if err != nil {
		t.Fatal(err)
	}
	u, err := c.Load(paths)
	if err != nil {
		t.Fatalf("examples unit does not compile: %v", err)
	}
	return u
}

// checkSources builds a unit from in-memory sources: no file is read or
// written. It returns the checker's errors rather than failing on them, so a
// refusal can be pinned as well as an acceptance.
func checkSources(t *testing.T, sources map[string]string) (*ir.Unit, []error) {
	t.Helper()
	names := make([]string, 0, len(sources))
	for name := range sources {
		names = append(names, name)
	}
	sort.Strings(names)
	var files []check.SourceFile
	for _, name := range names {
		src := sources[name]
		f, perrs := parser.Parse(name, []byte(src))
		if len(perrs) > 0 {
			t.Fatalf("parse %s: %v", name, perrs[0])
		}
		files = append(files, check.SourceFile{
			Path: name, Name: name, Base: strings.TrimSuffix(name, ".schema"),
			Bytes: []byte(src), AST: f,
		})
	}
	return check.Unit(files)
}

// mapped is one cpp_native mapping of a unit: the basis type, the hand type
// its storage speaks, the header declaring that type, and the file base the
// basis is declared in (where the mapping is off).
type mapped struct {
	basis, native, include, declBase string
}

func mappingsOf(u *ir.Unit) []mapped {
	var out []mapped
	for name, st := range u.Structs {
		if st.CppNative == "" {
			continue
		}
		out = append(out, mapped{basis: name, native: st.CppNative, include: st.CppInclude, declBase: u.DeclFile[name]})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].basis < out[j].basis })
	return out
}

// storageOf matches a generated storage declaration of the named type: the
// type spelled at the start of a member line, optionally namespace-qualified,
// followed by a member name.
func storageOf(typ string) *regexp.Regexp {
	q := regexp.QuoteMeta(typ)
	return regexp.MustCompile(`(?m)^\s*(?:[A-Za-z_][A-Za-z0-9_]*)?::` + q + `\s+[A-Za-z_]|^\s*` + q + `\s+[A-Za-z_][A-Za-z0-9_]*\s*(?:\[|;|=|\{)`)
}

// TestCppNativeHeaderIsIncludedWhenAMappingIsLive pins SPEC §4.2's promise on
// the examples unit itself: some generated C++ header outside the basis's own
// file references every mapped type, and every such header includes the
// mapped header and stores the type as ::<native>, never as the basis. Then
// it pins the rule for a mapping no other file uses, as the BASE tree states
// it (internal/check checkNativeMapping, SPEC §4.2): a unit of ONE file is
// refused by name; a multi-file unit that never references the type from
// another file includes nothing and names the hand type nowhere.
func TestCppNativeHeaderIsIncludedWhenAMappingIsLive(t *testing.T) {
	t.Parallel()

	t.Run("examples", func(t *testing.T) {
		t.Parallel()
		c := compiler.New()
		u := loadExamples(t, c)
		maps := mappingsOf(u)
		if len(maps) == 0 {
			t.Fatal("examples declares no cpp_native mapping: Types.schema's Vec3 / VecMath pair is the corpus's only exhibit of SPEC §4.2")
		}
		files, err := c.Generate(u, "cpp", nil)
		if err != nil {
			t.Fatal(err)
		}
		names := make([]string, 0, len(files))
		for name := range files {
			names = append(names, name)
		}
		sort.Strings(names)

		// The corpus's one cross-file reference, pinned by name (#675):
		// examples/Degenerate.schema's Vec3Sole { v Vec3 }. The resulting
		// Degenerate.h includes Types.h and then the mapped header, and
		// stores the field as ::VecMath; DegenerateWire.h reaches it
		// through WriteVec3 / ReadVec3.
		deg := string(files["Degenerate.h"])
		if deg == "" {
			t.Fatal("the C++ backend emitted no Degenerate.h for the examples unit")
		}
		typesAt := strings.Index(deg, `#include "Types.h"`)
		mathAt := strings.Index(deg, `#include "vec_math.h"`)
		if typesAt < 0 || mathAt < 0 || mathAt < typesAt {
			t.Errorf("Degenerate.h must include \"Types.h\" and then \"vec_math.h\" (Vec3Sole references Types.schema's mapped Vec3): Types.h at %d, vec_math.h at %d", typesAt, mathAt)
		}
		if !regexp.MustCompile(`(?m)^struct Vec3Sole\b[^{]*\{\s*::VecMath v;`).MatchString(deg) {
			t.Errorf("Degenerate.h does not declare Vec3Sole's storage as `::VecMath v;` (SPEC §4.2)")
		}
		degWire := string(files["DegenerateWire.h"])
		for _, want := range []string{`#include "TypesWire.h"`, "schema_write_object( stream, WriteVec3, ", "schema_read_object( stream, ReadVec3, "} {
			if !strings.Contains(degWire, want) {
				t.Errorf("DegenerateWire.h does not carry %q: Vec3Sole's wire reaches Vec3 through TypesWire.h", want)
			}
		}

		for _, m := range maps {
			word := regexp.MustCompile(`\b(?:` + regexp.QuoteMeta(m.basis) + `|` + regexp.QuoteMeta(m.native) + `)\b`)
			include := `#include "` + m.include + `"`
			nativeStorage := storageOf(m.native)
			basisStorage := storageOf(m.basis)
			var dataRefs []string
			for _, name := range names {
				base := strings.TrimSuffix(strings.TrimSuffix(name, ".h"), "Wire")
				if base == m.declBase || !strings.HasSuffix(name, ".h") {
					continue
				}
				src := string(files[name])
				if !word.MatchString(src) {
					continue
				}
				if strings.HasSuffix(name, "Wire.h") {
					// the wire half reaches the storage through its own data
					// header, which it includes first: that header must be a
					// referencing one, checked below
					data := base + ".h"
					if !strings.Contains(src, `#include "`+data+`"`) {
						t.Errorf("%s references %s but does not include its data header %s", name, m.basis, data)
					}
					if !word.MatchString(string(files[data])) {
						t.Errorf("%s references %s but its data header %s does not: the wire half has no storage to reach", name, m.basis, data)
					}
					continue
				}
				dataRefs = append(dataRefs, name)
				if !strings.Contains(src, include) {
					t.Errorf("%s references %s but does not %s (SPEC §4.2: every header that references a mapped type includes the mapped header)", name, m.basis, include)
				}
				if !nativeStorage.MatchString(src) {
					t.Errorf("%s references %s but declares no storage as ::%s (SPEC §4.2: storage speaks the native type)", name, m.basis, m.native)
				}
				if loc := basisStorage.FindString(src); loc != "" {
					t.Errorf("%s stores the basis type outside its own file: %q (the mapping is off only inside %s.h)", name, strings.TrimSpace(loc), m.declBase)
				}
			}
			if len(dataRefs) == 0 {
				t.Errorf("no generated C++ header outside %s.h references %s, so the corpus's cpp_native = %s / cpp_include = %q mapping rides nowhere: the examples advertise a feature their own output does not show (#675). Reference %s from a second file of the unit.",
					m.declBase, m.basis, m.native, m.include, m.basis)
			}
			// the basis's own headers keep the basis name and never include
			// the hand header: it derives from them, so that include would be
			// circular
			for _, own := range []string{m.declBase + ".h", m.declBase + "Wire.h"} {
				if strings.Contains(string(files[own]), include) {
					t.Errorf("%s includes %s: the hand header derives from the basis, so the mapping must be off in the basis's own headers", own, m.include)
				}
			}
		}
	})

	t.Run("a mapping no other file references includes nothing", func(t *testing.T) {
		t.Parallel()
		u, errs := checkSources(t, map[string]string{
			"Basis.schema": "package t\n\ntype V | cpp_native = VMath, cpp_include = \"v_math.h\"\n{\n    x float64\n}\n\ntype Pair\n{\n    a V\n    b V\n}\n",
			"Other.schema": "package t\n\ntype W\n{\n    n int32\n}\n",
		})
		if len(errs) > 0 {
			t.Fatalf("a multi-file unit with an unreferenced mapping must check clean (SPEC §4.2 refuses only the ONE-file unit): %v", errs[0])
		}
		files, err := cpp.Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		for name, data := range files {
			if strings.Contains(string(data), "v_math.h") || strings.Contains(string(data), "VMath") {
				t.Errorf("%s mentions the mapping no other file references; the rule is: it includes nothing\n%s", name, data)
			}
		}
		if !strings.Contains(string(files["Basis.h"]), "V a") {
			t.Errorf("Basis.h: a same-file reference keeps the basis type\n%s", files["Basis.h"])
		}
	})

	t.Run("a one-file unit is refused by name", func(t *testing.T) {
		t.Parallel()
		_, errs := checkSources(t, map[string]string{
			"Solo.schema": "package t\n\ntype V | cpp_native = VMath, cpp_include = \"v_math.h\"\n{\n    x float64\n}\n\ntype Pair\n{\n    a V\n}\n",
		})
		if len(errs) == 0 {
			t.Fatal("a cpp_native mapping in a one-file unit checked clean: it can never ride, and SPEC §4.2 makes that a compile error naming the file")
		}
		msg := errs[0].Error()
		for _, want := range []string{"cpp_native on type V can never ride", "Solo.schema is the unit's only file"} {
			if !strings.Contains(msg, want) {
				t.Errorf("refusal does not say %q:\n%s", want, msg)
			}
		}
	})
}

// TestNativeMappingIsCppOnly pins the other half of SPEC §4.2: cpp_* keys are
// C++'s alone, and every other target ignores them. Every other built-in
// language's generated examples unit (where the mapping is live in C++)
// carries no mention of the hand type or its header.
func TestNativeMappingIsCppOnly(t *testing.T) {
	t.Parallel()
	c := compiler.New()
	u := loadExamples(t, c)
	maps := mappingsOf(u)
	if len(maps) == 0 {
		t.Fatal("examples declares no cpp_native mapping")
	}
	targets := c.Targets()
	if !slices.Contains(targets, "cpp") || len(targets) < 9 {
		t.Fatalf("want cpp and every other built-in target, got %v", targets)
	}
	for _, target := range targets {
		if target == "cpp" {
			continue
		}
		files, err := c.Generate(u, target, nil)
		if err != nil {
			t.Fatalf("%s: %v", target, err)
		}
		if len(files) == 0 {
			t.Fatalf("%s generated nothing for the examples unit", target)
		}
		for name, data := range files {
			for _, m := range maps {
				for _, word := range []string{m.native, m.include} {
					if strings.Contains(string(data), word) {
						t.Errorf("%s %s mentions %q: cpp_native / cpp_include are C++'s keys and other targets ignore them (SPEC §4.2)", target, name, word)
					}
				}
			}
		}
	}
}
