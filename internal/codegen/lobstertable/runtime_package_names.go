package lobstertable

import (
	"fmt"
	"maps"
	"sort"
	"strings"
	"sync"
	"text/scanner"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
)

type bindingToken struct {
	text  string
	ident bool
}

// Scan bindings, not arbitrary words: comments, literals and class field
// names do not reserve package names. Lobster resolves a package before a
// same-named local in member expressions, so even nested locals matter.
func runtimeBindings(source string) map[string]bool {
	var scan scanner.Scanner
	scan.Init(strings.NewReader(source))
	scan.Mode = scanner.ScanIdents | scanner.ScanStrings | scanner.ScanRawStrings | scanner.ScanChars | scanner.ScanComments | scanner.SkipComments
	// The runtime is Lobster, not Go; non-Go literal escapes are immaterial
	// to this binding scan. Quoted tokens are never interpreted as code.
	scan.Error = func(*scanner.Scanner, string) {}
	var tokens []bindingToken
	for kind := scan.Scan(); kind != scanner.EOF; kind = scan.Scan() {
		tokens = append(tokens, bindingToken{scan.TokenText(), kind == scanner.Ident})
	}
	names := map[string]bool{}
	add := func(t bindingToken) {
		if t.ident && t.text != "_" {
			names[t.text] = true
		}
	}
	for i, t := range tokens {
		if t.text == "let" || t.text == "var" {
			for j := i + 1; j < len(tokens) && tokens[j].ident; j += 2 {
				add(tokens[j])
				if j+1 >= len(tokens) || tokens[j+1].text != "," {
					break
				}
			}
		}
		if t.text == "def" {
			j := i + 1
			for j < len(tokens) && tokens[j].text != "(" {
				j++
			}
			depth := 0
			for ; j < len(tokens); j++ {
				if tokens[j].text == "(" {
					depth++
				}
				if tokens[j].text == ")" {
					depth--
					if depth == 0 {
						break
					}
				}
				if tokens[j].ident && ((j+1 < len(tokens) && tokens[j+1].text == ":") || (depth == 1 && j > 0 && (tokens[j-1].text == "(" || tokens[j-1].text == ","))) {
					add(tokens[j])
				}
			}
		}
		// A callback binds names after its closing argument list: for(xs) x,i:
		if t.text == ")" {
			var pending []bindingToken
			j := i + 1
			for j < len(tokens) && tokens[j].ident {
				pending = append(pending, tokens[j])
				j++
				if j >= len(tokens) || tokens[j].text != "," {
					break
				}
				j++
			}
			if j < len(tokens) && tokens[j].text == ":" {
				for _, name := range pending {
					add(name)
				}
			}
		}
	}
	return names
}

var runtimePackageBindings = sync.OnceValue(func() map[string]string {
	files := Runtime()
	maps.Copy(files, lobster.Runtime())
	var paths []string
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	out := map[string]string{}
	for _, path := range paths {
		for name := range runtimeBindings(string(files[path])) {
			if out[name] == "" {
				out[name] = path
			}
		}
	}
	return out
})

func checkRuntimePackageName(name string) error {
	if path, ok := runtimePackageBindings()[name]; ok {
		return fmt.Errorf("lobster: package %s collides with a runtime binding in %s; rename the package because Lobster resolves package names before local member access", name, path)
	}
	return nil
}
