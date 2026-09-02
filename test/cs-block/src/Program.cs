// The BLOCK FORM's C# leg (SPEC-TABLES.md §19, §19.5) — the CONSUMER half of
// the two-language gate, and the twin of test/tables/block_main.cpp.
//
// A C++ producer laid a block out, filled it, and pinned its bytes into
// testdata/wire/tables/block_render.bin. This program points at those very
// bytes — copied once into 64-byte-aligned unmanaged memory, exactly as a
// host engine would receive them — opens the block, and compares every field
// of every row against the values the producer wrote, reproduced here from the
// row index alone. Sizes and offsets are asserted by GENERATED code on both
// sides, so the test proves the two agree on the BYTES and not merely on the
// constants.
//
// It runs TWICE on this side: through the generated blittable struct, and
// through the block descriptors, because §19.2 offers both and both must land
// the same values.
//
// Prints OK and exits 0, exactly like its C++ twin.

using System;
using System.IO;
using System.Runtime.CompilerServices;
using System.Runtime.InteropServices;
using Blockdemo;
using Row = Blockdemo.Block;

static class Program
{
    static bool failed;

    static void Check(bool ok, string what)
    {
        if (!ok)
        {
            Console.WriteLine("FAILED: " + what);
            failed = true;
        }
    }

    static string FindGolden(string name)
    {
        string[] candidates =
        {
            Path.Combine("..", "..", "testdata", "wire", "tables", name + ".bin"),
            Path.Combine(AppContext.BaseDirectory, "..", "..", "..", "..", "..", "testdata", "wire", "tables", name + ".bin"),
        };
        foreach (string candidate in candidates)
        {
            if (File.Exists(candidate))
            {
                return candidate;
            }
        }
        return candidates[0];
    }

    // ---- the values, reproduced from the row index alone ----
    //
    // The C++ producer's own generator, written here from the same arithmetic.
    // The comparison is therefore over VALUES rather than over a blob both
    // sides copied, and every number is exact in binary floating point on
    // purpose: a mismatch is a layout defect, never a rounding one.

    static void Vec3(int salt, int i, out double x, out double y, out double z)
    {
        x = 1.5 * i + salt;
        y = 2.25 * i - salt;
        z = 0.5 * i + 2 * salt;
    }

    static void Quat(int salt, int i, out double x, out double y, out double z, out double w)
    {
        x = 0.25 * i + salt;
        y = 0.125 * i;
        z = 0.0625 * i - salt;
        w = 1.0 - 0.5 * (i % 3);
    }

    static void CheckVec3(in Row.RenderVector3 got, int salt, int i, string what)
    {
        double x, y, z;
        Vec3(salt, i, out x, out y, out z);
        Check(got.X == x && got.Y == y && got.Z == z, what);
    }

    static void CheckQuat(in Row.RenderQuaternion got, int salt, int i, string what)
    {
        double x, y, z, w;
        Quat(salt, i, out x, out y, out z, out w);
        Check(got.X == x && got.Y == y && got.Z == z && got.W == w, what);
    }

    // ---- the frame the producer pinned ----

    const int Cameras = 1;
    const int Ships = 5;
    const int Turrets = 7;
    const int Missiles = 3;
    const int DynamicProps = 2;
    const int StaticProps = 4;
    const int CosmeticProps = 3;
    const int Lasers = 6;
    const int Explosions = 2;

