# make/ruby-toolchain.mk: the pinned toolchain, the same pins, URLs, lock
# entries and refusals as mas-bandwidth/schema's make/ruby-toolchain.mk.
# Included by the Makefile before its fallbacks.
#
# TWO INTERPRETERS AND ONE ANALYZER, all built or installed into dist/
# (gitignored), none of them found on PATH:
#
#   RUBY_FLOOR  CRuby 3.4.12, the oldest series in normal maintenance, built
#               from its ruby-lang.org source tarball
#   RUBY        CRuby 4.0.9, the newest stable release at pin time, built the
#               same way
#   RUBOCOP     RuboCop 1.92.2, installed into the floor interpreter from the
#               hash-locked list make/ruby-gems.lock, offline, with gem
#               install --local --ignore-dependencies
#
# Each tarball is checked against the sha256 ruby-lang.org publishes for it
# before anything is unpacked, and any other sha256 is refused by name; each
# .gem file is held to the lock's sha256 the same way. The build is
# ./configure && make, with no optimization flags, and needs only a C compiler
# and the standard library's own dependencies.
#
# When dist/ holds a pin, the Makefile's RUBY_FLOOR and RUBY default to it (and
# RUBOCOP, the floor's own RuboCop gem, with them); an explicit value on the
# command line or in the environment still wins, and without dist/ the
# Makefile falls back to ruby from PATH (a contributor's machine). CI never
# takes the fallback: .github/actions/ruby restores or builds dist/ first, and
# make ci-check refuses a workflow that runs the tests without it.
RUBY_FLOOR_VERSION    := 3.4.12
RUBY_CURRENT_VERSION  := 4.0.9
RUBY_SHA256_3.4.12    := 5e5d85ad93b0adcccbad47733680f4892bd0078e3342437f7babce01c1872479
RUBY_SHA256_4.0.9     := c35350682ff050b79702d57ade4516e7336063f510155d19fb2fb5b3f3909511
RUBY_VERSIONS         := $(RUBY_FLOOR_VERSION) $(RUBY_CURRENT_VERSION)
RUBY_BUILD_JOBS       ?= 8

RUBY_RUBOCOP_VERSION  := 1.92.2
RUBY_GEMS_LOCK        := make/ruby-gems.lock

RUBY_FLOOR_PIN := $(CURDIR)/dist/ruby-$(RUBY_FLOOR_VERSION)/bin/ruby
RUBY_PIN       := $(CURDIR)/dist/ruby-$(RUBY_CURRENT_VERSION)/bin/ruby

ifneq ($(wildcard dist/ruby-$(RUBY_FLOOR_VERSION)/PIN),)
RUBY_FLOOR ?= $(RUBY_FLOOR_PIN)
endif
ifneq ($(wildcard dist/ruby-$(RUBY_CURRENT_VERSION)/PIN),)
RUBY ?= $(RUBY_PIN)
endif

# sha256 of a file, on Linux (sha256sum) or macOS (shasum)
ruby_sha256 = { sha256sum $(1) 2>/dev/null || shasum -a 256 $(1); } | cut -d" " -f1

# A BENCH THAT ALREADY HOLDS THE PINS (as schema's make/ruby-toolchain.mk).
# Its SDK (the directory $NOVA_SDK names) has CRuby <v> at ruby-<v>/, built
# from the same ruby-lang.org tarball, with the locked RuboCop installed. A
# pin whose SDK interpreter reports exactly its release is staged at
# dist/ruby-<v>/ with links to that tree, so every path below is unchanged;
# any other answer, or no NOVA_SDK, builds from the tarball.
ruby_sdk_version = $(shell $(NOVA_SDK)/ruby-$(1)/bin/ruby --disable-gems -e 'print RUBY_VERSION' 2>/dev/null)
RUBY_SDK_STAGED := $(if $(NOVA_SDK),$(foreach v,$(RUBY_VERSIONS),$(if $(filter $(v),$(call ruby_sdk_version,$(v))),$(v))))

