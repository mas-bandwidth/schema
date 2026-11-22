# make/unity.mk — the Unity C# leg (--lang unity, docs/UNITY.md;
# docs/CONTRIBUTING.md, "Adding a language"). Included by the Makefile's
# wildcard include; the Makefile names no language. The leg registers itself at
# the end of this file.
#
# The packet types, and the table core (internal/codegen/unitytable): bool and
# integer fields on the form-1 file, every other construct by descriptor only.
# Every harness here compiles the generated code as a netstandard2.1 library at
# C# 9 with warnings as errors (test/unity/UnityDialect.props) — the surface
# and the language version Unity 2021.2 through Unity 6 compile — and drives it
# from a small .NET host, because no Unity editor runs in CI.

# The same pinned SDK command the C# leg uses (make/cs.mk); the Unity dialect
# is a TargetFramework and a LangVersion inside it, not another toolchain.
DOTNET ?= dotnet

.PHONY: toolchain-unity
toolchain-unity:
	@$(call toolchain_probe,unity,DOTNET,$(DOTNET))

# ---- the committed corpus ----

generated/unity/.stamp: bin/schema $(SCHEMAS)
	./bin/schema generate --lang unity --out generated/unity examples
	@touch $@

generated/unity-ludicrous/.stamp: bin/schema $(SCHEMAS128)
	./bin/schema generate --lang unity --out generated/unity-ludicrous examples128
	@touch $@

# THE WIRE TEST (test/unity): every pinned instance of the corpus and the
# 128-bit corpus written and byte-compared against the C++-pinned goldens in
# testdata/wire, round-tripped, branch-zeroed and refused where hostile — in
# Debug, where the write contracts are live, and in Release, where they are
# gone. The buffer layout the Span views rest on is asserted first.
.PHONY: unity-wire unity-wire-negative-control
unity-wire: build/mustrun generated/unity/.stamp generated/unity-ludicrous/.stamp
	cd test/unity && ../../$(MUSTRUN) -name unity-wire-debug -count 1037 -- $(DOTNET) run -c Debug
	cd test/unity && ../../$(MUSTRUN) -name unity-wire-release -count 1039 -- $(DOTNET) run -c Release