    static unsafe int Main()
    {
        string path = FindGolden("block_render");
        if (!File.Exists(path))
        {
            Console.WriteLine("FAILED: missing block golden " + path + " (run: make update-goldens)");
            return 1;
        }
        byte[] bytes = File.ReadAllBytes(path);

        // A host engine receives a POINTER, and a block's base is 64-byte
        // aligned by construction (§19.1). A pinned managed array is not, so
        // the bytes are copied once into aligned unmanaged memory — which is
        // what the real boundary looks like, and it keeps the alignment check
        // BlockOpen makes a real one rather than one this test disables.
        IntPtr raw = Marshal.AllocHGlobal(bytes.Length + 64);
        try
        {
            long aligned = ((long) raw + 63) & ~63L;
            IntPtr pointer = new IntPtr(aligned);
            Marshal.Copy(bytes, 0, pointer, bytes.Length);

            RenderFrameBlock block;
            Check(RenderFrameBlock.Open(out block, pointer, bytes.Length),
                "Open accepts the block the C++ producer wrote — the two sides agree on the magic, the byte order and the build version");
            if (failed)
            {
                Console.WriteLine("FAILED");
                return 1;
            }

            Check(block.Bytes == bytes.Length, "Open reports the used extent the producer wrote");

            // ---- the projection's own fields, and the facts about its rows ----

            ref readonly Row.RenderFrameProjection projection = ref block.Projection;
            Check(projection.Magic == Schema.TableBlockMagic, "the prologue's magic");
            Check(projection.BuildVersion == Schema.BuildVersion, "the prologue's build version is this build's own");
            Check(projection.ByteOrder == Schema.TableBlockByteOrder, "the prologue's byte order is this build's own");
            Check(projection.Version == 1, "the table's own declared field, read where it lies");

            Check(projection.Ships.Count == Ships, "the ships triple's count");
            Check(projection.Ships.Stride == Unsafe.SizeOf<Row.RenderShip>(),
                "the ships triple's stride is this build's own sizeof — the pitch IS sizeof (§2.7)");
            Check(projection.Ships.OffsetOf >= (ulong) Unsafe.SizeOf<Row.RenderFrameProjection>(),
                "the ships array starts past the projection");
            Check(projection.Cameras.Count == Cameras && projection.Turrets.Count == Turrets &&
                  projection.Missiles.Count == Missiles && projection.DynamicProps.Count == DynamicProps &&
                  projection.StaticProps.Count == StaticProps && projection.CosmeticProps.Count == CosmeticProps &&
                  projection.Lasers.Count == Lasers && projection.Explosions.Count == Explosions,
                "every triple's count is the count the producer wrote");

            // ---- pass one: the generated blittable struct ----

            CheckStruct(block);

            // ---- pass two: the descriptors ----

            CheckDescriptors(block);

            // ---- the refusals, from this side ----

            RenderFrameBlock refused;
            Check(!RenderFrameBlock.Open(out refused, IntPtr.Zero, bytes.Length), "Open refuses a null pointer");
            Check(!RenderFrameBlock.Open(out refused, pointer, 8), "Open refuses a length shorter than the projection");
            Check(!RenderFrameBlock.Open(out refused, pointer, bytes.Length - 64), "Open refuses a length shorter than the used extent");
            Check(!RenderFrameBlock.Open(out refused, pointer + 8, bytes.Length), "Open refuses an unaligned base");

            byte* at = (byte*) pointer;
            ulong version = *(ulong*) (at + 8);
            *(ulong*) (at + 8) = version ^ 1;
            Check(!RenderFrameBlock.Open(out refused, pointer, bytes.Length),
                "Open refuses a block from a build this one does not match — there is ONE entry point, and a mismatch is a refusal");
            *(ulong*) (at + 8) = version;

            ulong order = *(ulong*) (at + 16);
            *(ulong*) (at + 16) = order == 1UL ? 2UL : 1UL;
            Check(!RenderFrameBlock.Open(out refused, pointer, bytes.Length), "Open refuses a block of the other byte order");
            *(ulong*) (at + 16) = order;

            Check(RenderFrameBlock.Open(out refused, pointer, bytes.Length), "and the restored block opens again");
        }
        finally
        {
            Marshal.FreeHGlobal(raw);
        }

        CheckPadded();

        Console.WriteLine(failed ? "FAILED" : "OK");
        return failed ? 1 : 0;
    }

    // ---- the PADDING and inline-storage gate (§19.3, §19.5) ----
    //
    // Render.schema is declared largest-alignment-first and has zero interior
    // padding. Padded.schema declares the opposite on purpose, so the GENERATED
    // PADDING FIELDS on this side are exercised rather than assumed — delete
    // one and the values below land in the wrong place, which is precisely what
    // the Makefile's block-padding-negative-control target proves.

