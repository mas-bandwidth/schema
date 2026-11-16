"""Read and write a batch under its connection's announcement."""

from _common import absent, entries, read_bytes, root_class, unit_package, write_bytes


def run(manifest, outdir):
    connections = {row[1]: row for row in entries(manifest, "connection")}
    for row in entries(manifest, "message"):
        case, connection, root, _, wire = row[1:6]
        conn = connections[connection]
        package = unit_package(conn[2])
        cls = root_class(conn[2], root)
        message = getattr(getattr(package, "_schema", None), "_message_unit", None)
        if wire == "-" or package is None or cls is None or message is None:
            absent(outdir, case, "message form is not emitted for this root")
            continue
        vocab = message.MessageVocabulary()
        message.announce_read(vocab, read_bytes(conn[4]))
        values = message.load_messages(cls, vocab, read_bytes(wire))
        write_bytes(outdir, case, message.save_messages(cls, values))
