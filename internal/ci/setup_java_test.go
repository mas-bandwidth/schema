package ci

import (
	"strings"
	"testing"
)

// setupJavaPin is the actions/setup-java commit every workflow must pin the
// JDK step to, with its version comment, kept current by the weekly dependabot
// github-actions pass. v6.0.1 (de7274f) fixes the failing temurin 17 job
// (actions/setup-java#1259) and the macOS GPG socket overflow on long runner
// paths (#1266); v6.0.0 (dd06d9c) predates both.
const setupJavaPin = "de7274f081f381c8f8158605e0321c36c376e2e6" // v6.0.1

// TestSetupJavaIsPinnedToV601 requires every actions/setup-java `uses:` in the
// workflow files to name the current pin, so a bump is a red-then-green act
// with a witness rather than a silent re-pointing of six steps.
func TestSetupJavaIsPinnedToV601(t *testing.T) {
	for name, data := range workflows(t, repoRoot(t)) {
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
				t.Errorf("%s: %s is not the current setup-java pin %s (v6.0.1) — bump it so every JDK step runs the temurin 17 and macOS GPG fixes", l.where, action, setupJavaPin)
			}
		}
	}
}