    static unsafe void CheckPadded()
    {
        string path = FindGolden("block_padded");
        if (!File.Exists(path))
        {
            Console.WriteLine("FAILED: missing block golden " + path + " (run: make update-goldens)");
            failed = true;
            return;
        }
        byte[] bytes = File.ReadAllBytes(path);
        IntPtr raw = Marshal.AllocHGlobal(bytes.Length + 64);
        try
        {
            long aligned = ((long) raw + 63) & ~63L;
            IntPtr pointer = new IntPtr(aligned);
            Marshal.Copy(bytes, 0, pointer, bytes.Length);

            PaddedFrameBlock block;
            Check(PaddedFrameBlock.Open(out block, pointer, bytes.Length), "Open accepts the padded frame");
            if (failed)
            {
                return;
            }
            Check(Unsafe.SizeOf<Row.PaddedRow>() == 72, "a padded row is 72 bytes in the managed model, as it is in C++");

            ref readonly Row.PaddedFrameProjection projection = ref block.Projection;
            Check(projection.Marker == 7, "the projection's scalar before the hole");
            Check(projection.Stamp == 0x0123456789abcdefUL, "the projection's scalar after it");
            Check(projection.BlobLength == 12, "the projection's inline bytes length");

            int i = 0;
            foreach (ref readonly Row.PaddedRow row in block.Rows)
            {
                Check(row.Tag == (byte) (10 + i), "padded row tag, before seven bytes of hole");
                Check(row.Value == 0.5 * i + 100.0, "padded row value, after them");
                Check(row.Flag == ((i % 2) == 0), "padded row flag");
                Check(row.Id == (uint) (i * 1000 + 3), "padded row id, after three more");
                fixed (Row.PaddedRow* p = &row)
                {
                    string label = Marshal.PtrToStringAnsi(new IntPtr(p->Label), row.LabelLength);
                    Check(label == "row-" + i, "padded row label — an inline string buffer");
                    for (int s = 0; s < 4; s++)
                    {
                        Check(p->Slots[s] == (ushort) (i * 4 + s), "padded row slot — an inline fixed array");
                    }
                    // an enum-keyed array stays INLINE (§2.7): one slot per
                    // variant, indexed by the variant's own value, slot 0 None's
                    for (int t = 1; t < 5; t++)
                    {
                        Check(p->Teams[t] == (byte) (i + t), "padded row team slot — an inline enum-keyed array");
                    }
                }
                Check(row.Counter == i * 9, "padded row optional value");
                Check(row.CounterPresent == ((i % 2) == 1), "padded row optional presence companion");
                i++;
            }
            Check(i == 4, "the padded frame carries its four rows");
        }
        finally
        {
            Marshal.FreeHGlobal(raw);
        }
    }

    // ---- pass one: the TYPED FAST PATH ----
    //
    // The generated blittable struct, read at the pitch the instance gives —
    // and its contiguous twin, available because the pitch IS sizeof.

