package ci

import (
	"strings"
	"testing"
)

// setupJavaPin is the actions/setup-java commit every workflow must pin the
// JDK step to, with its version comment, kept current by the weekly dependabot
// github-actions pass. v6.3.0 supersedes v6.2.0, which superseded v6.0.1 (de7274f), which fixed the
// failing temurin 17 job (actions/setup-java#1259) and the macOS GPG socket
// overflow on long runner paths (#1266); v6.0.0 (dd06d9c) predates both.
const setupJavaPin = "d1374f8ac74474200d16ef8ecee22c7ae7a51880" // v6.3.0

// TestSetupJavaIsPinnedToV630 requires every actions/setup-java `uses:` in the
// workflow files to name the current pin, so a bump is a red-then-green act
// with a witness rather than a silent re-pointing of six steps.
func TestSetupJavaIsPinnedToV630(t *testing.T) {
	allWorkflows := workflows(t, repoRoot(t))
	if len(allWorkflows) == 0 {
		t.Fatal("workflows(t, repoRoot(t)) is empty: the loop below would check nothing")
	}
	for name, data := range allWorkflows {
		allLines := lines(name, data)
		if len(allLines) == 0 {
			t.Fatal("lines(name, data) is empty: the loop below would check nothing")
		}
		for _, l := range allLines {
			if !strings.HasPrefix(l.text, "uses:") {
				continue
			}
			action := strings.TrimSpace(strings.TrimPrefix(l.text, "uses:"))
			if !strings.HasPrefix(action, "actions/setup-java@") {
				continue
			}
			at := strings.LastIndex(action, "@")
			if got := action[at+1:]; got != setupJavaPin {
				t.Errorf("%s: %s is not the current setup-java pin %s (v6.3.0) — bump it so every JDK step runs the temurin 17 and macOS GPG fixes", l.where, action, setupJavaPin)
			}
		}
	}
}
