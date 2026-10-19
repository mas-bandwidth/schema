// THE C# HOSTILE-CORPUS RUNNER (docs/HOSTILE-CORPUS.md).
//
// For every row of the shared manifest, decode the raw input with the carrier
// the row names, then compare the verdict, the named reason and (for an
// accept) the SHA-256 of the generated reader's canonical text. The corpus and
// manifest are data; nothing here holds an expected verdict.
//
//   driver corpus <corpus-dir>

using System;
using System.IO;
using System.Security.Cryptography;
using System.Text;

static partial class Program
{
    static string CorpusUnit(string schema)
    {
        switch (schema)
        {
            case "tables/examples": return "tabledemo";
            case "tables/blobs": return "blobdemo";
            case "tables/stream": return "streamdemo";
            case "tables/messages": return "messagedemo";
            case "tables/scalars": return "scalars";
            case "tables/pointers": return "graphdemo";
            case "schemas/probe.schema": return "probe";
            default: return null;
        }
    }

    static string Digest(Codec codec, object value)
    {
        long size = codec.ToJsonMeasure(value);
        if (size < 0) { throw new InvalidOperationException("ToJson refuses an accepted value"); }
        byte[] text = new byte[checked((int)size)];
        if (codec.ToJson(value, text) != size) { throw new InvalidOperationException("ToJson wrote a size its measure did not name"); }
        // The C# writer ends its text with a newline; the engine's canonical
        // text (the digest's subject) does not. The decoded VALUE is the same,
        // so the digest is taken over the text without that one terminator.
        int length = text.Length;
        if (length > 0 && text[length - 1] == (byte)'\n') { length--; }
        return Convert.ToHexString(SHA256.HashData(new ReadOnlySpan<byte>(text, 0, length))).ToLowerInvariant();
    }