    static void CheckStruct(RenderFrameBlock block)
    {
        {
            int i = 0;
            foreach (ref readonly Row.RenderCamera camera in block.Cameras)
            {
                CheckVec3(in camera.Position, 1, i, "camera position");
                CheckQuat(in camera.Rotation, 1, i, "camera rotation");
                Check(camera.CameraId == (uint) (i * 7 + 1), "camera id");
                Check(camera.CameraType == (uint) (i % 4), "camera type");
                Check(camera.TargetObjectId == (uint) (i * 13 + 2), "camera target");
                Check(camera.Fov == 0.5f * i + 60.0f, "camera fov");
                i++;
            }
            Check(i == Cameras, "the cameras accessor yields count rows");
        }
        {
            int i = 0;
            foreach (ref readonly Row.RenderShip ship in block.Ships)
            {
                CheckVec3(in ship.Position, 2, i, "ship position");
                CheckQuat(in ship.Rotation, 2, i, "ship rotation");
                Check(ship.Flags == (ulong) (i % 16), "ship flags");
                Check(ship.ObjectId == (uint) (i * 3 + 11), "ship object id");
                Check(ship.TargetObjectId == (uint) (i * 5 + 7), "ship target");
                Check(ship.Thrust == 0.25f * i, "ship thrust");
                Check(ship.ObjectSequence == (byte) (i % 251), "ship sequence");
                Check(ship.ShipType == (ShipType) (i % 4), "ship type");
                Check(ship.Team == (Team) (i % 5), "ship team");
                // THE BOOL ROW (§19.3, §19.5): one byte here and one in C++,
                // four under default marshalling — the case where C#'s two
                // layout models disagree, pinned so a port cannot pick the
                // wrong one and pass.
                Check(ship.HasTargetLock == ((i % 2) == 0), "ship has_target_lock — a bool is ONE byte in the managed model");
                Check(ship.PredictedExplode == ((i % 3) == 0), "ship predicted_explode");
                i++;
            }
            Check(i == Ships, "the ships accessor yields count rows");
            Check(Unsafe.SizeOf<Row.RenderShip>() == 88, "a ship row is 88 bytes in the managed model, as it is in C++");
        }
        {
            ReadOnlySpan<Row.RenderShip> ships = block.ShipsSpan;
            Check(ships.Length == Ships, "the contiguous view carries the same count");
            Check(ships.Length == 0 || ships[Ships - 1].ObjectId == (uint) ((Ships - 1) * 3 + 11),
                "and the contiguous view lands the same values as the iterator");
        }
        {
            int i = 0;
            foreach (ref readonly Row.RenderTurret turret in block.Turrets)
            {
                CheckQuat(in turret.Rotation, 3, i, "turret rotation");
                Check(turret.Flags == (ulong) (i * 17), "turret flags");
                Check(turret.ObjectId == (uint) (i * 2 + 1), "turret object id");
                Check(turret.ParentObjectId == (uint) (i / 3), "turret parent");
                Check(turret.TurretIndex == (uint) (i % 8), "turret index");
                Check(turret.TargetObjectId == (uint) (i * 11), "turret target");
                Check(turret.ObjectSequence == (byte) (i % 253), "turret sequence");
                Check(turret.Team == (Team) (i % 5), "turret team");
                Check(turret.HasTargetLock == ((i % 5) == 0), "turret has_target_lock");
                i++;
            }
            Check(i == Turrets, "the turrets accessor yields count rows");
        }
        {
            int i = 0;
            foreach (ref readonly Row.RenderMissile missile in block.Missiles)
            {
                CheckVec3(in missile.Position, 4, i, "missile position");
                CheckQuat(in missile.Rotation, 4, i, "missile rotation");
                Check(missile.Flags == (ulong) (i * 19), "missile flags");
                Check(missile.ObjectId == (uint) (i * 23), "missile object id");
                Check(missile.ObjectSequence == (byte) (i % 249), "missile sequence");
                Check(missile.MissileType == (MissileType) (i % 3), "missile type");
                Check(missile.Team == (Team) (i % 5), "missile team");
                i++;
            }
            Check(i == Missiles, "the missiles accessor yields count rows");
        }
        {
            int i = 0;
            foreach (ref readonly Row.RenderDynamicProp prop in block.DynamicProps)
            {
                CheckVec3(in prop.Position, 5, i, "dynamic prop position");
                Check(prop.Flags == (ulong) (i * 29), "dynamic prop flags");
                Check(prop.ObjectId == (uint) (i * 31), "dynamic prop object id");
                Check(prop.ObjectSequence == (byte) (i % 247), "dynamic prop sequence");
                Check(prop.PropType == (PropType) (i % 4), "dynamic prop type");
                Check(prop.Team == (Team) (i % 5), "dynamic prop team");
                i++;
            }
            Check(i == DynamicProps, "the dynamic props accessor yields count rows");
        }
        {
            int i = 0;
            foreach (ref readonly Row.RenderStaticProp prop in block.StaticProps)
            {
                CheckVec3(in prop.Position, 6, i, "static prop position");
                Check(prop.Scale == 0.5 + 0.25 * (i % 7), "static prop scale");
                Check(prop.Flags == (ulong) (i * 37), "static prop flags");
                Check(prop.StaticPropId == (uint) (i * 41), "static prop id");
                Check(prop.PropType == (PropType) (i % 4), "static prop type");
                Check(prop.Team == (Team) (i % 5), "static prop team");
                i++;
            }
            Check(i == StaticProps, "the static props accessor yields count rows");
        }
        {
            int i = 0;
            foreach (ref readonly Row.RenderCosmeticProp prop in block.CosmeticProps)
            {
                CheckVec3(in prop.Position, 7, i, "cosmetic prop position");
                Check(prop.Scale == 0.25 + 0.125 * (i % 5), "cosmetic prop scale");
                Check(prop.Flags == (ulong) (i * 43), "cosmetic prop flags");
                Check(prop.CosmeticPropId == (uint) (i * 47), "cosmetic prop id");
                Check(prop.PropSequence == (byte) (i % 241), "cosmetic prop sequence");
                Check(prop.PropType == (PropType) (i % 4), "cosmetic prop type");
                Check(prop.Team == (Team) (i % 5), "cosmetic prop team");
                i++;
            }
            Check(i == CosmeticProps, "the cosmetic props accessor yields count rows");
        }
        {
            int i = 0;
            foreach (ref readonly Row.RenderLaser laser in block.Lasers)
            {
                CheckVec3(in laser.Start, 8, i, "laser start");
                CheckVec3(in laser.Finish, 9, i, "laser finish");
                Check(laser.T == 0.125 * (i % 8), "laser t");
                Check(laser.LaserId == (uint) (i * 53), "laser id");
                Check(laser.LaserType == (LaserType) (i % 3), "laser type");
                Check(laser.Team == (Team) (i % 5), "laser team");
                i++;
            }
            Check(i == Lasers, "the lasers accessor yields count rows");
        }
        {
            int i = 0;
            foreach (ref readonly Row.RenderExplosion explosion in block.Explosions)
            {
                CheckVec3(in explosion.Position, 10, i, "explosion position");
                Check(explosion.T == 0.0625 * (i % 16), "explosion t");
                Check(explosion.ExplosionId == (uint) (i * 59), "explosion id");
                Check(explosion.ParentObjectId == (uint) (i * 61), "explosion parent");
                Check(explosion.ExplosionType == (ExplosionType) (i % 3), "explosion type");
                Check(explosion.Team == (Team) (i % 5), "explosion team");
                i++;
            }
            Check(i == Explosions, "the explosions accessor yields count rows");
        }
    }

