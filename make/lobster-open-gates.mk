# make/lobster-open-gates.mk — the Lobster port's open-gate entries that are
# not wire-fuzz arms (docs/PORT-LOBSTER.md; docs/PORTING.md I3, I5, I6, I7,
# I12, I13, J2, M5, M19). Included by the Makefile's wildcard include; each
# gate and each reversed witness is a prerequisite of test-lobster below, and
# each reversed witness is also registered in make/negative-controls.json.
#
# The gates run through test/slowgate/proof where the Go test shells out to the
# pinned toolchain (it sets SCHEMA_SLOW=1, refuses a gate skip and requires a
# named --- PASS), and as a plain go test where the test is pure Go.

# M5: the key accessor of an enum-keyed array (keyed_test.go): None, a key past
# Max and a negative key refused by name with asserts compiled away; the
# controls take each end of the guard out and misplace the shift.
.PHONY: tables-lobster-keyed tables-lobster-keyed-negative-control
tables-lobster-keyed: lobster-dist-present
	sh test/slowgate/proof tables-lobster-keyed \
		'TestLobsterKeyedAccessorCoversBothEnds' \
		./internal/codegen/lobstertable -run '^TestLobsterKeyedAccessorCoversBothEnds$$' -count=1 -timeout 600s
tables-lobster-keyed-negative-control: lobster-dist-present
	sh test/slowgate/proof tables-lobster-keyed-negative-control \
		'TestLobsterKeyedNegativeControls/none_guard_removed TestLobsterKeyedNegativeControls/past_max_guard_removed TestLobsterKeyedNegativeControls/key_misplaced_by_one' \
		./internal/codegen/lobstertable -run '^TestLobsterKeyedNegativeControls$$' -count=1 -timeout 600s

# M19: the writer holds the map sort (map_sort_test.go): every map of every
# mapdemo instance reversed at every depth, saved, and held to the engine's
# save; the control takes the sort's compare out.
.PHONY: tables-lobster-maps tables-lobster-maps-sort-negative-control
tables-lobster-maps: lobster-dist-present
	sh test/slowgate/proof tables-lobster-maps \
		'TestLobsterMapSaveSortsTheEntries' \
		./internal/codegen/lobstertable -run '^TestLobsterMapSaveSortsTheEntries$$' -count=1 -timeout 600s
tables-lobster-maps-sort-negative-control: lobster-dist-present
	sh test/slowgate/proof tables-lobster-maps-sort-negative-control \
		'TestLobsterMapSaveSortNegativeControl' \
		./internal/codegen/lobstertable -run '^TestLobsterMapSaveSortNegativeControl$$' -count=1 -timeout 600s

# I5 and I7: every block image at every lead 0..64 (lead_order_test.go), and
# the order word's reversed witness over the block and cook forgery batteries.
.PHONY: tables-lobster-block-lead tables-lobster-block-lead-negative-control tables-lobster-order-negative-control
tables-lobster-block-lead: lobster-dist-present
	sh test/slowgate/proof tables-lobster-block-lead \
		'TestLobsterBlockLead TestLobsterBlockForgeries TestLobsterCookForgeries' \
		./internal/codegen/lobstertable -run '^(TestLobsterBlockLead|TestLobsterBlockForgeries|TestLobsterCookForgeries)$$' -count=1 -timeout 600s
tables-lobster-block-lead-negative-control: lobster-dist-present
	sh test/slowgate/proof tables-lobster-block-lead-negative-control \
		'TestLobsterBlockLeadNegativeControl' \
		./internal/codegen/lobstertable -run '^TestLobsterBlockLeadNegativeControl$$' -count=1 -timeout 600s
tables-lobster-order-negative-control: lobster-dist-present
	sh test/slowgate/proof tables-lobster-order-negative-control \
		'TestLobsterOrderWordNegativeControl/block TestLobsterOrderWordNegativeControl/cook' \
		./internal/codegen/lobstertable -run '^TestLobsterOrderWordNegativeControl$$' -count=1 -timeout 600s

# I6: every name the Lobster table runtime defines in namespace schema_rt is
# registered (internal/tablenames/lobster.go) and every registered name is
# defined, and a schema file named for a runtime module is refused beside a
# table and kept in a table-free unit (compiler/tableslobsternames_test.go).
# The control plants an unregistered name in each spelling the scan reads and
# a registered name nothing defines. Pure Go.
.PHONY: tables-lobster-names tables-lobster-names-negative-control
tables-lobster-names:
	go test ./compiler -run '^(TestLobsterTableRuntimeNamesAreClaimed|TestLobsterRuntimeFileNamesAreClaimed)$$' -count=1 -timeout 600s
