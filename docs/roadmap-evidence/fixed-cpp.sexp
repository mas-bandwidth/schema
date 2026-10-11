(:id "enum-values/cpp/valid-data" :state :done :evidence ("internal/codegen/cpptable/fixedroadmap_t10_test.go: TestFixedRoadmapCppT10FloatsComposites/enum-values/cpp/valid-data") :tested-revision "57f7d98c8c887a29683f161deb982c4092ee0afa")
(:id "nested-keyed-arrays/cpp/valid-data" :state :done :evidence ("internal/codegen/cpptable/fixedroadmap_t10_test.go: TestFixedRoadmapCppT10FloatsComposites/nested-keyed-arrays/cpp/valid-data") :tested-revision "57f7d98c8c887a29683f161deb982c4092ee0afa")
; appended by hand: sbcl does not resolve on this machine (command -v sbcl), so the
; ev.lisp ledger program could not run; these are the forms it would have written,
; in its exact shape, one per verdict line of $JOB/verdicts.txt.
(:id "cpp/C3" :state :done :evidence ("internal/codegen/cpptable/fixedroadmap_t05_test.go: TestFixedRoadmapCppT05Evolution/cpp/C3") :tested-revision "774c15b2a")
(:id "cpp/C5" :state :done :evidence ("internal/codegen/cpptable/fixedroadmap_t05_test.go: TestFixedRoadmapCppT05Evolution/cpp/C5") :tested-revision "774c15b2a")
(:id "cpp/C4" :state :unknown :note "the cpp emitter sets report->malformed and defaults the field (internal/codegen/cpptable/fixedform.go:863 emitFixedTextContent) where docs/FIXED-FORM-ALGORITHM.md:341 rules a refusal by name; closing it moves the shared TableMessageReason vocabulary and contradicts test/conformance/cpp/rows/C5.cpp, outside this card's PATHS")
(:id "cpp/R24" :state :unknown :note "text_ill_formed is absent from the cpp refusal vocabulary and the reference sets malformed instead (docs/FIXED-FORM-ALGORITHM.md:912, and :1144's row 8 failing line)")
