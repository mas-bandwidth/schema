package ci

import (
	"strings"
	"testing"
)

// releasePublishCommand is the one line that turns a pushed release tag into a
// GitHub release. --verify-tag is what keeps the step from ever making a tag:
// `make release` is the only thing that tags.
const releasePublishCommand = `gh release create "$VERSION" --verify-tag --title "$VERSION" --generate-notes --latest`

// releaseTagCommand is the line that makes and pushes the tag.
const releaseTagCommand = `make release RELEASE_COMMIT="$GITHUB_SHA" PROOF=build/proof/proof.json PUSH=1`

func releasePublishesErrors(content string) []string {
	var errs []string
	tag := strings.Index(content, releaseTagCommand)
	if tag < 0 {
		errs = append(errs, "release.yml does not run `"+releaseTagCommand+"`")
	}
	publish := strings.Index(content, "\n          "+releasePublishCommand+"\n")
	if publish < 0 {
		errs = append(errs, "release.yml does not run `"+releasePublishCommand+"`: a release would be a bare tag with no GitHub release")
	}
	if tag >= 0 && publish >= 0 && publish < tag {
		errs = append(errs, "release.yml publishes the GitHub release before `make release` has pushed the tag")
	}
	if !strings.Contains(content, "\n  contents: write\n") {
		errs = append(errs, "release.yml does not grant contents: write, which pushing the tag and publishing the release both need")
	}
	return errs
}

// TestReleaseWorkflowPublishesTheGitHubRelease holds the owner's rule of
// 2026-11-21: a release is a tag and a GitHub release. v3.0.0 and v3.0.1 were
// dispatched through release.yml and left as bare tags, because the workflow
// stopped at the tag.
func TestReleaseWorkflowPublishesTheGitHubRelease(t *testing.T) {
	t.Parallel()
	wf, ok := workflows(t, repoRoot(t))["release.yml"]
	if !ok {
		t.Fatal("missing .github/workflows/release.yml")
	}
	if errs := releasePublishesErrors(wf); len(errs) > 0 {
		for _, e := range errs {
			t.Error(e)
		}
		t.FailNow()
	}

	publishLine := "          " + releasePublishCommand + "\n"
	tagLine := "          " + releaseTagCommand + "\n"
	for _, tc := range []struct {
		name    string
		mutated string
		want    string
	}{
		{
			name:    "the publish step removed",
			mutated: strings.Replace(wf, publishLine, "", 1),
			want:    "a bare tag with no GitHub release",
		},
		{
			name:    "the publish step may make the tag itself",
			mutated: strings.Replace(wf, publishLine, strings.Replace(publishLine, " --verify-tag", "", 1), 1),
			want:    "a bare tag with no GitHub release",
		},
		{
			name:    "published before the tag is pushed",
			mutated: strings.Replace(strings.Replace(wf, publishLine, "", 1), tagLine, publishLine+tagLine, 1),
			want:    "before `make release` has pushed the tag",
		},
		{
			name:    "the write permission dropped",
			mutated: strings.Replace(wf, "\n  contents: write\n", "\n  contents: read\n", 1),
			want:    "does not grant contents: write",
		},
	} {
		if tc.mutated == wf {
			t.Fatalf("negative control %q did not mutate release.yml", tc.name)
		}
		errs := releasePublishesErrors(tc.mutated)
		if !strings.Contains(strings.Join(errs, "\n"), tc.want) {
			t.Errorf("NEGATIVE CONTROL FAILED: %q: got %q, want an error containing %q", tc.name, errs, tc.want)
		}
	}
}
