# THE VARIABLE SAVE'S WRITE CONTRACT, DEBUG BUILD (test/cpp-hostile/contract/):
# docs/SPEC.md §5.1 rows 1 (ranged integer), 2 (bits(N)) and 5 (enum outside
# its set) on a field, an array element and a union arm's payload, each with a
# twin at its edge that writes; row 10 (a ranged float off its range) on every
# writer; and row 3 one record down (a child's count below its own minimum) on
# the fixed and message writers and the fixed load. Built WITHOUT NDEBUG, where
# schema_assert is live; a release build asserts nothing, by the assert-leg rule.
CPP_CONTRACT_BUILD := build/cpp-write-contract
CPP_CONTRACT_SRC   := test/cpp-hostile/contract

$(CPP_CONTRACT_BUILD)/.stamp: bin/schema make/cpp-write-contract.mk $(CPP_CONTRACT_SRC)/Contract.schema
	@rm -rf $(CPP_CONTRACT_BUILD) && mkdir -p $(CPP_CONTRACT_BUILD)/generated
	./bin/schema generate --lang cpp --out $(CPP_CONTRACT_BUILD)/generated $(CPP_CONTRACT_SRC)/Contract.schema >/dev/null
	@touch $@

$(CPP_CONTRACT_BUILD)/contract: $(CPP_CONTRACT_BUILD)/.stamp $(CPP_CONTRACT_SRC)/contract.cpp
	$(CXX) -std=c++17 -Wall -Wextra -Werror -ffp-contract=off -O1 -UNDEBUG -I$(SERIALIZE) -I$(CPP_CONTRACT_BUILD)/generated \
		$(CPP_CONTRACT_SRC)/contract.cpp $(wildcard $(CPP_CONTRACT_BUILD)/generated/*.cpp) -o $@

.PHONY: cpp-write-contract
cpp-write-contract: $(CPP_CONTRACT_BUILD)/contract
	./$(CPP_CONTRACT_BUILD)/contract

test: cpp-write-contract
_verify-cpp-bench: cpp-write-contract
