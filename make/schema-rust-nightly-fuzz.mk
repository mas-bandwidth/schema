# make/schema-rust-nightly-fuzz.mk — nightly coverage-guided fuzzing of the
# Rust port's generated readers (schema-rust-nightly-fuzz, 2026-10-18).
#
# The fuzzer is cargo-fuzz (libFuzzer) on a pinned nightly toolchain. It is a
# FUNCTIONAL gate with a nightly time budget, never per push. The fast half is
# the replay of every saved regression input: no clock, no mutation, built with
# the host cargo (it needs neither nightly nor cargo-fuzz).
#
# THE CORPUS IS THE PORT'S OWN. Every table unit a Rust generate target in
# make/rust.mk reads: $(RUST_TABLE_UNITS) (the tables stamp), the paired bench
# unit $(RUST_TABLE_BENCH_SCHEMAS) (the same stamp), test/tables/RT1.schema
# (tblrt1) and tables/cards (cards-rust-generate), plus tables/arms. No second
# corpus is invented: the list is written to $(SCHEMA_RUST_FUZZ_DIR)/CORPUS.txt
# and the harness reads that file. A unit is <crate>:<path>[+<path>...].
#
# EVERY READER ENTRY POINT IS FUZZED. The manifest READERS.txt is grepped out of
# the generated sources, one row per entry point, and the harness is generated
# from it. Then a COVERAGE GATE lists every pub fn of the generated corpus that
# takes input bytes or text (&[u8], &str, *const u8): each one is a manifest
# row or matches $(SCHEMA_RUST_FUZZ_HELPERS), the justified list of helpers the
# roots call. Anything else fails the harness build by name, so a reader the
# emitter adds tomorrow is fuzzed or justified before the nightly can pass.
# The reader kinds, one fuzz_target each:
#   load     <root>_load, <root>_load_retain, <root>_load_inner (both retain
#            arms) and <root>_load_measure          (variable wire)
#   strict   <root>_fixed_load, TableFixedBlock::parse (fixed form, strict read)
#   text     <root>_from_json, <enum>_from_json_name, table_desc
#   message  announce_read then <root>_load_messages (message form)
#   block    <T>Block::open and ::open_reason       (block form)
#   cook     <T>Cook::open and table_cook_open_reason (cook form)
#
# THE ORACLE (fuzz/src/lib.rs): no crash, no hang, no sanitizer or assert
# finding, and a refusal is a clean typed answer. A reader that returns a value
# while its report says refused PANICS (it is never only logged), and so does a
# None with no reason named (malformed, refused, or the -1 of load_measure that
# SPEC-TABLES.md §6.5 reads with no report). malformed with a value is §4's
# tolerant read and is not a finding. load, load_retain and load_inner
# must agree; a delivered value has a non-negative load_measure; a fixed or
# message count fits the caller's storage; a block or cook open agrees with its
# open_reason; an opened cook region lies inside the input. The measure check
# holds only a root whose load_inner itself consults load_measure (the
# `measured` rows): a FIXED-CLASS root's form-1 load never does
# (internal/codegen/rusttable/variablecodec.go emits that call only for a
# non-fixed root), so the two answering differently there is by design.
#
# THE TOOLS ARE FLEET-WIDE, INSTALLED THROUGH rowan-tools/fleet (Glenn's
# standing grant), never by hand. A missing tool is reported BY NAME and is a
# not-done, never a faked pass. Pins: cargo-fuzz $(SCHEMA_RUST_FUZZ_VERSION)
# (crates.io, `cargo install cargo-fuzz --version ... --locked`) on rustup's
# $(SCHEMA_RUST_FUZZ_NIGHTLY) (https://rustup.rs). The runtime sibling
# serialize.rs is fetched at $(SERIALIZE_RS_PIN) from its official repository
# when the bench has no ../serialize.rs.

# The time budget is PER PORT, in seconds, split evenly over the reader kinds.
SCHEMA_RUST_FUZZ_TIME ?= 600
SCHEMA_RUST_FUZZ_VERSION ?= 0.13.1
SCHEMA_RUST_FUZZ_NIGHTLY ?= nightly-2026-10-01
# libFuzzer's per-input hang bound and memory bound: a hang is a finding.
SCHEMA_RUST_FUZZ_HANG ?= 10
SCHEMA_RUST_FUZZ_RSS ?= 2048
SCHEMA_RUST_FUZZ_DIR ?= build/schema-rust-nightly-fuzz
SCHEMA_RUST_FUZZ_PLANT_DIR ?= build/schema-rust-nightly-fuzz-plant
SCHEMA_RUST_FUZZ_CORPUS ?= test/rust-nightly-fuzz-corpus
SCHEMA_RUST_FUZZ_SEEDS ?= $(SCHEMA_RUST_FUZZ_DIR)/seeds

SERIALIZE_RS ?= ../serialize.rs
SERIALIZE_RS_PIN ?= v2.4.0
SERIALIZE_RS_REPO ?= https://github.com/mas-bandwidth/serialize.rs.git
SERIALIZE_RS_FETCHED ?= build/serialize.rs
SERIALIZE_RS_RUNTIME = $(if $(wildcard $(SERIALIZE_RS)/Cargo.toml),$(SERIALIZE_RS),$(SERIALIZE_RS_FETCHED))

# The port's own corpus (see the header). benchfixed is two schema files as one
# unit, exactly as the tables stamp generates it.
SCHEMA_RUST_FUZZ_SPACE := $(eval) $(eval)
SCHEMA_RUST_FUZZ_UNITS := $(RUST_TABLE_UNITS) \
	benchfixed:$(subst $(SCHEMA_RUST_FUZZ_SPACE),+,$(strip $(RUST_TABLE_BENCH_SCHEMAS))) \
	tblrt1:test/tables/RT1.schema cards:tables/cards armdemo:tables/arms

SCHEMA_RUST_FUZZ_TARGETS := load strict text message block cook
SCHEMA_RUST_FUZZ_PER_KIND = $(shell expr $(SCHEMA_RUST_FUZZ_TIME) / $(words $(SCHEMA_RUST_FUZZ_TARGETS)))

