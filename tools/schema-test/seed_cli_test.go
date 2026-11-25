package main

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

// seedFake passes every instance and names, in each reason, the seed the
// run handed it (testreg.SeedEnv), as a nightly fuzz row does.
type seedFake struct{}

func (seedFake) Answers(t testreg.Test) bool { return t.PerLang() }

func (seedFake) Run(ctx context.Context, _ string, insts []testreg.Instance) ([]testreg.Result, error) {
	var out []testreg.Result
	for _, inst := range insts {
		out = append(out, testreg.Result{Instance: inst.Full, Verdict: testreg.Pass, Reason: "seed " + testreg.EnvFrom(ctx)[testreg.SeedEnv]})
	}
	return out, nil
}

// TestTheRunsSeedIsPrintedAndHandedToEveryAdapter: --seed N is printed
// before the run, handed to the adapter as SCHEMA_TEST_SEED, and a pass's
// reason (its seed) is printed with it; with no --seed the seed is the UTC
// date of the run's clock; a seed that is not a decimal number is a usage
// error. Not parallel: it sets the package clock, and the parallel tests
// wait until it is done.
func TestTheRunsSeedIsPrintedAndHandedToEveryAdapter(t *testing.T) {
	code, out := cli(t, seedFake{}, "run", "go.functional.roundtrip.fam.one", "--seed", "7")
	if code != 0 || !strings.Contains(out, "schema-test run: seed 7 (") || !regexp.MustCompile(`PASS go\.functional\.roundtrip\.fam\.one \([0-9.]+s\): seed 7\n`).MatchString(out) {
		t.Errorf("--seed 7: exit %d:\n%s", code, out)
	}
	was := now
	now = func() time.Time { return time.Date(2026, 3, 4, 23, 0, 0, 0, time.FixedZone("west", -5*3600)) }
	defer func() { now = was }()
	code, out = cli(t, seedFake{}, "run", "go.functional.roundtrip.fam.one")
	if code != 0 || !regexp.MustCompile(`PASS go\.functional\.roundtrip\.fam\.one \([0-9.]+s\): seed 20260305\n`).MatchString(out) {
		t.Errorf("the date's seed (the 5th in UTC): exit %d:\n%s", code, out)
	}
	// `schema-test seconds` still reads a pass whose line carries a reason
	if m := passLine.FindStringSubmatch("PASS go.nightly.fuzz.f.c (1.25s): seed 7"); m == nil || m[1] != "go.nightly.fuzz.f.c" || m[2] != "1.25" {
		t.Errorf("a pass with a reason is not read for its seconds: %q", m)
	}
	// an empty --seed is refused, never taken for the date's
	for _, bad := range []string{"x", "-3", "1.5", ""} {
		if code, out := cli(t, seedFake{}, "run", "go.functional.roundtrip.fam.one", "--seed", bad); code != 2 || !strings.Contains(out, "--seed") {
			t.Errorf("--seed %s: exit %d, want 2:\n%s", bad, code, out)
		}
	}
}