    // ---- pass two: THE REFLECTIVE READ (§19.2) ----
    //
    // The descriptors carry the projection offset of every field, the offsets
    // of the three members inside each triple, and the element's own layout —
    // so a consumer reads the facts out of an instance and points at rows with
    // no hand-written struct per table and no knowledge of the spelling that
    // produced any of it. That is what retired the mirror: the layout became
    // data, not because someone generated a replacement for it.

    static unsafe void CheckDescriptors(RenderFrameBlock block)
    {
        TableBlockInfo info = RenderFrameBlock.Type;
        Check(info.Name == "RenderFrame", "the block descriptor names its table");
        Check(info.BuildVersion == Schema.BuildVersion, "the block descriptor carries the unit's build version");
        Check(info.Size == Unsafe.SizeOf<Row.RenderFrameProjection>(), "the block descriptor's size is the projection's own");

        byte* at = block.Base;
        int outOfLine = 0;
        foreach (TableBlockFieldInfo field in info.Fields)
        {
            if (!field.OutOfLine)
            {
                continue;
            }
            outOfLine++;
            ulong offsetOf = *(ulong*) (at + field.OffsetOfOffset);
            uint count = *(uint*) (at + field.CountOffset);
            uint stride = *(uint*) (at + field.StrideOffset);
            Check(stride == (uint) field.Stride, "the instance's pitch is this build's own, in a block this build's producer wrote");
            Check(field.Element != null, "an out-of-line array's descriptor names its element's layout");

            if (field.Name != "ships")
            {
                continue;
            }
            TableBlockInfo row = field.Element;
            Check(row.Name == "RenderShip", "the ships array's element descriptor names RenderShip");
            Check(row.Size == (int) stride, "the row descriptor's size is the pitch the instance carries");

            TableBlockFieldInfo objectId = null;
            TableBlockFieldInfo position = null;
            TableBlockFieldInfo hasLock = null;
            foreach (TableBlockFieldInfo rowField in row.Fields)
            {
                if (rowField.Name == "object_id") { objectId = rowField; }
                if (rowField.Name == "position") { position = rowField; }
                if (rowField.Name == "has_target_lock") { hasLock = rowField; }
            }
            Check(objectId != null && position != null && hasLock != null, "the row descriptor names the row's own fields");
            if (objectId == null || position == null || hasLock == null)
            {
                continue;
            }
            Check(hasLock.Size == 1, "a bool row field is ONE byte in the descriptors too (§19.3)");

            ReadOnlySpan<Row.RenderShip> ships = block.ShipsSpan;
            int mismatches = 0;
            for (uint r = 0; r < count; r++)
            {
                byte* rowAt = at + (long) offsetOf + (long) r * stride;
                uint reflected = *(uint*) (rowAt + objectId.Offset);
                if (reflected != ships[(int) r].ObjectId) { mismatches++; }

                // one level further down: a nested record's own layout, reached
                // through the same column
                TableBlockInfo vec = position.Element;
                double x = *(double*) (rowAt + position.Offset + vec.Fields[0].Offset);
                if (x != ships[(int) r].Position.X) { mismatches++; }
            }
            Check(mismatches == 0,
                "the reflective read lands the same values as the generated struct — §19.2's two ways, over one instance");
        }
        Check(outOfLine == 9, "the block descriptor carries all nine out-of-line arrays");
    }
}
