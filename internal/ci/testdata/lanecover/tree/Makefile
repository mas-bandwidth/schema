SPECIAL := special
test-tagged:
	go test -count=1 -tags $(SPECIAL) ./b/ \
		-run '^TestTagged$$'
test-nested:
	cd nested && go test -count=1 .
test-all: test-tagged test-nested
orphan:
	go test ./a/
