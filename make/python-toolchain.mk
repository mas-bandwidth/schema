# make/python-toolchain.mk — the Python leg's pinned toolchain
# (docs/PORT-PYTHON.md, THE PIN). Included by the Makefile's wildcard include;
# the stem has a dash, so it is a support fragment and never probed as a
# --lang port. The leg registers its pins at the end of this file.
#
# TWO INTERPRETERS AND ONE ANALYZER, all built or installed into dist/
# (gitignored), none of them found on PATH:
#
#   PYTHON_FLOOR  CPython 3.11.18, the oldest release the leg supports
#                 (the newest 3.11.x), built from the python.org source
#                 tarball
#   PYTHON        CPython 3.15.2, the newest stable release at pin time,
#                 built the same way
#   MYPY          mypy 1.18.2, installed into the floor interpreter from the
#                 hash-locked list make/python-mypy.lock, offline, with pip
#                 --require-hashes
#
# Each tarball is checked against the sha256 python.org publishes for it
# before anything is unpacked, and any other sha256 is refused by name. The
# build is ./configure && make, no optimization flags (PGO is a later,
# measured decision), and needs only a C compiler: the leg's code uses struct,
# sys and os, none of which needs an optional system library. Modules that do
# (ssl, sqlite3, tkinter) are built where the host has their headers and left
# out where it does not, and nothing in the leg imports them.
#
# The two interpreters build in parallel under make -j, each with its own
# make -j$(PYTHON_BUILD_JOBS). $PYTHON, $PYTHON_FLOOR and $MYPY override;
# .github/actions/python restores dist/ from a cache keyed on the pins.
PYTHON_FLOOR_VERSION   := 3.11.18
PYTHON_CURRENT_VERSION := 3.15.2
PYTHON_SHA256_3.11.18  := 5f62711b12470fd435f83f1451c1b1ef4a179aebc715c716874b0967567d814e
PYTHON_SHA256_3.15.2   := f74e1db0eabd651cd01e4e2a00201bd41866abe74869ab716f9bf33cbbaa0943
PYTHON_VERSIONS        := $(PYTHON_FLOOR_VERSION) $(PYTHON_CURRENT_VERSION)
PYTHON_BUILD_JOBS      ?= 8

PYTHON_FLOOR ?= $(CURDIR)/dist/python-$(PYTHON_FLOOR_VERSION)/bin/python3.11
PYTHON       ?= $(CURDIR)/dist/python-$(PYTHON_CURRENT_VERSION)/bin/python3.15
MYPY         ?= $(CURDIR)/dist/python-$(PYTHON_FLOOR_VERSION)/bin/mypy
# the Go tests (internal/codegen/python/pyrun) and the conformance driver are
# processes this make spawns, so they read the pins from the environment
export PYTHON PYTHON_FLOOR MYPY

# THE ANALYZER'S WHEELS. mypy 1.18.2 is the newest mypy whose every dependency
# ships a pure-Python wheel (1.19 adds librt, 2.0 adds ast-serialize, both
# compiled), so one sha256 per wheel serves every OS. make/python-mypy.lock is
# the hash-locked list pip installs from; these are where the wheels come
# from. pip then refuses any wheel whose sha256 is not the lock's.
PYTHON_MYPY_VERSION := 1.18.2
PYTHON_MYPY_LOCK   := make/python-mypy.lock
PYTHON_MYPY_WHEELS := \
	https://files.pythonhosted.org/packages/87/e3/be76d87158ebafa0309946c4a73831974d4d6ab4f4ef40c3b53a385a66fd/mypy-1.18.2-py3-none-any.whl \
	https://files.pythonhosted.org/packages/79/7b/2c79738432f5c924bef5071f933bcc9efd0473bac3b4aa584a6f7c1c8df8/mypy_extensions-1.1.0-py3-none-any.whl \
	https://files.pythonhosted.org/packages/18/67/36e9267722cc04a6b9f15c7f3441c2363321a3ea07da7ae0c0707beb2a9c/typing_extensions-4.15.0-py3-none-any.whl \
	https://files.pythonhosted.org/packages/cc/20/ff623b09d963f88bfde16306a54e12ee5ea43e9b597108672ff3a408aad6/pathspec-0.12.1-py3-none-any.whl

