package ci

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// goReleaseSoak is the share of an hour the tables-go-release target hands to
// TestSoak. The soak is COUNTED, not clocked: tables-go-release passes
// -soak-iterations $(TABLES_GO_RELEASE_SOAK_ITERATIONS), and
// TABLES_GO_SOAK_ITERATIONS (the Makefile) is the count stated to be about an
// hour, so the release soak's length is its count as a fraction of that hour.
// It drives the Go soak docs/SPEC-TABLES.md describes under "What allocates,
// and what never does", and tables-go-release is one of the release gates that
// SPEC-TABLES.md says hold the Go backend's file/message/cook surfaces.
func goReleaseSoak(t *testing.T, root string) time.Duration {
	t.Helper()
	gomk, err := os.ReadFile(filepath.Join(root, "make", "go.mk"))
	if err != nil {
		t.Fatalf("read make/go.mk: %v", err)
	}
	makefile, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	count := func(text, name, where string) int64 {
		def := regexp.MustCompile(`(?m)^` + name + ` \?= (\S+)$`).FindStringSubmatch(text)
		if def == nil {
			t.Fatalf("%s sets no %s default", where, name)
		}
		n, err := strconv.ParseInt(def[1], 10, 64)
		if err != nil || n <= 0 {
			t.Fatalf("%s default %q is not a positive pass count", name, def[1])
		}
		return n
	}
	release := count(string(gomk), "TABLES_GO_RELEASE_SOAK_ITERATIONS", "make/go.mk")
	hour := count(string(makefile), "TABLES_GO_SOAK_ITERATIONS", "Makefile")

	if !regexp.MustCompile(`(?m)-soak-iterations \$\(TABLES_GO_RELEASE_SOAK_ITERATIONS\)`).MatchString(string(gomk)) {
		t.Fatal("no `-soak-iterations $(TABLES_GO_RELEASE_SOAK_ITERATIONS)` in make/go.mk — the release gate does not run the soak it claims")
	}
	return time.Duration(float64(time.Hour) * float64(release) / float64(hour))
}

// releaseSoakBudget is the -timeout the tables-go-release recipe names for
// its soak's go test.
var releaseSoakBudget = regexp.MustCompile(`go test [^\n]*-timeout (\S+) -soak-iterations \$\(TABLES_GO_RELEASE_SOAK_ITERATIONS\)`)

// TestGoReleaseSoakFitsItsBudget keeps the soak the Go release gate runs
// inside the budget its go test names, so a soak that has grown reads red.
//
// The certification job that runs it used to carry a 60-minute cap, and the
// soak was sized to fit it (certify.yml's note, 2026-09-18: a cap raised to
// fit a growing suite stops meaning anything). The nightly has no cap now
// (the owner, 2026-11-16: "Nightly runs don't have a timeout... they can run
// as long as they need."), so the soak runs to the end and the budget is read
// after it: tools/pgroup lifts the recipe's -timeout in the nightly and
// records it, and tools/gotestreport names the package OVER BUDGET when its
// test binary ran past it. This test keeps that budget meaningful:
//
//   - certify.yml carries no timeout-minutes (no cap cuts the soak short);
//   - the soak plus a stated 15-minute pre-soak allowance fits the -timeout
//     the recipe names, so a green soak is inside its budget by design.
func TestGoReleaseSoakFitsItsBudget(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	cf, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "certify.yml"))
	if err != nil {
		t.Fatalf("read certify.yml: %v", err)
	}
	if _, ok := yamlJobs(string(cf))["release-gates"]; !ok {
		t.Fatal("certify.yml has no release-gates job — the job that runs tables-go-release is missing")
	}
	for _, l := range lines("certify.yml", string(cf)) {
		if strings.HasPrefix(l.text, "timeout-minutes:") {
			t.Errorf("%s: certify.yml carries timeout-minutes; a nightly run has no timeout, it runs as long as it needs", l.where)
		}
	}

	gomk, err := os.ReadFile(filepath.Join(root, "make", "go.mk"))
	if err != nil {
		t.Fatalf("read make/go.mk: %v", err)
	}
	m := releaseSoakBudget.FindStringSubmatch(string(gomk))
	if m == nil {
		t.Fatal("the tables-go-release soak's go test names no -timeout in make/go.mk — the report would hold it to go test's 10m default")
	}
	budget, err := time.ParseDuration(m[1])
	if err != nil || budget <= 0 {
		t.Fatalf("the tables-go-release soak's -timeout %q is not a positive duration", m[1])
	}

	soak := goReleaseSoak(t, root)
	const allowance = 15 * time.Minute
	if need := soak + allowance; need >= budget {
		t.Errorf("tables-go-release passes a %v soak to TestSoak (TABLES_GO_RELEASE_SOAK_ITERATIONS as a share of TABLES_GO_SOAK_ITERATIONS' hour) plus a %v pre-soak allowance = %v, but its go test names -timeout %v — the soak does not fit its budget and the nightly report will name it OVER BUDGET", soak, allowance, need, budget)
	}
}