.PHONY: dist-ruby
dist-ruby: $(foreach v,$(RUBY_VERSIONS),dist/ruby-$(v)/PIN) dist/ruby-$(RUBY_FLOOR_VERSION)/GEMS
	@$(foreach v,$(RUBY_VERSIONS),echo "dist-ruby: CRuby $(v) sha256 $(RUBY_SHA256_$(v)) at dist/ruby-$(v)";)
	@echo "dist-ruby: RuboCop $$(dist/ruby-$(RUBY_FLOOR_VERSION)/bin/rubocop --version) in dist/ruby-$(RUBY_FLOOR_VERSION)"

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
	@echo "dist-ruby: CRuby $* from the SDK $(NOVA_SDK)/ruby-$*, no download"
	@echo "$* $(RUBY_SHA256_$*) $(NOVA_SDK)/ruby-$*" > $@
endif

# THE ANALYZER, into the floor interpreter (make lint runs RuboCop under the
# floor): an SDK's own install is checked against the lock; a tarball build
# installs the lock's gems offline, each .gem file held to its sha256 before
# gem install sees it. No index is consulted and nothing is resolved: the lock
# names every gem RuboCop needs at run time.
dist/ruby-$(RUBY_FLOOR_VERSION)/GEMS: dist/ruby-$(RUBY_FLOOR_VERSION)/PIN $(RUBY_GEMS_LOCK)
ifneq ($(filter $(RUBY_FLOOR_VERSION),$(RUBY_SDK_STAGED)),)
	@got=$$(dist/ruby-$(RUBY_FLOOR_VERSION)/bin/rubocop --version 2>/dev/null); \
	if [ "$$got" != "$(RUBY_RUBOCOP_VERSION)" ]; then \
		echo "dist-ruby REFUSES: the SDK's RuboCop reports \"$$got\", the lock is RuboCop $(RUBY_RUBOCOP_VERSION)"; exit 1; fi
else
	@mkdir -p dist/ruby-gems
	@while IFS= read -r line; do \
		case "$$line" in ''|'#'*) continue;; esac; \
		spec=$${line%% *}; hash=$${line#* }; \
		name=$${spec%%==*}; ver=$${spec#*==}; f=dist/ruby-gems/$$name-$$ver.gem; \
		if [ ! -f "$$f" ]; then \
			curl -fsSL -o "$$f.part" "https://rubygems.org/downloads/$$name-$$ver.gem" && mv "$$f.part" "$$f" || exit 1; fi; \
		got=$$($(call ruby_sha256,$$f)); \
		if [ "$$got" != "$${hash#sha256:}" ]; then \
			echo "dist-ruby REFUSES: $$f has sha256 $$got, the lock is $$hash"; exit 1; fi; \
	done < $(RUBY_GEMS_LOCK)
	@env -u GEM_HOME -u GEM_PATH -u RUBYOPT -u RUBYLIB dist/ruby-$(RUBY_FLOOR_VERSION)/bin/gem install \
		--local --no-document --ignore-dependencies --quiet dist/ruby-gems/*.gem
endif
	@cp $(RUBY_GEMS_LOCK) $@

# THE PINS ARE WHAT THEY SAY: each interpreter's own RUBY_VERSION is its
# pinned release and RuboCop under the floor reports the locked version. Run
# on the pins (the defaults above once dist/ holds them); a refusal names the
# pin.
.PHONY: ruby-toolchain-check
ruby-toolchain-check:
	@RUBY_CURRENT_VERSION=$(RUBY_CURRENT_VERSION) RUBY_FLOOR_VERSION=$(RUBY_FLOOR_VERSION) \
		RUBY_RUBOCOP_VERSION=$(RUBY_RUBOCOP_VERSION) RUBY='$(RUBY_PIN)' RUBY_FLOOR='$(RUBY_FLOOR_PIN)' \
		sh make/ruby-toolchain-check.sh
