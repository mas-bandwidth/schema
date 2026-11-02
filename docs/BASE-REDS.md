# Base reds

This is the ledger of tests and make targets that are red at the base of the
schema stream, so a reader does not re-derive them. Cite a row as
`docs/BASE-REDS.md: <Red>`, for example `docs/BASE-REDS.md: TestRowC8`.

Each row names the card that is expected to fix it. A card that lands a fix
deletes its row (or rows) in the same commit that makes the test green; a row
that outlives its fix is a lie about the tree.

The Go conformance rows are not run by any make target. To run them by hand,
`cd test/conformance/go; go test ./rows/` with `SCHEMA_SLOW=1`, after
`make build/tables-generated-go/.stamp tables-fixedform-corpus` and a sibling
`serialize.go` link; that is why they sat red unseen.

Measured at sprint/schema-go f820a0c1f on 2026-10-17.

| Red | Where | Cause | Card | Seen at |
| --- | --- | --- | --- | --- |
| TestRowC8 | test/conformance/go/rows/C8_test.go:47 | RangedWidthsMeasure returns -1 for the overloaded bits(12) value, so make([]byte, -1) panics and kills the rows binary. | schema-conf-row-c8-writer-refusal | f820a0c1f |
| TestRowC14 | test/conformance/go/rows/C14_test.go:43 | CfgFixedSave returns -1 on a value whose Mode is 99, so the save refusal ends the row before the live-count bound is checked. | schema-conf-rows-c14-w2-hostile-values | f820a0c1f |
| TestRowW2 | test/conformance/go/rows/W2_test.go:102 | The writer leaves the absent optional's stained payload on the wire instead of the template's zeros, so the wire-identity assertions count red. | schema-conf-rows-c14-w2-hostile-values | f820a0c1f |
| TestRowF8 | test/conformance/go/rows/F8_test.go:121 | The generated probe accepts a ragged tail instead of reporting it malformed, so its own go test exits non-zero. | schema-conf-row-f8-ragged-tail | f820a0c1f |
| TestRowR14 | test/conformance/go/rows/R14_test.go:86 | The generated runtime does not carry the layout_record_too_large check, so scanGeneratedCode returns false. | schema-conf-rows-r14-r26-r31-generated-text | f820a0c1f |
| TestRowR26 | test/conformance/go/rows/R26_test.go:63 | The generated tableFixedLineagePlans does not record layout_malformed on a parse or compile failure. | schema-conf-rows-r14-r26-r31-generated-text | f820a0c1f |
| TestRowR31 | test/conformance/go/rows/R31_test.go:40 | tableFixedTagAt is not emitted as a full-width uint64 read, so the tag-width check fails. | schema-conf-rows-r14-r26-r31-generated-text | f820a0c1f |
| make slow-gate-scan | make/go.mk:667, make/go.mk:918 | The scan finds the tables-go-runtime-home and tables-go-subsets recipes running gated go test lines without SCHEMA_SLOW=1 or the proof wrapper, so it exits non-zero. | schema-go-slow-gate-scan-two-recipes | f820a0c1f |
| make tables-go-fixedform | make/go.mk:60 | On a fresh checkout the generated fixed-form units need the sibling serialize.go runtime, which is absent, so the proof cannot build them. | schema-go-test-go-builds-fixedform-runtime | f820a0c1f |
| TestPointerArmLengthBeforeResolution | internal/codegen/gotable/armreport_test.go:13 | FeedLoadMeasure returns -1 on the damaged wire, so the generated probe's make of size+63 panics. | schema-go-load-measure-stays-tolerant | f820a0c1f |
| TestRegionCountCrossesLength | internal/codegen/gotable/lists_test.go:91 | AlbumLoadMeasure returns -1 on the damaged wire where the oracle says 40, so the measure assertion fails. | schema-go-load-measure-stays-tolerant | f820a0c1f |
| TestRegionUnionAndBlobValues | internal/codegen/gotable/regions_test.go:116 | RootLoadMeasure returns -1 on the damaged pointer-arm wire, so the generated probe's make of size+16 panics. | schema-go-load-measure-stays-tolerant | f820a0c1f |
| TestUnboundedTextRefusedOffGo | compiler/unboundedtext_refusal_test.go:70 | The test pins sha256 digests of generated output instead of checking the refusals it names, so unrelated runtime changes turn it red. | schema-go-refusal-checks-compute-not-pin | f820a0c1f |
| TestJSFixedLanesAgree | internal/codegen/jstable/fixedlanes_agree_test.go:130 | The emitted JS fixed runtime has 11 lanes, but fixedroadmap_t08_test.go (P1's comment, length check, failure line and lane slice; W6's guard-lane comment) and fixedroadmap_t02_test.go (W14's identity plan width) still say 9, so the agreement check and its negative control fail. Also red at sprint/schema-squirrel 5a10abe52. | schema-js-fixed-lanes-tests-name-eleven | 8694cf96c |
