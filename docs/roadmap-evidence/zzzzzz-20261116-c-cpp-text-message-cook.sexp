(:id "vt/text-form/cpp" :state :done
 :evidence ("test/tables/main.cpp: test_json_corpus_round_trip, test_json_dialect, test_json_hostile_kinds, test_json_pinned_text"
            "make tables-json-walk tables-json-negative-control"
            "test/conformance/cpp/main.cpp: json-read, json-write, json-hostile")
 :tested-revision "88513786d1f5b27a058f55010d91d029d12f1201"
 :note "2026-11-16: Linux x86-64 under default nova-reserve: the complete C++ table executable and JSON walk and its negative control passed. At cc47751ae4209f491ea9f8b1c0480a3a5a7bf302 the conformance JSON read, write and hostile surfaces passed 108/108, 110/110 and 155/155. The table test's tolerant-announcement fixture was corrected separately at 9ae3c81639fef20ff5511ad6b5fe17a874b1025b. At 88513786d1f5b27a058f55010d91d029d12f1201 all C++ conformance surfaces passed, including the same JSON rows.")

(:id "vt/text-form/c" :state :done
 :evidence ("test/conformance/c/main.c: json-read, json-write, json-hostile"
            "compiler/tablescwire_test.go: TestCTableWireJsonIntegerDomain"
            "make tables-c-json-walk conformance-negative-control-c")
 :tested-revision "cc47751ae4209f491ea9f8b1c0480a3a5a7bf302"
 :note "2026-11-16: Linux x86-64 under default nova-reserve: C JSON read, write and hostile conformance passed 108/108, 110/110 and 155/155; the generated C walk, integer-domain test and planted field-index control passed. The separate C framing surface failed frame_unreferenced_entry and is not claimed by this text evidence.")

(:id "vt/text-form-variable/cpp" :state :done
 :evidence ("test/tables/main.cpp: test_json_variable_class, test_message_variable_arm"
            "make tables-json-graph-walk"
            "test/conformance/cpp/main.cpp: json-read, json-write, json-hostile")
 :tested-revision "88513786d1f5b27a058f55010d91d029d12f1201"
 :note "2026-11-16: Linux x86-64 under default nova-reserve: graph JSON walk and the complete C++ table executable passed, including variable-class and union-arm cases. The fixture correction is 9ae3c81639fef20ff5511ad6b5fe17a874b1025b; full unfiltered C++ conformance at 88513786d1f5b27a058f55010d91d029d12f1201 passed all JSON read, write and hostile rows.")

(:id "vt/message-form/cpp" :state :done
 :evidence ("test/tables/message_form.h: test_message_form_goldens, test_message_form_batch, test_message_form_pointered, test_message_form_two_peers, test_message_form_refusals"
            "internal/codegen/cpptable/messageform_held_test.go: TestMessageFormHeldCPP"
            "make tables-message-form-negative-control"
            "test/conformance/cpp/main.cpp: message")
 :tested-revision "88513786d1f5b27a058f55010d91d029d12f1201"
 :note "2026-11-16: Linux x86-64 under default nova-reserve: C++ message conformance passed 13/13 and the full C++ table executable passed after fixture correction 9ae3c81639fef20ff5511ad6b5fe17a874b1025b. The complete 48-row mutation gate passed on bbe544f9bff885dba2a31f73adf7237a859da682; successor d1b0cfb220d4e907904233a69d7ed239e7d42b9f updated two born-count assertions after the Go minimum repair and passed the affected tests and six mutation controls. Full C++ conformance passed at 88513786d1f5b27a058f55010d91d029d12f1201.")

(:id "vt/cook-write/cpp" :state :done
 :evidence ("test/tables/cook_main.cpp: mode_cookwrite, mode_cookwrite_pointered"
            "make tables-cook-write tables-cook-write-negative-control tables-cook-write-hooks-negative-control"
            "make conformance-cpp-cook-write conformance-cpp-cook-write-negative-control")
 :tested-revision "88513786d1f5b27a058f55010d91d029d12f1201"
 :note "2026-11-16: Linux x86-64 under default nova-reserve: cook-write passed all 142/142 conformance rows; the dead-slot sabotage produced 132/142 and was detected, and allocation and hook negative controls fired. Normal and sanitized cook-write tests passed. The separate full C++ conformance run at 88513786d1f5b27a058f55010d91d029d12f1201 passed every registered surface.")
