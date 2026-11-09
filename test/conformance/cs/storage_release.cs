// The storage-bound refusal in a Release build (docs/SPEC.md §5.1): a counted
// array whose count is 9 indexes past the storage it describes, and a build
// that compiled every Debug.Assert out still refuses it before the first byte.

using System;
using Storagerelease;

internal static class StorageRelease
{
    private static int Main()
    {
        var value = new Root();
        value.ItemsCount = 9; // the declared bound is 8

        try
        {
            if (Schema.RootMeasureMessages(new[] { value }) != -1)
            {
                Console.Out.WriteLine("FAIL storage release: MeasureMessages wrote a count past its storage");
                return 1;
            }
            if (Schema.RootSaveMessages(new[] { value }, new byte[4096]) != -1)
            {
                Console.Out.WriteLine("FAIL storage release: SaveMessages wrote a count past its storage");
                return 1;
            }
        }
        catch (Exception e)
        {
            Console.Out.WriteLine("FAIL storage release: " + e.GetType().Name + " instead of a refusal");
            return 1;
        }

        Console.Out.WriteLine("storage release gate: a count past its storage is refused in a Release build");
        return 0;
    }
}
