# Message flags follow the debug writer domain contract, including arithmetic measures.
.PHONY: tables-cpp-message-flags-domain tables-cpp-message-flags-domain-negative-control
tables-cpp-message-flags-domain:
	$(GO_TEST) ./internal/codegen/cpptable -count=1 -timeout 2m -run '^TestCppMessageFlagsWriteDomain$$' -v
tables-cpp-message-flags-domain-negative-control: tables-cpp-message-flags-domain
	$(GO_TEST) ./internal/codegen/cpptable -count=1 -timeout 2m -run '^TestCppMessageFlagsWriteDomainNegativeControl$$' -v

test-cpp: tables-cpp-message-flags-domain
