package testreg

// SetupActions are the repository's own actions (.github/actions/<name>) a
// test-running workflow may use: each puts in place what tests need, and
// none runs a test. schema-test ci holds a generated workflow's local actions
// to this list, and the extract does not count a job whose only work is
// these and the driver as a test. Each action is pinned in
// test/registry/ci-pins.tsv, so a change to one is a reviewed change.
var SetupActions = map[string]string{
	"merge-main":        "merges current main into a lane pull request's checkout",
	"schema-test-setup": "the sibling runtimes, the runner's SDK and the row's interpreter pins",
	"fixed-corpus":      "builds and shares the fixed corpus, held to the checkout's source",
	"produce":           "builds and shares a producer's tree (a leg the env plants run), held to the checkout's source",
	"main-tree":         "exports main's tree and ledger for schema-test extract",
	"dist-pins":         "the lua, luau, goose and lobster pins",
	"python":            "the python pins",
	"ruby":              "the ruby pins",
	"moo":               "the moo pin",
}
