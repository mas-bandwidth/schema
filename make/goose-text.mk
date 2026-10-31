# make/goose-text.mk — the Goose leg's JSON TEXT FORM gates (docs/SPEC-TABLES.md
# §16; docs/PORT-GOOSE.md, THE CARD MAP: schema-goose-json-form and the text
# walk of schema-goose-text-form). The emitter is
# internal/codegen/goosetable/json_form.go, the one walk
# internal/codegen/goosetable/runtime/schema_json.goose; `make test-goose` runs
# every test named here as part of the package, and these targets are the
# gates the PORTING register cites, each beside its negative control
# (make/negative-controls.json, group goose-text). Every target installs the
# pinned compiler if it is missing (make dist-goose); none needs anything but
# Go and Goose.

# tables-goose-json-walk (PORTING M9): the walk is one runtime, byte for byte in
# every unit that carries the form; its control sabotages the GENERATED read
# accessor's field dispatch by one ordinal and requires exactly the goldens
# that pass through a RootConfig red (I11's shape, at the gate's own grain).
.PHONY: tables-goose-json-walk tables-goose-json-walk-negative-control
tables-goose-json-walk:
	@test -x "$(GOOSE)" || $(MAKE) dist-goose
	go test -count=1 -run '^(TestGooseJSONWalkIsOneRuntime|TestGooseJSONRoundTripsTheGoldens|TestGooseJSONHostile)$$' ./internal/codegen/goosetable/
tables-goose-json-walk-negative-control:
	@test -x "$(GOOSE)" || $(MAKE) dist-goose
	go test -count=1 -run '^TestGooseJSONWalkNegativeControl$$' ./internal/codegen/goosetable/

# tables-goose-json-differential (PORTING I13): 200 random instances, the leg's
# text against the compiler's own engine's, both directions; its control caps
# the walk's float32 precision at six digits and requires red on the text.
.PHONY: tables-goose-json-differential tables-goose-json-differential-negative-control
tables-goose-json-differential:
	@test -x "$(GOOSE)" || $(MAKE) dist-goose
	go test -count=1 -run '^TestGooseTextDifferential$$' ./internal/codegen/goosetable/
tables-goose-json-differential-negative-control:
	@test -x "$(GOOSE)" || $(MAKE) dist-goose
	go test -count=1 -run '^TestGooseTextDifferentialNegativeControl$$' ./internal/codegen/goosetable/

# The leg gate reaches them (as make/cs.mk's test-cs reaches its differential):
# a prerequisite added here, so make/goose.mk is not edited.
test-goose: tables-goose-json-walk tables-goose-json-walk-negative-control tables-goose-json-differential tables-goose-json-differential-negative-control

# I11: regenerate from an actual text-reader emitter sabotage and require the
# harness to localise it. The normal conformance CI also runs these surfaces.
.PHONY: tables-goose-json-conformance conformance-negative-control-goose-walk
tables-goose-json-conformance: build/conformance-harness build/conformance-goose
	@test -x "$(GOOSE)" || $(MAKE) dist-goose
	sh test/conformance/goose/json-conformance
conformance-negative-control-goose-walk: build/conformance-harness build/conformance-goose
	@test -x "$(GOOSE)" || $(MAKE) dist-goose
	sh test/conformance/goose/walk-negative-control
