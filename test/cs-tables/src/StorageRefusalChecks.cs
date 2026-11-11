using System;
using Demo = Tabledemo;
using B = Backenddemo;

// A COUNT OR A LENGTH PAST ITS STORAGE IS REFUSED IN EVERY BUILD (docs/SPEC.md
// §5.1). Each binary writer gets a buffer poisoned with one byte and a value
// whose count or length overruns its storage, and must answer -1 before its
// first byte, so every byte of the poison is intact (this leg's writers check
// before they write). This leg runs Debug and Release, so both configurations
// hold it.
static partial class Program
{
    const byte Poison = 0xA5;

    static byte[] Poisoned(int n) { byte[] b = new byte[n]; Array.Fill(b, Poison); return b; }

    static bool Intact(byte[] b) { foreach (byte x in b) { if (x != Poison) { return false; } } return true; }

    static Demo.RootConfig StorageRoot()
    {
        Demo.RootConfig root = new Demo.RootConfig();
        root.VersionNote[0] = (byte)'a'; root.VersionNoteLength = 1;
        root.WeaponsCount = 1;
        return root;
    }

    static void TestStorageRefusal()
    {
        foreach ((string what, Action<Demo.RootConfig> spoil) in new (string, Action<Demo.RootConfig>)[] {
            ("a weapons count past its storage", r => r.WeaponsCount = r.Weapons.Length + 1),
            ("a negative weapons count", r => r.WeaponsCount = -1),
            ("a version_note length past its storage", r => r.VersionNoteLength = r.VersionNote.Length + 1),
            ("a negative version_note length", r => r.VersionNoteLength = -1),
        })
        {
            Demo.RootConfig good = StorageRoot();
            Demo.RootConfig bad = StorageRoot(); spoil(bad);

            long fixedNeed = Demo.Schema.RootConfigFixedMeasure(1);
            Check(Demo.Schema.RootConfigFixedSave(good, Poisoned((int)fixedNeed)) == fixedNeed, "storage: FixedSave writes the in-bound twin of " + what);
            byte[] fixedBuffer = Poisoned((int)fixedNeed);
            Check(Demo.Schema.RootConfigFixedSave(bad, fixedBuffer) == -1, "storage: FixedSave refuses " + what);
            Check(Intact(fixedBuffer), "storage: FixedSave leaves the buffer untouched on " + what);

            byte[] variableBuffer = Poisoned(4096);
            Check(Demo.Schema.RootConfigSave(good, Poisoned(4096)) > 0, "storage: Save writes the in-bound twin of " + what);
            Check(Demo.Schema.RootConfigSave(bad, variableBuffer) == -1, "storage: Save refuses " + what);
            Check(Intact(variableBuffer), "storage: Save leaves the buffer untouched on " + what);
        }

        // SaveMessages: a sku length past its storage
        foreach ((string what, int length) in new (string, int)[] { ("a sku length past its storage", 33), ("a negative sku length", -1) })
        {
            B.StorePurchase purchase = new B.StorePurchase();
            purchase.Sku[0] = (byte)'a'; purchase.SkuLength = 1;
            Check(B.Schema.StorePurchaseSaveMessages(new[] { purchase }, Poisoned(4096)) > 0, "storage: SaveMessages writes the in-bound twin of " + what);
            purchase.SkuLength = length;
            byte[] messageBuffer = Poisoned(4096);
            Check(B.Schema.StorePurchaseSaveMessages(new[] { purchase }, messageBuffer) == -1, "storage: SaveMessages refuses " + what);
            Check(Intact(messageBuffer), "storage: SaveMessages leaves the buffer untouched on " + what);
        }
    }
}
