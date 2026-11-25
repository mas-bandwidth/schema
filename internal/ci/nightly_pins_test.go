package ci

import "testing"

// TestNightlyPinsMatchMake holds the sibling-runtime tags schema-test's
// nightly clones to the pins make resolves. The nightly group builds every
// accepted name in every target against those runtimes (the name-buildability
// rows, tools/namebuild), so a workflow left on an older runtime keeps failing
// rows a newer pin fixed (serialize v2.1.6 and serialize.cs v2.2.1 carry the
// C++ uint8_t/memcpy and the C# Math fixes) and the nightly reads red for a
// reason that is not true.
func TestNightlyPinsMatchMake(t *testing.T) {
	t.Parallel()

	const name = "schema-test-nightly.yml"
	root := repoRoot(t)
	workflow, ok := workflows(t, root)[name]
	if !ok {
		t.Fatal(name + " is absent")
	}
	ciPins := mainRedsWorkflowPins(t, workflow)
	makePins := mainRedsEffectivePins(t, root, nil)
	if len(ciPins) == 0 {
		t.Fatal(name + " pins no sibling runtime")
	}
	owner := map[string]string{}
	for _, pin := range mainRedsSiblingPins {
		owner[pin.ciEnv] = pin.makeVar
	}
	for env, ci := range ciPins {
		makeVar, ok := owner[env]
		if !ok {
			t.Errorf("%s pins %s, which no make variable owns", name, env)
			continue
		}
		if mk := makePins[makeVar]; ci != mk {
			t.Errorf("%s %s=%s != the make files' effective %s=%s", name, env, ci, makeVar, mk)
		}
	}
}
