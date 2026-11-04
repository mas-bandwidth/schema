package python

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// HelperPrefix starts every name the generated code invents for itself. A
// schema name that starts with it is refused, so no schema name collides with
// a helper.
const HelperPrefix = "_s_"

// generatedMembers are the names the generated classes define beside their
// fields (docs/PORT-PYTHON.md, ERRORS and THE VALUE MAPPING). A field spelled
// like one would replace the member, so it is refused.
var generatedMembers = wordSet(`read write load save descriptor MAX_BITS MAX_BYTES`)

// tagMembers are the names an enum, a union tag or a flags class defines
// beside its members: a member spelled like one is refused.
var tagMembers = wordSet(`NONE MAX COUNT`)

// packageNames are the names the package's __init__ binds beside the
// modules: a schema file spelled like one is refused.
var packageNames = wordSet(`InvalidValue PROTOCOL_ID Refused`)

// CheckNames holds every name the generated package spells to
// docs/PORT-PYTHON.md, IDENTIFIERS, and refuses the first that breaks a rule,
// by name. The reserved words every target refuses are the checker's
// (internal/check, Python's row); the keywords are held here too, so a unit
// built in memory rather than by the checker is refused the same way. The
// rest are the rules only the Python target has: the package, the module
// names, dunder and helper prefixes, builtins at module level, and the
// generated members.
func CheckNames(u *ir.Unit) error {
	if err := checkPackage(u.Package); err != nil {
		return err
	}
	folded := map[string]string{}
	bases := make([]string, 0, len(u.Files))
	for _, f := range u.Files {
		bases = append(bases, f.Base)
	}
	sort.Strings(bases)
	for _, base := range bases {
		if err := checkModule(base); err != nil {
			return err
		}
		key := strings.ToLower(base)
		if other, ok := folded[key]; ok {
			return fmt.Errorf("python: the schema files %s and %s become the modules %s.py and %s.py, which differ only in case; macOS and Windows fold case, so rename one", other, base, other, base)
		}
		folded[key] = base
	}
	for _, f := range u.Files {
		for _, d := range f.Decls {
			if err := checkDecl(f.Base, d); err != nil {
				return err
			}
		}
		for _, t := range f.Tables {
			if err := checkDeclName(f.Base, "table", t.Name); err != nil {
				return err
			}
			if err := checkFields(f.Base, t.Name, t.Fields); err != nil {
				return err
			}
		}
		for _, un := range f.TableUnions {
			if err := checkDeclName(f.Base, "union", un.Name); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkPackage(pkg string) error {
	if !isIdentifier(pkg) {
		return fmt.Errorf("python: the package %q is not a Python identifier, and the generated package is named for it", pkg)
	}
	if pythonKeywords[pkg] {
		return fmt.Errorf("python: the package %s is a Python keyword, and the generated package is named for it", pkg)
	}
	if pythonStdlibModules[pkg] {
		return fmt.Errorf("python: the package %s is a standard library module, and `import %s` would not reach the generated package; rename the package", pkg, pkg)
	}
	if strings.HasPrefix(pkg, "__") || strings.HasPrefix(pkg, HelperPrefix) {
		return fmt.Errorf("python: the package %s starts with %q, which Python or the generated code reserves", pkg, pkg[:min(len(pkg), 3)])
	}
	return nil
}

func checkModule(base string) error {
	if !isIdentifier(base) {
		return fmt.Errorf("python: the schema file %s.schema becomes the module %s.py, and %s is not a Python identifier; rename the file", base, base, base)
	}
	if pythonKeywords[base] {
		return fmt.Errorf("python: the schema file %s.schema becomes the module %s.py, and %s is a Python keyword; rename the file", base, base, base)
	}
	if base == RuntimeDir || base == "__init__" || packageNames[base] || strings.HasPrefix(base, "__") || strings.HasPrefix(base, HelperPrefix) {
		return fmt.Errorf("python: the schema file %s.schema becomes the module %s.py, a name the generated package reserves; rename the file", base, base)
	}
	return nil
}

func checkDecl(file string, d ir.Decl) error {
	switch d := d.(type) {
	case *ir.Const:
		return checkDeclName(file, "const", d.Name)
	case *ir.Enum:
		if err := checkDeclName(file, "enum", d.Name); err != nil {
			return err
		}
		for _, v := range d.Variants {
			if err := checkMemberName(file, d.Name, "variant", v); err != nil {
				return err
			}
			if tagMembers[v] {
				return fmt.Errorf("python: %s.schema: enum %s's variant %s is a name the generated class defines (NONE, MAX, COUNT); rename the variant", file, d.Name, v)
			}
		}
	case *ir.Flags:
		if err := checkDeclName(file, "flags", d.Name); err != nil {
			return err
		}
		for _, v := range d.Variants {
			if err := checkMemberName(file, d.Name, "flag", v); err != nil {
				return err
			}
			if tagMembers[v] {
				return fmt.Errorf("python: %s.schema: flags %s's member %s is a name the generated class defines (NONE, MAX, COUNT); rename the member", file, d.Name, v)
			}
		}
	case *ir.Struct:
		if err := checkDeclName(file, "type", d.Name); err != nil {
			return err
		}
		return checkFields(file, d.Name, d.Fields)
	case *ir.Union:
		if err := checkDeclName(file, "union", d.Name); err != nil {
			return err
		}
		for _, a := range d.Variants {
			if err := checkMemberName(file, d.Name, "arm", a.Name); err != nil {
				return err
			}
			if tagMembers[ir.GoExportName(a.Name)] {
				return fmt.Errorf("python: %s.schema: union %s's arm %s is a name the generated tag class defines (NONE, MAX, COUNT); rename the arm", file, d.Name, a.Name)
			}
			if a.Name == "type" || generatedMembers[a.Name] {
				return fmt.Errorf("python: %s.schema: union %s's arm %s is the name of a member the generated class defines (%s); rename the arm", file, d.Name, a.Name, memberList())
			}
		}
	}
	return nil
}

// checkDeclName holds a module-level name: no dunder or helper prefix, and no
// builtin the generated module names unqualified.
func checkDeclName(file, kind, name string) error {
	if pythonKeywords[name] {
		return fmt.Errorf("python: %s.schema: %s %s is a Python keyword; rename it", file, kind, name)
	}
	if strings.HasPrefix(name, "__") {
		return fmt.Errorf("python: %s.schema: %s %s starts with __, which Python reserves (and mangles inside a class body); rename it", file, kind, name)
	}
	if strings.HasPrefix(name, HelperPrefix) {
		return fmt.Errorf("python: %s.schema: %s %s starts with %s, the prefix of the generated code's own names; rename it", file, kind, name, HelperPrefix)
	}
	if pythonBuiltins[name] {
		return fmt.Errorf("python: %s.schema: %s %s is a Python builtin, and the generated module names builtins unqualified; rename it", file, kind, name)
	}
	return nil
}

func checkMemberName(file, owner, kind, name string) error {
	if pythonKeywords[name] {
		return fmt.Errorf("python: %s.schema: %s's %s %s is a Python keyword; rename it", file, owner, kind, name)
	}
	if strings.HasPrefix(name, "__") {
		return fmt.Errorf("python: %s.schema: %s's %s %s starts with __, which Python reserves (and mangles inside a class body); rename it", file, owner, kind, name)
	}
	if strings.HasPrefix(name, HelperPrefix) {
		return fmt.Errorf("python: %s.schema: %s's %s %s starts with %s, the prefix of the generated code's own names; rename it", file, owner, kind, name, HelperPrefix)
	}
	return nil
}

func checkFields(file, owner string, fields []*ir.Field) error {
	for _, fd := range fields {
		if err := checkMemberName(file, owner, "field", fd.Name); err != nil {
			return err
		}
		if generatedMembers[fd.Name] {
			return fmt.Errorf("python: %s.schema: %s's field %s is the name of a member the generated class defines (%s); rename the field", file, owner, fd.Name, memberList())
		}
	}
	return nil
}

func memberList() string {
	names := make([]string, 0, len(generatedMembers))
	for n := range generatedMembers {
		names = append(names, n)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// isIdentifier reports whether s is an ASCII Python identifier. Schema names
// are ASCII (SPEC: [A-Za-z_][A-Za-z0-9_]*), so the ASCII rule is the whole
// rule; a file name is checked the same way.
func isIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '_', c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}
