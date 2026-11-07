package darttable

// SPEC-TABLES §7: a cook library is a self-contained Dart library that names
// every value class it reads or writes. A class it names must be declared in
// the library itself or in a library it imports from the same generated tree;
// a value class that lives only in the variable form's <Base>Wire.dart is not
// in a cook library's reach, so a cook that names it does not compile. This
// pins that boundary over the generated text: every capitalized type a cook
// library uses in a declaration or call is declared in it or in a library it
// imports from the same generated tree.

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/dart"
)

func TestCookLibrariesDefineEveryClassTheyName(t *testing.T) {
	t.Parallel()

	// dart:core is implicitly imported; these are the core and typed-data
	// classes the generated cook surface names without a tree import.
	core := map[string]bool{
		"ByteData": true, "Uint8List": true, "Endian": true,
		"Float32List": true, "Uint32List": true, "Float64List": true,
		"Uint64List": true, "String": true, "int": true, "bool": true,
		"double": true, "List": true, "Iterable": true, "Map": true,
		"Object": true, "Function": true, "Record": true, "void": true,
		"BigInt": true,
		"Error":  true, "StateError": true, "RangeError": true, "num": true,
	}

	for _, c := range []struct {
		name, dir string
	}{
		{"lists", "../../../tables/lists"},
		{"pointers", "../../../tables/pointers"},
		{"examples", "../../../tables/examples"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			schemas, err := filepath.Glob(filepath.Join(c.dir, "*.schema"))
			if err != nil || len(schemas) == 0 {
				t.Fatalf("no schema files under %s: %v", c.dir, err)
			}
			u := loadUnit(t, schemas...)
			files, err := Generate(u)
			if err != nil {
				t.Fatalf("Generate %s: %v", c.name, err)
			}
			pkt, err := dart.Generate(u)
			if err != nil {
				t.Fatalf("dart.Generate %s: %v", c.name, err)
			}
			for name, data := range pkt {
				if _, ok := files[name]; !ok {
					files[name] = data
				}
			}

			declared := map[string]map[string]bool{}
			for name, data := range files {
				declared[name] = declaredClasses(string(data))
			}

			for name, data := range files {
				if !strings.HasSuffix(name, "Cook.dart") {
					continue
				}
				visible := map[string]bool{}
				for cls := range declared[name] {
					visible[cls] = true
				}
				for cls := range core {
					visible[cls] = true
				}
				// a generic method's own type parameter (`sorted<T>(`) is
				// declared where it is used
				for _, m := range typeParamRe.FindAllStringSubmatch(string(data), -1) {
					visible[m[1]] = true
				}
				// relative imports bring every class they declare, restricted by
				// a `show` list when one is present.
				for imp, show := range importsOf(string(data)) {
					if strings.HasPrefix(imp, "dart:") || strings.HasPrefix(imp, "package:") {
						for _, cls := range show {
							visible[cls] = true
						}
						continue
					}
					file := filepath.Base(imp)
					endpoints := declared[file]
					if endpoints == nil {
						continue
					}
					if len(show) == 0 {
						for cls := range endpoints {
							visible[cls] = true
						}
						continue
					}
					for _, cls := range show {
						if endpoints[cls] {
							visible[cls] = true
						}
					}
				}
				for _, id := range capitalizedIdentifiers(string(data)) {
					if !visible[id] {
						t.Errorf("%s uses the type %s, which is neither declared in it nor imported from the generated tree", name, id)
					}
				}
			}
		})
	}
}

// classRe is a library-scope TYPE declaration: a class, or an enum such as the
// accelerators' TableRefuseReason (docs/SPEC-TABLES.md §7, §19.2).
// an extension type (the packet emitter's enums and union tags) declares a
// type exactly as a class does.
var classRe = regexp.MustCompile(`\b(?:class|enum|extension type(?: const)?)[ \t]+([A-Za-z_][A-Za-z0-9_]*)`)

func declaredClasses(src string) map[string]bool {
	out := map[string]bool{}
	for _, m := range classRe.FindAllStringSubmatch(src, -1) {
		out[m[1]] = true
	}
	return out
}

var identRe = regexp.MustCompile(`\b[A-Z][A-Za-z0-9_]*`)

// capitalizedIdentifiers names the whole capitalized tokens of a Dart library
// with comments and string literals removed: the class names a cook library
// could name. CamelCase method members (sublistView, getInt64) are not whole
// tokens, so they are not named as types.
// typeParamRe matches a generic method's type parameter declaration.
var typeParamRe = regexp.MustCompile(`\b[a-z]\w*<([A-Z])>\(`)

func capitalizedIdentifiers(src string) []string {
	var out []string
	seen := map[string]bool{}
	src = stripComments(src)
	src = stripStrings(src)
	for _, id := range identRe.FindAllString(src, -1) {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

func stripComments(src string) string {
	var b strings.Builder
	inBlock := false
	for i := 0; i < len(src); i++ {
		switch {
		case inBlock:
			if i+1 < len(src) && src[i] == '*' && src[i+1] == '/' {
				inBlock = false
				i++
			}
		case src[i] == '/' && i+1 < len(src) && src[i+1] == '/':
			for i < len(src) && src[i] != '\n' {
				i++
			}
			i--
		case src[i] == '/' && i+1 < len(src) && src[i+1] == '*':
			inBlock = true
			i++
		default:
			b.WriteByte(src[i])
		}
	}
	return b.String()
}

func stripStrings(src string) string {
	var b strings.Builder
	quote := byte(0)
	for i := 0; i < len(src); i++ {
		c := src[i]
		if quote != 0 {
			if c == '\\' && i+1 < len(src) {
				i++
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

func importsOf(src string) map[string][]string {
	out := map[string][]string{}
	re := regexp.MustCompile(`import '([^']*)'`)
	for _, loc := range re.FindAllStringIndex(src, -1) {
		path := src[loc[0]+8 : loc[1]-1]
		semi := strings.IndexByte(src[loc[1]:], ';')
		if semi < 0 {
			continue
		}
		tail := src[loc[1] : loc[1]+semi]
		if _, after, ok := strings.Cut(tail, "show"); ok {
			var show []string
			for s := range strings.SplitSeq(after, ",") {
				if s = strings.TrimSpace(s); s != "" {
					show = append(show, s)
				}
			}
			out[path] = show
		} else {
			out[path] = nil
		}
	}
	return out
}
