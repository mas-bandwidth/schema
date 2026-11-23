"""Measure and write each recipe in both target byte orders."""

from importlib import import_module
from _common import answering, no_codec, entries, read_bytes, unit_package, write_bytes


def run(manifest, outdir):
    instances = {row[1]: row for row in entries(manifest, "instance")}
    for row in entries(manifest, "cook-write"):
        with answering(row[1]):
            name, text, source, alias = row[1:5]
            instance = instances[name]
            package = unit_package(instance[2])
            if package is None:
                no_codec(outdir, name)
                no_codec(outdir, name + "-be")
                continue
            cls = getattr(package, instance[3])
            if text == "-":
                value = cls.load(read_bytes(instance[4]))
            else:
                value = cls.from_json(read_bytes(text).decode("utf-8"))
            if source != "-":
                # A BlobRef explicitly identifies one shared in-memory node.
                body = getattr(value, source)
                if isinstance(body, (bytearray, memoryview)):
                    body = bytes(body)
                shared = package.BlobRef(body)
                setattr(value, source, shared)
                setattr(value, alias, shared)
            cook = import_module(package.__name__ + "._schema.cook")
            size = cook.cook_measure(value)
            for order, suffix in ((1, ""), (2, "-be")):
                try:
                    cook.cook(value, order, size - 1)
                except package.InvalidValue:
                    pass
                else:
                    raise RuntimeError(name + ": short capacity did not refuse")
                result = cook.cook(value, order, size)
                if len(result) != size:
                    raise RuntimeError(name + ": measure and write disagree")
                write_bytes(outdir, name + suffix, result)
