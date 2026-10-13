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
using System.Text.RegularExpressions;

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
        // input is preserved byte for byte. Load enforces the known-field
        // bounds without rewriting its input.
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
            CheckCard(card);
            var copy = new byte[wire.Length];
            Array.Copy(wire, copy, wire.Length);
            return new Record(copy, card, report);
        }

        // FromJson builds a Record from JSON text through the generated text
        // read and the generated save (docs/SPEC-TABLES.md §16;
        // tables/cards/cards.go: FromJSON). Unknown, mismatched, clamped and
        // malformed inputs are refused, as are the customer bounds. Bytes are
        // published only on complete success; a refused write leaves no
        // destination and mutates nothing.
        public static Record FromJson(byte[] text)
        {
            int i = 0;
            while (i < text.Length && (text[i] == (byte)' ' || text[i] == (byte)'\t' || text[i] == (byte)'\n' || text[i] == (byte)'\r')) i++;
            if (i >= text.Length || text[i] != (byte)'{')
            {
                throw new CardException("cards: JSON is malformed");
            }
            var card = new Card();
            var report = new TableReport();
            if (!Schema.CardFromJson(card, text, report) || report.Malformed)
            {
                throw new CardException("cards: JSON is malformed");
            }
            if (report.Unknown > 0 || report.KindMismatch > 0 || report.Clamped > 0)
            {
                throw new CardException("cards: JSON has loss events (unknown=" + report.Unknown
                    + " kindMismatch=" + report.KindMismatch + " clamped=" + report.Clamped
                    + "): refused, never truncated");
            }
            CheckCard(card);
            long size = Schema.CardMeasure(card);
            if (size < 0)
            {
                throw new CardException("cards: measure refused the built card");
            }
            var wire = new byte[size];
            if (Schema.CardSave(card, wire) != size)
            {
                throw new CardException("cards: save did not fill the measured wire");
            }
            return new Record(wire, card, report);
        }

        // ToJson writes the record's known-field JSON text (docs/SPEC-TABLES.md
        // §16; tables/cards/cards.go: ToJSON). It is the known-field view, not
        // a lossless archival rewrite.
        public byte[] ToJson()
        {
            long size = Schema.CardToJsonMeasure(_card);
            if (size < 0)
            {
                throw new CardException("cards: the card has no JSON spelling");
            }
            var buf = new byte[size];
            if (Schema.CardToJson(_card, buf) != size)
            {
                throw new CardException("cards: JSON write did not fill the measured text");
            }
            return buf;
        }

        // CheckCard is the checked-write helper: the customer bounds and the
        // well-formedness of the opaque body and every collection element,
        // enforced before bytes are published (docs/SPEC-TABLES.md §2.5,
        // §2.9; tables/cards/cards.go: checkCard).
        internal static void CheckCard(Card card)
        {
            if (card.Body != null && card.Body.Data != null)
            {
                byte[] body = card.Body.Data;
                if (body.Length > MaxBodyBytes)
                {
                    throw new CardException("cards: body is " + body.Length
                        + " bytes, past the " + MaxBodyBytes + "-byte ceiling");
                }
                if (!ValidUtf8NoNul(body))
                {
                    throw new CardException("cards: body is not well-formed UTF-8 without NUL");
                }
            }
            CheckPathList(card.Paths, card.PathsCount, "paths");
            CheckPathList(card.New, card.NewCount, "_new");
            CheckPathList(card.Shared, card.SharedCount, "shared");
            CheckIdList(card.Needs, card.NeedsCount, "needs");
            CheckExtras(card.Extra, card.ExtraCount);
            CheckConsumers(card.Consumers, card.ConsumersCount);
        }

        private static void CheckPathList(PathText[] list, int count, string name)
        {
            if (count < 0 || count > MaxListEntries)
            {
                throw new CardException("cards: " + name + " has " + count
                    + " entries, past the " + MaxListEntries + "-entry ceiling");
            }
            for (int i = 0; i < count && i < list.Length; i++)
            {
                if (!ValidUtf8NoNul(list[i].S, list[i].SLength))
                {
                    throw new CardException("cards: " + name
                        + " has an element that is not well-formed UTF-8 without NUL");
                }
            }
        }

        private static void CheckIdList(CardID[] list, int count, string name)
        {
            if (count < 0 || count > MaxListEntries)
            {
                throw new CardException("cards: " + name + " has " + count
                    + " entries, past the " + MaxListEntries + "-entry ceiling");
            }
            for (int i = 0; i < count && i < list.Length; i++)
            {
                if (!ValidUtf8NoNul(list[i].S, list[i].SLength))
                {
                    throw new CardException("cards: " + name
                        + " has an element that is not well-formed UTF-8 without NUL");
                }
            }
        }

        // CheckExtras enforces the extras rules before any facade write
        // publishes: the 512-entry ceiling shared with the other collections,
        // the admitted header key grammar, no CR or LF in a key or value,
        // well-formed UTF-8 keys and values without NUL, and no key
        // duplicating a mapped header name (docs/SPEC-TABLES.md §2.9;
        // tables/cards/cards.go: checkExtras).
        private static void CheckExtras(KeyValue[] list, int count)
        {
            if (count < 0 || count > MaxListEntries)
            {
                throw new CardException("cards: extra has " + count
                    + " entries, past the " + MaxListEntries + "-entry ceiling");
            }
            for (int i = 0; i < count && i < list.Length; i++)
            {
                KeyValue e = list[i];
                if (!ValidUtf8NoNul(e.Key, e.KeyLength))
                {
                    throw new CardException("cards: extra has a key that is not well-formed UTF-8 without NUL");
                }
                string k = Str(e.Key, e.KeyLength);
                if (!HeaderKeyRegex.IsMatch(k))
                {
                    throw new CardException("cards: extra key \"" + k + "\" is not an admitted header key");
                }
                if (k.IndexOf('\r') >= 0 || k.IndexOf('\n') >= 0)
                {
                    throw new CardException("cards: extra has a key with a CR or LF");
                }
                if (ReservedHeaderKeys.Contains(k))
                {
                    throw new CardException("cards: extra key \"" + k + "\" duplicates a mapped header key");
                }
                if (e.Value != null && e.Value.Data != null)
                {
                    if (!ValidUtf8NoNul(e.Value.Data))
                    {
                        throw new CardException("cards: extra has a value that is not well-formed UTF-8 without NUL");
                    }
                    string v = Encoding.UTF8.GetString(e.Value.Data);
                    if (v.IndexOf('\r') >= 0 || v.IndexOf('\n') >= 0)
                    {
                        throw new CardException("cards: extra has a value with a CR or LF");
                    }
                }
            }
        }

        // CheckConsumers enforces the consumers collection ceiling and the
        // well-formedness of every consumer string, using the same rules as
        // the other collections (docs/SPEC-TABLES.md §2.9;
        // tables/cards/cards.go: checkConsumers).
        private static void CheckConsumers(Consumer[] list, int count)
        {
            if (count < 0 || count > MaxListEntries)
            {
                throw new CardException("cards: consumers has " + count
                    + " entries, past the " + MaxListEntries + "-entry ceiling");
            }
            for (int i = 0; i < count && i < list.Length; i++)
            {
                Consumer e = list[i];
                var fields = new (byte[] Buf, int Len, string Name)[]
                {
                    (e.Kind, e.KindLength, "kind"),
                    (e.Who, e.WhoLength, "who"),
                    (e.Route, e.RouteLength, "route"),
                    (e.Model, e.ModelLength, "model"),
                    (e.Outcome, e.OutcomeLength, "outcome"),
                    (e.Line, e.LineLength, "line"),
                };
                foreach ((byte[] buf, int len, string name) in fields)
                {
                    if (!ValidUtf8NoNul(buf, len))
                    {
                        throw new CardException("cards: consumers has a " + name
                            + " that is not well-formed UTF-8 without NUL");
                    }
                }
            }
        }

        private static readonly UTF8Encoding StrictUtf8 = new UTF8Encoding(false, true);

        // ValidUtf8NoNul reports whether data is well-formed UTF-8 holding
        // no NUL byte (tables/cards/cards.go: checkCard's element rule).
        internal static bool ValidUtf8NoNul(byte[] data)
        {
            return ValidUtf8NoNul(data, data.Length);
        }

        internal static bool ValidUtf8NoNul(byte[] buf, int length)
        {
            if (length < 0) length = 0;
            if (length > buf.Length) length = buf.Length;
            for (int i = 0; i < length; i++)
            {
                if (buf[i] == 0) return false;
            }
            try
            {
                StrictUtf8.GetString(buf, 0, length);
                return true;
            }
            catch (DecoderFallbackException)
            {
                return false;
            }
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

        // HeaderKeyRegex is the admitted key grammar alone: one letter then
        // letters/digits/hyphens (tables/cards/parse.go: headerKeyRE).
        internal static readonly Regex HeaderKeyRegex = new Regex("^[A-Za-z][A-Za-z0-9-]*$");

        // ReservedHeaderKeys is the mapped header vocabulary, including
        // RESULT and the agreeing tier line (tables/cards/parse.go:
        // reservedHeaderKeys).
        internal static readonly System.Collections.Generic.HashSet<string> ReservedHeaderKeys =
            new System.Collections.Generic.HashSet<string>
            {
                "RESULT", "tier", "REPO", "BASE", "KIND", "PRIORITY", "PATHS",
                "NEW", "SHARED", "DEPENDS-ON", "TEST", "DONE-WHEN", "DEADLINE",
                "SOURCE",
            };
    }
}
