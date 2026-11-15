# make/ruby-toolchain.mk — the Ruby leg's pinned toolchain
# (docs/PORT-RUBY.md, THE PIN; docs/plans/ruby-leg.md, R1). Included by the
# Makefile's wildcard include; the stem has a dash, so it is a support
# fragment and never probed as a --lang port. The leg registers its pins at
# the end of this file.
#
# TWO INTERPRETERS AND ONE ANALYZER LOCK, all built or installed into dist/
# (gitignored), none of them found on PATH:
#
#   RUBY_FLOOR  CRuby 3.4.12, the floor pin, built from its ruby-lang.org
#               source tarball
#   RUBY        CRuby 4.0.9, the current pin, built the same way
#   RUBOCOP     RuboCop 1.92.2, installed from make/ruby-gems.lock
#   STEEP       Steep 2.1.0, installed from the same lock
#   RBS         RBS 4.2.0, installed from the same lock
#
# Each tarball is checked against the sha256 ruby-lang.org publishes for it
# before anything is unpacked, and any other sha256 is refused by name. The
# build is ./configure && make, with no optimization flags, and needs only a
# C compiler and the standard library's own dependencies. The two
# interpreters build in parallel under make -j, each with its own
# make -j$(RUBY_BUILD_JOBS).
#
# The analyzers go into each interpreter offline from the hash-locked list
# make/ruby-gems.lock, with gem install --local --ignore-dependencies, so no
# index is consulted and nothing is resolved. $RUBY, $RUBY_FLOOR, $RUBOCOP,
# $STEEP and $RBS override; .github/actions/ruby restores dist/ from a store
# keyed on every pin and the lock.
RUBY_FLOOR_VERSION    := 3.4.12
RUBY_CURRENT_VERSION  := 4.0.9
RUBY_SHA256_3.4.12   := 5e5d85ad93b0adcccbad47733680f4892bd0078e3342437f7babce01c1872479
RUBY_SHA256_4.0.9    := c35350682ff050b79702d57ade4516e7336063f510155d19fb2fb5b3f3909511
RUBY_VERSIONS         := $(RUBY_FLOOR_VERSION) $(RUBY_CURRENT_VERSION)
RUBY_BUILD_JOBS       ?= 8

RUBY_RUBOCOP_VERSION := 1.92.2
RUBY_STEEP_VERSION   := 2.1.0
RUBY_RBS_VERSION     := 4.2.0
RUBY_GEMS_LOCK       := $(CURDIR)/make/ruby-gems.lock

RUBY_FLOOR ?= $(CURDIR)/dist/ruby-$(RUBY_FLOOR_VERSION)/bin/ruby
RUBY       ?= $(CURDIR)/dist/ruby-$(RUBY_CURRENT_VERSION)/bin/ruby
RUBOCOP    ?= $(CURDIR)/dist/ruby-$(RUBY_CURRENT_VERSION)/bin/rubocop
STEEP      ?= $(CURDIR)/dist/ruby-$(RUBY_CURRENT_VERSION)/bin/steep
RBS        ?= $(CURDIR)/dist/ruby-$(RUBY_CURRENT_VERSION)/bin/rbs
# the Go tests (internal/codegen/ruby/rubyrun) and the conformance driver are
# processes this make spawns, so they read the pins from the environment
export RUBY RUBY_FLOOR RUBOCOP STEEP RBS

# sha256 of a file, on Linux (sha256sum) or macOS (shasum)
ruby_sha256 = { sha256sum $(1) 2>/dev/null || shasum -a 256 $(1); } | cut -d" " -f1

# THE FLEET'S BENCHES ALREADY HOLD THE PINS. Their SDK (the directory
# $NOVA_SDK names) has CRuby <v> at ruby-<v>/, built from the same
# ruby-lang.org tarball, with the locked analyzers installed, and a sandboxed
# bench job has no network to download them. A pin whose SDK interpreter
# reports exactly its release is staged at dist/ruby-<v>/ with links to that
# tree, so every path below is unchanged; any other answer, or no NOVA_SDK,
# builds from the tarball as before.
ruby_sdk_version = $(shell $(NOVA_SDK)/ruby-$(1)/bin/ruby --disable-gems -e 'print RUBY_VERSION' 2>/dev/null)
RUBY_SDK_STAGED := $(if $(NOVA_SDK),$(foreach v,$(RUBY_VERSIONS),$(if $(filter $(v),$(call ruby_sdk_version,$(v))),$(v))))

