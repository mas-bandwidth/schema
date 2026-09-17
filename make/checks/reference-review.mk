# Compiled closure and widening regressions from the shared reference review.
.PHONY: tables-reference-review
tables-reference-review:
	sh test/slowgate/proof tables-reference-review \
		'TestCppVariableWideAlignment/narrow TestCppVariableWideAlignment/direct TestCppVariableWideAlignment/pointer TestCppVariableWideAlignment/union TestCppVariableWideAlignment/list TestCppVariableWideAlignment/map TestCppVariableWideAlignment/array TestCppVariableWideAlignment/nested TestCppCollectionWidening/list TestCppCollectionWidening/map TestFlagsElementWidening TestCppHiddenUnionExtentRefusal/false TestCppHiddenUnionExtentRefusal/true TestRepeatedArrayTailDefaults/c TestRepeatedArrayTailDefaults/cpp TestRepeatedArrayTailDefaults/c-bounded-extent' \
		./compiler -run '^Test(CppVariableWideAlignment|CppCollectionWidening|FlagsElementWidening|CppHiddenUnionExtentRefusal|RepeatedArrayTailDefaults)$$' -count=1

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

.PHONY: tables-reference-hidden-union-negative-control
tables-reference-hidden-union-negative-control:
	sh test/tables/reference-review-control hidden

.PHONY: tables-reference-counted-tail-negative-control
tables-reference-counted-tail-negative-control:
	sh test/tables/reference-review-control tail
