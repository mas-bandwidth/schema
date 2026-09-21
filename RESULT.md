RESULT cell-java-r4 sha=205a85684bc0 — schema matrix cell java/R4 (the hash is fnv1a64 over the layout bytes then DIGEST(T), the digest computed at the hash site from the schema; a runtime never derives a hash from layout bytes it holds): the assertion that proves it at the tip, in its own file, red first
DONE
BRANCH rowan/cell-java-r4
REPO mas-bandwidth/schema
law: docs/SPEC-TABLES.md:6578
cell: java/R4
red: 52 (R4.java:52)
green: 52 (R4.java:52)
control: R4 FAILED: layout hash != fnv1a64 over layout as written
run: java -cp test/conformance/java/rows test.conformance.java.rows.R4
files: test/conformance/java/rows/R4.java
preflight: /home/ubuntu/sdk/bin/javac --release 17 -Xlint:all -Werror -d build/conformance-java
        build/tables-generated-java/examples/*.java build/tables-generated-java/pointers/*.java \
        build/tables-generated-java/block/*.java build/tables-generated-java/v1/*.java \
        build/tables-generated-java/v2/*.java build/tables-generated-java/p1/*.java \
        build/tables-generated-java/p3/*.java test/conformance/java/src/Driver.java
unsure: none
gofmt: passed (no output)
go vet: passed (no output)
go test ./internal/ci/: ok