# The wire test must go red on one wrong wire constant in the generated code.
UNITY_WIRE_NC := build/unity-wire-negative
unity-wire-negative-control: generated/unity/.stamp generated/unity-ludicrous/.stamp
	@rm -rf $(UNITY_WIRE_NC) && mkdir -p $(UNITY_WIRE_NC)/generated
	@cp generated/unity/*.cs $(UNITY_WIRE_NC)/generated/
	@sed -i.orig 's/ulong f0 = (171UL)/ulong f0 = (170UL)/' $(UNITY_WIRE_NC)/generated/Wire.cs
	@cmp -s generated/unity/Wire.cs $(UNITY_WIRE_NC)/generated/Wire.cs && { echo 'NEGATIVE CONTROL: the planted constant patched nothing'; exit 1; } || true
	@rm -f $(UNITY_WIRE_NC)/generated/*.orig
	$(DOTNET) build test/unity/unitytest.csproj -c Debug -o $(UNITY_WIRE_NC)/bin --nologo -v quiet \
		-p:UnityGeneratedDir="$(CURDIR)/$(UNITY_WIRE_NC)/generated"
	@if ( cd test/unity && $(DOTNET) ../../$(UNITY_WIRE_NC)/bin/unitytest.dll ) > $(UNITY_WIRE_NC)/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: a wrong wire constant passed the Unity wire test'; exit 1; fi
	@grep -Fq 'FAILED: wire golden probe_header' $(UNITY_WIRE_NC)/log || \
		{ echo 'NEGATIVE CONTROL FAILED: the Unity wire test failed for another reason'; cat $(UNITY_WIRE_NC)/log; exit 1; }
	@echo 'unity wire negative control: a wrong const byte fails the C++-pinned golden'

# ---- packet defaults: string, bytes and flags defaults, the construction form ----

build/packet-defaults/unity/.stamp: bin/schema test/packet-defaults/Defaults.schema test/packet-defaults/Plain.schema make/unity.mk
	./bin/schema generate --lang unity --out build/packet-defaults/unity/defaults test/packet-defaults/Defaults.schema
	./bin/schema generate --lang unity --out build/packet-defaults/unity/plain test/packet-defaults/Plain.schema
	@touch $@

.PHONY: packet-defaults-unity packet-defaults-unity-negative-control
packet-defaults-unity: build/packet-defaults/unity/.stamp
	$(DOTNET) run --project test/packet-defaults/unity/packet-defaults-unity.csproj -- testdata/wire/packet-defaults
	$(DOTNET) run --configuration Release --project test/packet-defaults/unity/packet-defaults-unity.csproj -- testdata/wire/packet-defaults

# Init without its byte defaults must fail the construction-form check.
UNITY_DEFAULTS_NC := build/packet-defaults/unity-negative
packet-defaults-unity-negative-control: build/packet-defaults/unity/.stamp
	@rm -rf $(UNITY_DEFAULTS_NC) && mkdir -p $(UNITY_DEFAULTS_NC)/generated
	@cp build/packet-defaults/unity/defaults/*.cs $(UNITY_DEFAULTS_NC)/generated/
	@sed -i.orig '/value\.Name\.E[0-9]* = 0x/d' $(UNITY_DEFAULTS_NC)/generated/Defaults.cs
	@cmp -s build/packet-defaults/unity/defaults/Defaults.cs $(UNITY_DEFAULTS_NC)/generated/Defaults.cs && { echo 'NEGATIVE CONTROL: the planted removal patched nothing'; exit 1; } || true
	@rm -f $(UNITY_DEFAULTS_NC)/generated/*.orig
	$(DOTNET) build test/packet-defaults/unity/packet-defaults-unity.csproj -o $(UNITY_DEFAULTS_NC)/bin --nologo -v quiet \
		-p:DefaultsDir="$(CURDIR)/$(UNITY_DEFAULTS_NC)/generated"
	@if $(DOTNET) $(UNITY_DEFAULTS_NC)/bin/packet-defaults-unity.dll testdata/wire/packet-defaults > $(UNITY_DEFAULTS_NC)/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: missing default bytes passed in Unity C#'; exit 1; fi
	@grep -Fq 'FAILED: packet-default constructor bytes' $(UNITY_DEFAULTS_NC)/log || \
		{ echo 'NEGATIVE CONTROL FAILED: Unity C# failed for another reason'; cat $(UNITY_DEFAULTS_NC)/log; exit 1; }
	@echo 'packet defaults Unity negative control: missing default bytes fail the construction-form check'

# ---- packet UTF-8: the read refusals agree with the C++ oracle ----

build/packet-text/unity/.stamp: bin/schema test/packet-text/Narrow.schema
	./bin/schema generate --lang unity --out build/packet-text/unity test/packet-text/Narrow.schema
	@touch $@

.PHONY: packet-utf8-unity packet-utf8-unity-negative-control
packet-utf8-unity: build/packet-text/unity/.stamp build/packet-text/cpp/driver build/packet-text/harness
	$(DOTNET) build test/packet-text/unity/packet-text-unity.csproj -c Debug -o build/packet-text/unity-debug --nologo -v quiet
	./build/packet-text/harness dotnet build/packet-text/unity-debug/packet-text-unity.dll
	$(DOTNET) build test/packet-text/unity/packet-text-unity.csproj -c Release -o build/packet-text/unity-release --nologo -v quiet
	./build/packet-text/harness dotnet build/packet-text/unity-release/packet-text-unity.dll

# With the UTF-8 content check removed, the bit-flip sweep must disagree.
UNITY_TEXT_NC := build/packet-text/unity-negative
packet-utf8-unity-negative-control: build/packet-text/unity/.stamp build/packet-text/cpp/driver build/packet-text/harness
	@rm -rf $(UNITY_TEXT_NC) && mkdir -p $(UNITY_TEXT_NC)/generated
	@cp build/packet-text/unity/*.cs $(UNITY_TEXT_NC)/generated/
	@sed -i.orig 's/if (!ValidUtf8Text(bytes))/if (bytes.Length < 0)/' $(UNITY_TEXT_NC)/generated/Narrow.cs
	@cmp -s build/packet-text/unity/Narrow.cs $(UNITY_TEXT_NC)/generated/Narrow.cs && { echo 'NEGATIVE CONTROL: the planted removal patched nothing'; exit 1; } || true
	@rm -f $(UNITY_TEXT_NC)/generated/*.orig
	$(DOTNET) build test/packet-text/unity/packet-text-unity.csproj -c Release -o $(UNITY_TEXT_NC)/bin --nologo -v quiet \
		-p:TextDir="$(CURDIR)/$(UNITY_TEXT_NC)/generated"
	@if ./build/packet-text/harness -mutations-only dotnet $(UNITY_TEXT_NC)/bin/packet-text-unity.dll > $(UNITY_TEXT_NC)/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: Unity C# UTF-8 removal passed'; exit 1; fi
	@grep -Fq 'FAILED: packet-text verdict on ' $(UNITY_TEXT_NC)/log || { cat $(UNITY_TEXT_NC)/log; exit 1; }
	@echo 'packet UTF-8 Unity negative control: removed read validation fails bit-flip agreement'

# ---- packet write refusal: the write-refusal-packet surface (SPEC §5.1) ----

# The packet Write on a generated type with one field per row. Unity C# is an
# assert leg, so the driver runs a Debug build where each check is a
# Debug.Assert, answered through a throwing TraceListener; a refused value
# must trap and its lawful twin at the same boundary must be written.
build/packet-write-refusal/unity/.stamp: bin/schema test/packet-write-refusal/RefusalPacket.schema make/unity.mk
	@rm -rf build/packet-write-refusal/unity && mkdir -p build/packet-write-refusal/unity
	./bin/schema generate --lang unity --out build/packet-write-refusal/unity test/packet-write-refusal/RefusalPacket.schema
	@touch $@

.PHONY: packet-write-refusal-unity packet-write-refusal-unity-negative-control
packet-write-refusal-unity: build/packet-write-refusal/unity/.stamp
	$(DOTNET) build test/packet-write-refusal/unity/packet-write-refusal-unity.csproj -c Debug -o build/packet-write-refusal/unity-debug --nologo -v quiet
	$(DOTNET) build/packet-write-refusal/unity-debug/packet-write-refusal-unity.dll test/packet-write-refusal/cases.txt

# One upper bound moved in a copy of the generated code must turn the boundary
# case red, by name, while every other case stays green.
UNITY_WRITE_REFUSAL_NC := build/packet-write-refusal/unity-negative
packet-write-refusal-unity-negative-control: build/packet-write-refusal/unity/.stamp
	@rm -rf $(UNITY_WRITE_REFUSAL_NC) && mkdir -p $(UNITY_WRITE_REFUSAL_NC)/generated
	@cp build/packet-write-refusal/unity/*.cs $(UNITY_WRITE_REFUSAL_NC)/generated/
	@sed -i.orig 's/value\.Scalar >= 0 \&\& value\.Scalar <= 10/value.Scalar >= 0 \&\& value.Scalar < 10/' $(UNITY_WRITE_REFUSAL_NC)/generated/RefusalPacket.cs
	@cmp -s build/packet-write-refusal/unity/RefusalPacket.cs $(UNITY_WRITE_REFUSAL_NC)/generated/RefusalPacket.cs && { echo 'NEGATIVE CONTROL: the planted bound patched nothing'; exit 1; } || true
	@rm -f $(UNITY_WRITE_REFUSAL_NC)/generated/*.orig
	$(DOTNET) build test/packet-write-refusal/unity/packet-write-refusal-unity.csproj -c Debug -o $(UNITY_WRITE_REFUSAL_NC)/bin --nologo -v quiet \
		-p:RefusalDir="$(CURDIR)/$(UNITY_WRITE_REFUSAL_NC)/generated"
	@if $(DOTNET) $(UNITY_WRITE_REFUSAL_NC)/bin/packet-write-refusal-unity.dll test/packet-write-refusal/cases.txt > $(UNITY_WRITE_REFUSAL_NC)/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: a moved upper bound passed the Unity packet write-refusal driver'; exit 1; fi
	@grep -Fq 'FAILED: scalar_at_max lawful value at the boundary was refused' $(UNITY_WRITE_REFUSAL_NC)/log || \
		{ echo 'NEGATIVE CONTROL FAILED: the boundary case scalar_at_max was not the failure'; cat $(UNITY_WRITE_REFUSAL_NC)/log; exit 1; }
	@test "$$(grep -c '^FAILED: ' $(UNITY_WRITE_REFUSAL_NC)/log)" = 1 || \
		{ echo 'NEGATIVE CONTROL FAILED: a case other than the boundary went red'; cat $(UNITY_WRITE_REFUSAL_NC)/log; exit 1; }
	@grep -m1 '^FAILED: ' $(UNITY_WRITE_REFUSAL_NC)/log
	@echo 'packet write-refusal Unity negative control: a moved upper bound turns the boundary case red and nothing else'

# ---- the leg ----

.PHONY: test-unity
test-unity: toolchain-unity
	$(MAKE) unity-wire
	$(MAKE) unity-wire-negative-control
	$(MAKE) packet-defaults-unity
	$(MAKE) packet-defaults-unity-negative-control
	$(MAKE) packet-utf8-unity
	$(MAKE) packet-utf8-unity-negative-control
	$(MAKE) packet-write-refusal-unity
	$(MAKE) packet-write-refusal-unity-negative-control

TEST_LEGS          += test-unity
TOOLCHAIN_LEGS     += unity
TOOLCHAIN_PINS_unity := DOTNET
REGISTRY_NA        += bench-tables:unity goldens:unity
REGISTRY_NA_WHY.bench-tables.unity := bench/tables/unity/leg build generates and builds its own unit, so the aggregate has no prerequisite to add
REGISTRY_NA_WHY.goldens.unity      := no committed goldens of any kind: its generated tables are checked against the shared wire goldens (testdata/wire/tables)

# ---- the table core ----

# THE TABLE CORE GATE (compiler/tablesunity_test.go): the surface, then the
# generated tables compiled in the Unity dialect and driven by the conformance
# driver frame (test/conformance/unity) against the Go engine over a seeded
# corpus and its mutants — verdicts, counters and re-saved bytes — with each
# refusal class sabotaged in a copy and required to go red by name. It needs
# serialize.cs beside the tree (make deps).
.PHONY: unity-tables
unity-tables: toolchain-unity
	SCHEMA_SLOW=1 go test -count=1 -run '^TestUnityTable' ./compiler/

test-unity: unity-tables

# ---- the fixed form (form 3) ----

# THE FIXED-FORM GATE (internal/codegen/unitytable, compiler/unityfixedform_test.go):
# every fixed-table value, container, union, optional, default, evolution and
# hostile-input row compiled in the Unity dialect (C# 9 over netstandard2.1,
# warnings as errors, unsafe disallowed) and proved against the live C++
# reference and build/fixedform-corpus, each with its planted negative control.
.PHONY: unity-fixed
unity-fixed: toolchain-unity build/fixedform-corpus/.stamp
	SCHEMA_SLOW=1 SCHEMA_REQUIRE_CORPUS=1 go test -count=1 -run '^TestUnityFixed' ./compiler/ ./internal/codegen/unitytable/

test-unity: unity-fixed

# ---- variable tables, blocks, cooks, retain, JSON & reflection ----

.PHONY: unity-vt
unity-vt: toolchain-unity
	SCHEMA_SLOW=1 go test -count=1 -run '^TestUnityVT' ./internal/codegen/unitytable/

test-unity: unity-vt

# ---- tables conformance & release wire-fuzz ----

# Every MANIFEST.txt unit plus tables/cards compiled in the Unity dialect
# (test/conformance/unity/unit/schemaunit.csproj: C# 9, netstandard2.1,
# AllowUnsafeBlocks=false, Debug so §5.1 write assertions trap), hosted by
# build/conformance-unity/driver/unityconformance.dll.
build/conformance-unity/.stamp: bin/schema testdata/conformance/tables/MANIFEST.txt tables/cards/Card.schema make/unity.mk $(CONFORMANCE_UNIT_SCHEMAS) test/conformance/unity/unityconformance.csproj test/conformance/unity/unit/schemaunit.csproj test/conformance/unity/src/Driver.cs test/conformance/unity/src/Surfaces.cs
	@rm -rf build/conformance-unity && mkdir -p build/conformance-unity
	$(DOTNET) build test/conformance/unity/unityconformance.csproj -c Release \
		-p:ArtifactsPath="$(CURDIR)/build/conformance-unity/_art_driver" \
		-o build/conformance-unity/driver --nologo -v quiet
	$(DOTNET) restore test/conformance/unity/unit/schemaunit.csproj \
		-p:ArtifactsPath="$(CURDIR)/build/conformance-unity/_art_restore" --nologo -v quiet
	@python3 -c 'import concurrent.futures,os,subprocess,sys; dotnet,cur=sys.argv[1],sys.argv[2]; units=[(p[1],p[2:]) for l in open("testdata/conformance/tables/MANIFEST.txt") for p in [l.split()] if p and p[0]=="unit"]+[("cards",["tables/cards"])]; run=lambda u:(os.makedirs(f"build/conformance-unity/_gen/{u[0]}",exist_ok=True),subprocess.run(["./bin/schema","generate","--lang","unity","--out",f"build/conformance-unity/_gen/{u[0]}",*u[1]],check=True),subprocess.run([dotnet,"build","test/conformance/unity/unit/schemaunit.csproj","-c","Debug",f"-p:UnityUnitDir={cur}/build/conformance-unity/_gen/{u[0]}",f"-p:ArtifactsPath={cur}/build/conformance-unity/_art/{u[0]}","-o",f"build/conformance-unity/{u[0]}","--nologo","-v","quiet"],check=True,stdout=subprocess.DEVNULL)); list(concurrent.futures.ThreadPoolExecutor(max_workers=8).map(run,units))' "$(DOTNET)" "$(CURDIR)"
	@touch $@

CONFORMANCE_LEGS   += $(call unless_skipped,unity,build/conformance-unity/.stamp)
