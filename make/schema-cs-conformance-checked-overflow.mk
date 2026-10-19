# C# CONFORMANCE UNDER CHECKED ARITHMETIC
# CheckForOverflowUnderflow is supplied as an MSBuild global property to both
# generated-conformance and cs-cook project builds, including serialize.cs.
CS_CHECKED_DIR := build/cs-checked-overflow
CS_CHECKED_PROJECT := test/conformance/cs/schemaconformance.csproj

# Build both C# assemblies with the MSBuild global property. The harness runs
# both with --no-build, so the cs-cook output it consumes must be the checked
# build too. Restore the ordinary cs-cook output after the driver completes.
.PHONY: tables-cs-checked-overflow-conformance
tables-cs-checked-overflow-conformance: build/tables-generated-cs/.stamp cards-cs-generate build/conformance-harness build-cs-cook
	@mkdir -p $(CS_CHECKED_DIR)/work
	$(DOTNET) build $(CS_CHECKED_PROJECT) --nologo -v q --no-incremental \
		-p:CheckForOverflowUnderflow=true
	$(DOTNET) build test/cs-cook/schemacook.csproj --nologo -v q --no-incremental \
		-p:CheckForOverflowUnderflow=true
	./build/conformance-harness run --only cs --work $(CS_CHECKED_DIR)/work
	$(DOTNET) build test/cs-cook/schemacook.csproj --nologo -v q --no-incremental
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
	@mkdir -p $(CS_CHECKED_DIR)/negative/cs-cook/bin $(CS_CHECKED_DIR)/negative/cs-cook/obj
	@printf '%s\n' \
		'using System.Runtime.CompilerServices;' \
		'internal static class SchemaCheckedCookOverflowPlant {' \
		'  [ModuleInitializer] internal static void Plant() {' \
		'    int value = int.MaxValue;' \
		'    _ = value + 1;' \
		'  }' \
		'}' > $(abspath $(CS_CHECKED_DIR)/negative/cs-cook/CheckedOverflowPlant.cs)
	@# The cs-cook project is the port's test tree, which this card does not
	@# own: copy it to scratch and add the plant there, leaving the tracked
	@# project byte-identical to base.
	@sed -e 's|Include="src/\*.cs"|Include="$(abspath test/cs-cook)/src/*.cs"|' \
		-e 's|</ItemGroup>|<Compile Include="$(abspath $(CS_CHECKED_DIR)/negative/cs-cook/CheckedOverflowPlant.cs)" /></ItemGroup>|' \
		test/cs-cook/schemacook.csproj > $(abspath $(CS_CHECKED_DIR)/negative/cs-cook/schemacook.csproj)
	$(DOTNET) build $(abspath $(CS_CHECKED_DIR)/negative/cs-cook/schemacook.csproj) --nologo -v q --no-incremental \
		-o $(abspath $(CS_CHECKED_DIR)/negative/cs-cook/bin) \
		-p:BaseIntermediateOutputPath=$(abspath $(CS_CHECKED_DIR)/negative/cs-cook/obj)/ \
		-p:CookGeneratedDir=$(abspath build/tables-generated-cs/pointers) \
		-p:CheckForOverflowUnderflow=true
	@if $(DOTNET) $(CS_CHECKED_DIR)/negative/cs-cook/bin/schemacooktest.dll > $(CS_CHECKED_DIR)/negative/cs-cook.log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: cs-cook accepted the planted overflow'; \
		cat $(CS_CHECKED_DIR)/negative/cs-cook.log; exit 1; \
	fi
	@grep -q 'OverflowException' $(CS_CHECKED_DIR)/negative/cs-cook.log || { \
		echo 'NEGATIVE CONTROL FAILED: cs-cook plant failed for a reason other than OverflowException'; \
		cat $(CS_CHECKED_DIR)/negative/cs-cook.log; exit 1; \
	}
	@echo 'tables C# checked-overflow negative control: cs-cook plant raised OverflowException'
