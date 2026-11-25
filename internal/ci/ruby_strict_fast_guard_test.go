package ci

import (
	"fmt"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// THE STRICT-RUBY FAST FORM MAY NOT SHRINK. The nightly-fast job strict-ruby
// (make schema-ruby-generated-strict-gate-fast) lints the generated Ruby with
// one RuboCop process. A job that was killed at its time cap invites the quick
// fix of linting less, so the file set and the cop set are pinned here: the
// number of generated units, the number of distinct files handed to RuboCop,
// and the set of cops the generator's rubocop.yml turns off. Adding a unit or
// a file passes; removing one, turning another cop off, excluding a path or
// dropping NewCops fails by name. To lower a pin, edit this file in a change
// that says why.
const (
	rubyStrictFastMinUnits = 64
	rubyStrictFastMinFiles = 354
)

// rubyStrictFastDisabledCops is every cop rubocop.yml turns off today, 56 of
// them. A cop in the file's disabled set but not in this list is a cop newly
// turned off.
var rubyStrictFastDisabledCops = []string{
	"Layout/EmptyLineAfterGuardClause",
	"Layout/EmptyLineBetweenDefs",
	"Layout/EmptyLinesAroundAttributeAccessor",
	"Layout/EmptyLinesAroundClassBody",
	"Layout/EmptyLinesAroundMethodBody",
	"Layout/LineLength",
	"Lint/DuplicateBranch",
	"Lint/FloatComparison",
	"Lint/IdentityComparison",
	"Lint/UnderscorePrefixedVariableName",
	"Metrics",
	"Naming/AccessorMethodName",
	"Naming/BinaryOperatorParameterName",
	"Naming/ConstantName",
	"Naming/FileName",
	"Naming/MethodName",
	"Naming/MethodParameterName",
	"Naming/PredicateMethod",
	"Naming/RescuedExceptionsVariableName",
	"Naming/VariableNumber",
	"Style/BitwisePredicate",
	"Style/CaseLikeIf",
	"Style/ClassAndModuleChildren",
	"Style/Documentation",
	"Style/EmptyElse",
	"Style/EmptyMethod",
	"Style/GlobalVars",
	"Style/GuardClause",
	"Style/IdenticalConditionalBranches",
	"Style/IfUnlessModifier",
	"Style/MultilineBlockChain",
	"Style/MultipleComparison",
	"Style/MutableConstant",
	"Style/NegatedIf",
	"Style/NegatedIfElseCondition",
	"Style/Next",
	"Style/NumericLiterals",
	"Style/NumericPredicate",
	"Style/RaiseArgs",
	"Style/RedundantInitialize",
	"Style/RedundantParentheses",
	"Style/RedundantReturn",
	"Style/RedundantSelf",
	"Style/Semicolon",
	"Style/SingleLineMethods",
	"Style/SlicingWithRange",
	"Style/SpecialGlobalVars",
	"Style/StringLiterals",
	"Style/SymbolArray",
	"Style/SymbolProc",
	"Style/TrailingCommaInArguments",
	"Style/TrailingCommaInArrayLiteral",
	"Style/TrailingCommaInHashLiteral",
	"Style/WhileUntilModifier",
	"Style/WordArray",
	"Style/ZeroLengthPredicate",
}

var (
	rubocopCopHead  = regexp.MustCompile(`^([A-Z][A-Za-z]*(?:/[A-Za-z0-9]+)?):\s*$`)
	rubocopNarrowed = regexp.MustCompile(`(?m)^\s*(Exclude|Include|inherit_from|inherit_gem|inherit_mode|require):`)
)

// rubyStrictFastGuard returns an error naming why the strict-ruby fast form
// would lint less than it does today: fewer units or files than the current
// tree generates (treeUnits, treeFiles: rubyStrictFastTree), a tree smaller
// than the pins, a cop turned off that is not in the pinned list, any
// Exclude/Include/inherit key in rubocop.yml, or NewCops no longer enabled.
// The tree, not the pins, is what the fast form is held to, so a tree that
// grew past the pins still turns every dropped unit or file red.
func rubyStrictFastGuard(units, files, treeUnits, treeFiles int, rubocopYML string) error {
	var errs []string
	if units < treeUnits {
		errs = append(errs, fmt.Sprintf("the fast form covers %d generated units, the tree generates %d", units, treeUnits))
	}
	if files < treeFiles {
		errs = append(errs, fmt.Sprintf("RuboCop is handed %d distinct files, the tree holds %d", files, treeFiles))
	}
	if treeUnits < rubyStrictFastMinUnits {
		errs = append(errs, fmt.Sprintf("the tree generates %d generated units, the pin is %d", treeUnits, rubyStrictFastMinUnits))
	}
	if treeFiles < rubyStrictFastMinFiles {
		errs = append(errs, fmt.Sprintf("the tree holds %d distinct files for RuboCop, the pin is %d", treeFiles, rubyStrictFastMinFiles))
	}
	pinned := map[string]bool{}
	for _, c := range rubyStrictFastDisabledCops {
		pinned[c] = true
	}
	cop := ""
	for line := range strings.SplitSeq(rubocopYML, "\n") {
		if m := rubocopCopHead.FindStringSubmatch(line); m != nil {
			cop = m[1]
			continue
		}
		if strings.TrimSpace(line) == "Enabled: false" && cop != "" && !pinned[cop] {
			errs = append(errs, "rubocop.yml turns off "+cop+", which is not in rubyStrictFastDisabledCops")
		}
	}
	if m := rubocopNarrowed.FindStringSubmatch(rubocopYML); m != nil {
		errs = append(errs, "rubocop.yml has a "+m[1]+" key: it narrows what RuboCop checks")
	}
	if !regexp.MustCompile(`(?m)^\s+NewCops: enable\s*$`).MatchString(rubocopYML) {
		errs = append(errs, "rubocop.yml no longer has NewCops: enable")
	}
	if len(errs) > 0 {
		return fmt.Errorf("the strict-ruby fast form would lint less than it did:\n  %s", strings.Join(errs, "\n  "))
	}
	return nil
}

// rubyStrictFastTree counts what the tree's generated units hold for RuboCop,
// read from the units themselves and not from the plan: the units, and the
// distinct files once each unit's module name is set aside (the shared
// runtime counts once). The guard holds the plan to these counts.
func rubyStrictFastTree(t *testing.T, units map[string]map[string][]byte) (int, int) {
	t.Helper()
	modLine := regexp.MustCompile(`(?m)^module (\w+)`)
	distinct := map[string]bool{}
	for label, files := range units {
		pkg := ""
		for name := range files {
			if path.Dir(name) == "." && strings.HasSuffix(name, ".rb") && !strings.HasPrefix(name, "_") && (pkg == "" || name > pkg+".rb") {
				pkg = strings.TrimSuffix(name, ".rb")
			}
		}
		m := modLine.FindSubmatch(files[pkg+".rb"])
		if pkg == "" || m == nil {
			t.Fatalf("%s: no top-level <pkg>.rb naming a module", label)
		}
		for name, src := range files {
			if strings.HasSuffix(name, ".rb") {
				distinct[strings.ReplaceAll(string(src), string(m[1]), "SchemaUnit")] = true
			}
		}
	}
	return len(units), len(distinct)
}

// rubyStrictFastUnit is one generated unit written to disk for the fast form.
type rubyStrictFastUnit struct{ label, dir, pkg string }

// rubyStrictFastPlan writes every unit under allDir and returns them with the
// distinct paths, relative to allDir, to hand RuboCop.
//
// THE RUNTIME IS LINTED ONCE. Every unit carries the same Ruby runtime
// (_table/, serialize/, _fixed.rb ...) under its own module name, which is
// most of its lines. A file whose text, with the module name of its unit
// replaced, equals one already queued is the same RuboCop input, so the
// runtime is linted once and the schema files of every unit each time.
func rubyStrictFastPlan(t *testing.T, allDir string, units map[string]map[string][]byte) ([]rubyStrictFastUnit, []string) {
	t.Helper()
	var rcPaths []string
	queued := map[string]bool{}
	modLine := regexp.MustCompile(`(?m)^module (\w+)`)
	var all []rubyStrictFastUnit
	for _, label := range slices.Sorted(maps.Keys(units)) {
		sub := strings.ReplaceAll(label, "/", "__")
		dir := filepath.Join(allDir, sub)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		pkg := rubyStrictGateWriteAndPkg(t, dir, units[label])
		m := modLine.FindSubmatch(units[label][pkg+".rb"])
		if m == nil {
			t.Fatalf("%s: %s.rb names no module", label, pkg)
		}
		for _, name := range slices.Sorted(maps.Keys(units[label])) {
			if !strings.HasSuffix(name, ".rb") {
				continue
			}
			key := strings.ReplaceAll(string(units[label][name]), string(m[1]), "SchemaUnit")
			if queued[key] {
				continue
			}
			queued[key] = true
			rcPaths = append(rcPaths, path.Join(sub, name))
		}
		all = append(all, rubyStrictFastUnit{label, dir, pkg})
	}
	return all, rcPaths
}

func rubyStrictFastConfig(t *testing.T, root string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, "internal", "codegen", "ruby", "rubocop.yml"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestRubyStrictFastGuard holds the real file set and the real rubocop.yml to
// the pins. It needs no Ruby: it generates the units and counts.
func TestRubyStrictFastGuard(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	units := rubyStrictGateUnits(t, root)
	_, rcPaths := rubyStrictFastPlan(t, t.TempDir(), units)
	tu, tf := rubyStrictFastTree(t, units)
	if err := rubyStrictFastGuard(len(units), len(rcPaths), tu, tf, rubyStrictFastConfig(t, root)); err != nil {
		t.Fatal(err)
	}
	// the pinned disabled list is exactly today's: a cop turned back ON leaves
	// a stale name behind, which is how the list stays the true record
	yml := rubyStrictFastConfig(t, root)
	for _, c := range rubyStrictFastDisabledCops {
		if !regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(c) + `:\s*\n\s+Enabled: false`).MatchString(yml) {
			t.Errorf("rubyStrictFastDisabledCops names %s, which rubocop.yml no longer turns off: remove it from the pin", c)
		}
	}
}

// TestRubyStrictFastGuardWitness is the guard's reversed witness: each way of
// linting less turns it red, by name.
func TestRubyStrictFastGuardWitness(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	yml := rubyStrictFastConfig(t, root)
	units := rubyStrictGateUnits(t, root)
	_, rcPaths := rubyStrictFastPlan(t, t.TempDir(), units)
	n, f := len(units), len(rcPaths)
	// every plant is measured against the tree as it is, never the pins, so
	// a tree that grew cannot turn a plant green (fullrun ab1e1e195a: the
	// tree outgrew the pins and "a file dropped" stayed green)
	tu, tf := rubyStrictFastTree(t, units)

	if err := rubyStrictFastGuard(n, f, tu, tf, yml); err != nil {
		t.Fatalf("the guard is red on the real set, so its witnesses prove nothing: %v", err)
	}
	// a real unit left out of the plan
	short := maps.Clone(units)
	delete(short, "manifest/tblann4096")
	_, shortPaths := rubyStrictFastPlan(t, t.TempDir(), short)
	for name, c := range map[string]struct {
		units, files int
		yml          string
		want         string
	}{
		"a unit dropped":        {len(short), len(shortPaths), yml, "generated units"},
		"a file dropped":        {n, f - 1, yml, "distinct files"},
		"a cop turned off":      {n, f, yml + "\nStyle/HashAsLastArrayItem:\n  Enabled: false\n", "Style/HashAsLastArrayItem"},
		"a path excluded":       {n, f, yml + "\nStyle/Foo:\n  Exclude:\n    - '**/_fixed_data.rb'\n", "Exclude"},
		"NewCops dropped":       {n, f, strings.ReplaceAll(yml, "NewCops: enable", "NewCops: pending"), "NewCops"},
		"a config inherited":    {n, f, "inherit_from: other.yml\n" + yml, "inherit_from"},
		"the whole set emptied": {0, 0, "", "generated units"},
	} {
		err := rubyStrictFastGuard(c.units, c.files, tu, tf, c.yml)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: want the guard red naming %q, got %v", name, c.want, err)
		}
	}
	// the tree itself below the pins: generation that lost units or files
	if err := rubyStrictFastGuard(rubyStrictFastMinUnits-1, rubyStrictFastMinFiles-1, rubyStrictFastMinUnits-1, rubyStrictFastMinFiles-1, yml); err == nil || !strings.Contains(err.Error(), "the pin is") {
		t.Errorf("a tree below the pins: want the guard red naming the pin, got %v", err)
	}
	if len(shortPaths) >= f {
		t.Errorf("dropping a unit left %d files, want fewer than %d: the witness unit has no files of its own", len(shortPaths), f)
	}
}
