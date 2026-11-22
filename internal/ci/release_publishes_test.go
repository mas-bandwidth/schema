package ci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// releasePublishCommand is the one line that turns a pushed release tag into a
// GitHub release. --verify-tag is what keeps the step from ever making a tag:
// `make release` is the only thing that tags. --draft keeps it private until
// the assets job has attached and checked every archive (F-171).
const releasePublishCommand = `gh release create "$VERSION" --verify-tag --title "$VERSION" --generate-notes --draft`

// releaseTagCommand is the line that makes and pushes the tag.
const releaseTagCommand = `make release RELEASE_COMMIT="$GITHUB_SHA" PROOF=build/proof/proof.json PUSH=1`

// The assets job's lines, in the order a release must reach them (F-171): the
// upload, the built set against SHA256SUMS and the draft's asset names, every
// archive downloaded back and checksummed, and only then public and latest.
const (
	releaseUploadCommand    = `gh release upload "$VERSION" build/release-binaries/schema_"$VERSION"_* build/release-binaries/SHA256SUMS --clobber`
	releaseListedCheck      = `if [ "$built" != "$listed" ]; then`
	releaseAttachedCheck    = `if [ "$attached" != "$built" ]; then`
	releaseManifestCheck    = `if ! cmp -s build/release-binaries/SHA256SUMS "$published/SHA256SUMS"; then`
	releaseChecksumCommand  = `(cd "$published" && sha256sum --strict -c SHA256SUMS)`
	releaseGoPublicCommand  = `gh release edit "$VERSION" --draft=false --latest`
	releasePublicOnlyWhole  = "F-171"
	releaseWorkflowFixtures = "testdata/f171"
)

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

