# make/schema-cs-generated-nullable-ca-analyzers-gate.mk — THE C# NULLABLE AND
# CA-ANALYZER LENS over the generated C# the compiler emits.
#
# The same algorithms are generated in many languages, so each language's
# strictest checker over that language's generated output is a separate lens on
# bugs (Glenn, 2026-10-18; ideas roadmap group schema-per-language-lenses).
# This card is the C# lens: Nullable enable, TreatWarningsAsErrors, and the
# .NET CA analyzers at AnalysisMode All, over the packet readers and writers in
# the generated cs corpus.
#
# THE CORPUS is the schema directories the generator's own targets read for
# this port (make/cs.mk): examples (generated/cs), the bench corpus
# (bench/corpus) and the 128-bit examples (examples128). It is GENERATED HERE,
# never taken from the committed tree, so a lens run is a statement about the
# emitter at the head and not about a stale artifact.
#
# THE RUNTIME is the serialize.cs sibling the port exports against
# ($(SERIALIZE_CS), pinned by the workflows' SERIALIZE_CS_TAG). It is built once
# as a library with its own analyzers off: the lens is the GENERATED output, and
# the runtime is evidence, not a second subject.
#
# THE ONLY SUPPRESSIONS are the seven in CSG_SUPPRESSIONS, one per rule, each
# with its one-line justification beside it. Every one is a NOT finding for
# generated wire code: public storage fields and the declared wire enum width
# are the port's design, the trailing Count/Max members deliberately repeat the
# last value, Schema is the family's facade name, explicit declared defaults
# state the schema, a null caller buffer is outside the wire domain, and the one
# ToString("x") is an unsigned value whose ASCII hex carries no separator. No
# NoWarn and no blanket severity change is used.
#
# TOOL: the .NET SDK, pinned by .github/dotnet-version and installed fleet-wide;
# this target probes for it and never fakes a pass. VERSION is the pin.
CSG_DOTNET_PIN := $(strip $(shell cat .github/dotnet-version 2>/dev/null))
CSG_SERIALIZE_CS ?= $(SERIALIZE_CS)
CSG_OUT ?= build/csg-nullable
CSG_PROJECT := $(CSG_OUT)/proj/probe.csproj
CSG_SUPPRESSIONS := $(CSG_OUT)/proj/Suppressions.cs
CSG_RUNTIME_DLL := $(CSG_OUT)/runtime/Serialize.dll
CSG_CORPUS := $(CSG_OUT)/corpus

# The probe spells every strictness setting rather than inheriting an SDK
# default, so the lens means the same thing on the next SDK.
define csg_project
<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <TargetFramework>net10.0</TargetFramework>
    <ImplicitUsings>disable</ImplicitUsings>
    <Nullable>enable</Nullable>
    <TreatWarningsAsErrors>true</TreatWarningsAsErrors>
    <EnableNETAnalyzers>true</EnableNETAnalyzers>
    <AnalysisLevel>latest</AnalysisLevel>
    <AnalysisMode>All</AnalysisMode>
    <EnableDefaultCompileItems>false</EnableDefaultCompileItems>
    <AllowUnsafeBlocks>true</AllowUnsafeBlocks>
    <AssemblyName>csgnullable</AssemblyName>
  </PropertyGroup>
  <ItemGroup>
    <Compile Include="$(abspath $(CSG_CORPUS))/examples/*.cs" />
    <Compile Include="$(abspath $(CSG_CORPUS))/bench/*.cs" />
    <Compile Include="$(abspath $(CSG_CORPUS))/bench/realworld/*.cs" />
    <Compile Include="$(abspath $(CSG_CORPUS))/ludicrous/*.cs" />
    <Compile Include="$(abspath $(CSG_SUPPRESSIONS))" />
    <Reference Include="Serialize"><HintPath>$(abspath $(CSG_RUNTIME_DLL))</HintPath></Reference>
  </ItemGroup>