# THE JUSTIFIED HELPERS: pub fns of the generated corpus that take input bytes
# or text but are not reader entry points. Each is reached only through a root
# above, on offsets and lengths that root has already bounded, so fuzzing the
# roots fuzzes them on every input they can be handed by a reader:
#   *_load_body, *_fixed_scatter, *_fixed_read_body, *_decode: the node-level
#     bodies <root>_load / <root>_fixed_load call after the root's checks;
#   leb_read leb_skip table_scalar_read scalar_read_128 text_valid
#     table_wide_units table_wide_fit scan visit_blob retain_record_len
#     retain_capture: the variable runtime's cursor helpers under <root>_load;
#   json_escape json_b64 json_unb64 json_wide_clamp: the JSON codec's helpers
#     under <root>_from_json (escape and b64 are the writer's);
#   table_utf8_valid table_fixed_hash table_fixed_compile table_fixed_run: the
#     fixed runtime's hash, plan compile and plan run under <root>_fixed_load;
#   new: Jr::new and TableBitReader::new, the JSON and bit readers' constructors
#     over the input <root>_from_json and <root>_load_messages hand them;
#   table_message_utf8 table_message_utf8_clamp table_message_batch_open raw:
#     the message runtime's helpers under <root>_load_messages (raw is the bit
#     writer's);
#   table_block_read64 table_cook_read64 table_cook_write_record
#     table_cook_open: an unaligned load, a pointer read, the cook writer and
#     the body of every <T>Cook::open, under the block and cook opens.
SCHEMA_RUST_FUZZ_HELPERS := _load_body$$|_fixed_scatter$$|_fixed_read_body$$|_decode$$|^leb_read$$|^leb_skip$$|^table_scalar_read$$|^scalar_read_128$$|^text_valid$$|^table_wide_units$$|^table_wide_fit$$|^scan$$|^visit_blob$$|^retain_record_len$$|^retain_capture$$|^json_escape$$|^json_b64$$|^json_unb64$$|^json_wide_clamp$$|^table_utf8_valid$$|^table_fixed_hash$$|^table_message_utf8$$|^table_message_utf8_clamp$$|^table_message_batch_open$$|^raw$$|^table_fixed_compile$$|^table_fixed_run$$|^new$$|^table_block_read64$$|^table_cook_read64$$|^table_cook_write_record$$|^table_cook_open$$

# The runtime sibling: ../serialize.rs when it is a checkout, otherwise the pin
# fetched from the official repository (the mirror first, as the Makefile's
# runtime fetches do).
$(SERIALIZE_RS_FETCHED)/Cargo.toml:
	@rm -rf "$(SERIALIZE_RS_FETCHED)"
	@mkdir -p "$(dir $(SERIALIZE_RS_FETCHED))"
	@if test -n "$(NOVA_MIRRORS)" && test -d "$(NOVA_MIRRORS)/mas-bandwidth/serialize.rs.git" && \
		git clone --quiet --depth 1 --branch "$(SERIALIZE_RS_PIN)" "file://$(NOVA_MIRRORS)/mas-bandwidth/serialize.rs.git" "$(SERIALIZE_RS_FETCHED)"; then \
		echo "schema-rust-nightly-fuzz: fetched serialize.rs $(SERIALIZE_RS_PIN) from the mirror"; \
	else \
		rm -rf "$(SERIALIZE_RS_FETCHED)"; \
		git clone --quiet --depth 1 --branch "$(SERIALIZE_RS_PIN)" "$(SERIALIZE_RS_REPO)" "$(SERIALIZE_RS_FETCHED)" \
			|| { echo "tool missing: serialize.rs@$(SERIALIZE_RS_PIN) (no sibling $(SERIALIZE_RS) and the fetch of $(SERIALIZE_RS_REPO) failed)"; exit 2; }; \
		echo "schema-rust-nightly-fuzz: fetched serialize.rs $(SERIALIZE_RS_PIN)"; \
	fi
	@test -f "$@" || { echo "tool missing: serialize.rs@$(SERIALIZE_RS_PIN) (the fetch produced no $@)"; exit 2; }

.PHONY: schema-rust-nightly-fuzz-runtime
schema-rust-nightly-fuzz-runtime:
	@if [ ! -f "$(SERIALIZE_RS)/Cargo.toml" ] && [ ! -f "$(SERIALIZE_RS_FETCHED)/Cargo.toml" ]; then \
		$(MAKE) --no-print-directory "$(SERIALIZE_RS_FETCHED)/Cargo.toml" || exit 2; \
	fi

# The probe: every missing tool by name, exit 2, never a pass.
.PHONY: schema-rust-nightly-fuzz-tool
schema-rust-nightly-fuzz-tool: schema-rust-nightly-fuzz-runtime
	@missing=; \
	if ! command -v rustup >/dev/null 2>&1; then missing="$$missing rustup"; \
	elif ! rustup toolchain list 2>/dev/null | grep -q '^$(SCHEMA_RUST_FUZZ_NIGHTLY)'; then \
		missing="$$missing $(SCHEMA_RUST_FUZZ_NIGHTLY)"; fi; \
	if ! cargo fuzz --version >/dev/null 2>&1; then missing="$$missing cargo-fuzz@$(SCHEMA_RUST_FUZZ_VERSION)"; \
	elif ! cargo fuzz --version 2>/dev/null | grep -q ' $(SCHEMA_RUST_FUZZ_VERSION)'; then \
		missing="$$missing cargo-fuzz@$(SCHEMA_RUST_FUZZ_VERSION)(found:$$(cargo fuzz --version 2>/dev/null | tr ' ' '-'))"; fi; \
	if [ ! -f "$(SERIALIZE_RS_RUNTIME)/Cargo.toml" ]; then \
		missing="$$missing serialize.rs@$(SERIALIZE_RS_PIN)"; fi; \
	if [ -n "$$missing" ]; then \
		for name in $$missing; do echo "tool missing: $$name"; done; \
		echo "schema-rust-nightly-fuzz: install through rowan-tools/fleet: rustup (https://rustup.rs) with $(SCHEMA_RUST_FUZZ_NIGHTLY), and cargo-fuzz $(SCHEMA_RUST_FUZZ_VERSION) from crates.io (cargo install cargo-fuzz --version $(SCHEMA_RUST_FUZZ_VERSION) --locked)"; \
		exit 2; \
	fi

# The harness reads this file. It is generated, not a second hand-written list.
.PHONY: schema-rust-nightly-fuzz-corpus
schema-rust-nightly-fuzz-corpus:
	@mkdir -p $(SCHEMA_RUST_FUZZ_DIR)
	@printf '%s\n' $(SCHEMA_RUST_FUZZ_UNITS) > $(SCHEMA_RUST_FUZZ_DIR)/CORPUS.txt
	@set -e; while IFS=: read -r name paths; do \
		[ -n "$$name" ] || continue; \
		for path in $$(printf '%s' "$$paths" | tr '+' ' '); do \
			[ -e "$$path" ] || { echo "corpus entry $$name names missing path $$path"; exit 1; }; \
		done; \
	done < $(SCHEMA_RUST_FUZZ_DIR)/CORPUS.txt
	@echo "schema-rust-nightly-fuzz: corpus $(words $(SCHEMA_RUST_FUZZ_UNITS)) units -> $(SCHEMA_RUST_FUZZ_DIR)/CORPUS.txt"

