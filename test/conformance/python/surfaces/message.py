"""Read and write a batch under its connection's announcement."""

from _common import absent, answering, entries, no_codec, read_bytes, root_class, unit_package, write_bytes


def run(manifest, outdir):
    connections = {row[1]: row for row in entries(manifest, "connection")}
    for row in entries(manifest, "message"):
        with answering(row[1]):
            case, connection, root, _, wire = row[1:6]
            conn = connections[connection]
            package = unit_package(conn[2])
            cls = root_class(conn[2], root)
            message = getattr(getattr(package, "_schema", None), "_message_unit", None)
            # NO CODEC FIRST, whatever the row owes: a unit, root or message
            # form this leg does not generate is an absence, on a must-refuse
            # row as on any other
            if package is None or cls is None or message is None:
                no_codec(outdir, case)
                continue
            if wire == "-":
                # a present codec is held to the whole inapplicability check
                if root in message.ROOTS:
                    raise RuntimeError(f"{case}: root unexpectedly carries message form")
                try:
                    message.save_messages(cls, [cls()])
                except package.InvalidValue as error:
                    if error.rule != "message root or vocabulary is invalid":
                        raise
                else:
                    raise RuntimeError(f"{case}: message writer accepted an inapplicable root")
                try:
                    message.load_messages(cls, message.MessageVocabulary(), b"")
                except package.Refused as error:
                    if error.reason != "invalid_root":
                        raise
                else:
                    raise RuntimeError(f"{case}: message reader accepted an inapplicable root")
                absent(outdir, case, "message form is inapplicable to this variable root")
                continue
            vocab = message.MessageVocabulary()
            try:
                message.announce_read(vocab, read_bytes(conn[4]))
                values = message.load_messages(cls, vocab, read_bytes(wire))
            except package.Refused as error:
                # the present codec's public refusal is its answer (the
                # verdict, never the reason, is compared)
                absent(outdir, case, error.reason)
                continue
            write_bytes(outdir, case, message.save_messages(cls, values))
    # message-batch <case> <connection> <root> <wire>: load with room for every
    # body and answer the count load_messages returned (docs/SPEC-TABLES.md
    # §3.3; FORMAT.md, `message-batch`). The Python read raises Refused rather
    # than returning a count, so a refused batch counts 0.
    for row in entries(manifest, "message-batch"):
        case, connection, root, wire = row[1:5]
        conn = connections[connection]
        package = unit_package(conn[2])
        cls = root_class(conn[2], root)
        message = getattr(getattr(package, "_schema", None), "_message_unit", None)
        if package is None or cls is None or message is None:
            absent(outdir, case, "message form is not emitted for this root")
            continue
        vocab = message.MessageVocabulary()
        message.announce_read(vocab, read_bytes(conn[4]))
        try:
            count = len(message.load_messages(cls, vocab, read_bytes(wire), 256))
        except package.Refused:
            count = 0
        write_bytes(outdir, case, f"count {count}\n".encode())
