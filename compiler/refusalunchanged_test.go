// The refusal tests' "unchanged" claim, computed at test time (docs/SPEC-TABLES.md
// §2.13, §2.13.1): a construct Go carries first and every other target refuses
// leaves the generated output of the declarations that do not use it unchanged.
// No digest of generated output is committed, so an emitter change that touches
// every unit moves nothing here, while a change keyed on the construct still
// goes red.
package compiler

import (
	"bytes"
	"fmt"
	"go/ast"
	goparser "go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// generateFunc is the generator a refusal check drives: c.Generate in the real
// tests, a wrapper around it in the planted ones.
type generateFunc func(u *ir.Unit, lang string) (map[string][]byte, error)

// unchangedProbe is one construct's differential. Using and control are the
// same declarations under the same names, once with the construct and once
// with its nearest form that does not use it (a control may add the table its
// element names), so the two units differ by the construct alone; keep names
// the declarations that use neither.
type unchangedProbe struct {
	construct string            // the construct, for the failure line
	base      map[string]string // per target: the declarations that use neither (a whole source)
	using     string            // declarations that use the construct (no package line)
	control   string            // the same declarations without it (no package line)
	keep      []string          // the declarations of base, by name
	refusal   string            // what a refusing target's error must name
	uses      func(*ir.Unit) bool
}

// checkRefusalUnchanged is the computed check (docs/SPEC-TABLES.md §2.13,
// §2.13.1). For the carrier (go) it generates base+using and base+control and
// cuts each output into top-level Go declarations; a declaration belongs to a
// keep declaration when its name, or its method's receiver, is the keep name
// or the keep name followed by an upper-case letter (Word, WordBuilder,
// WordSaveBody). The keep declarations must be the same set in the two
// outputs and the same bytes, numeric literals masked (maskUnitNumbers): a
// byte of a non-using declaration's code that depends on the construct is a
// leak, named by construct, target and declaration. Code that is the unit's
// as a whole (runtime, registries, the build version) belongs to no keep
// declaration and may carry the construct's support. For a refusing target
// the construct's unit must be refused by name (the refusal is central, in
// Generate, before any emitter runs, so no construct-keyed byte reaches its
// output), and the base must still generate. The probe declarations sort
// after every keep declaration, so the keep declarations' ordinals do not
// move between the two units either.
func checkRefusalUnchanged(gen generateFunc, targets []string, p unchangedProbe) error {
	var errs []string
	for _, lang := range targets {
		src, ok := p.base[lang]
		if !ok {
			src = p.base["go"]
		}
		if lang != "go" {
			if _, err := genSource(gen, src, lang); err != nil {
				errs = append(errs, fmt.Sprintf("%s: target %s: a unit using no %s does not generate: %v", p.construct, lang, p.construct, err))
			}
			_, err := genSource(gen, src+"\n"+p.using, lang)
			if err == nil || !strings.Contains(err.Error(), p.refusal) || !strings.Contains(err.Error(), "target \""+lang+"\"") {
				errs = append(errs, fmt.Sprintf("%s: target %s did not refuse the construct by name: %v", p.construct, lang, err))
			}
			continue
		}
		with, err := genSource(gen, src+"\n"+p.using, lang)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: target %s: %v", p.construct, lang, err))
			continue
		}
		without, err := genSource(gen, src+"\n"+p.control, lang)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: target %s: the control unit: %v", p.construct, lang, err))
			continue
		}
		a, err := keepDecls(with, p.keep)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: target %s: %v", p.construct, lang, err))
			continue
		}
		b, err := keepDecls(without, p.keep)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: target %s: the control unit: %v", p.construct, lang, err))
			continue
		}
		if len(b) == 0 {
			errs = append(errs, fmt.Sprintf("%s: target %s: no declaration of %s found in the output", p.construct, lang, strings.Join(p.keep, ", ")))
		}
		keys := map[string]bool{}
		for k := range a {
			keys[k] = true
		}
		for k := range b {
			keys[k] = true
		}
		sorted := make([]string, 0, len(keys))
		for k := range keys {
			sorted = append(sorted, k)
		}
		sort.Strings(sorted)
		for _, k := range sorted {
			if !bytes.Equal(maskUnitNumbers(a[k]), maskUnitNumbers(b[k])) {
				errs = append(errs, fmt.Sprintf("%s: target %s: the declaration %s, which does not use the construct, changed when the unit uses it", p.construct, lang, k))
			}
		}
		if p.uses != nil {
			uw, _ := unitFromText(src + "\n" + p.using)
			uc, _ := unitFromText(src + "\n" + p.control)
			if uw, uc := p.uses(uw), p.uses(uc); !uw || uc {
				errs = append(errs, fmt.Sprintf("%s: the probe is wrong: using uses the construct %v, control uses it %v", p.construct, uw, uc))
			}
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "\n"))
	}
	return nil
}

