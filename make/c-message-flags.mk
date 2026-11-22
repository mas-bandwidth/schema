.PHONY: tables-c-message-flags-domain tables-c-message-flags-domain-negative-control
tables-c-message-flags-domain:
	SCHEMA_SLOW=1 SCHEMA_REQUIRE_CORPUS=1 $(GOTESTRUN) ./internal/codegen/ctable -count=1 -timeout 2m -run '^TestCMessageFlagsWriteDomain$$' -v
tables-c-message-flags-domain-negative-control: tables-c-message-flags-domain
	SCHEMA_SLOW=1 SCHEMA_REQUIRE_CORPUS=1 $(GOTESTRUN) ./internal/codegen/ctable -count=1 -timeout 2m -run '^TestCMessageFlagsWriteDomainNegativeControl$$' -v

test-c: tables-c-message-flags-domain
