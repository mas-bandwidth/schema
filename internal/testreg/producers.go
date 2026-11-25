package testreg

// Producer is one row of ci-fast.yml's produce matrix: a tree a later job
// reads, built once per run by .github/actions/produce. Target is the make
// target that builds it, Paths what it makes. A producer builds inputs and
// runs no test.
type Producer struct {
	Name   string `json:"name"`
	Target string `json:"target"`
	Paths  string `json:"paths"`
}

// Producers are the trees schema-test ci builds in ci-fast.yml's produce
// job. The extract holds a job's every target: to this list before it
// reads the job as driver-only (a produce step with any other target may
// run a check).
var Producers = []Producer{
	{"conformance-leg-go", "conformance-leg-go", "bin/schema build/conformance-go build/conformance-harness-vm build/tables-counted-min-variable build/tables-generated-go build/tables-generated-go-announce build/tables-generated-go-framing build/tables-generated-go-r19 build/tables-lists-variable build/tables-r1-variable"},
	{"conformance-leg-goose", "conformance-leg-goose", "bin/schema build/conformance-goose build/conformance-harness-vm build/schema-goose"},
	{"conformance-leg-cpp", "conformance-leg-cpp", "bin/schema build/conformance-cpp build/conformance-cpp-objs build/conformance-harness-vm build/schema_test_cook build/tables-generated"},
	{"lint-c", "tables-c-clean-inputs", "bin/schema build/tables-generated-c generated/c generated/c-ludicrous generated/bench/c generated/bench/tables/c generated/bench/paired/c"},
	{"lint-cpp", "tables-cpp-clean-inputs", "bin/schema build/tables-generated generated/cpp generated/bench/cpp generated/bench/tables/cpp generated/bench/paired/cpp"},
	{"lint-js", "tables-js-clean-inputs", "bin/schema build/tables-generated-js generated/js generated/js-ludicrous generated/bench/js generated/bench/paired/js"},
	{"lint-rust", "tables-rust-clippy-inputs", "bin/schema build/tables-generated-rust build/tables-generated-rust-witnesses"},
	{"lint-lua", "tables-lua-luacheck-inputs", "bin/schema build/lua-luacheck-corpus build/lua-luacheck.rc"},
}