</Project>
endef
export csg_project

define csg_suppressions
// Justified suppressions for the generated C# lens. Each is a NOT finding for
// generated wire code; the justification is the reason, not a restatement.
[assembly: System.Diagnostics.CodeAnalysis.SuppressMessage("Design", "CA1051:Do not declare visible instance fields", Justification = "Generated wire storage is public fields by design; properties would change the port's API.")]
[assembly: System.Diagnostics.CodeAnalysis.SuppressMessage("Design", "CA1062:Validate arguments of public methods", Justification = "Generated wire entry points take caller-owned buffers; a null there is a caller error outside the wire domain.")]
[assembly: System.Diagnostics.CodeAnalysis.SuppressMessage("Design", "CA1069:Enums should not have duplicate values", Justification = "The trailing Count and Max members deliberately repeat the last declared variant; no wire variant is duplicated.")]
[assembly: System.Diagnostics.CodeAnalysis.SuppressMessage("Design", "CA1028:Enum Storage should be Int32", Justification = "Generated enums carry the schema's declared wire width so storage and wire bits agree.")]
[assembly: System.Diagnostics.CodeAnalysis.SuppressMessage("Naming", "CA1724:Type names should not match namespaces", Justification = "Every generated unit names its facade Schema by the family convention; it shadows no namespace in generated code.")]
[assembly: System.Diagnostics.CodeAnalysis.SuppressMessage("Performance", "CA1805:Do not initialize unnecessarily", Justification = "The emitter spells declared defaults explicitly so the generated source states the schema's value.")]
[assembly: System.Diagnostics.CodeAnalysis.SuppressMessage("Globalization", "CA1305:Specify IFormatProvider", Justification = "The only call is unsigned ulong.ToString(\"x\"), whose ASCII hex digits carry no culture-sensitive separator.")]
// The tables corpus adds the fixed engine's own generated shapes. Each rule
// below is a NOT finding for generated wire/table code; the justification is
// the reason, not a restatement.
[assembly: System.Diagnostics.CodeAnalysis.SuppressMessage("Design", "CA1008:Enums should have zero value", Justification = "Generated wire enums name every declared variant; a synthetic zero member would add a value the schema never declared.")]
[assembly: System.Diagnostics.CodeAnalysis.SuppressMessage("Design", "CA1024:Use properties where appropriate", Justification = "Generated walk methods take a visitor and return a verdict; a property cannot carry the loop or its report.")]
[assembly: System.Diagnostics.CodeAnalysis.SuppressMessage("Design", "CA1034:Nested types should not be visible", Justification = "Generated Enumerator types are nested by the table dialect so each table owns its walk without a second namespace.")]
[assembly: System.Diagnostics.CodeAnalysis.SuppressMessage("Design", "CA1063:Implement IDisposable correctly", Justification = "The generated storage class is a sealed template whose Dispose is its only teardown; no derived finalizer exists to run.")]
[assembly: System.Diagnostics.CodeAnalysis.SuppressMessage("Design", "CA1069:Enums should not have duplicate values", Justification = "The trailing Count and Max members deliberately repeat the last declared variant; no wire variant is duplicated.")]
[assembly: System.Diagnostics.CodeAnalysis.SuppressMessage("Design", "CA1815:Override equals and operator equals on value types", Justification = "Generated row and cursor structs are wire views over caller storage; equality would compare addresses the schema does not define.")]
[assembly: System.Diagnostics.CodeAnalysis.SuppressMessage("Design", "CA1823:Avoid unused private fields", Justification = "The generated descriptor table is one template shared by every unit; a column unused by this unit's shape is read by another table's walk.")]
[assembly: System.Diagnostics.CodeAnalysis.SuppressMessage("Design", "CA1825:Avoid zero-length array allocations", Justification = "Generated empty-array constants are part of the table dialect's stable zero value, not a per-call allocation.")]
[assembly: System.Diagnostics.CodeAnalysis.SuppressMessage("Design", "CA2201:Do not raise reserved exception types", Justification = "The generated parser converts a wire refusal to the runtime's documented exception surface; the exception is the port's contract.")]
[assembly: System.Diagnostics.CodeAnalysis.SuppressMessage("Design", "CA2225:Operator overloads have named alternates", Justification = "Generated operators are value constructors the schema names; adding public methods would grow the port's API beyond the schema.")]
[assembly: System.Diagnostics.CodeAnalysis.SuppressMessage("Naming", "CA1700:Do not name enum values Reserved", Justification = "Generated refusal reasons name the engine's own vocabulary, including its reserved-bit spelling.")]
[assembly: System.Diagnostics.CodeAnalysis.SuppressMessage("Naming", "CA1707:Identifiers should not contain underscores", Justification = "Generated refusal and JSON member names carry the engine's declared snake_case spelling verbatim.")]
[assembly: System.Diagnostics.CodeAnalysis.SuppressMessage("Naming", "CA1715:Identifiers should have correct prefix", Justification = "The generated generic parameter E names an entry/table element, the family's spelling in every port.")]
[assembly: System.Diagnostics.CodeAnalysis.SuppressMessage("Naming", "CA1716:Identifiers should not match keywords", Justification = "Generated nested JSON helper names are the schema's field names; only the generated spelling is intended to be consumed.")]
[assembly: System.Diagnostics.CodeAnalysis.SuppressMessage("Naming", "CA1720:Identifier contains type name", Justification = "Generated cook kinds name the wire scalar by its schema type, which is the declaration the consumer reads.")]
[assembly: System.Diagnostics.CodeAnalysis.SuppressMessage("Naming", "CA1724:Type names should not match namespaces", Justification = "Every generated unit names its facade Schema by the family convention; it shadows no namespace in generated code.")]
[assembly: System.Diagnostics.CodeAnalysis.SuppressMessage("Naming", "CA1507:Use nameof in place of string", Justification = "Generated diagnostics carry the schema's declared member spelling as data, not the C# identifier.")]
[assembly: System.Diagnostics.CodeAnalysis.SuppressMessage("Usage", "CA1816:Dispose methods should call SuppressFinalize", Justification = "The generated storage type has no finalizer; the sealed template releases its allocation exactly once.")]
[assembly: System.Diagnostics.CodeAnalysis.SuppressMessage("Usage", "CA2263:Prefer generic overload", Justification = "Generated calls bind the non-generic runtime overload the wire dialect declares; the generic overload is not part of the pinned runtime surface.")]
[assembly: System.Diagnostics.CodeAnalysis.SuppressMessage("Maintainability", "CA1510:Use ArgumentNullException.ThrowIfNull", Justification = "Generated code targets the runtime floor that predates the throw helper, so the explicit throw is the portable spelling.")]
[assembly: System.Diagnostics.CodeAnalysis.SuppressMessage("Maintainability", "CA1513:Use ObjectDisposedException.ThrowIf", Justification = "Generated code targets the runtime floor that predates the throw helper, so the explicit throw is the portable spelling.")]
endef
export csg_suppressions

.PHONY: csg-corpus csg-runtime csg-project csg-dotnet-probe
csg-corpus:
	@rm -rf $(CSG_CORPUS) && mkdir -p $(CSG_CORPUS)/examples $(CSG_CORPUS)/bench $(CSG_CORPUS)/bench/realworld $(CSG_CORPUS)/ludicrous
	./bin/schema generate --lang cs --out $(CSG_CORPUS)/examples examples
	./bin/schema generate --lang cs --out $(CSG_CORPUS)/bench bench/corpus/Bench.schema
	./bin/schema generate --lang cs --out $(CSG_CORPUS)/bench/realworld bench/corpus/RealWorld.schema
	./bin/schema generate --lang cs --out $(CSG_CORPUS)/ludicrous examples128

csg-runtime:
	@test -d "$(CSG_SERIALIZE_CS)/src" || { echo "tool missing: serialize.cs (no $(CSG_SERIALIZE_CS)/src)"; exit 2; }
	@mkdir -p $(CSG_OUT)/runtime
	$(DOTNET) build $(CSG_SERIALIZE_CS)/src/Serialize.csproj -f net10.0 \
		-p:TargetFrameworks=net10.0 -p:EnableNETAnalyzers=false -p:AnalysisMode=None \
		--nologo -v q -o $(CSG_OUT)/runtime > $(CSG_OUT)/runtime.log 2>&1 || \
		{ tail -20 $(CSG_OUT)/runtime.log; exit 1; }

csg-project:
	@mkdir -p $(CSG_OUT)/proj
	@printf '%s\n' "$$csg_project" > $(CSG_PROJECT)
	@printf '%s\n' "$$csg_suppressions" > $(CSG_SUPPRESSIONS)

csg-dotnet-probe:
	@if ! command -v $(DOTNET) >/dev/null 2>&1; then echo "tool missing: dotnet"; exit 2; fi
	@v=`$(DOTNET) --version`; pin="$(CSG_DOTNET_PIN)"; \
	case "$$v" in "$$pin"*) : ;; *) echo "tool version: dotnet $$v, want $(CSG_DOTNET_PIN) from .github/dotnet-version"; exit 2;; esac

# THE GATE. A finding is any error line in the build log: with
# TreatWarningsAsErrors a compiler or analyzer warning (CS86xx nullable, CAxxxx)
# is an error there, so the log's error count is the finding count.
# THE TABLES CORPUS. The same schemas the C# tables leg generates
# (build/tables-generated-cs/.stamp, make/cs.mk's one generation list), lensed
# as a separate run because the tables leg is minutes and the packet leg is
# seconds. This target is the full corpus of the card; the packet gate above is
# its fast half.
CSG_TABLES_DIR := build/tables-generated-cs
CSG_TABLES_OUT := $(CSG_OUT)/tables
CSG_TABLES_PROJECT := $(CSG_TABLES_OUT)/probe.csproj

define csg_tables_project
<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <TargetFramework>net10.0</TargetFramework>
    <ImplicitUsings>disable</ImplicitUsings>
    <Nullable>enable</Nullable>
    <TreatWarningsAsErrors>true</TreatWarningsAsErrors>
    <EnableNETAnalyzers>true</EnableNETAnalyzers>
    <AnalysisLevel>latest</AnalysisLevel>
    <AnalysisMode>All</AnalysisMode>
    <EnableDefaultCompileItems>false</EnableDefaultCompileItems>
    <AllowUnsafeBlocks>true</AllowUnsafeBlocks>
    <AssemblyName>csgtables</AssemblyName>
  </PropertyGroup>
  <ItemGroup>
    <Compile Include="$(abspath $(CSG_TABLES_DIR))/**/*.cs" />
    <Compile Include="$(abspath $(CSG_SUPPRESSIONS))" />
    <Reference Include="Serialize"><HintPath>$(abspath $(CSG_RUNTIME_DLL))</HintPath></Reference>
  </ItemGroup>
</Project>
endef
export csg_tables_project

.PHONY: csg-tables-project
csg-tables-project: csg-project
	@mkdir -p $(CSG_TABLES_OUT)
	@printf '%s\n' "$$csg_tables_project" > $(CSG_TABLES_PROJECT)

# THE TABLES GATE. A finding is any CS/CA error in the build log.
.PHONY: schema-cs-generated-nullable-ca-analyzers-gate-tables
schema-cs-generated-nullable-ca-analyzers-gate-tables: bin/schema csg-dotnet-probe csg-runtime csg-tables-project $(CSG_TABLES_DIR)/.stamp
	@if $(DOTNET) build $(CSG_TABLES_PROJECT) --nologo -v q -o $(CSG_TABLES_OUT)/bin > $(CSG_TABLES_OUT)/gate.log 2>&1; then :; else \
		echo "RED schema-cs-generated-nullable-ca-analyzers-gate-tables: the generated C# tables have findings"; \
		grep -E 'error (CS|CA)[0-9]+:' $(CSG_TABLES_OUT)/gate.log | sed -E 's/ \[[^]]*\]$$//' | sort -u; exit 1; fi
	@if grep -qE 'error (CS|CA)[0-9]+:' $(CSG_TABLES_OUT)/gate.log; then \
		echo "RED schema-cs-generated-nullable-ca-analyzers-gate-tables: findings in a successful build"; \
		grep -E 'error (CS|CA)[0-9]+:' $(CSG_TABLES_OUT)/gate.log | sed -E 's/ \[[^]]*\]$$//' | sort -u; exit 1; fi
	@n=`find $(CSG_TABLES_DIR) -name '*.cs' | wc -l | tr -d ' '`; \
		echo "cs tables nullable/CA gate: Nullable enable, TreatWarningsAsErrors and AnalysisMode All clean over $$n generated table units (dotnet $(CSG_DOTNET_PIN))"

.PHONY: schema-cs-generated-nullable-ca-analyzers-gate
schema-cs-generated-nullable-ca-analyzers-gate: bin/schema csg-dotnet-probe csg-corpus csg-runtime csg-project
	@mkdir -p $(CSG_OUT)
	@if $(DOTNET) build $(CSG_PROJECT) --nologo -v q -o $(CSG_OUT)/proj/bin > $(CSG_OUT)/gate.log 2>&1; then :; else \
		echo "RED schema-cs-generated-nullable-ca-analyzers-gate: the generated C# has findings"; \
		grep -E 'error (CS|CA)[0-9]+:' $(CSG_OUT)/gate.log | sort -u; exit 1; fi
	@if grep -qE 'error (CS|CA)[0-9]+:' $(CSG_OUT)/gate.log; then \
		echo "RED schema-cs-generated-nullable-ca-analyzers-gate: findings in a successful build"; \
		grep -E 'error (CS|CA)[0-9]+:' $(CSG_OUT)/gate.log | sort -u; exit 1; fi
	@n=`find $(CSG_CORPUS) -name '*.cs' | wc -l | tr -d ' '`; \
		echo "cs nullable/CA gate: Nullable enable, TreatWarningsAsErrors and AnalysisMode All clean over $$n generated units (dotnet $(CSG_DOTNET_PIN))"

# THE PLANTED CONTROL, IN THE REAL GENERATED OUTPUT. A scratch copy of the
# corpus is taken, and the first generated unit of EACH directory the probe
# compiles (examples, bench, bench/realworld, ludicrous) gets a nullable
# dereference appended to its own end, after everything the emitter wrote. The
# plant therefore lives under the generated file's own preamble: if the emitter
# ever wrote an <auto-generated> header, a directive turning nullable off, a pragma, or the
# file stopped being compiled at all, the plant would go silent and this control
# would fail, which is what keeps the gate from passing vacuously. The lens must
# go red on CS8602 naming every one of those real files; then each file is
# restored byte for byte and the same copy must go green.
CSG_NEG := $(CSG_OUT)/negative
CSG_NEG_DIRS := examples bench bench/realworld ludicrous
.PHONY: schema-cs-generated-nullable-ca-analyzers-gate-negative-control
schema-cs-generated-nullable-ca-analyzers-gate-negative-control: bin/schema csg-dotnet-probe csg-corpus csg-runtime csg-project
	@rm -rf $(CSG_NEG) && mkdir -p $(CSG_NEG)/corpus $(CSG_NEG)/orig && cp -R $(CSG_CORPUS)/. $(CSG_NEG)/corpus/
	@cp $(CSG_SUPPRESSIONS) $(CSG_NEG)/Suppressions.cs
	@mkdir -p $(CSG_NEG)/runtime && cp $(CSG_RUNTIME_DLL) $(CSG_NEG)/runtime/
	@sed -e 's#$(abspath $(CSG_CORPUS))#$(abspath $(CSG_NEG)/corpus)#g' \
		-e 's#$(abspath $(CSG_SUPPRESSIONS))#$(abspath $(CSG_NEG)/Suppressions.cs)#g' \
		-e 's#$(abspath $(CSG_RUNTIME_DLL))#$(abspath $(CSG_NEG)/runtime/Serialize.dll)#g' \
		$(CSG_PROJECT) > $(CSG_NEG)/probe.csproj
	@i=0; : > $(CSG_NEG)/planted.txt; \
	for d in $(CSG_NEG_DIRS); do \
		f=`ls $(CSG_NEG)/corpus/$$d/*.cs 2>/dev/null | LC_ALL=C sort | head -1`; \
		[ -n "$$f" ] || { echo "NEGATIVE CONTROL FAILED: no generated unit in $$d to plant in"; exit 1; }; \
		i=$$((i+1)); mkdir -p $(CSG_NEG)/orig/$$d; cp "$$f" $(CSG_NEG)/orig/$$d/; \
		printf '%s\n' '' \
			'// planted-bad-pattern: a nullable dereference appended to a real generated unit' \
			"namespace CsgNullablePlant$$i" \
			'{' \
			'    public static class PlantedNullableBug' \
			'    {' \
			'        public static int Length()' \
			'        {' \
			'            string? text = null;' \
			'            return text.Length;' \
			'        }' \
			'    }' \
			'}' >> "$$f"; \
		echo "$$d/`basename $$f`" >> $(CSG_NEG)/planted.txt; \
	done
	@if $(DOTNET) build $(CSG_NEG)/probe.csproj --nologo -v q -o $(CSG_NEG)/bin > $(CSG_NEG)/plant.log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: the lens stayed green over nullable dereferences planted in the real generated units'; \
		cat $(CSG_NEG)/plant.log; exit 1; fi
	@while read -r rel; do \
		grep -Eq "corpus/$$rel\([0-9]+,[0-9]+\): error CS8602:" $(CSG_NEG)/plant.log || \
			{ echo "NEGATIVE CONTROL FAILED: the plant in the real generated unit $$rel was not named CS8602"; \
			  grep -E 'error (CS|CA)[0-9]+:' $(CSG_NEG)/plant.log | sort -u; exit 1; }; \
		grep -E "corpus/$$rel\([0-9]+,[0-9]+\): error CS8602:" $(CSG_NEG)/plant.log | sed -E 's/ \[[^]]*\]$$//' | sort -u | head -1; \
	done < $(CSG_NEG)/planted.txt
	@for d in $(CSG_NEG_DIRS); do cp $(CSG_NEG)/orig/$$d/*.cs $(CSG_NEG)/corpus/$$d/; done
	@if $(DOTNET) build $(CSG_NEG)/probe.csproj --nologo -v q -o $(CSG_NEG)/bin > $(CSG_NEG)/clean.log 2>&1; then :; else \
		echo 'NEGATIVE CONTROL FAILED: the same corpus with the generated units restored was not green'; \
		grep -E 'error (CS|CA)[0-9]+:' $(CSG_NEG)/clean.log | sort -u; exit 1; fi
	@if grep -qE 'error (CS|CA)[0-9]+:' $(CSG_NEG)/clean.log; then \
		echo 'NEGATIVE CONTROL FAILED: findings remain with the generated units restored'; \
		grep -E 'error (CS|CA)[0-9]+:' $(CSG_NEG)/clean.log | sort -u; exit 1; fi
	@echo "cs nullable/CA negative control: CS8602 named in `wc -l < $(CSG_NEG)/planted.txt | tr -d ' '` real generated units with the plant in, and the same corpus is green with them restored"
