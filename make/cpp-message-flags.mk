# Message flags follow the debug writer domain contract, including arithmetic measures.
.PHONY: tables-cpp-message-flags-domain tables-cpp-message-flags-domain-negative-control
tables-cpp-message-flags-domain:
tables-cpp-message-flags-domain-negative-control: tables-cpp-message-flags-domain

test-cpp: tables-cpp-message-flags-domain
