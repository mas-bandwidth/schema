package slowtest

import "os"

// A SLOW TEST IN CI CANNOT SKIP FOR WANT OF THE ORACLE CORPUS.
//
// The fixed-form byte oracle (build/fixedform-corpus, make
// tables-fixedform-corpus) is a prerequisite a test cannot build for itself.
// A test that runs only when SCHEMA_SLOW=1 and then finds no corpus used to
// skip, and a skip exits 0: a lane that asked for the slow half and never
// built the oracle went green over tests that compared nothing. So where the
// run is CI (the CI variable, which GitHub Actions sets on every job) and has
// asked for the slow half (SCHEMA_SLOW=1), a missing corpus is a failure, the
// same as under SCHEMA_REQUIRE_CORPUS. A laptop, or a default run, skips as
// before, with the reason the skip ledger reads.

// CorpusRequired reports whether a missing oracle corpus is a failure here:
// SCHEMA_REQUIRE_CORPUS is set (the make leg gates say so), or this is CI
// running the slow half.
func CorpusRequired() bool {
	if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
		return true
	}
	return os.Getenv("CI") != "" && os.Getenv("SCHEMA_SLOW") == "1"
}