# Seed the nightly fuzzer from the Go fuzz corpus (internal/fuzz/testdata, in
# `go test fuzz v1` encoding, decoded to the bytes it holds) and the
# conformance and wire vectors (raw files, the JSON ones seeding the text
# target). Seeds are copied, never referenced.
.PHONY: schema-rust-nightly-fuzz-seeds
schema-rust-nightly-fuzz-seeds:
	@rm -rf $(SCHEMA_RUST_FUZZ_SEEDS); mkdir -p $(SCHEMA_RUST_FUZZ_SEEDS)
	@find internal/fuzz/testdata -type f | sort | python3 -I -c 'import ast,hashlib,os,sys; out=sys.argv[1]; n=0;\
	exec("for p in sys.stdin.read().split():\n lines=open(p,encoding=\"utf-8\",errors=\"replace\").read().splitlines()\n if not lines or lines[0].strip()!=\"go test fuzz v1\": continue\n for v in lines[1:]:\n  v=v.strip(); k=v.find(\"(\")\n  if k<0 or not v.endswith(\")\"): continue\n  typ,lit=v[:k],v[k+1:-1]\n  try:\n   b=ast.literal_eval(\"b\"+lit) if typ==\"[]byte\" else (ast.literal_eval(lit).encode() if typ==\"string\" else None)\n  except Exception: b=None\n  if not b: continue\n  open(os.path.join(out,\"go-\"+hashlib.sha1(b).hexdigest()[:16]),\"wb\").write(b); n+=1\nprint(\"schema-rust-nightly-fuzz:\",n,\"seed(s) decoded from the Go fuzz corpus\")")' $(SCHEMA_RUST_FUZZ_SEEDS)
	@set -e; find testdata/wire testdata/conformance test/conformance tables/cards/conformance -type f \
		! -name '*.go' ! -name '*.rs' ! -name '*.md' ! -name '*.sh' 2>/dev/null | sort | while read -r f; do \
		cp "$$f" "$(SCHEMA_RUST_FUZZ_SEEDS)/v-$$(printf '%s' "$$f" | tr '/ ' '__')"; \
	done
	@n=$$(find $(SCHEMA_RUST_FUZZ_SEEDS) -type f | wc -l); [ "$$n" -gt 0 ] || { echo "schema-rust-nightly-fuzz: no seeds"; exit 1; }; \
	echo "schema-rust-nightly-fuzz: $$n seed(s) from the Go fuzz corpus and the conformance and wire vectors"