# sha256 of a file, on Linux (sha256sum) or macOS (shasum)
python_sha256 = { sha256sum $(1) 2>/dev/null || shasum -a 256 $(1); } | cut -d" " -f1

.PHONY: dist-python
dist-python: $(foreach v,$(PYTHON_VERSIONS),dist/python-$(v)/PIN)
	@$(MAKE) --no-print-directory dist/python-$(PYTHON_FLOOR_VERSION)/MYPY
	@$(foreach v,$(PYTHON_VERSIONS),echo "dist-python: CPython $(v) sha256 $(PYTHON_SHA256_$(v)) at dist/python-$(v)";)
	@echo "dist-python: $$($(MYPY) --version) at $(MYPY)"

# One interpreter: download, check, unpack into build/, configure with the
# dist/ prefix, build, install. PIN is written last, so a build that stopped
# part way leaves no PIN and the next run starts it again.
dist/python-%/PIN:
	@sha='$(PYTHON_SHA256_$*)'; if [ -z "$$sha" ]; then \
		echo "dist-python REFUSES: $* is not a pinned CPython version (the pins are $(PYTHON_VERSIONS))"; exit 1; fi
	@mkdir -p dist build
	@if [ ! -f dist/Python-$*.tgz ]; then \
		curl -fsSL -o dist/Python-$*.tgz.part https://www.python.org/ftp/python/$*/Python-$*.tgz && \
		mv dist/Python-$*.tgz.part dist/Python-$*.tgz; fi
	@got=$$($(call python_sha256,dist/Python-$*.tgz)); \
	if [ "$$got" != "$(PYTHON_SHA256_$*)" ]; then \
		echo "dist-python REFUSES: dist/Python-$*.tgz has sha256 $$got, the pin is $(PYTHON_SHA256_$*)"; \
		echo "  delete dist/Python-$*.tgz and run make dist-python again; a tarball that does not match the pin is never built"; \
		exit 1; fi
	@rm -rf build/python-src-$* dist/python-$* && mkdir -p build/python-src-$*
	@tar -xzf dist/Python-$*.tgz -C build/python-src-$*
	@echo "dist-python: building CPython $* (log: build/python-src-$*/build.log)"
	@cd build/python-src-$*/Python-$* && \
		{ ./configure --prefix=$(CURDIR)/dist/python-$* --disable-test-modules --with-ensurepip=install && \
		  $(MAKE) -j$(PYTHON_BUILD_JOBS) && $(MAKE) install; } > ../build.log 2>&1 || \
		{ tail -40 ../build.log; echo "dist-python: CPython $* did not build"; exit 1; }
	@echo "$* $(PYTHON_SHA256_$*)" > $@

