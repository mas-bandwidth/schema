"""Dump the records reached through each opened cooked image."""

from importlib import import_module
from _common import answering, no_codec, entries, read_bytes, unit_package, write_bytes


def run(manifest, outdir):
    for row in entries(manifest, "cook"):
        with answering(row[1]):
            name, unit, root, path = row[1:5]
            package = unit_package(unit)
            if package is None:
                no_codec(outdir, name)
                continue
            cook = import_module(package.__name__ + "._schema.cook")
            handle = cook.open_cook(getattr(package, root), read_bytes(path))
            write_bytes(outdir, name, cook.dump_cook(handle).encode("utf-8"))
