# C++ authorship of the two populated positive feature witnesses. This target
# never rewrites the committed Go-authored comparison pins.
build/feature-witness-cpp/.stamp: bin/schema testdata/conformance/feature-witnesses/full/Features.schema
	@mkdir -p build/feature-witness-cpp
	./bin/schema generate --lang cpp --out build/feature-witness-cpp testdata/conformance/feature-witnesses/full/Features.schema
	@touch $@

build/feature-witness-cpp/produce: build/feature-witness-cpp/.stamp test/conformance/cpp/feature_witnesses.cpp
	$(CXX) $(TABLES_CXXFLAGS) -I$(SERIALIZE) -Ibuild/feature-witness-cpp test/conformance/cpp/feature_witnesses.cpp build/feature-witness-cpp/FeaturesTable.cpp -o $@

.PHONY: conformance-cpp-positive-features
conformance-cpp-positive-features: build/feature-witness-cpp/produce
	@mkdir -p build/feature-witness-cpp/outputs
	@rm -f build/feature-witness-cpp/outputs/Root.bin build/feature-witness-cpp/outputs/ArrayRoot.bin
	./build/feature-witness-cpp/produce build/feature-witness-cpp/outputs