// unitFromText is unitFromSource for a check that returns its failure.
func unitFromText(src string) (*ir.Unit, error) {
	f, perrs := parser.Parse("Probe.schema", []byte(src))
	if len(perrs) > 0 {
		return nil, fmt.Errorf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(src), AST: f,
	}})
	if len(cerrs) > 0 {
		return nil, fmt.Errorf("check: %v", cerrs[0])
	}
	return u, nil
}

func genSource(gen generateFunc, src, lang string) (map[string][]byte, error) {
	u, err := unitFromText(src)
	if err != nil {
		return nil, err
	}
	return gen(u, lang)
}

// unitNumber is a numeric literal in generated Go. A declaration's code
// carries numbers that are the unit's as a whole (a record index, an intern
// slot, the bit width of a vocabulary reference), and those move whenever the
// unit gains a declaration, so the check compares the keep declarations with
// every numeric literal masked: a construct-keyed line, call, name or branch
// in them still differs.
var unitNumber = regexp.MustCompile(`\b(?:0[xX][0-9a-fA-F]+|[0-9]+)\b`)

func maskUnitNumbers(b []byte) []byte {
	if b == nil {
		return nil
	}
	return unitNumber.ReplaceAll(b, []byte("#"))
}

// keepDecls cuts generated Go files into top-level declarations and returns
// those belonging to a keep name, keyed file:kind name, with their doc
// comments.
func keepDecls(files map[string][]byte, keep []string) (map[string][]byte, error) {
	pats := make([]*regexp.Regexp, len(keep))
	for i, k := range keep {
		pats[i] = regexp.MustCompile("^" + regexp.QuoteMeta(k) + "([A-Z]|$)")
	}
	belongs := func(name string) bool {
		for _, p := range pats {
			if p.MatchString(name) {
				return true
			}
		}
		return false
	}
	out := map[string][]byte{}
	for name, src := range files {
		if !strings.HasSuffix(name, ".go") {
			continue
		}
		fs := token.NewFileSet()
		f, err := goparser.ParseFile(fs, name, src, goparser.ParseComments)
		if err != nil {
			return nil, fmt.Errorf("generated %s does not parse: %v", name, err)
		}
		for _, d := range f.Decls {
			var keys []string
			start := d.Pos()
			switch d := d.(type) {
			case *ast.FuncDecl:
				if d.Doc != nil {
					start = d.Doc.Pos()
				}
				if d.Recv != nil && len(d.Recv.List) > 0 {
					if r := recvName(d.Recv.List[0].Type); belongs(r) {
						keys = append(keys, "method "+r+"."+d.Name.Name)
					}
				} else if belongs(d.Name.Name) {
					keys = append(keys, "func "+d.Name.Name)
				}
			case *ast.GenDecl:
				if d.Doc != nil {
					start = d.Doc.Pos()
				}
				for _, s := range d.Specs {
					switch s := s.(type) {
					case *ast.TypeSpec:
						if belongs(s.Name.Name) {
							keys = append(keys, d.Tok.String()+" "+s.Name.Name)
						}
					case *ast.ValueSpec:
						for _, n := range s.Names {
							if belongs(n.Name) {
								keys = append(keys, d.Tok.String()+" "+n.Name)
							}
						}
					}
				}
			}
			if len(keys) == 0 {
				continue
			}
			key := name + ":" + strings.Join(keys, ",")
			body := src[fs.Position(start).Offset:fs.Position(d.End()).Offset]
			if prev, dup := out[key]; dup {
				body = append(append(append([]byte(nil), prev...), '\n'), body...)
			}
			out[key] = body
		}
	}
	return out, nil
}

