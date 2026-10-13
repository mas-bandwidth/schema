// Cards.cs — the C# owning facade for a typed work card (docs/SPEC-TABLES.md
// §2). It is the C# spelling of the Go facade tables/cards/cards.go, parse.go
// and render.go: a Record that keeps its own defensive copy of the wire,
// checked writes that refuse before publishing, the two customer ceilings the
// schema deliberately leaves unbounded (§2.5, §2.9), and the brief text parsed
// once and rendered many times. The generated Card codec (Schema.CardLoad,
// Schema.CardSave, Schema.CardFromJson, Schema.CardToJson) answers the wire
// and text forms; this facade is the thin owning layer a customer imports.
//
// The Go facade's calls, in C#'s own spelling:
//   Load(wire)          -> Record.Load(byte[]) (plus an out TableReport)
//   FromJSON(text)      -> Record.FromJson(byte[])
//   ToJSON(record)      -> record.ToJson()
//   (r *Record) Wire()  -> record.Wire()
//   Parse(text)         -> Record.Parse(byte[])
//   Render(record)      -> record.Render()
// A Go error is a thrown CardException here; a Go Report rides on the Record.

using System;
using System.Text;

namespace Cards
{

    // CardException is the facade's error: a refused read or write, carrying
    // the reason text a driver maps to a refusal class
    // (tables/cards/conformance/FORMAT.md).
    public sealed class CardException : Exception
    {
        public CardException(string message) : base(message) { }
    }

    // Record owns a private defensive copy of the wire and the loaded Card.
    // No generated root escapes: the wire field is the caller's bytes copied,
    // Wire() returns a fresh copy, and the Card never leaves the record
    // (docs/SPEC-TABLES.md §6.2).
    public sealed class Record
    {
        // MaxBodyBytes is the customer ceiling on one card's opaque body. The
        // schema declares body *string with no bound (docs/SPEC-TABLES.md
        // §2.5); this facade refuses a larger body before any bytes are
        // published (tables/cards/cards.go: MaxBodyBytes).
        public const int MaxBodyBytes = 64 << 10;

        // MaxListEntries is the customer ceiling on each of a card's
        // collection fields (paths/new/shared/needs/extra/consumers), which
        // the schema leaves unbounded (docs/SPEC-TABLES.md §2.9;
        // tables/cards/cards.go: MaxListEntries).
        public const int MaxListEntries = 512;

        private readonly byte[] _wire;
        private readonly Card _card;
        private readonly TableReport _report;

        private Record(byte[] wire, Card card, TableReport report)
        {
            _wire = wire;
            _card = card;
            _report = report;
        }

        // Report is the tolerant read report the load filled (the generated
        // TableReport, docs/SPEC-TABLES.md §16).
        public TableReport Report => _report;

        // Wire returns a defensive copy of the record's wire bytes
        // (tables/cards/cards.go: (*Record).Wire).
        public byte[] Wire()
        {
            var w = new byte[_wire.Length];
            Array.Copy(_wire, w, _wire.Length);
            return w;
        }

        // Load reads wire into a Record using the generated load
        // (docs/SPEC-TABLES.md §6.2; tables/cards/cards.go: Load). Failures
        // and malformed wire throw; tolerant events ride in the returned
        // report. The ORIGINAL wire is copied, never rewritten, so an old
        // input is preserved byte for byte.
        public static Record Load(byte[] wire, out TableReport report)
        {
            report = new TableReport();
            var card = new Card();
            if (Schema.CardLoadMeasure(wire) < 0)
            {
                Schema.CardLoad(card, wire, report);
                throw new CardException("cards: wire is not a card");
            }
            if (!Schema.CardLoad(card, wire, report) || report.Malformed || report.Refused)
            {
                throw new CardException("cards: wire is malformed");
            }
            var copy = new byte[wire.Length];
            Array.Copy(wire, copy, wire.Length);
            return new Record(copy, card, report);
        }

        // Str reads a generated string(N) buffer's used bytes as text.
        internal static string Str(byte[] buf, int length)
        {
            if (length < 0) length = 0;
            if (length > buf.Length) length = buf.Length;
            return Encoding.UTF8.GetString(buf, 0, length);
        }

        // BlobText reads an optional unbounded string field's text, or null
        // when the field is absent.
        internal static string BlobText(TableBlob blob)
        {
            if (blob == null || blob.Data == null) return null;
            return Encoding.UTF8.GetString(blob.Data);
        }
    }
}