.PHONY: dist-ruby
dist-ruby: $(foreach v,$(RUBY_VERSIONS),dist/ruby-$(v)/PIN) \
	$(foreach v,$(RUBY_VERSIONS),dist/ruby-$(v)/GEMS)
	@$(foreach v,$(RUBY_VERSIONS),echo "dist-ruby: CRuby $(v) sha256 $(RUBY_SHA256_$(v)) at dist/ruby-$(v)";)
	@echo "dist-ruby: $$($(RUBOCOP) --version) at $(RUBOCOP)"
	@echo "dist-ruby: steep $$($(STEEP) --version) at $(STEEP)"
	@echo "dist-ruby: $$($(RBS) --version) at $(RBS)"

# One interpreter: download, check, unpack into build/, configure with the
# dist/ prefix, build, install. PIN is written last, so a build that stopped
# part way leaves no PIN and the next run starts it again.
dist/ruby-%/PIN:
	@sha='$(RUBY_SHA256_$*)'; if [ -z "$$sha" ]; then \
		echo "dist-ruby REFUSES: $* is not a pinned CRuby version (the pins are $(RUBY_VERSIONS))"; exit 1; fi
	@mkdir -p dist build
	@series='$(basename $*)'; if [ ! -f dist/ruby-$*.tar.gz ]; then \
		curl -fsSL -o dist/ruby-$*.tar.gz.part https://cache.ruby-lang.org/pub/ruby/$$series/ruby-$*.tar.gz && \
		mv dist/ruby-$*.tar.gz.part dist/ruby-$*.tar.gz; fi
	@got=$$($(call ruby_sha256,dist/ruby-$*.tar.gz)); \
	if [ "$$got" != "$(RUBY_SHA256_$*)" ]; then \
		echo "dist-ruby REFUSES: dist/ruby-$*.tar.gz has sha256 $$got, the pin is $(RUBY_SHA256_$*)"; \
		echo "  delete dist/ruby-$*.tar.gz and run make dist-ruby again; a tarball that does not match the pin is never built"; \
		exit 1; fi
	@rm -rf build/ruby-src-$* dist/ruby-$* && mkdir -p build/ruby-src-$*
	@tar -xzf dist/ruby-$*.tar.gz -C build/ruby-src-$*
	@echo "dist-ruby: building CRuby $* (log: build/ruby-src-$*/build.log)"
	@cd build/ruby-src-$*/ruby-$* && \
		{ ./configure --prefix=$(CURDIR)/dist/ruby-$* --disable-install-doc && \
		  $(MAKE) -j$(RUBY_BUILD_JOBS) && $(MAKE) install; } > ../build.log 2>&1 || \
		{ tail -40 ../build.log; echo "dist-ruby: CRuby $* did not build"; exit 1; }
	@echo "$* $(RUBY_SHA256_$*)" > $@

ifneq ($(RUBY_SDK_STAGED),)
$(foreach v,$(RUBY_SDK_STAGED),dist/ruby-$(v)/PIN): dist/ruby-%/PIN:
	@rm -rf dist/ruby-$* && mkdir -p dist/ruby-$*
	@for e in $(NOVA_SDK)/ruby-$*/*; do ln -s "$$e" dist/ruby-$*/; done
	@echo "dist-ruby: CRuby $* from the fleet SDK $(NOVA_SDK)/ruby-$*, no download"
	@echo "$* $(RUBY_SHA256_$*) $(NOVA_SDK)/ruby-$*" > $@
endif

