"""The write contract through the cook writer (test/conformance/README.md,
`write-refusal-cook`; SPEC-TABLES §7.6): set one field, then cook_measure and
cook in little-endian order. `wrote` when both complete, `refused` when the
cook writer raises its InvalidValue (FL-28 data_cycle, SF-29, SF-37)."""

from importlib import import_module

from _common import absent, entries, root_class, supported, unit_package, write_bytes
from _mutate import UNREPRESENTABLE, fresh_or_load


def run(manifest, outdir):
    setter = import_module("write-refusal")._set
    for row in entries(manifest, "write-refusal-cook"):
        case, unit, root, wire, path, rule, token = row[1:8]
        cls = root_class(unit, root)
        if cls is None:
            absent(outdir, case, "unit not generated for python")
            continue
        if not supported(cls):
            absent(outdir, case, "the python table runtime does not carry this construct")
            continue
        package = unit_package(unit)
        cook = import_module(package.__name__ + "._schema.cook")
        value = fresh_or_load(cls, wire)
        if setter(value, path, rule, token) is UNREPRESENTABLE:
            write_bytes(outdir, case, b"unrepresentable\n")
            continue
        try:
            size = cook.cook_measure(value)
            cook.cook(value, cook.ORDER_LITTLE, size)
        except package.InvalidValue:
            write_bytes(outdir, case, b"refused\n")
        else:
            write_bytes(outdir, case, b"wrote\n")
