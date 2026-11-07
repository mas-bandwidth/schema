package lobstertable

import (
	"fmt"
	"maps"
	"sort"
	"strings"
	"sync"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// runtimeBindings scans NAMESPACE-SCOPE declarations only. A schema package
// name can shadow a top-level runtime symbol, but a parameter or a local is
// scoped to its own function: `tag` is a descriptors parameter and `t` is a
// cook local, and both are ordinary package names. Comments, literals and
// class fields are not declarations and reserve nothing.
func runtimeBindings(source string) map[string]bool {
	names := map[string]bool{}
	add := func(name string) {
		if name != "" && name != "_" {
			names[name] = true
		}
	}
	for _, line := range strings.Split(source, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || line[0] == ' ' || line[0] == '\t' {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		switch fields[0] {
		case "def", "class":
			name := fields[1]
			if i := strings.IndexAny(name, "(: \t"); i >= 0 {
				name = name[:i]
			}
			add(name)
		case "let", "var":
			for _, field := range fields[1:] {
				if field == "=" {
					break
				}
				name := field
				if i := strings.IndexAny(name, ":=(,,"); i >= 0 {
					name = name[:i]
				}
				if name == "" {
					break
				}
				add(name)
			}
		}
	}
	return names
}

var runtimePackageBindings = sync.OnceValue(func() map[string]map[string]bool {
	files := Runtime()
	maps.Copy(files, lobster.Runtime())
	out := map[string]map[string]bool{}
	for path, data := range files {
		out[path] = runtimeBindings(string(data))
	}
	return out
})

func checkRuntimePackageName(u *ir.Unit) error {
	files := runtimeForUnit(u)
	maps.Copy(files, lobster.Runtime())
	var paths []string
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		if runtimePackageBindings()[path][u.Package] {
			return fmt.Errorf("lobster: package %s collides with a runtime namespace binding in %s; rename the package", u.Package, path)
		}
	}
	return nil
}
