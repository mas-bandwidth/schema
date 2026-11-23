package compiler

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/cqualify"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE C NAMESPACE AT CHECK (SPEC §4.6). C has no namespace: the C target
// qualifies every name its files declare with the package
// (internal/codegen/cqualify) and refuses a unit where two names qualify to
// one spelling: two declarations differing only in case (kind and Kind are
// both <pkg>_kind_t), or a declaration whose own names land on the table
// runtime's (a type TableReport is <pkg>_table_report_t, which the view
// declares; a table table_message's open is the runtime's
// <pkg>_table_message_open). schema check refuses that unit for every
// target, naming C. A unit with no tables writes no table runtime, so the
// runtime's names are its own.
//
// The names are read off the C target: the control unit
// emitted in C, its declared names qualified; a qualified name that spells
// a control declaration in one of its forms (ZqTab, zq_tab, ZQ_TAB) is that
// kind's template, and one that spells none is the runtime's.

// cNamespace is the C target's control reading: the runtime's qualified
// names and, per declaration kind, the qualified names a declaration of
// that kind brings, the package written {p} or {P} and the declaration
// {n} (as written), {s} (snake) or {S} (screaming snake).
type cNamespace struct {
	Runtime   []string    `json:"runtime"`
	Templates []cTemplate `json:"templates"`
}

type cTemplate struct {
	Kind string `json:"kind"`
	Form string `json:"form"`
}

// cControlKinds are the control declarations, by the kind a unit's
// declaration is matched against.
var cControlKinds = map[string]string{
	"ZqType": "type", "ZqTab": "table", "ZqFix": "fixed table", "ZqKind": "enum",
	"ZqFlg": "flags", "ZqUni": "union", "ZqMax": "const",
}

// readCNamespace sorts the control unit's C names, unqualified to
// qualified, into the runtime's and the templates.
func readCNamespace(names map[string]string) *cNamespace {
	ns := &cNamespace{}
	runtime := map[string]bool{}
	templates := map[cTemplate]bool{}
	for _, q := range names {
		q = strings.ReplaceAll(strings.ReplaceAll(q, controlPackage, "{p}"), strings.ToUpper(controlPackage), "{P}")
		matched := false
		for ctl, kind := range cControlKinds {
			for _, form := range []struct{ spell, mark string }{
				{ctl, "{n}"}, {ir.RustSnake(ctl), "{s}"}, {ir.RustConstName(ctl), "{S}"},
			} {
				if !strings.Contains(q, form.spell) {
					continue
				}
				matched = true
				if t := strings.ReplaceAll(q, form.spell, form.mark); !strings.Contains(strings.ToLower(t), "zq") {
					templates[cTemplate{kind, t}] = true
				}
			}
		}
		if !matched && !strings.Contains(strings.ToLower(q), "zq") {
			runtime[q] = true
		}
	}
	for q := range runtime {
		ns.Runtime = append(ns.Runtime, q)
	}
	for t := range templates {
		ns.Templates = append(ns.Templates, t)
	}
	sort.Strings(ns.Runtime)
	sort.Slice(ns.Templates, func(i, j int) bool {
		if ns.Templates[i].Kind != ns.Templates[j].Kind {
			return ns.Templates[i].Kind < ns.Templates[j].Kind
		}
		return ns.Templates[i].Form < ns.Templates[j].Form
	})
	return ns
}

// cDeclKinds is every declaration of the unit by the control kind it is
// matched against.
func cDeclKinds(u *ir.Unit) map[string]string {
	out := map[string]string{}
	for n := range u.Consts {
		out[n] = "const"
	}
	for n := range u.Enums {
		out[n] = "enum"
	}
	for n := range u.Flags {
		out[n] = "flags"
	}
	for n := range u.Structs {
		out[n] = "type"
	}
	for n := range u.Unions {
		out[n] = "union"
	}
	for n := range u.TableUnions {
		out[n] = "union"
	}
	for n, st := range u.Tables {
		if st.FixedDeclared {
			out[n] = "fixed table"
		} else {
			out[n] = "table"
		}
	}
	return out
}

