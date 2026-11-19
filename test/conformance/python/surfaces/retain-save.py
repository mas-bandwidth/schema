"""The retained rewrite surface, using the caller's two bounded stores."""

from _common import absent, entries, read_bytes, root_class, supported, unit_package, write_bytes


def run(manifest, outdir):
    for row in entries(manifest, "retain"):
        case, unit, root, wire, capacity, ids = row[1:7]
        try:
            package = unit_package(unit)
            cls = root_class(unit, root)
            if package is None or cls is None or not hasattr(package, "TableRetain") or not supported(cls):
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
            try:
                value = cls.load_retain(data, retain, report)
            except ValueError:
                continue
            write_bytes(outdir, case, value.save_retain(retain, report))
        except Exception as err:
            raise RuntimeError(f"{case}: retaining file operation failed: {err}") from err
