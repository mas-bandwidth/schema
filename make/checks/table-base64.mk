# Public JSON Base64 compatibility checks for emitters with table text form.
build/table-base64/harness: test/table-base64/harness/main.go
	@mkdir -p build/table-base64
	go build -o $@ ./test/table-base64/harness

build/table-base64/%/.stamp: bin/schema test/table-base64/Bytes.schema
	./bin/schema generate --lang $* --out build/table-base64/$* test/table-base64/Bytes.schema
	@touch $@

.PHONY: table-base64-cpp table-base64-c table-base64-go table-base64-cs
table-base64-cpp: build/table-base64/harness build/table-base64/cpp/.stamp
	$(CXX) -std=c++17 -ffp-contract=off -O2 -Wall -Wextra -Werror -x c++ -Ibuild/table-base64/cpp test/table-base64/driver.c build/table-base64/cpp/BytesTable.cpp -o build/table-base64/cpp/driver
	./build/table-base64/harness ./build/table-base64/cpp/driver

table-base64-c: build/table-base64/harness build/table-base64/c/.stamp
	$(CC) -std=c11 -ffp-contract=off -O2 -Wall -Wextra -Werror -Ibuild/table-base64/c test/table-base64/driver.c build/table-base64/c/BytesTable.c -lm -o build/table-base64/c/driver
	./build/table-base64/harness ./build/table-base64/c/driver

table-base64-go: build/table-base64/harness build/table-base64/go/.stamp
	@printf 'module base64test\n\ngo 1.24.0\n' > build/table-base64/go/go.mod
	cd test/table-base64/go && go build -o ../../../build/table-base64/go/driver .
	./build/table-base64/harness ./build/table-base64/go/driver

table-base64-cs: build/table-base64/harness build/table-base64/cs/.stamp
	$(DOTNET) build --configuration Release --nologo test/table-base64/cs/table-base64.csproj
	./build/table-base64/harness $(DOTNET) test/table-base64/cs/bin/Release/net10.0/table-base64.dll

test: table-base64-cpp
test-c: table-base64-c
test-go: table-base64-go
test-cs: table-base64-cs
