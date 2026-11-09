package ci

import (
	"strings"
	"testing"
)

// setupJavaPin is the actions/setup-java commit every workflow must pin the
// JDK step to, with its version comment, kept current by the weekly dependabot
// github-actions pass. v6.2.0 (77dd5b2) is the current pin (dependabot #2818).
// v6.0.1 (de7274f) fixed the failing temurin 17 job (actions/setup-java#1259)
// and the macOS GPG socket overflow on long runner paths (#1266); v6.0.0
// (dd06d9c) predates both.
const setupJavaPin = "77dd5b2ce225ea11469928ac317850245a74ae78" // v6.2.0

// TestSetupJavaIsPinnedToV601 requires every actions/setup-java `uses:` in the
// workflow files to name the current pin, so a bump is a red-then-green act
// with a witness rather than a silent re-pointing of six steps.
//
// ci-fast.yml and ci-full.yml are EXEMPT in this tree: the split that created
// them (the ci.yml → ci-fast/ci-full cut) belongs to a different stream's
// files. Their setup-java steps already name v6.2.0 (77dd5b2). Every other
// workflow — certify.yml, leg.yml, nightly-conformance.yml, and
// schema-java-runs-hostile-corpus.yml — is held to the pin.
func TestSetupJavaIsPinnedToV601(t *testing.T) {
	exempt := map[string]bool{"ci-fast.yml": true, "ci-full.yml": true}
	for name, data := range workflows(t, repoRoot(t)) {
		if exempt[name] {
			continue
		}
		for _, l := range lines(name, data) {
			if !strings.HasPrefix(l.text, "uses:") {
				continue
			}
			action := strings.TrimSpace(strings.TrimPrefix(l.text, "uses:"))
			if !strings.HasPrefix(action, "actions/setup-java@") {
				continue
			}
			at := strings.LastIndex(action, "@")
			if got := action[at+1:]; got != setupJavaPin {
				t.Errorf("%s: %s is not the current setup-java pin %s (v6.2.0) — bump it so every JDK step runs the temurin 17 and macOS GPG fixes", l.where, action, setupJavaPin)
			}
		}
	}
}
