"""The write contract through the block builder (test/conformance/README.md,
`write-refusal-block`; SPEC-TABLES §19.1, SF-31): set one field, then build
the root's block from the value. `wrote` when BeginFrom completes, `refused`
when it raises InvalidValue."""

from importlib import import_module

from _common import absent, entries, root_class, supported, unit_package, write_bytes
from _mutate import UNREPRESENTABLE, fresh_or_load


def run(manifest, outdir):
    setter = import_module("write-refusal")._set
    for row in entries(manifest, "write-refusal-block"):
        case, unit, root, wire, path, rule, token = row[1:8]
        cls = root_class(unit, root)
        if cls is None:
            absent(outdir, case, "unit not generated for python")
            continue
        if not supported(cls):
            absent(outdir, case, "the python table runtime does not carry this construct")
            continue
        package = unit_package(unit)
        blockbuild = import_module(package.__name__ + "._schema.blockbuild")
        value = fresh_or_load(cls, wire)
        if setter(value, path, rule, token) is UNREPRESENTABLE:
            write_bytes(outdir, case, b"unrepresentable\n")
            continue
        try:
            blockbuild.BeginFrom(package, root, value)
        except package.InvalidValue:
            write_bytes(outdir, case, b"refused\n")
        else:
            write_bytes(outdir, case, b"wrote\n")
