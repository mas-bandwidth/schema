SPECIAL := special
test-tagged:
	go test -count=1 -tags $(SPECIAL) ./b/ \
		-run '^TestTagged$$'
test-nested:
	cd nested && go test -count=1 .
test-all: test-nested
test-stella-never-on-any-matrix: test-tagged
orphan:
	go test ./a/
test-one: test-all
test-two:
test-rest:
