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

# THE PLANTED CONTROL. A scratch copy of the same corpus gains one file, a
# nullable dereference the lens must name; the gate is re-run over that copy and
# must go red on CS8602 naming PlantedNullableBug.cs, then the plant is removed
# and the same copy must go green. A gate that stayed green, or went red for
# another reason, fails the control.
CSG_NEG := $(CSG_OUT)/negative
.PHONY: schema-cs-generated-nullable-ca-analyzers-gate-negative-control
schema-cs-generated-nullable-ca-analyzers-gate-negative-control: bin/schema csg-dotnet-probe csg-corpus csg-runtime csg-project
	@rm -rf $(CSG_NEG) && mkdir -p $(CSG_NEG)/corpus && cp -R $(CSG_CORPUS)/. $(CSG_NEG)/corpus/
	@cp $(CSG_SUPPRESSIONS) $(CSG_NEG)/Suppressions.cs
	@mkdir -p $(CSG_NEG)/runtime && cp $(CSG_RUNTIME_DLL) $(CSG_NEG)/runtime/
	@sed -e 's#$(abspath $(CSG_CORPUS))#$(abspath $(CSG_NEG)/corpus)#g' \
		-e 's#$(abspath $(CSG_SUPPRESSIONS))#$(abspath $(CSG_NEG)/Suppressions.cs)#g' \
		-e 's#$(abspath $(CSG_RUNTIME_DLL))#$(abspath $(CSG_NEG)/runtime/Serialize.dll)#g' \
		$(CSG_PROJECT) > $(CSG_NEG)/probe.csproj
	@printf '%s\n' \
		'namespace Bench' \
		'{' \
		'    // planted-bad-pattern: a nullable dereference the analyzer must name' \
		'    public sealed class PlantedNullableBug' \
		'    {' \
		'        public static int Length()' \
		'        {' \
		'            string? text = null;' \
		'            return text.Length;' \
		'        }' \
		'    }' \
		'}' > $(CSG_NEG)/corpus/bench/PlantedNullableBug.cs
	@if $(DOTNET) build $(CSG_NEG)/probe.csproj --nologo -v q -o $(CSG_NEG)/bin > $(CSG_NEG)/plant.log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: the lens stayed green over a planted nullable dereference'; \
		cat $(CSG_NEG)/plant.log; exit 1; fi
	@grep -Eq 'PlantedNullableBug\.cs.* error CS8602:' $(CSG_NEG)/plant.log || \
		{ echo 'NEGATIVE CONTROL FAILED: the planted nullable dereference was not named'; cat $(CSG_NEG)/plant.log; exit 1; }
	@rm -f $(CSG_NEG)/corpus/bench/PlantedNullableBug.cs
	@if $(DOTNET) build $(CSG_NEG)/probe.csproj --nologo -v q -o $(CSG_NEG)/bin > $(CSG_NEG)/clean.log 2>&1; then :; else \
		echo 'NEGATIVE CONTROL FAILED: the same corpus without the plant was not green'; \
		grep -E 'error (CS|CA)[0-9]+:' $(CSG_NEG)/clean.log | sort -u; exit 1; fi
	@grep -E 'PlantedNullableBug\.cs.* error CS8602:' $(CSG_NEG)/plant.log | head -1
	@echo 'cs nullable/CA negative control: PlantedNullableBug.cs is named CS8602 with the plant in and the same corpus is green with it out'
