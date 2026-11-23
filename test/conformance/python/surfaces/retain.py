"""The retaining read surface: report and complete known JSON value."""

from _common import answering, no_codec, entries, read_bytes, root_class, supported, unit_package, write_bytes


def run(manifest, outdir):
    for row in entries(manifest, "retain"):
        with answering(row[1]):
            case, unit, root, wire, capacity, ids = row[1:7]
            try:
                package = unit_package(unit)
                cls = root_class(unit, root)
                if package is None or cls is None or not hasattr(package, "TableRetain") or not supported(cls):
                    no_codec(outdir, case)
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
                    write_bytes(outdir, case, b"refused\n")
                    continue
                if not hasattr(value, "to_json"):
                    no_codec(outdir, case)
                    continue
                text = value.to_json()
                write_bytes(outdir, case, ("read\n" + text).encode("utf-8"))
            except Exception as err:
                raise RuntimeError(f"{case}: retaining file operation failed: {err}") from err
