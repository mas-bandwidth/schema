using System;
using System.IO;
using System.Runtime.InteropServices;
using Serialize;
using D = Packetdefaults;
using P = Packetplain;

static class Program
{
    delegate bool Encode<T>(WriteStream stream, ref T value);
    delegate bool Decode<T>(ReadStream stream, ref T value);

    static void Check(bool ok, string name) { if (!ok) throw new Exception("FAILED: " + name); }
    // Compare all storage, unused tails and inline buffers included, rather
    // than re-encoding expected values through the same generated codec under
    // test. A packet type is an unmanaged value, so its bytes are its storage.
    static bool Equal<T>(T a, T b) where T : unmanaged =>
        MemoryMarshal.AsBytes(new ReadOnlySpan<T>(ref a)).SequenceEqual(MemoryMarshal.AsBytes(new ReadOnlySpan<T>(ref b)));
    static D.Sample Zero() => default;
    static D.Sample Sample()
    {
        D.Sample s = Zero(); new byte[] { 195, 169, 240, 144, 128, 128 }.CopyTo(s.Name[..]); s.NameLength = 6;
        new byte[] { 92, 110, 92, 116 }.CopyTo(s.Token[..]); s.TokenLength = 4; s.Caps = (D.Caps)5; return s;
    }
    static D.Sample Short()
    {
        D.Sample s = Zero(); s.Name[0] = 65; s.NameLength = 1;
        s.Token[1] = 255; s.TokenLength = 2; s.Caps = D.Caps.Crouch; return s;
    }
    static D.Sample Dirty()
    {
        D.Sample s = Zero(); s.Name[..].Fill(100); s.NameLength = 6;
        s.Token[..].Fill(161); s.TokenLength = 4; s.Caps = (D.Caps)7;
        s.EmptyName[..].Fill(111); s.EmptyNameLength = 3;
        s.EmptyToken[..].Fill(177); s.EmptyTokenLength = 2; s.EmptyCaps = (D.Caps)7; return s;
    }
    static (byte[], long) Write<T>(T value, Encode<T> encode)
    {
        WriteStream stream = new WriteStream(new byte[4096]);
        Check(encode(stream, ref value) && stream.Ok, "write"); long bits = stream.BitsProcessed;
        stream.Flush(); Check(stream.Ok, "flush"); return (stream.Data.ToArray(), bits);
    }
    static void Read<T>(byte[] bytes, long bits, ref T initial, T want, Decode<T> decode) where T : unmanaged
    {
        ReadStream stream = new ReadStream(bytes);
        Check(decode(stream, ref initial) && stream.Ok, "read");
        Check(stream.BitsProcessed == bits, "read consumed bits");
        Check(Equal(initial, want), "read values and backing storage");
    }
    static void Golden<T>(string dir, string name, T value, T initial, T want,
        Encode<T> encode, Decode<T> decode) where T : unmanaged
    {
        byte[] bytes = File.ReadAllBytes(Path.Combine(dir, name + ".bin"));
        long bits = long.Parse(File.ReadAllText(Path.Combine(dir, name + ".bits")), System.Globalization.CultureInfo.InvariantCulture);
        var wire = Write(value, encode);
        Check(wire.Item2 == bits && wire.Item1.AsSpan().SequenceEqual(bytes), "C++ byte/bit pin " + name);
        Read(bytes, bits, ref initial, want, decode);
    }
    static D.Sample OverlayWant(D.Sample initial, D.Sample sent)
    {
        sent.Name[..sent.NameLength].CopyTo(initial.Name[..]); initial.NameLength = sent.NameLength;
        sent.Token[..sent.TokenLength].CopyTo(initial.Token[..]); initial.TokenLength = sent.TokenLength;
        initial.Caps = sent.Caps; initial.EmptyNameLength = 0; initial.EmptyTokenLength = 0; initial.EmptyCaps = 0;
        return initial;
    }
    static void Main(string[] args)
    {
        string dir = args[0];
        Check(Equal(new D.Sample(), Sample()), "packet-default constructor bytes");
        Check(Equal(default(D.Sample), Zero()), "default(T) is the zero form");
        D.Sample reused = Dirty();
        D.Schema.InitSample(ref reused);
        Check(Equal(reused, Sample()), "Init restores default bytes and zero tails");
        D.Schema.ZeroSample(ref reused); Check(Equal(reused, Zero()), "Zero stays distinct from Init");
        D.EmptyOnly empty = new D.EmptyOnly();
        Check(empty.NameLength == 0 && empty.TokenLength == 0 && empty.Caps == 0 &&
            empty.Name[..].SequenceEqual(new byte[2]) && empty.Token[..].SequenceEqual(new byte[1]), "explicit empty defaults");
        D.Prefix prefix = new D.Prefix();
        Check(prefix.Name[..].SequenceEqual(new byte[] { 195, 169, 0, 0, 0 }) && prefix.NameLength == 2 &&
            prefix.Token[..].SequenceEqual(new byte[] { 92, 110, 0, 0, 0 }) && prefix.TokenLength == 2, "short literal zero tails");
        prefix.Name[..].Fill(255); prefix.Token[..].Fill(255); D.Schema.InitPrefix(ref prefix);
        Check(Equal(prefix, new D.Prefix()), "Init clears stale short-literal tails");
        D.WideMask wide = new D.WideMask();
        Check((ulong)wide.High == 1UL << 63 && (ulong)wide.All == ulong.MaxValue, "unsigned 64-bit default masks");
        byte[] wideBytes = new byte[16]; wideBytes[7] = 128; wideBytes.AsSpan(8).Fill(255);
        var wideWire = Write(wide, D.Schema.WriteWideMask);
        Check(wideWire.Item2 == 128 && wideWire.Item1.AsSpan().SequenceEqual(wideBytes), "independent 64-bit wire");
        D.WideMask wideIn = new D.WideMask { High = 0, All = 0 };
        Read(wideBytes, 128, ref wideIn, wide, D.Schema.ReadWideMask);
        var split = new D.SplitMask { Lead = 5, Mask = (D.SplitCaps)(1UL << 32), Tail = 2 };
        var splitWire = Write(split, D.Schema.WriteSplitMask);
        Check(splitWire.Item2 == 38 && splitWire.Item1.AsSpan().SequenceEqual(new byte[] { 5, 0, 0, 0, 40 }), "independent 33-bit wire");
        D.SplitMask splitIn = new D.SplitMask();
        Read(new byte[] { 5, 0, 0, 0, 40 }, 38, ref splitIn, split, D.Schema.ReadSplitMask);
        D.Sample sample = Sample();
        P.Sample plain = new P.Sample { NameLength = 6, TokenLength = 4, Caps = (P.Caps)5 };
        sample.Name[..].CopyTo(plain.Name[..]); sample.Token[..].CopyTo(plain.Token[..]);
        var plainWire = Write(plain, P.Schema.WriteSample); var defaultWire = Write(new D.Sample(), D.Schema.WriteSample);
        Check(plainWire.Item2 == defaultWire.Item2 && plainWire.Item1.AsSpan().SequenceEqual(defaultWire.Item1), "defaultless twin has identical wire");
        Golden(dir, "sample-defaults", new D.Sample(), Zero(), Sample(), D.Schema.WriteSample, D.Schema.ReadSample);
        D.Batch batch = new D.Batch();
        Check(Equal(batch.Head, Sample()) && batch.CountedCount == 1, "nested defaults and count birth");
        foreach (D.Sample s in batch.Items) Check(Equal(s, Sample()), "fixed backing defaults");
        foreach (D.Sample s in batch.Counted) Check(Equal(s, Sample()), "counted backing defaults");
        D.Batch initial = new D.Batch(), want = new D.Batch();
        initial.Counted[1] = Dirty(); want.Counted[1] = Dirty(); initial.Counted[2] = Short(); want.Counted[2] = Short();
        Golden(dir, "batch-defaults", batch, initial, want, D.Schema.WriteBatch, D.Schema.ReadBatch);
        D.ZeroCount zeroCount = new D.ZeroCount();
        Check(zeroCount.ItemsCount == 0 && Equal(zeroCount.Items[0], Sample()) && Equal(zeroCount.Items[1], Sample()), "zero-count backing defaults");
        D.ZeroCount dirtyCount = new D.ZeroCount { ItemsCount = 2 }, dirtyWant = new D.ZeroCount();
        dirtyCount.Items[0] = Dirty(); dirtyCount.Items[1] = Short(); dirtyWant.Items[0] = Dirty(); dirtyWant.Items[1] = Short();
        Golden(dir, "zero-count", zeroCount, dirtyCount, dirtyWant, D.Schema.WriteZeroCount, D.Schema.ReadZeroCount);
        Check(new D.Conditional().Enabled && Equal(new D.Conditional().Value, Sample()), "conditional defaults");
        Golden(dir, "conditional-on", new D.Conditional(), new D.Conditional { Enabled = false, Value = Zero() },
            new D.Conditional(), D.Schema.WriteConditional, D.Schema.ReadConditional);
        Golden(dir, "conditional-off", new D.Conditional { Enabled = false }, new D.Conditional { Value = Dirty() },
            new D.Conditional { Enabled = false, Value = Zero() }, D.Schema.WriteConditional, D.Schema.ReadConditional);
        Golden(dir, "choice-sample", new D.Choice { Type = D.ChoiceType.Sample, Sample = Sample() }, new D.Choice { Type = D.ChoiceType.Conditional },
            new D.Choice { Type = D.ChoiceType.Sample, Sample = Sample() }, D.Schema.WriteChoice, D.Schema.ReadChoice);
        Golden(dir, "sample-short", Short(), Dirty(), OverlayWant(Dirty(), Short()), D.Schema.WriteSample, D.Schema.ReadSample);
        Golden(dir, "sample-empty", Zero(), Dirty(), OverlayWant(Dirty(), Zero()), D.Schema.WriteSample, D.Schema.ReadSample);
        foreach (D.Sample sent in new D.Sample[] { Short(), Zero() })
        {
            var wire = Write(new D.Choice { Type = D.ChoiceType.Sample, Sample = sent }, D.Schema.WriteChoice);
            D.Choice choice = new D.Choice { Type = D.ChoiceType.Conditional };
            for (int attempt = 0; attempt < 2; attempt++)
            {
                // a selected arm is initialized in place, whatever it held
                choice.Sample.Name[..].Fill(100); choice.Sample.Token[..].Fill(161);
                Read(wire.Item1, wire.Item2, ref choice, new D.Choice { Type = D.ChoiceType.Sample,
                    Sample = OverlayWant(Sample(), sent) }, D.Schema.ReadChoice);
            }
        }
        var off = Write(new D.Choice { Type = D.ChoiceType.Conditional, Conditional = new D.Conditional { Enabled = false } }, D.Schema.WriteChoice);
        D.Choice offChoice = new D.Choice { Type = D.ChoiceType.Sample };
        for (int attempt = 0; attempt < 2; attempt++)
            Read(off.Item1, off.Item2, ref offChoice, new D.Choice { Type = D.ChoiceType.Conditional,
                Conditional = new D.Conditional { Enabled = false, Value = Zero() } }, D.Schema.ReadChoice);
        // An array of a defaulted type: its elements are the zero form until Init.
        D.Sample[] samples = new D.Sample[3];
        Check(Equal(samples[1], Zero()), "a fresh array element is the zero form");
        for (int i = 0; i < samples.Length; i++) D.Schema.InitSample(ref samples[i]);
        Check(Equal(samples[0], Sample()) && Equal(samples[2], Sample()), "Init installs defaults in each element");
        // Warm the code first, then measure only in-place initialization.
        for (int i = 0; i < 100; i++) D.Schema.InitSample(ref reused);
        long before = GC.GetAllocatedBytesForCurrentThread();
        for (int i = 0; i < 1000; i++) D.Schema.InitSample(ref reused);
        Check(GC.GetAllocatedBytesForCurrentThread() == before, "Init allocates nothing");
        Console.WriteLine("packet defaults C#: constructors, eight C++ goldens and reused storage OK");
    }
}
