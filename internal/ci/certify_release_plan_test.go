package ci

import (
	"strings"
	"testing"
)

// certifyReleasePlanErrors holds certify.yml's release-gates planner to F-066
// (the reviewer, 2026-11-21): the plan is read from make's own database by
// tools/releasetargets, after the committed corpus of make spellings that
// fooled the old source scrape is replayed through the same make; it is never
// a grep or sed of the make source, and an empty plan is a refusal, never a
// skipped matrix the verdict reads as green.
func certifyReleasePlanErrors(wf string) []string {
	jobs := yamlJobs(wf)
	var errs []string
	plan, ok := jobs["release-targets"]
	if !ok {
		return []string{"certify.yml has no release-targets job"}
	}
	for _, need := range []string{
		"go run ./tools/releasetargets -fixtures tools/releasetargets/testdata/f066\n",
		"targets=$(go run ./tools/releasetargets -also ",
	} {
		if !strings.Contains(plan, need) {
			errs = append(errs, "certify.yml:release-targets does not run `"+strings.TrimSpace(need)+"`: the plan is not read from make's database, or the F-066 corpus is not replayed first")
		}
	}
	for _, scrape := range []string{"Makefile make/*.mk", "make/wire-fuzz-release.mk", "WIRE_FUZZ_RELEASE_LEGS *:="} {
		for raw := range strings.SplitSeq(plan, "\n") {
			if strings.HasPrefix(strings.TrimSpace(raw), "#") {
				continue
			}
			if strings.Contains(raw, scrape) {
				errs = append(errs, "certify.yml:release-targets reads the make SOURCE ("+scrape+"): a legal respelling of a rule or a += continuation drops jobs silently (F-066)")
			}
		}
	}
	if gates, ok := jobs["release-gates"]; !ok {
		errs = append(errs, "certify.yml has no release-gates job")
	} else if strings.Contains(gates, "outputs.targets != '[]'") {
		errs = append(errs, "certify.yml:release-gates skips on an empty plan: a plan of nothing must refuse, not skip")
	}
	if v, ok := jobs["verdict"]; ok && strings.Contains(v, `"skipped"`) {
		errs = append(errs, "certify.yml:verdict takes a skipped job as green")
	}
	return errs
}

// TestCertifyReleasePlanReadsMakesDatabase is F-066's workflow regression.
func TestCertifyReleasePlanReadsMakesDatabase(t *testing.T) {
	t.Parallel()
	wf, ok := workflows(t, repoRoot(t))["certify.yml"]
	if !ok {
		t.Fatal("missing .github/workflows/certify.yml")
	}
	for _, e := range certifyReleasePlanErrors(wf) {
		t.Error(e)
	}

	const fixtures = "          go run ./tools/releasetargets -fixtures tools/releasetargets/testdata/f066\n"
	// the planner line certify.yml ran before F-066, verbatim
	const legacy = `          targets=$( { grep -hoE '^tables-[a-z0-9]+-release:' Makefile make/*.mk | sed 's/:$//'; sed -n 's/^WIRE_FUZZ_RELEASE_LEGS *:= *//p' make/wire-fuzz-release.mk | tr ' ' '\n' | sed 's/^/tables-wire-fuzz-release-/'; printf '%s\n' tables-cs-wire-fuzz tables-cs-region-fuzz tables-js-wire-fuzz-plain tables-js-wire-fuzz-retain tables-dart-wire-fuzz; } | sort -u | jq -R . | jq -sc .)` + "\n"
	start := strings.Index(wf, "          targets=$(go run ./tools/releasetargets -also ")
	if start < 0 {
		t.Fatal("control cannot plant: the planner line is missing")
	}
	end := start + strings.Index(wf[start:], "\n") + 1
	planner := wf[start:end]
	for _, c := range []struct {
		name, from, to, want string
	}{
		{"the old source scrape back", planner, legacy, "reads the make SOURCE"},
		{"the corpus replay dropped", fixtures, "", "the F-066 corpus is not replayed first"},
		{"an empty plan skips the gates", "    needs: release-targets\n    name: release-gates", "    needs: release-targets\n    if: needs.release-targets.outputs.targets != '[]'\n    name: release-gates", "skips on an empty plan"},
		{"the verdict forgives a skip", `select(.value.result != "success")`, `select(.value.result != "success" and .value.result != "skipped")`, "takes a skipped job as green"},
	} {
		if !strings.Contains(wf, c.from) {
			t.Fatalf("control %q cannot plant: certify.yml does not contain %q", c.name, c.from)
		}
		errs := strings.Join(certifyReleasePlanErrors(strings.Replace(wf, c.from, c.to, 1)), "\n")
		if !strings.Contains(errs, c.want) {
			t.Errorf("NEGATIVE CONTROL FAILED: %q: got %q, want an error containing %q", c.name, errs, c.want)
		}
	}
}
