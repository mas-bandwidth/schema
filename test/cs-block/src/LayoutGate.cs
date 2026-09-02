// §19.3's LAYOUT CONTRACT as a GENERATION-TIME gate (SPEC-TABLES.md §19.3,
// §19.5).
//
// The C# half of the contract is a check that runs once and throws — C# has no
// `static_assert`, so a disagreement between the compiler's layout model and
// this runtime's cannot be a build error the way it is in C++. Left at that,
// the first thing to discover a bad layout is a consumer opening a block, at
// run time, in a game.
//
// This closes the distance. The project compiles the generated C# of EVERY
// corpus unit the backend emits a block form for — which is already half the
// gate, because a projected array where an inline one belongs does not produce
// the same struct — and this runs each unit's own `Verify()`, so every record
// of every unit has its size and every field's offset checked against the
// compiler's model before any test does anything else.
//
// The dogfood is why: a C# emitter that projected a bounded array INSIDE a
// nested record put a sixteen-byte triple where C++ put the whole array, and
// nothing said so until `Verify()` threw on the first `Open` — in the game.

using System;

static class LayoutGate
{
    internal static bool Run()
    {
        bool ok = true;
        // one call per unit: each carries its own TableBlockLayout, and the
        // check is idempotent, so calling it here costs one branch after the
        // first and gives every unit a verdict at start-up.
        ok &= Verify("blockdemo", () => Blockdemo.TableBlockLayout.Verify());
        ok &= Verify("blockhome", () => Blockhome.TableBlockLayout.Verify());
        ok &= Verify("tabledemo", () => Tabledemo.TableBlockLayout.Verify());
        return ok;
    }

    static bool Verify(string unit, Action verify)
    {
        try
        {
            verify();
            return true;
        }
        catch (Exception e)
        {
            Console.WriteLine("FAILED: " + unit + "'s block layout disagrees with the compiler's model: " + e.Message);
            return false;
        }
    }
}
