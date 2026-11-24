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
        if form == "retain":
            # `retain` (README, "The `retain` form"): the retaining load into ONE
            # PRE-FILLED report must leave what the same load leaves in a FRESH
            # report (SPEC-TABLES 6.5: every read zeroes a supplied report, the
            # retain family included). Every field this report has is settable.
            def retaining(data, rep):
                store = package.TableRetain(bytearray(1 << 20), bytearray(24 * (1 << 17)))
                return cls.load_retain(data, store, rep)

            lawful_bytes = read_bytes(lawful)
            fresh = package.LoadReport()
            retaining(lawful_bytes, fresh)
            report = package.LoadReport()
            report.unknown = report.widened = report.retained = report.retain_lost = 7
            report.refused = True
            report.reason = "stale"
            out = bytearray()

            def step(data):
                try:
                    retaining(data, report)
                except package.Refused:
                    return b"refused\n"
                return b"refused\n" if report.refused else b"read\n"

            def zeroed():
                if all(getattr(report, s) == getattr(fresh, s) for s in report.__slots__):
                    return b"zeroed\n"
                kept = {s: getattr(report, s) for s in report.__slots__}
                want = {s: getattr(fresh, s) for s in fresh.__slots__}
                return ("kept %r want %r\n" % (kept, want)).encode()

            out.extend(step(lawful_bytes))
            out.extend(zeroed())
            out.extend(step(read_bytes(damaged)))
            out.extend(step(lawful_bytes))
            out.extend(zeroed())
            write_bytes(outdir, case, bytes(out))
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
