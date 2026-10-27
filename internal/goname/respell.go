package goname

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// ownModule is the import prefix of the packages this compiler generates or
// owns (the shared table runtime, tables/goruntime): their identifiers are
// respelled with the unit's, so a reference into them is respelled too.
const ownModule = "github.com/mas-bandwidth/schema/"

// RespellFiles spells every identifier of the generated Go files with Go's
// initialisms (ProbeId -> ProbeID, TableJsonOptions -> TableJSONOptions,
// RootToJson -> RootToJSON), at every use and in the comments that name it.
// Every Go file the compiler emits goes through it, and so does the shared
// table runtime the compiler derives from that output, so a unit and the
// runtime it imports always agree. An identifier is left alone only where it
// names something outside this compiler's output: a selector on an imported
// package other than the compiler's own (the standard library,
// serialize.go). String literals are data and keep the schema's spelling. A
// file with nothing to respell is returned byte-identical.
func RespellFiles(files map[string][]byte) error {
	for name, src := range files {
		if !strings.HasSuffix(name, ".go") {
			continue
		}
		out, err := RespellSource(src)
		if err != nil {
			return err
		}
		files[name] = out
	}
	return nil
}

// RespellSource is RespellFiles for one file.
func RespellSource(src []byte) ([]byte, error) {
	if !needsRespell(src) {
		return src, nil
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	foreign := map[string]bool{}
	for _, imp := range f.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil || strings.HasPrefix(path, ownModule) {
			continue
		}
		name := path[strings.LastIndex(path, "/")+1:]
		if imp.Name != nil {
			name = imp.Name.Name
		}
		name = strings.TrimSuffix(name, ".go") // serialize.go is package serialize
		foreign[name] = true
	}
	skip := map[*ast.Ident]bool{}
	renamed := map[string]string{}
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.ImportSpec:
			return false
		case *ast.SelectorExpr:
			if pkg, ok := x.X.(*ast.Ident); ok && foreign[pkg.Name] {
				skip[x.Sel] = true
				skip[pkg] = true
			}
		case *ast.Ident:
			if skip[x] {
				return true
			}
			if to := Initialisms(x.Name); to != x.Name {
				renamed[x.Name] = to
				x.Name = to
			}
		}
		return true
	})
	if len(renamed) == 0 {
		return src, nil
	}
	words := make([]string, 0, len(renamed))
	for w := range renamed {
		words = append(words, regexp.QuoteMeta(w))
	}
	sort.Slice(words, func(i, j int) bool {
		if len(words[i]) != len(words[j]) {
			return len(words[i]) > len(words[j])
		}
		return words[i] < words[j]
	})
	re := regexp.MustCompile(`\b(` + strings.Join(words, "|") + `)\b`)
	for _, cg := range f.Comments {
		for _, c := range cg.List {
			c.Text = re.ReplaceAllStringFunc(c.Text, func(w string) string { return renamed[w] })
		}
	}
	var out bytes.Buffer
	if err := format.Node(&out, fset, f); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// needsRespell is a cheap pre-scan: no initialism word anywhere in the bytes
// means no identifier to respell, and the file is returned untouched.
func needsRespell(src []byte) bool {
	for i := 0; i < len(src); i++ {
		if !isUpper(src[i]) {
			continue
		}
		j := i + 1
		for j < len(src) && (isLower(src[j]) || isDigit(src[j])) {
			j++
		}
		if j > i+1 && initialisms[upper(src[i:j])] {
			return true
		}
		i = j - 1
	}
	return false
}
