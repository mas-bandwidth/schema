# Compiled closure and widening regressions from the shared reference review.
.PHONY: tables-reference-review
tables-reference-review:
	go test ./compiler -run '^Test(CppVariableWideAlignment|CppCollectionWidening|FlagsElementWidening)$$' -count=1

test: tables-reference-review

.PHONY: tables-reference-list-negative-control
tables-reference-list-negative-control:
	sh test/tables/reference-review-control list

.PHONY: tables-reference-map-negative-control
tables-reference-map-negative-control:
	sh test/tables/reference-review-control map

.PHONY: tables-reference-flags-negative-control
tables-reference-flags-negative-control:
	sh test/tables/reference-review-control flags

.PHONY: tables-reference-alignment-negative-control
tables-reference-alignment-negative-control:
	sh test/tables/reference-review-control alignment

.PHONY: tables-reference-include-negative-control
tables-reference-include-negative-control:
	sh test/tables/reference-review-control include
