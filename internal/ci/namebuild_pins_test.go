package ci

import "testing"

// TestNameBuildabilityPinsMatchMake holds the sibling-runtime tags the
// name-buildability nightly clones to the pins make resolves. The nightly
// builds every accepted name in every target against those runtimes, so a
// workflow left on an older runtime keeps failing rows a newer pin fixed
// (serialize v2.1.6 and serialize.cs v2.2.1 carry the C++ uint8_t/memcpy and
// the C# Math fixes) and the nightly reads red for a reason that is not true.
func TestNameBuildabilityPinsMatchMake(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	workflow, ok := workflows(t, root)["schema-name-buildability.yml"]
	if !ok {
		t.Fatal("schema-name-buildability.yml is absent")
	}
	ciPins := mainRedsWorkflowPins(t, workflow)
	makePins := mainRedsEffectivePins(t, root, nil)
	if len(ciPins) == 0 {
		t.Fatal("schema-name-buildability.yml pins no sibling runtime")
	}
	owner := map[string]string{}
	for _, pin := range mainRedsSiblingPins {
		owner[pin.ciEnv] = pin.makeVar
	}
	for name, ci := range ciPins {
		makeVar, ok := owner[name]
		if !ok {
			t.Errorf("schema-name-buildability.yml pins %s, which no make variable owns", name)
			continue
		}
		if mk := makePins[makeVar]; ci != mk {
			t.Errorf("schema-name-buildability.yml %s=%s != the make files' effective %s=%s", name, ci, makeVar, mk)
		}
	}
}
