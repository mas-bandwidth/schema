package compiler

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// The Unity table harness, exported to this directory's external tests: the
// fixed-form proof (unityfixedform_test.go) imports internal/fixedproof, which
// imports this package, so it lives in package compiler_test and reaches the
// harness through these.

// UnitFromSourceForTest is unitFromSource.
func UnitFromSourceForTest(t *testing.T, src string) *ir.Unit { return unitFromSource(t, src) }

// UnityGenerateForTest is unityGenerate.
func UnityGenerateForTest(t *testing.T, src string) map[string][]byte { return unityGenerate(t, src) }

// FindDotnetForTest is findDotnet.
func FindDotnetForTest() string { return findDotnet() }

// UnityHarnessForTest is the Unity table harness: a scratch directory, the
// dotnet it builds with and its environment, and the unit build.
type UnityHarnessForTest struct{ h *unityTableHarness }

// NewUnityHarnessForTest is newUnityTableHarness.
func NewUnityHarnessForTest(t *testing.T, dotnet string) UnityHarnessForTest {
	return UnityHarnessForTest{newUnityTableHarness(t, dotnet)}
}

func (u UnityHarnessForTest) Tmp() string    { return u.h.tmp }
func (u UnityHarnessForTest) Root() string   { return u.h.root }
func (u UnityHarnessForTest) Dotnet() string { return u.h.dotnet }
func (u UnityHarnessForTest) Env() []string  { return u.h.env }

// BuildUnit compiles one generated unit in the Unity dialect.
func (u UnityHarnessForTest) BuildUnit(generated, out, artifacts string) error {
	return u.h.buildUnit(generated, out, artifacts)
}

// WriteFilesForTest is writeFiles.
func WriteFilesForTest(t *testing.T, dir string, files map[string][]byte) { writeFiles(t, dir, files) }
