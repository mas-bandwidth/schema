#!/usr/bin/env python3
"""Emit a Dart driver that exercises every generated *Wire.dart unit.

The differential is over the generator's own corpus, not one hand-picked
example: this scans a generated tree (pass the web-safe copy for the runtime
lane and the unmodified copy for the compile lane), imports every unit, and
round-trips each declared table through its own `*WireSave`/`*WireLoad`
writer and reader. The runtime lane diffs the VM transcript against dart2js;
the compile lane hands the same driver to dart2js so every unit is compiled.

Nothing here rewrites generated code. The driver is written under build/ and
is not tracked.
"""
import glob
import os
import re
import sys

SAVE = re.compile(r"^Uint8List (\w+)\((\w+) v\) \{", re.M)


def main(root, out):
    units = sorted(glob.glob(os.path.join(root, "**", "*Wire.dart"), recursive=True))
    if not units:
        sys.exit("corpus_driver: no *Wire.dart under %s" % root)
    here = os.path.dirname(os.path.abspath(out))
    imports = []
    calls = []
    count = 0
    for i, path in enumerate(units):
        prefix = "u%d" % i
        rel = os.path.relpath(os.path.abspath(path), here).replace(os.sep, "/")
        imports.append("import '%s' as %s;" % (rel, prefix))
        with open(path) as f:
            src = f.read()
        for m in SAVE.finditer(src):
            save, cls = m.group(1), m.group(2)
            if not save.endswith("WireSave"):
                continue
            load = save[: -len("Save")] + "Load"
            label = "%s.%s" % (prefix, cls)
            calls.append(
                "  {\n"
                "    final v = %s.%s();\n"
                "    final b = %s.%s(v);\n"
                "    final r = %s.%s(b);\n"
                "    final b2 = %s.%s(r);\n"
                "    out.writeln('%s=' + hex(b) + '/' + hex(b2));\n"
                "  }" % (prefix, cls, prefix, save, prefix, load, prefix, save, label)
            )
            count += 1
    body = "\n".join(imports)
    body += (
        "\n\nString hex(List<int> bytes) =>\n"
        "    bytes.map((b) => b.toRadixString(16).padLeft(2, '0')).join();\n\n"
        "void main() {\n"
        "  final out = StringBuffer();\n"
    )
    body += "\n".join(calls)
    body += (
        "\n  print('corpus-units=%d');\n"
        "  print('corpus-tables=%d');\n"
        "  print(out.toString());\n"
        "}\n" % (len(units), count)
    )
    with open(out, "w") as f:
        f.write(body)
    print("corpus_driver: %d units, %d tables -> %s" % (len(units), count, out))


if __name__ == "__main__":
    main(sys.argv[1], sys.argv[2])
