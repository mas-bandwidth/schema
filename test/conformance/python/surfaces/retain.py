"""The retaining read surface: report and complete known JSON value."""

from _common import absent, entries, read_bytes, root_class, unit_package, write_bytes


def run(manifest, outdir):
    for row in entries(manifest, "retain"):
        case, unit, root, wire, capacity, ids = row[1:7]
        try:
            package = unit_package(unit)
            cls = root_class(unit, root)
            if package is None or cls is None or not hasattr(package, "TableRetain"):
                absent(outdir, case, "unit does not provide retaining file reads")
                continue
            id_count = 1 << 17 if ids == "full" else int(ids)
            data = read_bytes(wire)
            byte_count = 1 << 20
            if capacity == "short":
                probe = package.TableRetain(bytearray(byte_count), bytearray(24 * id_count))
                cls.load_retain(data, probe, package.LoadReport())
                byte_count = max(0, probe.used - 1)
            retain = package.TableRetain(bytearray(byte_count), bytearray(24 * id_count))
            report = package.LoadReport()
            value = cls.load_retain(data, retain, report)
            text = value.to_json()
            write_bytes(outdir, case, ("read\n" + text).encode("utf-8"))
        except Exception as err:  # noqa: BLE001 - a case this leg cannot answer is absent
            absent(outdir, case, err)
