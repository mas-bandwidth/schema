"""Read into the caller's values (test/conformance/README.md, `read-into`;
SPEC-TABLES §6.5).

The Python reads take ONE report but no caller values: load, from_json,
fixed_load and load_messages each return new values, and a refusal is raised
as Refused (docs/PORT-PYTHON.md, ERRORS). The driver holds `<count>` fresh
values as the caller would, rebinds them to what a read returns, and answers
what each call did: `read` when it returned, `threw` when it raised (the one
place the driver catches, only to say so; FL-29). It never answers `refused`
for the runtime.
"""

from _common import entries, read_bytes, root_class, unit_package, write_bytes


def run(manifest, outdir):
    for row in entries(manifest, "read-into"):
        case, unit, root, form, lawful, damaged, count, announcement = row[1:9]
        package = unit_package(unit)
        cls = root_class(unit, root)
        if package is None or cls is None:
            raise RuntimeError(case + ": unit " + unit + " root " + root + " is not generated")
        report = package.LoadReport()
        if form == "fixed-nocap":
            # FL-17: the fixed load with NO stated capacity
            try:
                values = cls.fixed_load(read_bytes(lawful), report=report)
            except package.Refused:
                write_bytes(outdir, case, b"threw\n")
            else:
                write_bytes(outdir, case, b"read\n%d\n" % len(values))
            continue
        n = int(count)
        if form == "variable":
            def call(data):
                return [cls.load(data, report)]
        elif form == "json":
            def call(data):
                return [cls.from_json(data, report)]
        elif form == "fixed":
            def call(data):
                return cls.fixed_load(data, n, report)
        elif form == "message":
            vocab = package.MessageVocabulary()
            package.announce_read(vocab, read_bytes(announcement))

            def call(data):
                return package.load_messages(cls, vocab, data, n, report)
        else:
            raise ValueError(case + ": unknown read-into form " + form)
        values = [cls() for _ in range(n)]
        out = bytearray()

        def read(path):
            nonlocal values
            try:
                values = call(read_bytes(path))
            except package.Refused:
                out.extend(b"threw\n")
            else:
                out.extend(b"read\n")

        read(lawful)
        read(damaged)
        for value in values:
            out.extend(value.to_json().encode("utf-8"))
        read(lawful)
        write_bytes(outdir, case, bytes(out))
