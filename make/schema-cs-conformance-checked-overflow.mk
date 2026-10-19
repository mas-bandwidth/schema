# C# CONFORMANCE UNDER CHECKED ARITHMETIC
# CheckForOverflowUnderflow is supplied as an MSBuild global property, so both
# the generated corpus and serialize.cs sources compiled into the driver use
# checked integer arithmetic without changing the shared project file.
CS_CHECKED_DIR := build/cs-checked-overflow
CS_CHECKED_PROJECT := test/conformance/cs/schemaconformance.csproj

# The checked assembly is built into the driver project's default output, so
# the committed test/conformance/cs/driver (which runs it with --no-build)
# exercises exactly the checked build over the whole conformance corpus.
.PHONY: tables-cs-checked-overflow-conformance
tables-cs-checked-overflow-conformance: build/tables-generated-cs/.stamp cards-cs-generate build/conformance-harness build-cs-cook
	@mkdir -p $(CS_CHECKED_DIR)/work
	$(DOTNET) build $(CS_CHECKED_PROJECT) --nologo -v q --no-incremental \
		-p:CheckForOverflowUnderflow=true
	./build/conformance-harness run --only cs --work $(CS_CHECKED_DIR)/work
	@echo 'tables C# checked-overflow conformance: generated corpus and runtime passed under /checked+'

# Plant one overflowing expression in a scratch copy of the generated corpus.
# The module initializer makes the planted add run before the conformance driver;
# /checked+ must turn it into OverflowException. The ordinary corpus remains
# untouched and the main target above is the green half of this control.
.PHONY: tables-cs-checked-overflow-negative-control
tables-cs-checked-overflow-negative-control: build/tables-generated-cs/.stamp cards-cs-generate
	@rm -r $(CS_CHECKED_DIR)/negative 2>/dev/null || true
	@mkdir -p $(CS_CHECKED_DIR)/negative/corpus $(CS_CHECKED_DIR)/negative/bin
	@cp -R build/tables-generated-cs/. $(CS_CHECKED_DIR)/negative/corpus/
	@printf '%s\n' \
		'using System.Runtime.CompilerServices;' \
		'internal static class SchemaCheckedOverflowPlant {' \
		'  [ModuleInitializer] internal static void Plant() {' \
		'    int value = int.MaxValue;' \
		'    _ = value + 1;' \
		'  }' \
		'}' > $(CS_CHECKED_DIR)/negative/corpus/rt1/CheckedOverflowPlant.cs
	$(DOTNET) build $(CS_CHECKED_PROJECT) --nologo -v q --no-incremental \
		-o $(abspath $(CS_CHECKED_DIR)/negative/bin) \
		-p:CheckForOverflowUnderflow=true \
		-p:TablesGeneratedDir=$(abspath $(CS_CHECKED_DIR)/negative/corpus)
	@if $(DOTNET) $(CS_CHECKED_DIR)/negative/bin/schemaconformance.dll > $(CS_CHECKED_DIR)/negative.log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: checked arithmetic accepted the planted overflow'; \
		cat $(CS_CHECKED_DIR)/negative.log; exit 1; \
	fi
	@grep -q 'OverflowException' $(CS_CHECKED_DIR)/negative.log || { \
		echo 'NEGATIVE CONTROL FAILED: the plant failed for a reason other than OverflowException'; \
		cat $(CS_CHECKED_DIR)/negative.log; exit 1; \
	}
	@echo 'tables C# checked-overflow negative control: planted generated-source overflow was refused'