// cNames refuses a unit two of whose names the C target qualifies to one
// spelling.
func cNames(u *ir.Unit) error {
	ns, err := loadCNamespace()
	if err != nil {
		return err
	}
	decls := cDeclKinds(u)
	order := make([]string, 0, len(decls))
	for n := range decls {
		order = append(order, n)
	}
	sort.Strings(order)
	pkg := strings.NewReplacer("{p}", u.Package, "{P}", strings.ToUpper(u.Package))
	owner := map[string]string{}
	for _, n := range order {
		kind := decls[n]
		forms := strings.NewReplacer("{n}", n, "{s}", ir.RustSnake(n), "{S}", ir.RustConstName(n))
		for _, tm := range ns.Templates {
			if tm.Kind != kind {
				continue
			}
			q := forms.Replace(tm.Form)
			spelled := pkg.Replace(q)
			if _, hit := slices.BinarySearch(ns.Runtime, q); hit && len(u.Tables) > 0 {
				return fmt.Errorf("the C emitter qualifies %s %s's name to %s, which the C table runtime also declares; rename %s", kind, n, spelled, n)
			}
			if prev, dup := owner[spelled]; dup && prev != n {
				return fmt.Errorf("the C emitter qualifies both %s and %s to %s (C has no namespace, and the package is its only qualifier); rename one", prev, n, spelled)
			}
			owner[spelled] = n
		}
	}
	return nil
}

// emitter_names_c.json is the C target's reading of the control unit, kept
// so a check pays nothing for it; TestCNamespaceCurrent regenerates it and
// fails when the target has moved (SCHEMA_UPDATE_EMITTER_TABLES=1 rewrites).
//
//go:embed emitter_names_c.json
var cNamespaceJSON []byte

var (
	cNamespaceOnce sync.Once
	cNamespaceVal  cNamespace
	cNamespaceErr  error
)

func loadCNamespace() (*cNamespace, error) {
	cNamespaceOnce.Do(func() { cNamespaceErr = json.Unmarshal(cNamespaceJSON, &cNamespaceVal) })
	return &cNamespaceVal, cNamespaceErr
}

const (
	controlPackage = "zqpkg"
	controlTables  = "ZqCtl" // the control file that declares tables
	controlPacket  = "ZqPkt" // the control file that declares none
)

var controlUnitFiles = map[string]string{
	controlPacket + ".schema": "package " + controlPackage + "\n\nconst ZqMax = 4\n\nenum ZqKind\n{ ZqA, ZqB }\n\n" +
		"flags ZqFlg\n{ ZqF1, ZqF2 }\n\n" +
		"type ZqType\n{\n    zq_field uint8\n    kind ZqKind\n    flg ZqFlg\n}\n\n" +
		"union ZqUni\n{\n    zq_arm ZqType\n}\n",
	controlTables + ".schema": "package " + controlPackage + "\n\ntable ZqTab\n{\n    zq_value uint32\n}\n\n" +
		"fixed table ZqFix\n{\n    zq_x uint32\n}\n",
}

// controlUnit is the unit the C names are read off: every name a token that
// appears nowhere else.
func controlUnit() (*ir.Unit, error) {
	var files []check.SourceFile
	names := make([]string, 0, len(controlUnitFiles))
	for name := range controlUnitFiles {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		src := []byte(controlUnitFiles[name])
		f, perrs := parser.Parse(name, src)
		if len(perrs) > 0 {
			return nil, perrs[0]
		}
		files = append(files, check.SourceFile{Path: name, Name: name, Base: strings.TrimSuffix(name, ".schema"), Bytes: src, AST: f})
	}
	u, errs := check.Unit(files)
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return u, nil
}

// computeCNamespace reads the C target off the control unit.
func computeCNamespace() (*cNamespace, error) {
	u, err := controlUnit()
	if err != nil {
		return nil, fmt.Errorf("the control unit: %w", err)
	}
	files, err := emitC(u, nil)
	if err != nil {
		return nil, fmt.Errorf("the control unit in C: %w", err)
	}
	names, err := cqualify.Names(files, controlPackage)
	if err != nil {
		return nil, fmt.Errorf("the control unit in C: %w", err)
	}
	return readCNamespace(names), nil
}
