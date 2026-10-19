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
using System.Collections.Generic;
using System.IO;
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

        // ---- the brief text, parsed once (tables/cards/parse.go) ----

        // HeaderLineRegex is the admitted header grammar: a key of one letter
        // then letters/digits/hyphens, a colon, optional whitespace, and the
        // rest as the value (tables/cards/parse.go: headerLineRE).
        private static readonly Regex HeaderLineRegex = new Regex(@"^([A-Za-z][A-Za-z0-9-]*):\s*(.*)$");

        // DeadlineRegex is the customer spelling DEADLINE accepts: "finish
        // within N minutes." (tables/cards/parse.go: deadlineRE).
        private static readonly Regex DeadlineRegex = new Regex(@"^finish within ([0-9]+) minutes\.$");

        // StrandedKeys is the customer's exact case-sensitive set refused
        // once prose has begun (tables/cards/parse.go: strandedKeys). Other
        // KEY-shaped body lines, such as NOTE:, are opaque and preserved.
        private static readonly HashSet<string> StrandedKeys = new HashSet<string>
        {
            "KIND", "PATHS", "TEST", "LEGS", "SOURCE", "START", "STOP",
            "DEPENDS-ON", "PRIORITY", "NEW", "SHARED", "REPO", "BASE", "tier",
            "RESULT",
        };

        // ParsedCard is the intermediate, temporary result of reading the
        // header lines before any generated storage exists
        // (tables/cards/parse.go: parsedCard).
        private sealed class ParsedCard
        {
            public string Id = "";
            public string Sha = "";
            public Tier Tier;
            public string Repo = "";
            public string Base = "";
            public string Kind = "";
            public Priority Priority;
            public string Stream = "";
            public List<string> Paths = new List<string>();
            public List<string> News = new List<string>();
            public List<string> Shared = new List<string>();
            public List<string> Needs = new List<string>();
            public string Test = "";
            public string DoneWhen = "";
            public string Source = "";
            public int Deadline;
            public List<KeyValuePair<string, string>> Extras = new List<KeyValuePair<string, string>>();
        }

        // LineRef is one physical line: its content without the LF/CRLF
        // terminator and the character offset just past its terminator
        // (tables/cards/parse.go: lineRef).
        private struct LineRef
        {
            public string Content;
            public int TermEnd;
            public int Num;
        }

        // Parse reads admitted brief text and publishes a typed Record only
        // on complete checked success (docs/SPEC-TABLES.md §2, §6.2;
        // tables/cards/parse.go: Parse). The body is the exact suffix
        // immediately after the last header line's terminator; header layout
        // may canonicalize, body bytes never do.
        public static Record Parse(byte[] bytes)
        {
            string text;
            try
            {
                text = StrictUtf8.GetString(bytes);
            }
            catch (DecoderFallbackException)
            {
                throw new CardException("cards: brief is not valid UTF-8");
            }
            if (text.IndexOf('\0') >= 0)
            {
                throw new CardException("cards: brief contains a NUL byte");
            }

            List<LineRef> lines = SplitLines(text);
            if (lines.Count == 0)
            {
                throw new CardException("cards: brief has no RESULT line");
            }

            string key, value;
            if (!MatchHeader(lines[0].Content, out key, out value) || key != "RESULT")
            {
                throw new CardException("cards: line 1 is not a RESULT line");
            }
            var p = new ParsedCard();
            ApplyResult(p, value);

            int lastHeaderEnd = lines[0].TermEnd;
            var seen = new Dictionary<string, int> { { "RESULT", 1 } };
            bool prose = false;
            for (int li = 1; li < lines.Count; li++)
            {
                LineRef ln = lines[li];
                if (IsBlank(ln.Content)) continue;
                string k, v;
                if (!MatchHeader(ln.Content, out k, out v))
                {
                    prose = true;
                    continue;
                }
                if (prose)
                {
                    if (StrandedKeys.Contains(k))
                    {
                        throw new CardException("cards: stranded header key \"" + k + "\" on line " + ln.Num);
                    }
                    continue;
                }
                ApplyHeader(p, k, v, ln.Num, seen);
                lastHeaderEnd = ln.TermEnd;
            }

            string body = text.Substring(lastHeaderEnd);
            return Build(p, body);
        }

        // SplitLines returns every physical line with its content and its
        // terminator end offset (tables/cards/parse.go: splitLines). A
        // trailing LF does not invent a final empty line.
        private static List<LineRef> SplitLines(string text)
        {
            var refs = new List<LineRef>();
            int i = 0, n = 1;
            while (i < text.Length)
            {
                int start = i;
                int j = text.IndexOf('\n', start);
                int lineEnd = text.Length, termEnd = text.Length;
                if (j >= 0)
                {
                    lineEnd = j;
                    termEnd = j + 1;
                }
                string content = text.Substring(start, lineEnd - start);
                if (content.EndsWith("\r", StringComparison.Ordinal))
                {
                    content = content.Substring(0, content.Length - 1);
                }
                refs.Add(new LineRef { Content = content, TermEnd = termEnd, Num = n });
                n++;
                i = termEnd;
            }
            return refs;
        }

        // MatchHeader recognizes a header key line; false for prose, blank
        // or any line with leading whitespace (tables/cards/parse.go:
        // matchHeader).
        private static bool MatchHeader(string line, out string key, out string value)
        {
            Match m = HeaderLineRegex.Match(line);
            if (!m.Success)
            {
                key = null;
                value = null;
                return false;
            }
            key = m.Groups[1].Value;
            value = m.Groups[2].Value;
            return true;
        }

        // ApplyResult parses the RESULT value "id sha=<sha> tier: <tier>"
        // and validates each part (docs/SPEC-TABLES.md §2, §4;
        // tables/cards/parse.go: applyResult).
        private static void ApplyResult(ParsedCard p, string value)
        {
            int i = value.LastIndexOf(" tier:", StringComparison.Ordinal);
            if (i < 0)
            {
                throw new CardException("cards: RESULT line has no \" tier:\"");
            }
            string tierStr = value.Substring(i + " tier:".Length).Trim();
            string rest = value.Substring(0, i);
            int j = rest.LastIndexOf(" sha=", StringComparison.Ordinal);
            if (j < 0)
            {
                throw new CardException("cards: RESULT line has no \" sha=\"");
            }
            string idStr = rest.Substring(0, j).Trim();
            string shaStr = rest.Substring(j + " sha=".Length);
            CheckStrBound("id", idStr, 64);
            if (!IsHex(shaStr) || (shaStr.Length != 12 && shaStr.Length != 40))
            {
                throw new CardException("cards: RESULT sha \"" + shaStr + "\" is not 12 or 40 hex digits");
            }
            p.Id = idStr;
            p.Sha = shaStr;
            p.Tier = ParseTier(tierStr);
        }

        // ApplyHeader dispatches one header line to its field, an agreeing
        // tier, or an extra, refusing duplicates (tables/cards/parse.go:
        // applyHeader).
        private static void ApplyHeader(ParsedCard p, string key, string value, int num, Dictionary<string, int> seen)
        {
            if (value.IndexOf('\r') >= 0 || value.IndexOf('\n') >= 0)
            {
                throw new CardException("cards: " + key + " on line " + num + " has a CR or LF in its value");
            }
            if (key == "tier")
            {
                int prev;
                if (seen.TryGetValue("tier", out prev))
                {
                    throw new CardException("cards: duplicate header key \"" + key + "\" on lines " + prev + " and " + num);
                }
                seen["tier"] = num;
                Tier t = ParseTier(value.Trim());
                if (t != p.Tier)
                {
                    throw new CardException("cards: tier on line " + num + " disagrees with RESULT");
                }
                return;
            }
            if (key == "RESULT")
            {
                throw new CardException("cards: duplicate header key \"" + key + "\" on lines " + seen["RESULT"] + " and " + num);
            }
            if (ReservedHeaderKeys.Contains(key))
            {
                int prev;
                if (seen.TryGetValue(key, out prev))
                {
                    throw new CardException("cards: duplicate header key \"" + key + "\" on lines " + prev + " and " + num);
                }
                seen[key] = num;
                AssignMapped(p, key, value, num);
                return;
            }
            CheckStrBound("extra key", key, 32);
            p.Extras.Add(new KeyValuePair<string, string>(key, value));
        }

        // AssignMapped fills one mapped header field with its checked value
        // (tables/cards/parse.go: assignMapped).
        private static void AssignMapped(ParsedCard p, string key, string value, int num)
        {
            switch (key)
            {
                case "REPO": p.Repo = AssignStr(value, 96, "REPO", num); break;
                case "BASE": p.Base = AssignStr(value, 96, "BASE", num); break;
                case "KIND": p.Kind = AssignStr(value, 24, "KIND", num); break;
                case "PRIORITY": p.Priority = ParsePriority(value.Trim()); break;
                case "PATHS": p.Paths = SplitListChecked(value, 192, true, "PATHS", num); break;
                case "NEW": p.News = SplitListChecked(value, 192, false, "NEW", num); break;
                case "SHARED": p.Shared = SplitListChecked(value, 192, false, "SHARED", num); break;
                case "DEPENDS-ON": p.Needs = SplitListChecked(value, 64, false, "DEPENDS-ON", num); break;
                case "TEST": p.Test = AssignStr(value, 192, "TEST", num); break;
                case "DONE-WHEN": p.DoneWhen = value; break;
                case "DEADLINE": p.Deadline = ParseDeadline(value); break;
                case "SOURCE": p.Source = value; break;
            }
        }

        private static string AssignStr(string value, int bound, string name, int num)
        {
            CheckStrBound(name, value, bound);
            return value;
        }

        // CheckStrBound refuses a value past its declared byte bound
        // (tables/cards/parse.go: checkStrBound).
        private static void CheckStrBound(string name, string value, int bound)
        {
            if (Encoding.UTF8.GetByteCount(value) > bound)
            {
                throw new CardException("cards: " + name + " is " + Encoding.UTF8.GetByteCount(value)
                    + " bytes, past the " + bound + "-byte bound");
            }
        }

        // SplitListChecked splits a comma-separated list, trims elements,
        // drops empty ones, and enforces the element bound and the entry
        // ceiling (docs/SPEC-TABLES.md §2.9; tables/cards/parse.go:
        // splitListChecked). A whole value of "-" or empty is an empty list;
        // PATHS alone also takes the customer sentinel "none".
        private static List<string> SplitListChecked(string value, int bound, bool noneSentinel, string name, int num)
        {
            var outList = new List<string>();
            string v = value.Trim();
            if (v == "" || v == "-") return outList;
            if (noneSentinel && v == "none") return outList;
            foreach (string partRaw in v.Split(','))
            {
                string part = partRaw.Trim();
                if (part == "") continue;
                CheckStrBound(name + " element", part, bound);
                outList.Add(part);
            }
            if (outList.Count > MaxListEntries)
            {
                throw new CardException("cards: " + name + " on line " + num + " has " + outList.Count
                    + " entries, past the " + MaxListEntries + "-entry ceiling");
            }
            return outList;
        }

        // ParseTier spells a tier header value (tables/cards/parse.go:
        // parseTier).
        private static Tier ParseTier(string s)
        {
            switch (s)
            {
                case "flash": return Tier.flash;
                case "pro": return Tier.pro;
                case "heavy": return Tier.heavy;
                case "frontier": return Tier.frontier;
            }
            throw new CardException("cards: unknown tier \"" + s + "\"");
        }

        // ParsePriority spells a priority header value
        // (tables/cards/parse.go: parsePriority).
        private static Priority ParsePriority(string s)
        {
            switch (s)
            {
                case "normal": return Priority.normal;
                case "high": return Priority.high;
                case "critical": return Priority.critical;
                case "blocker": return Priority.blocker;
            }
            throw new CardException("cards: unknown priority \"" + s + "\"");
        }

        // ParseDeadline converts "finish within N minutes." into
        // deadline_minutes (tables/cards/parse.go: parseDeadline).
        private static int ParseDeadline(string s)
        {
            Match m = DeadlineRegex.Match(s.Trim());
            if (!m.Success)
            {
                throw new CardException("cards: not of the form \"finish within N minutes.\"");
            }
            long n = long.Parse(m.Groups[1].Value);
            if (n < 0 || n > 100000)
            {
                throw new CardException("cards: minutes out of range");
            }
            return (int)n;
        }

        private static bool IsHex(string s)
        {
            if (s.Length == 0) return false;
            foreach (char c in s)
            {
                bool ok = (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F');
                if (!ok) return false;
            }
            return true;
        }

        private static bool IsBlank(string line)
        {
            foreach (char c in line)
            {
                if (c != ' ' && c != '\t' && c != '\r') return false;
            }
            return true;
        }

        // Build publishes the parsed card through the generated save and the
        // checked write (docs/SPEC-TABLES.md §6.2; tables/cards/parse.go:
        // build). Bytes are published only on complete success; a refused
        // write mutates nothing.
        private static Record Build(ParsedCard p, string body)
        {
            var card = new Card();
            card.IdLength = SetFixed(card.Id, p.Id);
            card.ShaLength = SetFixed(card.Sha, p.Sha);
            card.Tier = p.Tier;
            card.RepoLength = SetFixed(card.Repo, p.Repo);
            card.BaseLength = SetFixed(card.Base, p.Base);
            card.KindLength = SetFixed(card.Kind, p.Kind);
            card.Priority = p.Priority;
            card.StreamLength = SetFixed(card.Stream, p.Stream);
            card.Paths = MakePaths(p.Paths);
            card.PathsCount = p.Paths.Count;
            card.New = MakePaths(p.News);
            card.NewCount = p.News.Count;
            card.Shared = MakePaths(p.Shared);
            card.SharedCount = p.Shared.Count;
            card.Needs = MakeIds(p.Needs);
            card.NeedsCount = p.Needs.Count;
            card.TestLength = SetFixed(card.Test, p.Test);
            if (p.DoneWhen != "") card.Done = new TableBlob(Encoding.UTF8.GetBytes(p.DoneWhen));
            card.DeadlineMinutes = p.Deadline;
            if (p.Source != "") card.Source = new TableBlob(Encoding.UTF8.GetBytes(p.Source));
            card.Extra = MakeExtras(p.Extras);
            card.ExtraCount = p.Extras.Count;
            if (body.Length > 0) card.Body = new TableBlob(Encoding.UTF8.GetBytes(body));
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
            return new Record(wire, card, new TableReport());
        }

        private static int SetFixed(byte[] buf, string s)
        {
            byte[] b = Encoding.UTF8.GetBytes(s);
            Array.Copy(b, buf, b.Length);
            return b.Length;
        }

        private static PathText[] MakePaths(List<string> elems)
        {
            var rows = new PathText[elems.Count];
            for (int i = 0; i < elems.Count; i++)
            {
                var row = new PathText();
                row.SLength = SetFixed(row.S, elems[i]);
                rows[i] = row;
            }
            return rows;
        }

        private static CardID[] MakeIds(List<string> elems)
        {
            var rows = new CardID[elems.Count];
            for (int i = 0; i < elems.Count; i++)
            {
                var row = new CardID();
                row.SLength = SetFixed(row.S, elems[i]);
                rows[i] = row;
            }
            return rows;
        }

        private static KeyValue[] MakeExtras(List<KeyValuePair<string, string>> elems)
        {
            var rows = new KeyValue[elems.Count];
            for (int i = 0; i < elems.Count; i++)
            {
                var row = new KeyValue();
                row.KeyLength = SetFixed(row.Key, elems[i].Key);
                row.Value = new TableBlob(Encoding.UTF8.GetBytes(elems[i].Value));
                rows[i] = row;
            }
            return rows;
        }

        // ---- the informational text view (tables/cards/render.go) ----

        // Render returns the record's informational text: RESULT first, the
        // mapped keys in a fixed documented order, extras in stored order,
        // then the body bytes verbatim (docs/SPEC-TABLES.md §6.2, §16;
        // tables/cards/render.go: Render). It is a known-field view, never
        // an archival/machine-state rewrite.
        public byte[] Render()
        {
            Card card = _card;
            var outp = new MemoryStream();
            WriteText(outp, "RESULT: ");
            WriteText(outp, Str(card.Id, card.IdLength));
            WriteText(outp, " sha=");
            WriteText(outp, Str(card.Sha, card.ShaLength));
            WriteText(outp, " tier: ");
            WriteText(outp, TierString(card.Tier));
            outp.WriteByte((byte)'\n');

            EmitStr(outp, "REPO", Str(card.Repo, card.RepoLength));
            EmitStr(outp, "BASE", Str(card.Base, card.BaseLength));
            EmitStr(outp, "KIND", Str(card.Kind, card.KindLength));
            string prio = PriorityString(card.Priority);
            if (prio != "")
            {
                WriteText(outp, "PRIORITY: ");
                WriteText(outp, prio);
                outp.WriteByte((byte)'\n');
            }
            EmitList(outp, "PATHS", CollectPaths(card.Paths, card.PathsCount));
            EmitList(outp, "NEW", CollectPaths(card.New, card.NewCount));
            EmitList(outp, "SHARED", CollectPaths(card.Shared, card.SharedCount));
            EmitList(outp, "DEPENDS-ON", CollectIds(card.Needs, card.NeedsCount));
            EmitStr(outp, "TEST", Str(card.Test, card.TestLength));
            EmitPtr(outp, "DONE-WHEN", card.Done);
            EmitDeadline(outp, card.DeadlineMinutes);
            EmitPtr(outp, "SOURCE", card.Source);

            for (int i = 0; i < card.ExtraCount && i < card.Extra.Length; i++)
            {
                KeyValue e = card.Extra[i];
                WriteText(outp, Str(e.Key, e.KeyLength));
                WriteText(outp, ": ");
                if (e.Value != null && e.Value.Data != null)
                {
                    outp.Write(e.Value.Data, 0, e.Value.Data.Length);
                }
                outp.WriteByte((byte)'\n');
            }

            if (card.Body != null && card.Body.Data != null)
            {
                outp.Write(card.Body.Data, 0, card.Body.Data.Length);
            }
            return outp.ToArray();
        }

        private static void WriteText(MemoryStream outp, string s)
        {
            byte[] b = Encoding.UTF8.GetBytes(s);
            outp.Write(b, 0, b.Length);
        }

        private static List<string> CollectPaths(PathText[] list, int count)
        {
            var outList = new List<string>();
            for (int i = 0; i < count && i < list.Length; i++)
            {
                outList.Add(Str(list[i].S, list[i].SLength));
            }
            return outList;
        }

        private static List<string> CollectIds(CardID[] list, int count)
        {
            var outList = new List<string>();
            for (int i = 0; i < count && i < list.Length; i++)
            {
                outList.Add(Str(list[i].S, list[i].SLength));
            }
            return outList;
        }

        // TierString spells a tier for the RESULT line
        // (tables/cards/render.go: tierString).
        private static string TierString(Tier t)
        {
            switch (t)
            {
                case Tier.flash: return "flash";
                case Tier.pro: return "pro";
                case Tier.heavy: return "heavy";
                case Tier.frontier: return "frontier";
            }
            throw new CardException("cards: tier " + (int)t + " has no header spelling");
        }

        // PriorityString spells a priority for its header line; None spells
        // empty and emits no line (tables/cards/render.go: priorityString).
        private static string PriorityString(Priority p)
        {
            switch (p)
            {
                case Priority.None: return "";
                case Priority.normal: return "normal";
                case Priority.high: return "high";
                case Priority.critical: return "critical";
                case Priority.blocker: return "blocker";
            }
            throw new CardException("cards: priority " + (int)p + " has no header spelling");
        }

        private static void EmitStr(MemoryStream outp, string key, string value)
        {
            if (value == "") return;
            WriteText(outp, key);
            WriteText(outp, ": ");
            WriteText(outp, value);
            outp.WriteByte((byte)'\n');
        }

        private static void EmitPtr(MemoryStream outp, string key, TableBlob blob)
        {
            if (blob == null || blob.Data == null) return;
            WriteText(outp, key);
            WriteText(outp, ": ");
            outp.Write(blob.Data, 0, blob.Data.Length);
            outp.WriteByte((byte)'\n');
        }

        private static void EmitDeadline(MemoryStream outp, int minutes)
        {
            if (minutes == 0) return;
            WriteText(outp, "DEADLINE: finish within ");
            WriteText(outp, minutes.ToString());
            WriteText(outp, " minutes.");
            outp.WriteByte((byte)'\n');
        }

        private static void EmitList(MemoryStream outp, string key, List<string> elems)
        {
            if (elems.Count == 0) return;
            WriteText(outp, key);
            WriteText(outp, ": ");
            for (int i = 0; i < elems.Count; i++)
            {
                if (i > 0) WriteText(outp, ", ");
                WriteText(outp, elems[i]);
            }
            outp.WriteByte((byte)'\n');
        }
    }
}