// releaseWholeErrors holds F-171: no step makes the release public or latest
// before the assets job has uploaded the archives, matched the draft's assets
// to what it built and SHA256SUMS to the archives, and checksummed every
// archive downloaded back from the draft.
func releaseWholeErrors(content string) []string {
	var errs []string
	jobs := yamlJobs(content)
	release, assets := jobs["release"], jobs["assets"]
	if release == "" || assets == "" {
		return []string{"release.yml has no release job or no assets job"}
	}
	for _, j := range []struct{ name, body string }{{"release", release}, {"assets", assets}} {
		for raw := range strings.SplitSeq(j.body, "\n") {
			line := strings.TrimSpace(raw)
			if strings.HasPrefix(line, "#") || !strings.HasPrefix(line, "gh release") {
				continue
			}
			if strings.Contains(line, "--latest") && line != releaseGoPublicCommand {
				errs = append(errs, "release.yml:"+j.name+" runs `"+line+"`: a release made public and latest before its assets are checked can be empty to a downloader")
			}
			if strings.HasPrefix(line, "gh release create") && !strings.Contains(line, "--draft") {
				errs = append(errs, "release.yml:"+j.name+" creates the release without --draft: it is public before any asset exists")
			}
		}
	}
	if strings.Contains(assets, "gh release create") {
		errs = append(errs, "release.yml:assets creates a release: only the release job's draft step does")
	}
	at := -1
	for _, step := range []string{releaseUploadCommand, releaseListedCheck, releaseAttachedCheck, releaseManifestCheck, releaseChecksumCommand, releaseGoPublicCommand} {
		i := strings.Index(assets, step)
		if i < 0 {
			errs = append(errs, "release.yml:assets does not run `"+step+"`: the release could go public without every archive attached and checked")
			continue
		}
		if i < at {
			errs = append(errs, "release.yml:assets runs `"+step+"` out of order: the release must go public last, after the upload and every check")
		}
		at = i
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

// TestReleaseGoesPublicOnlyWhole is F-171's regression (the reviewer,
// 2026-11-21): v3.0.2 to v3.0.5 were public and latest 29 to 107 s before
// their first asset, and a failed build would have left an empty release
// public. It holds the live release.yml, and replays the committed corpus:
// testdata/f171/release-public-before-assets.yml (the workflow that made
// v3.0.5) must be refused, and testdata/f171/release-draft-until-assets.yml
// (the control) accepted.
func TestReleaseGoesPublicOnlyWhole(t *testing.T) {
	t.Parallel()
	wf, ok := workflows(t, repoRoot(t))["release.yml"]
	if !ok {
		t.Fatal("missing .github/workflows/release.yml")
	}
	for _, e := range releaseWholeErrors(wf) {
		t.Error(e)
	}

	for _, c := range []struct {
		file string
		red  bool
	}{
		{"release-public-before-assets.yml", true},
		{"release-draft-until-assets.yml", false},
	} {
		data, err := os.ReadFile(filepath.Join(releaseWorkflowFixtures, c.file))
		if err != nil {
			t.Fatalf("the %s corpus lost %s: %v", releasePublicOnlyWhole, c.file, err)
		}
		errs := releaseWholeErrors(string(data))
		switch {
		case c.red && len(errs) == 0:
			t.Errorf("CORPUS CASE FAILED: %s publishes before its assets and the gate accepted it", c.file)
		case !c.red && len(errs) > 0:
			t.Errorf("CORPUS CONTROL FAILED: %s: %q", c.file, errs)
		}
	}

	for _, c := range []struct {
		name, from, to, want string
	}{
		{"published as latest by the release job", releasePublishCommand, strings.Replace(releasePublishCommand, "--draft", "--latest", 1), "before its assets are checked"},
		{"the checksum check dropped", releaseChecksumCommand, "true", "sha256sum --strict -c SHA256SUMS"},
		{"the draft's names unchecked", releaseAttachedCheck, "if false; then", `"$attached" != "$built"`},
		{"the draft's manifest trusted (F-246)", releaseManifestCheck, "if false; then", `cmp -s build/release-binaries/SHA256SUMS`},
		{"published before the upload", releaseUploadCommand, releaseGoPublicCommand + "\n          " + releaseUploadCommand, "out of order"},
	} {
		if !strings.Contains(wf, c.from) {
			t.Fatalf("control %q cannot plant: release.yml does not contain %q", c.name, c.from)
		}
		errs := strings.Join(releaseWholeErrors(strings.Replace(wf, c.from, c.to, 1)), "\n")
		if !strings.Contains(errs, c.want) {
			t.Errorf("NEGATIVE CONTROL FAILED: %q: got %q, want an error containing %q", c.name, errs, c.want)
		}
	}
}

// releaseProofDownloadErrors holds the full lane's proof download: a failed
// `gh run download` of the proof artifact, or an artifact with no
// proof.json, refuses in that step by name. At v3.0.5 the step ended
// `|| echo "...; make release will refuse"`, so the download's own failure
// passed the step and the refusal came later under another name.
func releaseProofDownloadErrors(content string) []string {
	var errs []string
	for raw := range strings.SplitSeq(content, "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "#") || !strings.Contains(line, "gh run download") {
			continue
		}
		if strings.Contains(line, "|| echo") || strings.Contains(line, "|| true") {
			errs = append(errs, "release.yml swallows a failed proof download: `"+line+"`")
		}
	}
	for _, need := range []string{
		`if ! gh run download "$run" --name proof --dir build/proof; then`,
		`if [ ! -s build/proof/proof.json ]; then`,
	} {
		if !strings.Contains(content, need) {
			errs = append(errs, "release.yml does not refuse by name when the proof download fails: missing `"+need+"`")
		}
	}
	return errs
}

// TestReleaseProofDownloadRefusesByName holds the live release.yml and
// replays the F-171 workflow corpus: the v3.0.5 workflow (its `|| echo`) is
// refused and the control accepted.
func TestReleaseProofDownloadRefusesByName(t *testing.T) {
	t.Parallel()
	wf, ok := workflows(t, repoRoot(t))["release.yml"]
	if !ok {
		t.Fatal("missing .github/workflows/release.yml")
	}
	for _, e := range releaseProofDownloadErrors(wf) {
		t.Error(e)
	}
	for _, c := range []struct {
		file string
		red  bool
	}{
		{"release-public-before-assets.yml", true},
		{"release-draft-until-assets.yml", false},
	} {
		data, err := os.ReadFile(filepath.Join(releaseWorkflowFixtures, c.file))
		if err != nil {
			t.Fatal(err)
		}
		errs := releaseProofDownloadErrors(string(data))
		if c.red != (len(errs) > 0) {
			t.Errorf("CORPUS %s: red=%v, errors %q", c.file, c.red, errs)
		}
	}
	const from = `if ! gh run download "$run" --name proof --dir build/proof; then`
	if !strings.Contains(wf, from) {
		t.Fatal("control cannot plant: the download refusal is missing")
	}
	planted := strings.Replace(wf, from, `gh run download "$run" --name proof --dir build/proof || echo "no report"; if false; then`, 1)
	if errs := strings.Join(releaseProofDownloadErrors(planted), "\n"); !strings.Contains(errs, "swallows a failed proof download") {
		t.Errorf("NEGATIVE CONTROL FAILED: a swallowed download passed: %q", errs)
	}
}