# One crate per unit, exactly the wiring make/rust.mk's stamp uses, so the
# generated readers under test are the ones the port ships.
.PHONY: schema-rust-nightly-fuzz-generate
schema-rust-nightly-fuzz-generate: bin/schema schema-rust-nightly-fuzz-corpus schema-rust-nightly-fuzz-runtime
	@set -e; mkdir -p $(SCHEMA_RUST_FUZZ_DIR)/corpus; \
	runtime="$(abspath $(SERIALIZE_RS_RUNTIME))"; \
	test -f "$$runtime/Cargo.toml" || { echo "tool missing: serialize.rs@$(SERIALIZE_RS_PIN)"; exit 2; }; \
	while IFS=: read -r name paths; do \
		[ -n "$$name" ] || continue; \
		rm -rf $(SCHEMA_RUST_FUZZ_DIR)/corpus/$$name/src; \
		mkdir -p $(SCHEMA_RUST_FUZZ_DIR)/corpus/$$name/src; \
		./bin/schema generate --lang rust --out $(SCHEMA_RUST_FUZZ_DIR)/corpus/$$name/src $$(printf '%s' "$$paths" | tr '+' ' '); \
		printf '[package]\nname = "%s"\nversion = "0.0.0"\nedition = "2024"\n\n[features]\ndefault = ["block", "cook"]\nblock = []\ncook = []\n\n[dependencies]\nserialize = { package = "serialize-official", path = "%s" }\n' \
			$$name "$$runtime" > $(SCHEMA_RUST_FUZZ_DIR)/corpus/$$name/Cargo.toml; \
	done < $(SCHEMA_RUST_FUZZ_DIR)/CORPUS.txt
	@# THE MANIFEST: one row per reader entry point, `kind unit name type`,
	@# grepped from the generated sources (no second list).
	@set -e; : > $(SCHEMA_RUST_FUZZ_DIR)/READERS.txt; \
	while IFS=: read -r name paths; do \
		[ -n "$$name" ] || continue; \
		awk -v u="$$name" ' \
			/^pub fn [a-z0-9_]+_load\(wire: &\[u8\], report: &mut TableReport\) -> Option<[A-Za-z0-9_]+>/ { s=$$0; sub(/^pub fn /,"",s); n=s; sub(/_load\(.*/,"",n); t=s; sub(/.*Option</,"",t); sub(/>.*/,"",t); print "load", u, n, t } \
			/^pub fn [a-z0-9_]+_load_retain\(wire: &\[u8\]/ { s=$$0; sub(/^pub fn /,"",s); sub(/_load_retain\(.*/,"",s); print "load_retain", u, s, "-" } \
			/^pub fn [a-z0-9_]+_load_inner\(wire: &\[u8\]/ { s=$$0; sub(/^pub fn /,"",s); sub(/_load_inner\(.*/,"",s); print "load_inner", u, s, "-" } \
			/^    if [a-z0-9_]+_load_measure\(wire\) < 0 \{ return None; \}/ { s=$$0; sub(/^    if /,"",s); sub(/_load_measure\(.*/,"",s); print "measured", u, s, "-" } \
			/^pub fn [a-z0-9_]+_load_measure\(wire: &\[u8\]\) -> i64/ { s=$$0; sub(/^pub fn /,"",s); sub(/_load_measure\(.*/,"",s); print "load_measure", u, s, "-" } \
			fixed != "" { if ($$0 ~ /^ *values: &mut \[[A-Za-z0-9_]+\],/) { t=$$0; sub(/^ *values: &mut \[/,"",t); sub(/\].*/,"",t); print "strict", u, fixed, t } fixed="" } \
			/^pub fn [a-z0-9_]+_fixed_load\($$/ { s=$$0; sub(/^pub fn /,"",s); sub(/_fixed_load\(.*/,"",s); fixed=s } \
			/^pub fn [a-z0-9_]+_from_json\(text: &str, report: &mut TableReport\) -> Option<[A-Za-z0-9_]+>/ { s=$$0; sub(/^pub fn /,"",s); sub(/_from_json\(.*/,"",s); print "text", u, s, "-" } \
			/^pub fn [a-z0-9_]+_from_json_name\(s: &str\)/ { s=$$0; sub(/^pub fn /,"",s); sub(/_from_json_name\(.*/,"",s); print "name", u, s, "-" } \
			/^pub fn table_desc\(name: &str\)/ { print "desc", u, "table_desc", "-" } \
			/^pub fn announce_read\($$/ { print "announce", u, "announce_read", "-" } \
			/^pub fn [a-z0-9_]+_load_messages\(values: &mut \[[A-Za-z0-9_]+\], vocabulary: &TableVocabulary, data: &\[u8\]/ { s=$$0; sub(/^pub fn /,"",s); n=s; sub(/_load_messages\(.*/,"",n); t=s; sub(/^[^[]*\[/,"",t); sub(/\].*/,"",t); print "message", u, n, t } \
			/^    pub unsafe fn open\(base: \*const u8, bytes: i64\) -> Option<[A-Za-z0-9_]+>/ { t=$$0; sub(/.*Option</,"",t); sub(/>.*/,"",t); lastblock=t; print "block", u, t, "-" } \
			/^    pub unsafe fn open_reason\(base: \*const u8, bytes: i64\)/ { print "block_reason", u, lastblock, "-" } \
			/^    pub unsafe fn open\(bytes: \*const u8, length: u64\) -> Option<[A-Za-z0-9_]+>/ { t=$$0; sub(/.*Option</,"",t); sub(/>.*/,"",t); print "cook", u, t, "-" } \
			/^    pub fn parse\(bytes: &'"'"'a \[u8\]\) -> Result<TableFixedBlock<'"'"'a>, TableFixedReason>/ { print "fixed_parse", u, "TableFixedBlock", "-" } \
			/^pub unsafe fn table_cook_open_reason\(bytes: \*const u8, length: u64, root_size: u64, root_align: u64\)/ { print "cook_reason", u, "table_cook_open_reason", "-" } \
		' $(SCHEMA_RUST_FUZZ_DIR)/corpus/$$name/src/*.rs >> $(SCHEMA_RUST_FUZZ_DIR)/READERS.txt; \
	done < $(SCHEMA_RUST_FUZZ_DIR)/CORPUS.txt; \
	r=$(SCHEMA_RUST_FUZZ_DIR)/READERS.txt; sort $$r | uniq -d | awk '{ $$1 = $$1 "_ambiguous"; print }' > $$r.amb; \
	sort -u $$r | awk 'NR == FNR { k = $$1; sub(/_ambiguous$$/, "", k); a[k " " $$2 " " $$3] = 1; next } !(($$1 " " $$2 " " $$3) in a)' $$r.amb - > $$r.one; \
	cat $$r.one $$r.amb | sort > $$r; rm -f $$r.one $$r.amb; \
	awk '$$1 ~ /_ambiguous$$/ { print "schema-rust-nightly-fuzz: NOT CALLABLE: " $$2 "::" $$3 " is emitted by two modules and glob-ambiguous at the crate root (a generator finding; its body is fuzzed through the root readers that call it)" }' $$r; \
	test -s $(SCHEMA_RUST_FUZZ_DIR)/READERS.txt || { echo "schema-rust-nightly-fuzz: no readers found in the corpus"; exit 1; }
	@$(MAKE) --no-print-directory schema-rust-nightly-fuzz-coverage
	@echo "schema-rust-nightly-fuzz: $$(wc -l < $(SCHEMA_RUST_FUZZ_DIR)/READERS.txt) reader entry points across $(words $(SCHEMA_RUST_FUZZ_TARGETS)) reader kinds"

# THE COVERAGE GATE. Every pub fn of the generated corpus taking input bytes or
# text is a manifest row or a justified helper; anything else is an UNFUZZED
# READER ENTRY POINT and fails by name. Signatures are joined across lines, so
# a multi-line signature (the fixed form's) is held like a one-line one.
.PHONY: schema-rust-nightly-fuzz-coverage
schema-rust-nightly-fuzz-coverage:
	@set -e; d=$(SCHEMA_RUST_FUZZ_DIR); : > $$d/INPUT-FNS.txt; \
	while IFS=: read -r name paths; do \
		[ -n "$$name" ] || continue; \
		awk -v u="$$name" ' \
			function flush(  p, n) { if (sig == "") return; p = sig; sub(/\{.*/, "", p); sub(/\)[^)]*->.*/, ")", p); \
				if (p ~ /&('"'"'[a-z]+ )?\[u8\]|&('"'"'[a-z]+ )?str|\*const u8/) { n = p; sub(/^ *pub (unsafe )?fn /, "", n); sub(/[(<].*/, "", n); print u, n } sig = "" } \
			FNR == 1 { flush() } \
			/^ *pub (unsafe )?fn [a-z0-9_]+[(<]/ { flush(); sig = $$0 } \
			sig != "" && sig != $$0 { sig = sig " " $$0 } \
			sig != "" && sig ~ /\{/ { flush() } \
			END { flush() } \
		' $$d/corpus/$$name/src/*.rs >> $$d/INPUT-FNS.txt; \
	done < $$d/CORPUS.txt; \
	awk ' \
		FNR == NR { k = $$1; u = $$2; n = $$3; sub(/_ambiguous$$/, "", k); \
			if (k == "load") have[u " " n "_load"] = 1; \
			else if (k == "load_retain") have[u " " n "_load_retain"] = 1; \
			else if (k == "load_inner") have[u " " n "_load_inner"] = 1; \
			else if (k == "load_measure") have[u " " n "_load_measure"] = 1; \
			else if (k == "strict") have[u " " n "_fixed_load"] = 1; \
			else if (k == "text") have[u " " n "_from_json"] = 1; \
			else if (k == "name") have[u " " n "_from_json_name"] = 1; \
			else if (k == "message") have[u " " n "_load_messages"] = 1; \
			else if (k == "desc" || k == "announce" || k == "cook_reason") have[u " " n] = 1; \
			else if (k == "measured") { } \
			else if (k == "fixed_parse") have[u " parse"] = 1; \
			else if (k == "block") { blocks[u]++ } \
			else if (k == "block_reason") { reasons[u]++ } \
			else if (k == "cook") { cooks[u]++ } \
			next } \
		{ u = $$1; n = $$2; key = u " " n; \
			if (key in have) next; \
			if (n ~ /$(SCHEMA_RUST_FUZZ_HELPERS)/) next; \
			if (n == "open") { opens[u]++; next } \
			if (n == "open_reason") { oreasons[u]++; next } \
			print "UNFUZZED READER ENTRY POINT: " u "::" n; bad++ } \
		END { for (u in opens) if (opens[u] != blocks[u] + cooks[u]) { print "UNFUZZED READER ENTRY POINT: " u " has " opens[u] " open()s and the manifest " blocks[u] + cooks[u]; bad++ } \
			for (u in oreasons) if (oreasons[u] != reasons[u]) { print "UNFUZZED READER ENTRY POINT: " u " has " oreasons[u] " open_reason()s and the manifest " reasons[u]; bad++ } \
			if (bad) { print "schema-rust-nightly-fuzz: " bad " reader entry point(s) neither fuzzed nor a justified helper (SCHEMA_RUST_FUZZ_HELPERS)"; exit 1 } } \
	' $$d/READERS.txt $$d/INPUT-FNS.txt
	@echo "schema-rust-nightly-fuzz: coverage gate: all $$(wc -l < $(SCHEMA_RUST_FUZZ_DIR)/INPUT-FNS.txt) input-taking pub fns are fuzzed entry points or justified helpers"

# The harness crate: fuzz/src/lib.rs holds one fuzz_<kind> per reader kind,
# every manifest row inside it with the oracle; each fuzz target and the replay
# binary call those functions, so the nightly and the replay can never drift.
.PHONY: schema-rust-nightly-fuzz-harness
schema-rust-nightly-fuzz-harness: schema-rust-nightly-fuzz-generate
	@$(MAKE) --no-print-directory schema-rust-nightly-fuzz-harness-only SCHEMA_RUST_FUZZ_DIR=$(SCHEMA_RUST_FUZZ_DIR)

.PHONY: schema-rust-nightly-fuzz-harness-only
schema-rust-nightly-fuzz-harness-only:
	@set -e; d=$(SCHEMA_RUST_FUZZ_DIR)/fuzz; m=$(SCHEMA_RUST_FUZZ_DIR)/READERS.txt; \
	rm -rf $$d/fuzz_targets $$d/src; mkdir -p $$d/fuzz_targets $$d/src; \
	{ printf '[package]\nname = "schema-rust-nightly-fuzz"\nversion = "0.0.0"\nedition = "2024"\n\n[package.metadata]\ncargo-fuzz = true\n\n[workspace]\n\n[profile.dev]\ndebug = false\n\n[lib]\nname = "schema_rust_nightly_fuzz"\npath = "src/lib.rs"\n\n[dependencies]\nlibfuzzer-sys = "0.4"\n'; \
	  while IFS=: read -r name paths; do [ -n "$$name" ] && printf '%s = { path = "../corpus/%s" }\n' "$$name" "$$name"; done < $(SCHEMA_RUST_FUZZ_DIR)/CORPUS.txt; \
	  for kind in $(SCHEMA_RUST_FUZZ_TARGETS); do printf '\n[[bin]]\nname = "%s"\npath = "fuzz_targets/%s.rs"\ntest = false\ndoc = false\nbench = false\n' $$kind $$kind; done; \
	  printf '\n[[bin]]\nname = "replay"\npath = "src/replay.rs"\ntest = false\ndoc = false\nbench = false\n'; \
	} > $$d/Cargo.toml; \
	for kind in $(SCHEMA_RUST_FUZZ_TARGETS); do \
		printf '// Generated by make/schema-rust-nightly-fuzz.mk. DO NOT EDIT.\n#![no_main]\nuse libfuzzer_sys::fuzz_target;\nfuzz_target!(|data: &[u8]| schema_rust_nightly_fuzz::fuzz_%s(data));\n' $$kind > $$d/fuzz_targets/$$kind.rs; \
	done; \
	{ \
	printf '%s\n' '// Generated by make/schema-rust-nightly-fuzz.mk from READERS.txt. DO NOT EDIT.' \
	  '#![allow(unused_mut, unused_variables, clippy::all)]' \
	  '/// THE ORACLE for a reader returning a value or nothing: a value delivered' \
	  '/// while the report says REFUSED is a refusal that returned a value, and' \
	  '/// nothing delivered with no reason named is a silent failure. Both PANIC,' \
	  '/// so libFuzzer saves the input and the replay fails on it. `named` is the' \
	  '/// report said malformed or refused, or (variable form only) the wire is' \
	  '/// one load_measure answers -1 for, which SPEC-TABLES.md §6.5 reads with' \
	  '/// no report "because nothing was read". malformed WITH a value is the' \
	  '/// tolerant read of §4 (damage counted, the default kept) and is no finding.' \
	  'pub fn check(entry: &str, delivered: bool, refused: bool, named: bool) {' \
	  '    if delivered && refused { panic!("{entry}: a refusal returned a value"); }' \
	  '    if !delivered && !named { panic!("{entry}: refused with no reason named (silent failure)"); }' \
	  '}' \
	  '/// A 64-aligned copy of the input, for the block and cook opens.' \
	  'pub fn aligned(data: &[u8]) -> (Vec<u64>, usize) {' \
	  '    let mut store = vec![0u64; data.len() / 8 + 16];' \
	  '    let base = store.as_ptr() as usize;' \
	  '    let off = (64 - base % 64) % 64;' \
	  '    unsafe { std::ptr::copy_nonoverlapping(data.as_ptr(), (store.as_mut_ptr() as *mut u8).add(off), data.len()); }' \
	  '    (store, off)' \
	  '}' \
	  'pub fn fuzz_load(data: &[u8]) {'; \
	awk 'FNR == NR { if ($$1 == "measured") measured[$$2 " " $$3] = 1; next } \
	  $$1 == "load" { u=$$2; n=$$3; \
	  printf "    {\n        let mut r = %s::TableReport::default();\n        let v = %s::%s_load(data, &mut r);\n        let m = %s::%s_load_measure(data);\n        check(\"%s::%s_load\", v.is_some(), r.refused, r.malformed || r.refused || m < 0);\n", u, u, n, u, n, u, n; \
	  printf "        let mut r2 = %s::TableReport::default();\n        let mut keep = %s::TableRetain::default();\n        let v2 = %s::%s_load_retain(data, &mut keep, &mut r2);\n        check(\"%s::%s_load_retain\", v2.is_some(), r2.refused, r2.malformed || r2.refused || m < 0);\n        if v2.is_some() != v.is_some() { panic!(\"%s::%s_load_retain disagrees with %s_load\"); }\n", u, u, u, n, u, n, u, n, n; \
	  printf "        let mut r3 = %s::TableReport::default();\n        let v3 = %s::%s_load_inner(data, None, &mut r3);\n        if v3.is_some() != v.is_some() || r3 != r { panic!(\"%s::%s_load_inner(None) disagrees with %s_load\"); }\n", u, u, n, u, n, n; \
	  printf "        let mut r4 = %s::TableReport::default();\n        let mut keep4 = %s::TableRetain::default();\n        let v4 = %s::%s_load_inner(data, Some(&mut keep4), &mut r4);\n        if v4.is_some() != v2.is_some() || r4 != r2 { panic!(\"%s::%s_load_inner(Some) disagrees with %s_load_retain\"); }\n", u, u, u, n, u, n, n; \
	  if ((u " " n) in measured) printf "        if v.is_some() && m < 0 { panic!(\"%s::%s_load_measure refused ({m}) a wire %s_load delivered\"); }\n", u, n, n; \
	  printf "    }\n" }' $$m $$m; \
	printf '%s\n' '}' 'pub fn fuzz_strict(data: &[u8]) {'; \
	awk '$$1 == "strict" { u=$$2; n=$$3; t=$$4; \
	  printf "    {\n        let mut values = vec![%s::%s::default(); 4];\n        let mut plan = vec![%s::TableFixedEntry::default(); 4096];\n        let mut remap = vec![0u16; 4096];\n        let mut r = %s::TableFixedReport::default();\n        let v = %s::%s_fixed_load(&mut values, data, &mut plan, &mut remap, &mut r);\n        check(\"%s::%s_fixed_load\", v.is_some(), r.refused, r.malformed || r.refused);\n        if let Some(k) = v { if k > values.len() { panic!(\"%s::%s_fixed_load delivered {k} records into {} slots\", values.len()); } }\n    }\n", u, t, u, u, u, n, u, n, u, n } \
	     $$1 == "fixed_parse" { printf "    let _ = %s::TableFixedBlock::parse(data);\n", $$2 }' $$m; \
	printf '%s\n' '}' 'pub fn fuzz_text(data: &[u8]) {' '    let Ok(text) = std::str::from_utf8(data) else { return };'; \
	awk '$$1 == "text" { u=$$2; n=$$3; printf "    {\n        let mut r = %s::TableReport::default();\n        let v = %s::%s_from_json(text, &mut r);\n        check(\"%s::%s_from_json\", v.is_some(), r.refused, r.malformed || r.refused);\n    }\n", u, u, n, u, n } \
	     $$1 == "name" { printf "    let _ = %s::%s_from_json_name(text);\n", $$2, $$3 } \
	     $$1 == "desc" { printf "    let _ = %s::table_desc(text);\n", $$2 }' $$m; \
	printf '%s\n' '}' \
	  '/// The first byte splits the input: that many bytes of announcement, the' \
	  '/// rest the batch read against the vocabulary the announcement left.' \
	  'pub fn fuzz_message(data: &[u8]) {' \
	  '    if data.is_empty() { return; }' \
	  '    let cut = 1 + (data[0] as usize).min(data.len() - 1);' \
	  '    let (announcement, batch) = (&data[1..cut], &data[cut..]);'; \
	awk '$$1 == "announce" { u=$$2; printf "    {\n        let mut entries = vec![%s::TableMessageEntry::default(); 256];\n        let mut vocabulary = %s::TableVocabulary::new(&mut entries);\n        let mut ra = %s::TableReport::default();\n        let announced = %s::announce_read(&mut vocabulary, announcement, &mut ra);\n        check(\"%s::announce_read\", announced, ra.refused, ra.malformed || ra.refused);\n", u, u, u, u, u; \
	       while ((getline line < m) > 0) { split(line, f, " "); if (f[1] == "message" && f[2] == u) \
	         printf "        {\n            let mut values = vec![%s::%s::default(); 4];\n            let mut r = %s::TableReport::default();\n            let (count, ok) = %s::%s_load_messages(&mut values, &vocabulary, batch, &mut r);\n            check(\"%s::%s_load_messages\", ok, r.refused, r.malformed || r.refused);\n            if ok && count > values.len() { panic!(\"%s::%s_load_messages delivered {count} bodies into {} slots\", values.len()); }\n        }\n", u, f[4], u, u, f[3], u, f[3], u, f[3] } \
	       close(m); printf "    }\n" }' m=$$m $$m; \
	printf '%s\n' '}' 'pub fn fuzz_block(data: &[u8]) {' '    let (store, off) = aligned(data);' '    let base = unsafe { (store.as_ptr() as *const u8).add(off) };' '    let bytes = data.len() as i64;'; \
	awk '$$1 == "block" { u=$$2; t=$$3; printf "    unsafe {\n        let opened = %s::%s::open(base, bytes).is_some();\n        let reason = %s::%s::open_reason(base, bytes);\n        if opened != (reason == \"ok\") { panic!(\"%s::%s::open ({opened}) disagrees with open_reason ({reason})\"); }\n    }\n", u, t, u, t, u, t }' $$m; \
	printf '%s\n' '}' 'pub fn fuzz_cook(data: &[u8]) {' '    let (store, off) = aligned(data);' '    let base = unsafe { (store.as_ptr() as *const u8).add(off) };' '    let length = data.len() as u64;'; \
	awk '$$1 == "cook" { u=$$2; t=$$3; printf "    unsafe {\n        let opened = %s::%s::open(base, length);\n        if let Some(c) = &opened {\n            let (lo, hi) = (base as usize, base as usize + data.len());\n            let (r, n) = (c.region() as usize, c.region_length() as usize);\n            if r < lo || r.checked_add(n).map_or(true, |e| e > hi) { panic!(\"%s::%s::open answered a region outside the input\"); }\n        }\n        let reason = %s::table_cook_open_reason(base, length, %s::%s::ROOT_SIZE, %s::%s::ROOT_ALIGN);\n        if opened.is_some() != (reason == \"ok\") { panic!(\"%s::%s::open disagrees with table_cook_open_reason ({reason})\"); }\n    }\n", u, t, u, t, u, u, t, u, t, u, t }' $$m; \
	printf '%s\n' '}' \
	  '/// Every reader kind, in the order the replay runs them.' \
	  'pub const KINDS: &[(&str, fn(&[u8]))] = &['; \
	for kind in $(SCHEMA_RUST_FUZZ_TARGETS); do printf '    ("%s", fuzz_%s),\n' $$kind $$kind; done; \
	printf '%s\n' '];'; \
	} > $$d/src/lib.rs; \
	printf '%s\n' '// Generated by make/schema-rust-nightly-fuzz.mk. DO NOT EDIT.' \
	  '// THE REPLAY: every saved input through every reader kind, once. No' \
	  '// libFuzzer, no clock, no mutation. A missing or empty corpus is a FAILURE,' \
	  '// never a pass on nothing; any oracle panic fails the run, by file and kind.' \
	  'fn main() {' \
	  '    let dir = std::env::args().nth(1).expect("usage: replay <corpus dir>");' \
	  '    let mut files: Vec<std::path::PathBuf> = match std::fs::read_dir(&dir) {' \
	  '        Ok(entries) => entries.filter_map(|e| e.ok()).map(|e| e.path()).filter(|p| p.is_file() && p.file_name().map_or(false, |n| !n.to_string_lossy().starts_with(".")) ).collect(),' \
	  '        Err(e) => { eprintln!("replay: FAIL: the corpus {dir} cannot be read: {e}"); std::process::exit(2); }' \
	  '    };' \
	  '    files.sort();' \
	  '    if files.is_empty() { eprintln!("replay: FAIL: the corpus {dir} holds no saved input"); std::process::exit(2); }' \
	  '    std::panic::set_hook(Box::new(|_| {}));' \
	  '    let mut failed = 0;' \
	  '    for file in &files {' \
	  '        let data = std::fs::read(file).expect("read a saved input");' \
	  '        for (kind, run) in schema_rust_nightly_fuzz::KINDS {' \
	  '            if let Err(e) = std::panic::catch_unwind(|| run(&data)) {' \
	  '                let why = e.downcast_ref::<String>().cloned().or_else(|| e.downcast_ref::<&str>().map(|s| s.to_string())).unwrap_or_default();' \
	  '                println!("replay: FAIL {} [{kind}]: {why}", file.display());' \
	  '                failed += 1;' \
	  '            }' \
	  '        }' \
	  '    }' \
	  '    if failed > 0 { println!("replay: {failed} failure(s) over {} saved input(s)", files.len()); std::process::exit(1); }' \
	  '    println!("replay: {} saved input(s) clean through {} reader kinds (no clock, no mutation)", files.len(), schema_rust_nightly_fuzz::KINDS.len());' \
	  '}' > $$d/src/replay.rs
	@echo "schema-rust-nightly-fuzz: harness: $(SCHEMA_RUST_FUZZ_TARGETS) and replay in $(SCHEMA_RUST_FUZZ_DIR)/fuzz"

# THE FAST HALF: replay only. A missing or empty committed corpus fails here
# before the build, and the replay binary refuses it again on its own. It
# replays the files at the top of the corpus directory: the regressions that
# hold. A REAL FINDING NOT YET FIXED waits in $(SCHEMA_RUST_FUZZ_CORPUS)/pending/,
# which the push replay does not read and the nightly does (libFuzzer reads
# its corpus directories recursively), so the night stays red until the fix
# card lands and moves the input up beside the others.
.PHONY: schema-rust-nightly-fuzz-replay
schema-rust-nightly-fuzz-replay: schema-rust-nightly-fuzz-harness
	@test -d $(SCHEMA_RUST_FUZZ_CORPUS) || { echo "schema-rust-nightly-fuzz-replay: FAIL: the committed corpus $(SCHEMA_RUST_FUZZ_CORPUS) is missing"; exit 1; }
	@n=$$(find $(SCHEMA_RUST_FUZZ_CORPUS) -maxdepth 1 -type f ! -name '.*' | wc -l); [ "$$n" -gt 0 ] || { echo "schema-rust-nightly-fuzz-replay: FAIL: the committed corpus $(SCHEMA_RUST_FUZZ_CORPUS) is empty"; exit 1; }
	@cd $(SCHEMA_RUST_FUZZ_DIR)/fuzz && cargo run --quiet --bin replay -- $(abspath $(SCHEMA_RUST_FUZZ_CORPUS))

# THE NIGHTLY HALF. libFuzzer's own -max_total_time ends each kind's run with
# exit 0, so the budget is never mistaken for a failure; a crash, an oracle
# panic, a hang past SCHEMA_RUST_FUZZ_HANG seconds or an RSS past
# SCHEMA_RUST_FUZZ_RSS MB exits non-zero and FAILS the target, and libFuzzer
# writes the input (crash-*, timeout-*, oom-*) into the committed corpus
# directory, where the workflow keeps it and the replay then holds it. Every
# kind runs even after one fails, so one night reports them all.
.PHONY: schema-rust-nightly-fuzz
schema-rust-nightly-fuzz: schema-rust-nightly-fuzz-tool schema-rust-nightly-fuzz-harness schema-rust-nightly-fuzz-seeds
	@mkdir -p $(SCHEMA_RUST_FUZZ_CORPUS) $(SCHEMA_RUST_FUZZ_DIR)/live
	@cd $(SCHEMA_RUST_FUZZ_DIR)/fuzz && failed=; for kind in $(SCHEMA_RUST_FUZZ_TARGETS); do \
		mkdir -p ../live/$$kind; \
		echo "schema-rust-nightly-fuzz: $$kind for $(SCHEMA_RUST_FUZZ_PER_KIND)s"; \
		cargo +$(SCHEMA_RUST_FUZZ_NIGHTLY) fuzz run $$kind ../live/$$kind $(abspath $(SCHEMA_RUST_FUZZ_SEEDS)) $(abspath $(SCHEMA_RUST_FUZZ_CORPUS)) -- \
			-max_total_time=$(SCHEMA_RUST_FUZZ_PER_KIND) -timeout=$(SCHEMA_RUST_FUZZ_HANG) -rss_limit_mb=$(SCHEMA_RUST_FUZZ_RSS) \
			-artifact_prefix=$(abspath $(SCHEMA_RUST_FUZZ_CORPUS))/$$kind- || failed="$$failed $$kind"; \
	done; \
	if [ -n "$$failed" ]; then echo "schema-rust-nightly-fuzz: FAIL: a finding in:$$failed; the inputs are in $(SCHEMA_RUST_FUZZ_CORPUS)"; exit 1; fi
	@echo "schema-rust-nightly-fuzz: no crash, hang, sanitizer or oracle finding in $(SCHEMA_RUST_FUZZ_PER_KIND)s per reader kind"

# THE PLANTED-PATTERN CONTROL. A LENGTH CHECK IS DROPPED IN A REAL GENERATED
# READER: tabledemo's global_settings_load_inner, the root <root>_load and the
# load fuzz target call, loses its `wire.len() < 9` refusal, in a scratch copy
# of the whole harness ($(SCHEMA_RUST_FUZZ_PLANT_DIR)); no tracked file and
# not the nightly's own tree is touched. RED with the plant in: cargo-fuzz must
# find it inside the per-kind budget, and the saved input must fail the replay
# on the planted tree. GREEN with it out: the same input replays clean on the
# real tree.
SCHEMA_RUST_FUZZ_PLANT_FILE := corpus/tabledemo/src/pack_table.rs
SCHEMA_RUST_FUZZ_PLANT_FN := global_settings_load_inner
.PHONY: schema-rust-nightly-fuzz-plant
schema-rust-nightly-fuzz-plant: schema-rust-nightly-fuzz-harness
	@set -e; p=$(SCHEMA_RUST_FUZZ_PLANT_DIR); rm -rf $$p; mkdir -p $$p; \
	cp -r $(SCHEMA_RUST_FUZZ_DIR)/corpus $(SCHEMA_RUST_FUZZ_DIR)/fuzz $(SCHEMA_RUST_FUZZ_DIR)/CORPUS.txt $(SCHEMA_RUST_FUZZ_DIR)/READERS.txt $$p/; \
	rm -rf $$p/fuzz/target $$p/fuzz/corpus $$p/fuzz/artifacts; \
	f=$$p/$(SCHEMA_RUST_FUZZ_PLANT_FILE); \
	awk -v fn='pub fn $(SCHEMA_RUST_FUZZ_PLANT_FN)(' 'index($$0, fn) == 1 { inside = 1 } \
		inside && !done && /^    if wire\.len\(\) < 9 \{ report\.malformed = true; return None; \}$$/ { done = 1; next } { print } \
		END { if (!done) exit 3 }' $(SCHEMA_RUST_FUZZ_DIR)/$(SCHEMA_RUST_FUZZ_PLANT_FILE) > $$f \
		|| { echo "NEGATIVE CONTROL FAILED: the length-check plant did not apply to $(SCHEMA_RUST_FUZZ_PLANT_FN)"; exit 1; }; \
	! cmp -s $$f $(SCHEMA_RUST_FUZZ_DIR)/$(SCHEMA_RUST_FUZZ_PLANT_FILE) || { echo "NEGATIVE CONTROL FAILED: the planted reader is unchanged"; exit 1; }; \
	echo "schema-rust-nightly-fuzz plant: the length check of $(SCHEMA_RUST_FUZZ_PLANT_FN) is dropped in $$f"

.PHONY: schema-rust-nightly-fuzz-negative-control
schema-rust-nightly-fuzz-negative-control: schema-rust-nightly-fuzz-tool schema-rust-nightly-fuzz-seeds schema-rust-nightly-fuzz-plant
	@set -e; p=$(abspath $(SCHEMA_RUST_FUZZ_PLANT_DIR)); work=$$p/found; rm -rf $$work; mkdir -p $$work $$p/live; \
	echo "schema-rust-nightly-fuzz negative control: cargo-fuzz has $(SCHEMA_RUST_FUZZ_PER_KIND)s to find the plant"; \
	if (cd $$p/fuzz && cargo +$(SCHEMA_RUST_FUZZ_NIGHTLY) fuzz run load $$p/live $(abspath $(SCHEMA_RUST_FUZZ_SEEDS)) -- \
		-max_total_time=$(SCHEMA_RUST_FUZZ_PER_KIND) -timeout=$(SCHEMA_RUST_FUZZ_HANG) -artifact_prefix=$$work/); then \
		echo "NEGATIVE CONTROL FAILED: the dropped length check survived the budget"; exit 1; fi; \
	test -n "$$(find $$work -type f -print -quit)" || { echo "NEGATIVE CONTROL FAILED: the fuzzer stopped but saved no input"; exit 1; }; \
	echo "schema-rust-nightly-fuzz negative control: RED: the fuzzer found the plant and saved $$(ls $$work | tr '\n' ' ')"; \
	if (cd $$p/fuzz && cargo run --quiet --bin replay -- $$work); then \
		echo "NEGATIVE CONTROL FAILED: the saved input replays clean on the planted tree"; exit 1; fi; \
	echo "schema-rust-nightly-fuzz negative control: RED: the saved input fails the replay on the planted tree"; \
	cd $(SCHEMA_RUST_FUZZ_DIR)/fuzz && cargo run --quiet --bin replay -- $$work \
		|| { echo "NEGATIVE CONTROL FAILED: the saved input fails the real tree too, so it is a real finding, not the plant's"; exit 1; }
	@echo "schema-rust-nightly-fuzz negative control: red with the plant in, green with it out"

# THE REPLAY HALF OF THE CONTROL, with no fuzzer: the committed input
# $(SCHEMA_RUST_FUZZ_CORPUS)/plant-short-wire (a form byte and nothing else:
# shorter than the 9 bytes the dropped check guards) must FAIL the replay on
# the planted tree and pass on the real one. This is what a bench without
# cargo-fuzz can prove; it is not the fuzzer finding the plant.
.PHONY: schema-rust-nightly-fuzz-plant-replay
schema-rust-nightly-fuzz-plant-replay: schema-rust-nightly-fuzz-plant
	@set -e; p=$(abspath $(SCHEMA_RUST_FUZZ_PLANT_DIR)); one=$$p/one; rm -rf $$one; mkdir -p $$one; \
	cp $(SCHEMA_RUST_FUZZ_CORPUS)/plant-short-wire $$one/; \
	if (cd $$p/fuzz && cargo run --quiet --bin replay -- $$one); then \
		echo "NEGATIVE CONTROL FAILED: plant-short-wire replays clean on the planted tree"; exit 1; fi; \
	echo "schema-rust-nightly-fuzz plant replay: RED on the planted tree"; \
	cd $(SCHEMA_RUST_FUZZ_DIR)/fuzz && cargo run --quiet --bin replay -- $$one
	@echo "schema-rust-nightly-fuzz plant replay: red with the plant in, green with it out"
