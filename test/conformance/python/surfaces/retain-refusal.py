"""The retain-refusal surface: one store through a read, a save, a refused read
and a second save (test/conformance/README.md)."""

from _common import answering, no_codec, entries, read_bytes, root_class, supported, unit_package, write_bytes


def run(manifest, outdir):
    for row in entries(manifest, "retain-refusal"):
        with answering(row[1]):
            case, unit, root, first_wire, refused_wire = row[1:6]
            try:
                package = unit_package(unit)
                cls = root_class(unit, root)
                if package is None or cls is None or not hasattr(package, "TableRetain") or not supported(cls):
                    no_codec(outdir, case)
                    continue
                retain = package.TableRetain(bytearray(1 << 20), bytearray(24 * (1 << 17)))
                try:
                    value = cls.load_retain(read_bytes(first_wire), retain, package.LoadReport())
                except package.Refused:
                    write_bytes(outdir, case, b"refused\n")
                    continue
                write_bytes(outdir, case + ".before", value.save_retain(retain, package.LoadReport()))
                answer = "read\n"
                try:
                    cls.load_retain(read_bytes(refused_wire), retain, package.LoadReport())
                    answer += "read\n"
                except package.Refused:
                    answer += "refused\n"
                # a store the refused read unbound from the first value may
                # refuse the save rather than write without its records
                try:
                    after = value.save_retain(retain, package.LoadReport())
                except package.InvalidValue:
                    answer += "save-refused\n"
                else:
                    write_bytes(outdir, case + ".wire", after)
                    answer += "saved\n"
                write_bytes(outdir, case, answer.encode("utf-8"))
            except Exception as err:
                raise RuntimeError(f"{case}: retaining file operation failed: {err}") from err