tables-lobster-names-negative-control:
	go test ./compiler -run '^TestLobsterRuntimeNameScanGoesRed$$' -count=1 -timeout 600s

# I13: the random text differential against the Go engine
# (textdifferential_test.go); the control caps a float32 at six digits.
.PHONY: tables-lobster-json-differential tables-lobster-json-differential-negative-control
tables-lobster-json-differential: lobster-dist-present
	sh test/slowgate/proof tables-lobster-json-differential \
		'TestLobsterTextDifferential' \
		./internal/codegen/lobstertable -run '^TestLobsterTextDifferential$$' -count=1 -timeout 600s
tables-lobster-json-differential-negative-control: lobster-dist-present
	sh test/slowgate/proof tables-lobster-json-differential-negative-control \
		'TestLobsterTextDifferentialNegativeControl' \
		./internal/codegen/lobstertable -run '^TestLobsterTextDifferentialNegativeControl$$' -count=1 -timeout 600s

# J2: the runtime home. The Lobster table runtime is emitted as fixed-name
# modules (schema_tables.lobster, load.lobster, ...) beside the unit's
# <Base>Table.lobster modules, named by no schema file, so a schema file that
# sorts earlier relocates nothing: every runtime module keeps its name and its
# bytes, and the set of table modules is unchanged, across the two trees.
# test/lobster-runtime-home/check is the comparison; the gate runs it with
# bin/schema.
.PHONY: tables-lobster-runtime-home tables-lobster-runtime-home-negative-control
tables-lobster-runtime-home: bin/schema
	sh test/lobster-runtime-home/check bin/schema build/runtime-home-lobster

# Its NEGATIVE CONTROL: the Lobster emitter never had a file-order rule, so
# the control PLANTS one through `go build -overlay` — the runtime's registry
# module named by the schema file that sorts first — and requires the check red,
# on the registry module, when an earlier-sorting file joins the unit. A gate
# that has never gone red is watching nothing.
tables-lobster-runtime-home-negative-control: tables-lobster-runtime-home
	@mkdir -p build
	@sed 's|^	maps.Copy(out, Runtime())$$|	for name, data := range Runtime() { if name == RuntimeModule+".lobster" { name = u.Files[0].Base + "_tables.lobster" }; out[name] = data }; _ = maps.Keys[map[string][]byte] // SABOTAGED: the file order|' \
		internal/codegen/lobstertable/lobstertable.go > build/lobster-runtime-home-fileorder.gotext
	@grep -q SABOTAGED build/lobster-runtime-home-fileorder.gotext || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; }
	@printf '{"Replace":{"%s/internal/codegen/lobstertable/lobstertable.go":"%s/build/lobster-runtime-home-fileorder.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/lobster-runtime-home-overlay.json
	@go build -overlay=build/lobster-runtime-home-overlay.json -o build/schema-lobster-runtime-home-sabotaged ./cmd/schema
	@if sh test/lobster-runtime-home/check build/schema-lobster-runtime-home-sabotaged build/runtime-home-lobster-sabotage > build/runtime-home-lobster-sabotage.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a runtime named by the first schema file left the runtime-home gate green"; \
		cat build/runtime-home-lobster-sabotage.log; exit 1; fi
	@grep -q 'schema_tables.lobster' build/runtime-home-lobster-sabotage.log || \
		{ echo "NEGATIVE CONTROL FAILED: the gate went red, but not on the registry module"; cat build/runtime-home-lobster-sabotage.log; exit 1; }
	@echo "runtime home negative control (Lobster): a registry module named by the first schema file moves when an earlier-sorting file joins, and the gate goes red on it"

# The controls ride the leg with their gates: each is seconds, and a reversed
# witness that `make test` never ran is a witness nothing reaches.
test-lobster: tables-lobster-keyed tables-lobster-keyed-negative-control
test-lobster: tables-lobster-maps tables-lobster-maps-sort-negative-control
test-lobster: tables-lobster-block-lead tables-lobster-block-lead-negative-control tables-lobster-order-negative-control
test-lobster: tables-lobster-names tables-lobster-names-negative-control
test-lobster: tables-lobster-json-differential tables-lobster-json-differential-negative-control
test-lobster: tables-lobster-runtime-home tables-lobster-runtime-home-negative-control
