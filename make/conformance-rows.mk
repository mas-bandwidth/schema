# make/conformance-rows.mk — THE FIXED-FORM ROWS THAT ARE STANDALONE FILES.
#
# A row under test/conformance/<leg>/rows/ is a self-contained program that
# drives one law of docs/FIXED-FORM-ALGORITHM.md through the generated code.
# Most legs run their rows through the leg's conformance driver; these nine
# had no runner at all, and while nothing ran them they rotted against the
# generated code (a renamed type, a report field that no longer exists, a law
# that moved):
#
#   C     F4 F10 F12      C++   R15
#   Dart  E8 R8 R13       Java  F10 R8
#   Rust  F8
#
# Each target below generates only the unit its rows name, compiles them and
# runs them; a row that fails, or does not build, fails the target. They are
# prerequisites of verify-<leg>-conformance (make/verify-compiled.mk), the
# per-leg lane ci-fast.yml runs on every pull request that touches the leg,
# and the C++ lane also runs build/schema_test_tables there (the tables test
# binary the CI never ran).
#
# A row added to one of these legs goes in the list of its target; a row that
# is not in a list is not run.

CONFORMANCE_ROWS_DIR := build/conformance-rows

CONFORMANCE_ROWS_C    := F4 F10 F12
CONFORMANCE_ROWS_DART := E8 R8 R13
CONFORMANCE_ROWS_JAVA := F10 R8

.PHONY: conformance-rows-c conformance-rows-cpp conformance-rows-dart conformance-rows-java conformance-rows-rust

# C: F4 and F10 read V1's Cell, F12 reads K1's Root; every unit is the
# generated one the conformance driver builds (the package-prefixed names).
conformance-rows-c: bin/schema
	@mkdir -p ${CONFORMANCE_ROWS_DIR}/c
	./bin/schema generate --lang c --out ${CONFORMANCE_ROWS_DIR}/c/v1 test/tables/V1.schema
	./bin/schema generate --lang c --out ${CONFORMANCE_ROWS_DIR}/c/k1 test/tables/K1.schema
	@set -e; for row in ${CONFORMANCE_ROWS_C}; do \
		case $$row in F12) unit=k1; table=K1Table.c ;; *) unit=v1; table=V1Table.c ;; esac; \
		${CC} -std=c11 -Wall -Wextra -Werror -I${CONFORMANCE_ROWS_DIR}/c/$$unit -I${SERIALIZE_C} \
			test/conformance/c/rows/$$row.c ${CONFORMANCE_ROWS_DIR}/c/$$unit/$$table \
			-o ${CONFORMANCE_ROWS_DIR}/c/$$row -lm; \
		${CONFORMANCE_ROWS_DIR}/c/$$row; \
	done

# C++: R15 reads the four versioning pairs the tables stamp generates.
conformance-rows-cpp: build/tables-generated/.stamp
	@mkdir -p ${CONFORMANCE_ROWS_DIR}/cpp
	${CXX} -std=c++17 -Wall -Wextra -Werror -Wshadow -ffp-contract=off \
		-Ibuild/tables-generated/vnum \
		-Ibuild/tables-generated/vold_enum_append -Ibuild/tables-generated/vnew_enum_append \
		-Ibuild/tables-generated/vold_field_append -Ibuild/tables-generated/vnew_field_append \
		-I${SERIALIZE} test/conformance/cpp/rows/R15.cpp -o ${CONFORMANCE_ROWS_DIR}/cpp/R15
	${CONFORMANCE_ROWS_DIR}/cpp/R15

# Dart: the rows import build/tables-generated-dart/{v1,v2,pointers}.
conformance-rows-dart: toolchain-dart build/tables-generated-dart/.stamp
	@set -e; for row in ${CONFORMANCE_ROWS_DART}; do \
		${DART} --enable-asserts run test/conformance/dart/rows/$$row.dart; \
	done

# Java: V1's package tblv1 is compiled to a class directory the rows run
# against in source-file mode, as their headers say.
conformance-rows-java: toolchain-java bin/schema
	@mkdir -p ${CONFORMANCE_ROWS_DIR}/java/classes
	./bin/schema generate --lang java --out ${CONFORMANCE_ROWS_DIR}/java/v1 test/tables/V1.schema
	find ${CONFORMANCE_ROWS_DIR}/java/v1 -name '*.java' -print0 | xargs -0 ${JAVAC} -d ${CONFORMANCE_ROWS_DIR}/java/classes
	@set -e; for row in ${CONFORMANCE_ROWS_JAVA}; do \
		${JAVA} -ea -cp ${CONFORMANCE_ROWS_DIR}/java/classes test/conformance/java/rows/$$row.java; \
	done

# Rust: F8 reads the block unit's PaddedRow. The row is a cargo integration
# test of a probe crate over the generated one, built offline: the only
# dependency is the sibling serialize.rs by path.
conformance-rows-rust: bin/schema
	@mkdir -p ${CONFORMANCE_ROWS_DIR}/rust/probe/src ${CONFORMANCE_ROWS_DIR}/rust/probe/tests
	./bin/schema generate --lang rust --out ${CONFORMANCE_ROWS_DIR}/rust/blockdemo/src tables/block
	@printf '[package]\nname = "blockdemo"\nversion = "0.0.0"\nedition = "2024"\n\n[features]\ndefault = ["block", "cook"]\nblock = []\ncook = []\n\n[dependencies]\nserialize = { package = "serialize-official", path = "../../../../${SERIALIZE_RS}" }\n' \
		> ${CONFORMANCE_ROWS_DIR}/rust/blockdemo/Cargo.toml
	@printf '[package]\nname = "probe"\nversion = "0.0.0"\nedition = "2024"\n\n[dependencies]\nblockdemo = { path = "../blockdemo" }\n' \
		> ${CONFORMANCE_ROWS_DIR}/rust/probe/Cargo.toml
	@: > ${CONFORMANCE_ROWS_DIR}/rust/probe/src/lib.rs
	@cp test/conformance/rust/rows/F8.rs ${CONFORMANCE_ROWS_DIR}/rust/probe/tests/F8.rs
	PATH="${RUSTUP_BIN}:$$PATH" cargo test --quiet --offline --manifest-path ${CONFORMANCE_ROWS_DIR}/rust/probe/Cargo.toml
