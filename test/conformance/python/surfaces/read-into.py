"""Read into the caller's values (test/conformance/README.md, `read-into`;
SPEC-TABLES §6.5).

Uses load_into, from_json_into, fixed_load_into and load_messages_into with one
report and one set of caller values (FL-02, FL-03, FL-29), and fixed_load with
omitted capacity for fixed-nocap (FL-17).
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
        values = [cls() for _ in range(n)]
        if form == "variable":
            def call(data):
                return cls.load_into(values[0], data, report)
        elif form == "json":
            def call(data):
                return cls.from_json_into(values[0], data, report)
        elif form == "fixed":
            def call(data):
                return cls.fixed_load_into(values, data, n, report) >= 1
        elif form == "message":
            vocab = package.MessageVocabulary()
            package.announce_read(vocab, read_bytes(announcement))

            def call(data):
                return package.load_messages_into(cls, values, vocab, data, n, report) >= 1
        else:
            raise ValueError(case + ": unknown read-into form " + form)
        out = bytearray()

        def read(path):
            try:
                ok = call(read_bytes(path))
            except package.Refused:
                out.extend(b"threw\n")
            else:
                if ok and not report.refused:
                    out.extend(b"read\n")
                else:
                    out.extend(b"refused\n")

        read(lawful)
        read(damaged)
        for value in values:
            out.extend(value.to_json().encode("utf-8"))
        read(lawful)
        write_bytes(outdir, case, bytes(out))
