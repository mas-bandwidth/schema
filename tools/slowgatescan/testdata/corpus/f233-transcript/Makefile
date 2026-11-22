# F-233: both gated tests have an armed owner. full.jsonl is the whole go test
# -json stream; cut.jsonl is the same stream ended on the first complete skip
# record, before the second test and the package's closing record.
.PHONY: first second
first:
	SCHEMA_SLOW=1 go test -count=1 -run '^TestFirst$$' ./pkg/
second:
	SCHEMA_SLOW=1 go test -count=1 -run '^TestSecond$$' ./pkg/