# THE ANALYZERS, into each interpreter: the SDK's own install is checked
# against the lock; a tarball build installs the lock's gems offline, each
# .gem file held to its sha256 before gem install sees it. No index is
# consulted and nothing is resolved: the lock names every gem.
dist/ruby-%/GEMS: dist/ruby-%/PIN $(RUBY_GEMS_LOCK)
	@if echo " $(RUBY_SDK_STAGED) " | grep -q " $* "; then \
		for a in rubocop steep rbs; do \
			dist/ruby-$*/bin/$$a --version >/dev/null 2>&1 || { \
				echo "dist-ruby REFUSES: the fleet SDK's $(NOVA_SDK)/ruby-$*/bin/$$a does not answer --version"; exit 1; }; \
		done; \
	else \
		mkdir -p dist/ruby-gems; \
		while IFS= read -r line; do \
			case "$$line" in ''|'#'*) continue;; esac; \
			spec=$${line%% *}; hash=$${line#* }; \
			name=$${spec%%==*}; ver=$${spec#*==}; f=dist/ruby-gems/$$name-$$ver.gem; \
			if [ ! -f "$$f" ]; then \
				curl -fsSL -o "$$f.part" "https://rubygems.org/downloads/$$name-$$ver.gem" && mv "$$f.part" "$$f" || exit 1; fi; \
			got=$$($(call ruby_sha256,$$f)); \
			if [ "$$got" != "$${hash#sha256:}" ]; then \
				echo "dist-ruby REFUSES: $$f has sha256 $$got, the lock is $$hash"; exit 1; fi; \
		done < $(RUBY_GEMS_LOCK); \
		dist/ruby-$*/bin/gem install --local --no-document --ignore-dependencies dist/ruby-gems/*.gem; \
	fi
	@cp $(RUBY_GEMS_LOCK) $@

# THE TOOLCHAIN GATE, this leg's half (issue #599): one probe per pin.
.PHONY: toolchain-ruby
toolchain-ruby:
	@$(call toolchain_probe,ruby,RUBY,$(RUBY))
	@$(call toolchain_probe,ruby,RUBY_FLOOR,$(RUBY_FLOOR))
	@$(call toolchain_probe,ruby,RUBOCOP,$(RUBOCOP))
	@$(call toolchain_probe,ruby,STEEP,$(STEEP))
	@$(call toolchain_probe,ruby,RBS,$(RBS))

# THE PINS ARE WHAT THEY SAY. The probe above says a path resolves; this says
# each interpreter at it is the pinned release, that each analyzer is the
# locked gem, and that no file of the leg runs an interpreter it found on
# PATH. Each version is read from the program itself, and each interpreter
# runs isolated (--disable-gems: no gem environment, no RUBYOPT).
.PHONY: ruby-toolchain-check ruby-toolchain-negative-control
RUBY_CHECK_ENV = RUBY_CURRENT_VERSION=$(RUBY_CURRENT_VERSION) RUBY_FLOOR_VERSION=$(RUBY_FLOOR_VERSION) RUBY_RUBOCOP_VERSION=$(RUBY_RUBOCOP_VERSION) RUBY_STEEP_VERSION=$(RUBY_STEEP_VERSION) RUBY_RBS_VERSION=$(RUBY_RBS_VERSION) RUBY_GEMS_LOCK=$(RUBY_GEMS_LOCK)
ruby-toolchain-check: toolchain-ruby
	@$(RUBY_CHECK_ENV) sh make/ruby-toolchain-check.sh

# ITS NEGATIVE CONTROL: each rule the check names, broken one at a time, must
# turn it red by that rule's name. RUBY pointed at the floor interpreter (a
# real CRuby, the wrong release); RUBY_FLOOR pointed at the current one;
# RUBOCOP, STEEP and RBS each pointed at a program that is not the locked
# gem; and a copy of the leg's make files with one line that runs ruby from
# PATH. The clean check runs first, so each red is the break.
RUBY_NC := build/ruby-toolchain-nc
ruby-toolchain-negative-control: ruby-toolchain-check
	@rm -rf $(RUBY_NC) && mkdir -p $(RUBY_NC)/root/make
	@if $(RUBY_CHECK_ENV) RUBY=$(RUBY_FLOOR) sh make/ruby-toolchain-check.sh > $(RUBY_NC)/ruby.log 2>&1; then \
		cat $(RUBY_NC)/ruby.log; echo "NEGATIVE CONTROL FAILED: RUBY at CRuby $(RUBY_FLOOR_VERSION) passed as $(RUBY_CURRENT_VERSION)"; exit 1; fi
	@grep -q "pinned RUBY is Ruby $(RUBY_FLOOR_VERSION), the pin is $(RUBY_CURRENT_VERSION)" $(RUBY_NC)/ruby.log || \
		{ cat $(RUBY_NC)/ruby.log; echo "NEGATIVE CONTROL FAILED: the wrong RUBY went red, but not by its name"; exit 1; }
	@if $(RUBY_CHECK_ENV) RUBY_FLOOR=$(RUBY) sh make/ruby-toolchain-check.sh > $(RUBY_NC)/ruby-floor.log 2>&1; then \
		cat $(RUBY_NC)/ruby-floor.log; echo "NEGATIVE CONTROL FAILED: RUBY_FLOOR at CRuby $(RUBY_CURRENT_VERSION) passed as $(RUBY_FLOOR_VERSION)"; exit 1; fi
	@grep -q "pinned RUBY_FLOOR is Ruby $(RUBY_CURRENT_VERSION), the pin is $(RUBY_FLOOR_VERSION)" $(RUBY_NC)/ruby-floor.log || \
		{ cat $(RUBY_NC)/ruby-floor.log; echo "NEGATIVE CONTROL FAILED: the wrong RUBY_FLOOR went red, but not by its name"; exit 1; }
	@if $(RUBY_CHECK_ENV) RUBOCOP=/bin/echo sh make/ruby-toolchain-check.sh > $(RUBY_NC)/rubocop.log 2>&1; then \
		cat $(RUBY_NC)/rubocop.log; echo "NEGATIVE CONTROL FAILED: RUBOCOP at /bin/echo passed as RuboCop $(RUBY_RUBOCOP_VERSION)"; exit 1; fi
	@grep -q "pinned RUBOCOP reports" $(RUBY_NC)/rubocop.log || \
		{ cat $(RUBY_NC)/rubocop.log; echo "NEGATIVE CONTROL FAILED: the wrong RUBOCOP went red, but not by its name"; exit 1; }
	@if $(RUBY_CHECK_ENV) STEEP=/bin/echo sh make/ruby-toolchain-check.sh > $(RUBY_NC)/steep.log 2>&1; then \
		cat $(RUBY_NC)/steep.log; echo "NEGATIVE CONTROL FAILED: STEEP at /bin/echo passed as Steep $(RUBY_STEEP_VERSION)"; exit 1; fi
	@grep -q "pinned STEEP reports" $(RUBY_NC)/steep.log || \
		{ cat $(RUBY_NC)/steep.log; echo "NEGATIVE CONTROL FAILED: the wrong STEEP went red, but not by its name"; exit 1; }
	@if $(RUBY_CHECK_ENV) RBS=/bin/echo sh make/ruby-toolchain-check.sh > $(RUBY_NC)/rbs.log 2>&1; then \
		cat $(RUBY_NC)/rbs.log; echo "NEGATIVE CONTROL FAILED: RBS at /bin/echo passed as RBS $(RUBY_RBS_VERSION)"; exit 1; fi
	@grep -q "pinned RBS reports" $(RUBY_NC)/rbs.log || \
		{ cat $(RUBY_NC)/rbs.log; echo "NEGATIVE CONTROL FAILED: the wrong RBS went red, but not by its name"; exit 1; }
	@cp make/ruby*.mk make/ruby*.sh $(RUBY_GEMS_LOCK) $(RUBY_NC)/root/make/
	@printf '\nplanted:\n\t%s -c "print(1)"\n' 'ru''by' >> $(RUBY_NC)/root/make/ruby-toolchain.mk
	@if $(RUBY_CHECK_ENV) sh make/ruby-toolchain-check.sh $(RUBY_NC)/root > $(RUBY_NC)/path.log 2>&1; then \
		cat $(RUBY_NC)/path.log; echo "NEGATIVE CONTROL FAILED: an interpreter from PATH in make/ruby-toolchain.mk passed the path gate"; exit 1; fi
	@grep -q "make/ruby-toolchain.mk runs an interpreter from PATH" $(RUBY_NC)/path.log || \
		{ cat $(RUBY_NC)/path.log; echo "NEGATIVE CONTROL FAILED: the planted PATH interpreter went red, but not by its file"; exit 1; }
	@echo "negative control: a wrong RUBY, a wrong RUBY_FLOOR, a wrong RUBOCOP, STEEP and RBS, and an interpreter from PATH each turn ruby-toolchain-check RED by name"

# The leg registers its pins here; test-ruby and the rest of the leg are
# make/ruby.mk's, which arrives with the skeleton in R3.
TOOLCHAIN_LEGS        += ruby
TOOLCHAIN_PINS_ruby   := RUBY RUBY_FLOOR RUBOCOP STEEP RBS
