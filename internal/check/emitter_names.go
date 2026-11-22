package check

import (
	"sync"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// An emitter name rule is a name rule a target's emitter holds itself, run
// over the assembled unit at check time. A unit `schema check` accepts
// generates code that builds in every target (SPEC §4.6, strict wins), so a
// name one emitter would refuse at generate is refused here, for every
// target, naming the target. Today that is the package and the type names.
// The rules stay in their emitters, which also
// hold a unit built in memory rather than by the checker; the compiler
// registers them (compiler/emitter_names.go), because the emitters' own
// tests import this package and it cannot import them.
type emitterNameRule struct {
	target string
	check  func(*ir.Unit) error
}

var (
	emitterNameRulesMu sync.Mutex
	emitterNameRules   []emitterNameRule
)

// RegisterEmitterNameRule adds target's name rule to every check.Unit. The
// compiler registers each emitter's rule once, at init.
func RegisterEmitterNameRule(target string, rule func(*ir.Unit) error) {
	emitterNameRulesMu.Lock()
	defer emitterNameRulesMu.Unlock()
	emitterNameRules = append(emitterNameRules, emitterNameRule{target, rule})
}

// checkEmitterNames refuses a unit some target's emitter refuses by name.
func (c *checker) checkEmitterNames() {
	if len(c.files) == 0 {
		return
	}
	emitterNameRulesMu.Lock()
	rules := append([]emitterNameRule(nil), emitterNameRules...)
	emitterNameRulesMu.Unlock()
	pos := c.files[0].AST.PkgPos
	for _, r := range rules {
		if err := r.check(c.unit); err != nil {
			c.errf(pos, "the %s target cannot express a name in this unit, and a unit schema check accepts builds in every target, so it is refused for every target: %v (SPEC §4.6)", r.target, err)
		}
	}
}

// checkDerivedFileNames refuses a basename the C and C++ emitters derive from
// another file of the unit: they write <Base>Wire.h as Base's wire header, so
// Other.schema beside OtherWire.schema writes one header twice.
func (c *checker) checkDerivedFileNames() {
	bases := map[string]bool{}
	for _, f := range c.files {
		bases[f.Base] = true
	}
	for _, f := range c.files {
		if bases[f.Base+"Wire"] {
			c.errf(f.AST.PkgPos, "schema files %s.schema and %sWire.schema collide: the C and C++ emitters write %sWire.h as %s's wire header, so neither C nor C++ can express the pair; rename one (SPEC §4.6)", f.Base, f.Base, f.Base, f.Base)
		}
	}
}
