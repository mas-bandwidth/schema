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

// certifyJobTimeouts maps every top-level job name in certify.yml to its
// timeout-minutes value; a job with no timeout is absent.
func certifyJobTimeouts(data string) map[string]time.Duration {
	out := map[string]time.Duration{}
	var cur string
	for raw := range strings.SplitSeq(data, "\n") {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if jobKeyRe.MatchString(raw) {
			cur = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(raw), ":"))
			continue
		}
		if val, ok := strings.CutPrefix(trimmed, "timeout-minutes:"); ok {
			n, err := strconv.Atoi(strings.TrimSpace(val))
			if err != nil {
				continue
			}
			out[cur] = time.Duration(n) * time.Minute
		}
	}
	return out
}

// TestGoReleaseSoakFitsItsCertifyJob keeps the soak the Go release gate runs
// inside the certification cap, so the release-gates job finishes instead of
// being killed at its timeout. certify.yml's own note (2026-09-18) is explicit
// that the cap is NOT raised to fit a growing suite ("a cap raised to fit a
// growing suite stops meaning anything"); the soak must therefore shrink to fit.
//
// Two clauses:
//   - no timeout-minutes in certify.yml may exceed the 60-minute certification cap;
//   - the job that runs tables-go-release (release-gates) must allow the soak
//     plus a stated 15-minute pre-soak allowance without reaching its timeout.
func TestGoReleaseSoakFitsItsCertifyJob(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	cf, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "certify.yml"))
	if err != nil {
		t.Fatalf("read certify.yml: %v", err)
	}
	body := string(cf)

	// Clause 1 — the cap. Every timeout-minutes must sit at or under 60; the
	// cap is the certification contract and is never raised for a growing suite.
	reserved := 0
	for _, l := range lines("certify.yml", body) {
		if !strings.HasPrefix(l.text, "timeout-minutes:") {
			continue
		}
		val := strings.TrimSpace(strings.TrimPrefix(l.text, "timeout-minutes:"))
		n, err := strconv.Atoi(val)
		if err != nil {
			t.Errorf("%s: timeout-minutes: %q is not an integer", l.where, val)
			continue
		}
		if n > 60 {
			t.Errorf("%s: timeout-minutes %d is above the 60-minute certification cap — a cap raised to fit a growing suite stops meaning anything (certify.yml note, 2026-09-18)", l.where, n)
		}
		reserved++
	}
	if reserved == 0 {
		t.Fatal("certify.yml carries no timeout-minutes — this gate would pass over an empty set")
	}

	// Clause 2 — the soak fits. The job that runs tables-go-release must leave
	// room for the soak plus a stated 15-minute pre-soak allowance.
	soak := goReleaseSoak(t, root)
	const allowance = 15 * time.Minute

	gate, ok := certifyJobTimeouts(body)["release-gates"]
	if !ok {
		t.Fatal("certify.yml has no release-gates job — the job that runs tables-go-release is missing")
	}
	if need := soak + allowance; need >= gate {
		t.Errorf("tables-go-release passes a %v soak to TestSoak (TABLES_GO_RELEASE_SOAK_ITERATIONS as a share of TABLES_GO_SOAK_ITERATIONS' hour) plus a %v pre-soak allowance = %v, but the release-gates job allows %v — the soak does not fit and the job will be killed at the cap", soak, allowance, need, gate)
	}
}
