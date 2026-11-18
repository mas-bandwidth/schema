# Python CI and release tier notes

The tier's targets live in `make/python-ci-release.mk`. Every recipe there runs
Python through the pins `make/python-toolchain.mk` names (`$(PYTHON)`,
`$(PYTHON_FLOOR)`), never an interpreter found on PATH.

## The release target

`make tables-python-release` is the leg's release gate. `certify.yml`'s
release-targets job derives it from the make files by name, and its
release-gates job installs the pins through `.github/actions/python`. It runs:

- the release wire fuzz, `make tables-wire-fuzz-release-python`
  (`make/wire-fuzz-release.mk`: the plain and retain arms at seeds 1, 2 and 3),
  with the verdict pipe's control, `tables-python-wire-fuzz-negative-control`;
- the tables bench leg's gate, `tables-python-bench-gate`, under both pins, and
  its control, `tables-python-bench-gate-negative-control`;
- one measured pass of the tables bench, `bench/tables/run.sh --only python
  --bare`, so the certification log carries the leg's write and round-trip
  rates. A bench on a shared runner is a pairing check and certifies nothing.

## The tables bench leg

`bench/tables/python/leg` builds and runs `bench/tables/python/table_main.py`,
the Lua runner's sibling: the variant corpus drives it, variant 0 is compared
with `testdata/wire/bench_table.bin` and every one of the 64 records must load
and save back byte-identical before any clock starts, and a failing gate emits
no rows. The rows are `write` and `round_trip`; `read` is derived and printed
only. The clock is `time.process_time`.

The corpus declares its tables `fixed`, and a fixed class in Python reads and
writes only the fixed form (form 3): its `load` refuses a form-1 file as
`previous_form`. This bench measures form 1, so the leg generates a copy of
`bench/corpus/BenchTable.schema` whose tables are declared variable. The form-1
bytes are the same, which the gate proves on all 64 records. The interpreter is
`$PYTHON` when it is a path to one of the two pins, else the current pin under
`dist/`; a bare name is refused.

## The gates

`TestPythonReadiness` (`internal/codegen/pythontable/readiness_test.go`) holds
the release target's recipe, the leg's refusal of a bare interpreter, the
runner's single naming of the generated root, the three workflow steps that
install the pins (`ci-full.yml`'s conformance job and the reusable `leg.yml`'s
legs job, which certify's schema-legs job calls, each for a row whose
`ci.json` names `python`, and `certify.yml`'s release-gates job), the
`"python": "pin"` field of `test/conformance/python/ci.json`, the
nightly-readiness entry below and the tagged harness it runs. Under `SCHEMA_SLOW` it
runs the runner's gate under both pins over a unit generated as the leg
generates it. `TestPythonReadinessNegativeControl` plants one fault per rule in
a copy of the real file the rule reads and requires each finding by name, then
hands the gate a copy of the corpus with a flipped golden byte and one with a
flipped byte in variant 3 and requires each red by its own message.
`make test-python` runs both (`python-readiness`,
`python-readiness-negative-control`), and both are CI support rows.

## The nightly-readiness entry

`test/conformance/nightly-readiness python` runs the leg's acceptance in the
shape Goose and MOO run theirs (`test/conformance/nightly-readiness.md`,
"Python"): `implementation --lang python` through
`build/conformance-harness-python`, the harness built with
`schema_leg_python` (`make/python-ci-release.mk`; the untagged harness refuses
the name); the `python`, `pythontable` and `lockfile` packages whole under
`SCHEMA_SLOW` and the tag; the packet probes under both pins; the defaults,
UTF-8 and finite packet gates; the fixed writer's lineage, the message form
against the C++ reference and the bench leg's gate, each with its control; the
wire fuzz over `build/conformance-python` under the current pin, plain and
retaining at seeds 1 to 3 with 100000 random mutants; and the verdict-pipe and
retention controls. Every recipe that runs `go test` is run through a copy
with its 600-second cap lifted, as Goose's are. The contract tests in
`test/conformance/nightly_readiness_test.py` hold the plan, its phases and the
copied recipes for Python as for every other language.

The nightly workflow (`nightly-conformance.yml`) runs its readiness step for
the original nine only, as it does for Goose and MOO, and its port job has no
Python pin step; adding Python there is a change to that workflow.
