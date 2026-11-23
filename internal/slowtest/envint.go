package slowtest

import (
	"os"
	"strconv"
	"testing"
)

// EnvInt reads a test knob: def when name is unset or empty, else its value
// as an int. A value that is not one fails the test naming it: read as 0 or
// as the default, a typo would quietly change what is tested (a seed range
// of nothing, a corpus of one).
func EnvInt(t testing.TB, name string, def int) int {
	t.Helper()
	v := os.Getenv(name)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		t.Fatalf("%s=%q is not an integer: %v", name, v, err)
	}
	return n
}

// EnvSeedRange is EnvInt over SCHEMA_TABLE_DIFF_FROM and SCHEMA_TABLE_DIFF_TO,
// the inclusive seed range the random table tests run. A range that holds no
// seed fails: it would run zero cases and pass.
func EnvSeedRange(t testing.TB, from, to int) (int, int) {
	t.Helper()
	from, to = EnvInt(t, "SCHEMA_TABLE_DIFF_FROM", from), EnvInt(t, "SCHEMA_TABLE_DIFF_TO", to)
	if from > to {
		t.Fatalf("SCHEMA_TABLE_DIFF_FROM=%d is past SCHEMA_TABLE_DIFF_TO=%d: the range holds no seed, and a run of none passes", from, to)
	}
	return from, to
}