    static int HostileCorpus(string dir)
    {
        string manifest = Path.Combine(dir, "manifest.tsv");
        if (Find("tabledemo", "GlobalSettings") == null)
        {
            codecs.Add(Row<Tabledemo.GlobalSettings, Tabledemo.TableReport>("tabledemo", "GlobalSettings", () => new Tabledemo.TableReport(), Copy,
                Tabledemo.Schema.GlobalSettingsLoad, Tabledemo.Schema.GlobalSettingsMeasure, Tabledemo.Schema.GlobalSettingsSave,
                Tabledemo.Schema.GlobalSettingsFromJson, Tabledemo.Schema.GlobalSettingsToJsonMeasure, Tabledemo.Schema.GlobalSettingsToJson));
        }
        if (Find("tabledemo", "ShipEntry") == null)
        {
            codecs.Add(Row<Tabledemo.ShipEntry, Tabledemo.TableReport>("tabledemo", "ShipEntry", () => new Tabledemo.TableReport(), Copy,
                Tabledemo.Schema.ShipEntryLoad, Tabledemo.Schema.ShipEntryMeasure, Tabledemo.Schema.ShipEntrySave,
                Tabledemo.Schema.ShipEntryFromJson, Tabledemo.Schema.ShipEntryToJsonMeasure, Tabledemo.Schema.ShipEntryToJson));
        }
        int cases = 0, failures = 0;
        foreach (string raw in File.ReadAllLines(manifest))
        {
            if (raw.Length == 0 || raw[0] == '#') { continue; }
            string[] f = raw.Split('\t');
            if (f.Length != 9)
            {
                Console.Error.WriteLine("corpus: " + f[0] + ": " + f.Length + " fields, want 9");
                failures++;
                continue;
            }
            string name = f[0], carrier = f[1], schema = f[2], table = f[3], input = f[4], verdict = f[6], reason = f[7], digest = f[8];
            cases++;
            byte[] payload;
            try { payload = File.ReadAllBytes(Path.Combine(dir, input.Replace('/', Path.DirectorySeparatorChar))); }
            catch (Exception error) { Console.Error.WriteLine("corpus: " + name + ": " + error.Message); failures++; continue; }

            string gotVerdict = null, gotReason = "-", gotDigest = "-";
            try
            {
                Codec codec = Find(CorpusUnit(schema), table);
                if (carrier == "wire")
                {
                    if (schema == "schemas/probe.schema")
                    {
                        long size = Probe.Schema.RootLoadMeasure(payload, out Probe.TableRefuseReason why);
                        if (size < 0) { gotVerdict = "refuse"; gotReason = why.ToString(); }
                        else { gotVerdict = "accept"; gotReason = "-"; gotDigest = "-"; }
                    }
                    else
                    {
                        if (codec == null) { throw new InvalidOperationException("no codec for " + schema + " " + table); }
                        Report report = new Report();
                        object value = codec.Load(payload, report);
                        if (value == null || report.Refused || report.Malformed)
                        {
                            gotVerdict = "refuse";
                            gotReason = string.IsNullOrEmpty(report.Reason) ? "damaged" : report.Reason;
                        }
                        else { gotVerdict = "accept"; gotDigest = Digest(codec, value); }
                    }
                }
                else if (carrier == "text")
                {
                    if (codec == null) { throw new InvalidOperationException("no codec for " + schema + " " + table); }
                    Report report = new Report();
                    object value = codec.FromJson(payload, report);
                    if (value == null || report.Refused || report.Malformed)
                    {
                        gotVerdict = "refuse";
                        gotReason = string.IsNullOrEmpty(report.Reason) ? "damaged" : report.Reason;
                    }
                    else { gotVerdict = "accept"; gotDigest = Digest(codec, value); }
                }
                else if (carrier == "message" || carrier == "message_novocab")
                {
                    if (codec == null || codec.Unit != "messagedemo" || codec.Root != "ToolMessage")
                    {
                        throw new InvalidOperationException("no message codec for " + schema + " " + table);
                    }
                    var vocabulary = new Messagedemo.TableVocabulary();
                    var report = new Messagedemo.TableReport();
                    if (carrier == "message")
                    {
                        Messagedemo.Schema.AnnounceRead(vocabulary, codec.Announcement, report);
                    }
                    var values = new Messagedemo.ToolMessage[1];
                    values[0] = new Messagedemo.ToolMessage();
                    if (report.Refused || report.Malformed)
                    {
                        gotVerdict = "refuse";
                        gotReason = string.IsNullOrEmpty(report.Reason) ? "damaged" : report.Reason;
                    }
                    else
                    {
                        Messagedemo.Schema.ToolMessageLoadMessages(values, payload, vocabulary, report, out _);
                        if (report.Refused || report.Malformed)
                        {
                            gotVerdict = "refuse";
                            gotReason = string.IsNullOrEmpty(report.Reason) ? "damaged" : report.Reason;
                        }
                        else { gotVerdict = "accept"; gotDigest = Digest(codec, values[0]); }
                    }
                }
                else if (carrier == "announce" || carrier == "announce_second" || carrier == "announce_bytes" || carrier == "announce_entries")
                {
                    var vocabulary = new Tabledemo.TableVocabulary();
                    if (carrier == "announce_bytes") { vocabulary.MaxBytes = 8; }
                    if (carrier == "announce_entries") { vocabulary.MaxEntries = 1; }
                    var report = new Tabledemo.TableReport();
                    Tabledemo.Schema.AnnounceRead(vocabulary, payload, report);
                    if (carrier == "announce_second" && !report.Refused && !report.Malformed)
                    {
                        Tabledemo.Schema.AnnounceRead(vocabulary, payload, report);
                    }
                    if (report.Refused || report.Malformed)
                    {
                        gotVerdict = "refuse";
                        gotReason = string.IsNullOrEmpty(report.Reason) ? "damaged" : report.Reason;
                    }
                    else { gotVerdict = "accept"; gotDigest = "-"; }
                }
                else
                {
                    throw new InvalidOperationException("unknown carrier " + carrier);
                }
            }
            catch (Exception error)
            {
                Console.Error.WriteLine("corpus: " + name + ": " + error.Message);
                failures++;
                continue;
            }

            // TEST-ONLY PLANTED READER BUG (schema-cs-runs-hostile-corpus,
            // STEP 2): with SCHEMA_CS_HOSTILE_SABOTAGE=surrogate_pair the
            // decoded value of the surrogate_pair case is corrupted, as a
            // reader that decoded the pair one unit low would produce. It is
            // switched on only by the negative-control test and never by the
            // corpus or the manifest.
            if (Environment.GetEnvironmentVariable("SCHEMA_CS_HOSTILE_SABOTAGE") == "surrogate_pair" &&
                name == "fuzz-text-surrogate_pair" && gotDigest != "-")
            {
                gotDigest = gotDigest == new string('f', gotDigest.Length) ? new string('0', gotDigest.Length) : new string('f', gotDigest.Length);
            }

            // This port's enum names the builder count cap
            // `count_over_extent_cap`; the engine calls the same refusal
            // `count_refusal`. It is the one reason mapping in this runner.
            if (gotReason == "count_over_extent_cap") { gotReason = "count_refusal"; }
            if (gotVerdict != verdict || gotReason != reason || gotDigest != digest)
            {
                Console.Error.WriteLine("FIX-CARD: schema-cs-hostile-corpus: " + name + " want " + verdict + " " + reason + " " + digest + " got " + gotVerdict + " " + gotReason + " " + gotDigest);
                failures++;
            }
        }
        if (failures != 0)
        {
            Console.Error.WriteLine("C# hostile corpus: " + failures + " of " + cases + " cases disagree with the manifest");
            return 1;
        }
        Console.Out.WriteLine("C# hostile corpus: " + cases + " cases agree with the manifest (verdict, reason, digest)");
        return 0;
    }
}
