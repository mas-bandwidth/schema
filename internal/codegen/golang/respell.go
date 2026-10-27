// The Go spelling of top-level names. Members take Go's initialisms where the
// emitters name them (internal/goname.Member); a top-level name — a
// declaration and every name derived from one, ProbeIdMaxBits, WriteProbeId,
// the union tag constants, the unit's ProtocolId — is respelled here, once,
// over the parsed unit, so no emitter has to remember to.
package golang

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/goname"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// Respell renames every top-level identifier the files declare that spells a
// schema word with a Go initialism in it — ProbeId -> ProbeID, ProbeIdMaxBits
// -> ProbeIDMaxBits, ProtocolId -> ProtocolID — at every use and in the
// comments that name it. Only the schema's words are respelled (words, from
// SchemaWords): the generated surface's own vocabulary (ToJson, TableJson…)
// is the table runtime's documented API and keeps its spelling. keep holds
// names left alone outright (the runtime's). fields holds struct field names
// declared outside the files, so a composite-literal key that names a field is
// never mistaken for a top-level name. A file with nothing to rename is
// returned byte-identical.
func Respell(files map[string][]byte, words []string, keep, fields map[string]bool) error {
	fset := token.NewFileSet()
	parsed := map[string]*ast.File{}
	names := slices.Sorted(func(yield func(string) bool) {
		for n := range files {
			if strings.HasSuffix(n, ".go") && !yield(n) {
				return
			}
		}
	})
	declared := map[string]bool{}
	fieldNames := map[string]bool{}
	for f := range fields {
		fieldNames[f] = true
	}
	for _, n := range names {
		f, err := parser.ParseFile(fset, n, files[n], parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		parsed[n] = f
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil {
					declared[d.Name.Name] = true
				}
			case *ast.GenDecl:
				for _, s := range d.Specs {
					switch s := s.(type) {
					case *ast.TypeSpec:
						declared[s.Name.Name] = true
					case *ast.ValueSpec:
						for _, id := range s.Names {
							declared[id.Name] = true
						}
					}
				}
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if st, ok := n.(*ast.StructType); ok {
				for _, fl := range st.Fields.List {
					for _, id := range fl.Names {
						fieldNames[id.Name] = true
					}
				}
			}
			return true
		})
	}
	rename := map[string]string{}
	respelt := respellWords(words)
	for n := range declared {
		if keep[n] {
			continue
		}
		if to := respellToken(n, respelt); to != n {
			rename[n] = to
		}
	}
	if len(rename) == 0 {
		return nil
	}
	comment := renameRegexp(rename)
	for _, n := range names {
		f := parsed[n]
		changed := false
		skip := map[*ast.Ident]bool{}
		ast.Inspect(f, func(node ast.Node) bool {
			switch x := node.(type) {
			case *ast.SelectorExpr:
				skip[x.Sel] = true // a field, a method or another package's name
			case *ast.Field:
				for _, id := range x.Names {
					skip[id] = true // a struct field, a parameter or a result
				}
			case *ast.KeyValueExpr:
				if id, ok := x.Key.(*ast.Ident); ok && fieldNames[id.Name] {
					skip[id] = true
				}
			case *ast.FuncDecl:
				if x.Recv != nil {
					skip[x.Name] = true // a method
				}
			case *ast.Ident:
				if to, ok := rename[x.Name]; ok && !skip[x] {
					x.Name = to
					changed = true
				}
			}
			return true
		})
		for _, cg := range f.Comments {
			for _, c := range cg.List {
				if t := comment.ReplaceAllStringFunc(c.Text, func(w string) string { return rename[w] }); t != c.Text {
					c.Text = t
					changed = true
				}
			}
		}
		if !changed {
			continue
		}
		var out bytes.Buffer
		if err := format.Node(&out, fset, f); err != nil {
			return err
		}
		files[n] = out.Bytes()
	}
	return nil
}

// renameRegexp matches any renamed name as a whole word.
func renameRegexp(rename map[string]string) *regexp.Regexp {
	words := make([]string, 0, len(rename))
	for w := range rename {
		words = append(words, regexp.QuoteMeta(w))
	}
	// longest first, so ProbeIdMaxBits is not matched as ProbeId
	sort.Slice(words, func(i, j int) bool {
		if len(words[i]) != len(words[j]) {
			return len(words[i]) > len(words[j])
		}
		return words[i] < words[j]
	})
	return regexp.MustCompile(`\b(` + strings.Join(words, "|") + `)\b`)
}

// SchemaWords is every PascalCase word the unit's schema contributes to a
// generated Go name: declaration names, member and arm names as exported,
// variant names, and the unit's own ProtocolId.
func SchemaWords(u *ir.Unit) []string {
	words := []string{"ProtocolId"}
	member := func(fs []*ir.Field) {
		for _, f := range fs {
			words = append(words, ir.GoExportName(f.Name))
		}
	}
	for n, st := range u.Structs {
		words = append(words, n)
		member(st.Fields)
	}
	for n, st := range u.Tables {
		words = append(words, n)
		member(st.Fields)
	}
	for n := range u.Consts {
		words = append(words, n)
	}
	for n, e := range u.Enums {
		words = append(words, n)
		words = append(words, e.Variants...)
	}
	for n, f := range u.Flags {
		words = append(words, n)
		words = append(words, f.Variants...)
	}
	for _, us := range []map[string]*ir.Union{u.Unions, u.TableUnions} {
		for n, un := range us {
			words = append(words, n)
			for _, v := range un.Variants {
				words = append(words, ir.GoExportName(v.Name))
			}
		}
	}
	return words
}

// respellWords keeps the words a Go initialism changes, longest first so a
// longer word wins over a word it contains.
func respellWords(words []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, w := range words {
		if w == "" || seen[w] || goname.Initialisms(w) == w {
			continue
		}
		seen[w] = true
		out = append(out, w)
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i]) != len(out[j]) {
			return len(out[i]) > len(out[j])
		}
		return out[i] < out[j]
	})
	return out
}

// respellToken respells each schema word inside an identifier where it stands
// as whole camel-case words: it begins at the identifier's start or an
// upper-case letter, and is not followed by a lower-case one.
func respellToken(n string, words []string) string {
	b := []byte(n)
	for i := 0; i < len(b); {
		matched := false
		if i == 0 || isUpperByte(b[i]) {
			for _, w := range words {
				end := i + len(w)
				if end > len(b) || string(b[i:end]) != w {
					continue
				}
				if end < len(b) && b[end] >= 'a' && b[end] <= 'z' {
					continue
				}
				copy(b[i:end], goname.Initialisms(w))
				i = end
				matched = true
				break
			}
		}
		if !matched {
			i++
		}
	}
	return string(b)
}

func isUpperByte(c byte) bool { return c >= 'A' && c <= 'Z' }