# The analyzer, into the floor interpreter: each wheel fetched by URL, then pip
# installs from that directory alone (--no-index), every wheel held to the
# lock's sha256 (--require-hashes). No index is consulted and nothing is
# resolved: the lock names every dependency.
dist/python-$(PYTHON_FLOOR_VERSION)/MYPY: dist/python-$(PYTHON_FLOOR_VERSION)/PIN $(PYTHON_MYPY_LOCK)
	@mkdir -p dist/python-wheels
	@for u in $(PYTHON_MYPY_WHEELS); do f=dist/python-wheels/$${u##*/}; \
		if [ ! -f $$f ]; then curl -fsSL -o $$f.part $$u && mv $$f.part $$f || exit 1; fi; done
	@$(PYTHON_FLOOR) -I -m pip install --quiet --no-index --no-cache-dir --disable-pip-version-check \
		--require-hashes --find-links dist/python-wheels -r $(PYTHON_MYPY_LOCK)
	@cp $(PYTHON_MYPY_LOCK) $@

# THE TOOLCHAIN GATE, this leg's half (issue #599): one probe per pin.
.PHONY: toolchain-python
toolchain-python:
	@$(call toolchain_probe,python,PYTHON,$(PYTHON))
	@$(call toolchain_probe,python,PYTHON_FLOOR,$(PYTHON_FLOOR))
	@$(call toolchain_probe,python,MYPY,$(MYPY))

# THE PINS ARE WHAT THEY SAY. The probe above says a path resolves; this says
# the interpreter at it is the pinned release, that the analyzer is the locked
# mypy, and that no file of the leg runs an interpreter it found on PATH. Each
# version is read from the program itself, and each interpreter runs isolated
# (-I: no environment, no user site, no script directory).
.PHONY: python-toolchain-check python-toolchain-negative-control
PYTHON_CHECK_ENV = PYTHON_CURRENT_VERSION=$(PYTHON_CURRENT_VERSION) PYTHON_FLOOR_VERSION=$(PYTHON_FLOOR_VERSION) PYTHON_MYPY_VERSION=$(PYTHON_MYPY_VERSION)
python-toolchain-check: toolchain-python
	@$(PYTHON_CHECK_ENV) sh make/python-toolchain-check.sh

# ITS NEGATIVE CONTROL: each rule the check names, broken one at a time, must
# turn it red by that rule's name. PYTHON pointed at the floor interpreter (a
# real CPython, the wrong release); MYPY pointed at a program that is not the
# locked mypy; and a copy of the leg's make files with one line that runs
# python3 from PATH. The clean check runs first, so each red is the break.
PYTHON_NC := build/python-toolchain-nc
python-toolchain-negative-control: python-toolchain-check
	@rm -rf $(PYTHON_NC) && mkdir -p $(PYTHON_NC)/root/make
	@if $(PYTHON_CHECK_ENV) PYTHON=$(PYTHON_FLOOR) sh make/python-toolchain-check.sh > $(PYTHON_NC)/version.log 2>&1; then \
		cat $(PYTHON_NC)/version.log; echo "NEGATIVE CONTROL FAILED: PYTHON at CPython $(PYTHON_FLOOR_VERSION) passed as $(PYTHON_CURRENT_VERSION)"; exit 1; fi
	@grep -q "pinned PYTHON is CPython $(PYTHON_FLOOR_VERSION), the pin is $(PYTHON_CURRENT_VERSION)" $(PYTHON_NC)/version.log || \
		{ cat $(PYTHON_NC)/version.log; echo "NEGATIVE CONTROL FAILED: the wrong PYTHON went red, but not by its name"; exit 1; }
	@if $(PYTHON_CHECK_ENV) MYPY=/bin/echo sh make/python-toolchain-check.sh > $(PYTHON_NC)/mypy.log 2>&1; then \
		cat $(PYTHON_NC)/mypy.log; echo "NEGATIVE CONTROL FAILED: MYPY at /bin/echo passed as mypy $(PYTHON_MYPY_VERSION)"; exit 1; fi
	@grep -q "pinned MYPY reports" $(PYTHON_NC)/mypy.log || \
		{ cat $(PYTHON_NC)/mypy.log; echo "NEGATIVE CONTROL FAILED: the wrong MYPY went red, but not by its name"; exit 1; }
	@cp make/python*.mk make/python*.sh $(PYTHON_NC)/root/make/
	@printf '\nplanted:\n\t%s3 -c "print(1)"\n' python >> $(PYTHON_NC)/root/make/python-toolchain.mk
	@if $(PYTHON_CHECK_ENV) sh make/python-toolchain-check.sh $(PYTHON_NC)/root > $(PYTHON_NC)/path.log 2>&1; then \
		cat $(PYTHON_NC)/path.log; echo "NEGATIVE CONTROL FAILED: an interpreter from PATH in make/python-toolchain.mk passed the path gate"; exit 1; fi
	@grep -q "make/python-toolchain.mk runs an interpreter from PATH" $(PYTHON_NC)/path.log || \
		{ cat $(PYTHON_NC)/path.log; echo "NEGATIVE CONTROL FAILED: the planted PATH interpreter went red, but not by its file"; exit 1; }
	@echo "negative control: a wrong PYTHON, a wrong MYPY and an interpreter from PATH each turn python-toolchain-check RED by name"