func recvName(e ast.Expr) string {
	for {
		switch x := e.(type) {
		case *ast.StarExpr:
			e = x.X
		case *ast.IndexExpr:
			e = x.X
		case *ast.IndexListExpr:
			e = x.X
		case *ast.Ident:
			return x.Name
		default:
			return ""
		}
	}
}

// compilerGenerate is c.Generate as a generateFunc.
func compilerGenerate(c *Compiler) generateFunc {
	return func(u *ir.Unit, lang string) (map[string][]byte, error) { return c.Generate(u, lang, nil) }
}

// unchangedTargets are the targets the "unchanged" claim was pinned on: the
// carrier and one refusing target.
var unchangedTargets = []string{"go", "cpp"}

func allUnchangedProbes() []unchangedProbe {
	return append(append([]unchangedProbe(nil), stringArraysProbes...), unboundedTextProbes...)
}

func TestRefusalUnchangedIgnoresTrivialEmitterChange(t *testing.T) {
	t.Parallel()
	c := New()
	// every generated file of every unit gains a line after its header, and
	// every function of every unit a line before it
	planted := func(u *ir.Unit, lang string) (map[string][]byte, error) {
		files, err := c.Generate(u, lang, nil)
		if err != nil {
			return nil, err
		}
		out := make(map[string][]byte, len(files))
		for name, b := range files {
			s := string(b)
			if i := strings.IndexByte(s, '\n'); i >= 0 {
				s = s[:i+1] + "// planted trivial header line\n" + s[i+1:]
			}
			out[name] = []byte(strings.ReplaceAll(s, "\nfunc ", "\n// planted trivial line\nfunc "))
		}
		return out, nil
	}
	for _, p := range allUnchangedProbes() {
		if err := checkRefusalUnchanged(planted, unchangedTargets, p); err != nil {
			t.Errorf("a trivial emitter change on every unit tripped the check:\n%v", err)
		}
	}
}

func TestRefusalUnchangedCatchesConstructKeyedLeak(t *testing.T) {
	t.Parallel()
	c := New()
	for _, p := range allUnchangedProbes() {
		p := p
		decl := regexp.MustCompile(`(?m)^((?:func|type|var|const) ` + regexp.QuoteMeta(p.keep[0]) + `\b)`)
		// only when the unit uses the construct, the first declaration that
		// does not use it gains a line
		planted := func(u *ir.Unit, lang string) (map[string][]byte, error) {
			files, err := c.Generate(u, lang, nil)
			if err != nil || !p.uses(u) {
				return files, err
			}
			out := make(map[string][]byte, len(files))
			for name, b := range files {
				out[name] = decl.ReplaceAll(b, []byte("// planted construct-keyed leak\n$1"))
			}
			return out, nil
		}
		err := checkRefusalUnchanged(planted, unchangedTargets, p)
		if err == nil {
			t.Errorf("%s: a construct-keyed leak into %s passed the check", p.construct, p.keep[0])
			continue
		}
		msg := err.Error()
		for _, want := range []string{p.construct + ":", "target go", "declaration ", " " + p.keep[0]} {
			if !strings.Contains(msg, want) {
				t.Errorf("%s: the failure does not name %q: %s", p.construct, want, msg)
			}
		}
	}
}

// pinnedDigest is a quoted 64-hex-digit literal: a sha256 committed beside the
// code whose output it pins.
var pinnedDigest = regexp.MustCompile(`"[0-9a-f]{64}"`)

func TestNoPinnedGeneratedOutputDigest(t *testing.T) {
	t.Parallel()
	var names []string
	for _, glob := range []string{"*_test.go", filepath.Join("..", "internal", "codegen", "gotable", "*_test.go")} {
		m, err := filepath.Glob(glob)
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, m...)
	}
	if len(names) == 0 {
		t.Fatal("no test files found")
	}
	sort.Strings(names)
	for _, name := range names {
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(b), "\n") {
			if pinnedDigest.MatchString(line) {
				t.Errorf("%s:%d pins a digest; compute the claim at test time (checkRefusalUnchanged): %s", name, i+1, strings.TrimSpace(line))
			}
		}
	}
}
