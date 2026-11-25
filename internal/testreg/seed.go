package testreg

import (
	"fmt"
	"strconv"
	"time"
)

// SeedEnv is the environment variable a run hands every adapter its seed in
// (WithEnv). A nightly fuzz row reads its random pass's seed from it, and
// never from a clock: the seed is the run's, printed in its account, and a
// red re-runs by name with that seed (schema-test run NAME --seed N). A fuzz
// adapter that finds it missing or malformed fails the row, never defaults.
const SeedEnv = "SCHEMA_TEST_SEED"

// DateSeed is the seed of a run on t's UTC date: YYYYMMDD as a number, so
// every night's fuzz rows walk new mutants and a night's seed can be read
// off its date.
func DateSeed(t time.Time) uint64 {
	y, m, d := t.UTC().Date()
	return uint64(y)*10000 + uint64(m)*100 + uint64(d)
}

// ParseSeed reads a seed as SeedEnv or --seed gives it: a decimal number,
// nothing else.
func ParseSeed(s string) (uint64, error) {
	if s == "" {
		return 0, fmt.Errorf("%s is not set: a nightly fuzz row's seed comes from its run (schema-test run sets it, --seed or the date)", SeedEnv)
	}
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s=%q is not a decimal seed", SeedEnv, s)
	}
	return n, nil
}
