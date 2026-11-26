// The per-PR happy path, Unity C# leg (make/happy-path.mk), compiled in the
// Unity dialect (C# 9 over netstandard2.1): one ShipCreate
// written with the generated packet writer, read back into a fresh value,
// every field compared.
using System;
using Serialize;
using Happy;

static class HappyMain
{
    const string Lang = "unity";

    static int Fail(string why)
    {
        Console.Error.WriteLine("happy " + Lang + ": FAILED: " + why);
        return 1;
    }

    static int Main()
    {
        ShipCreate sent = default;
        sent.ShipType = ShipType.Corvette;
        sent.Position.X = 1.5;
        sent.Position.Y = -2.25;
        sent.Position.Z = 3.0;
        sent.Health = 777;
        sent.Alive = true;

        WriteStream ws = new WriteStream(new byte[Schema.ShipCreateMaxBytes]);
        if (!Schema.WriteShipCreate(ws, ref sent))
        {
            return Fail("WriteShipCreate returned false");
        }
        ws.Flush();
        byte[] wire = ws.Data.ToArray();

        ShipCreate got = default;
        ReadStream rs = new ReadStream(wire);
        if (!Schema.ReadShipCreate(rs, ref got))
        {
            return Fail("ReadShipCreate returned false, error " + rs.Error);
        }

        if (got.ShipType != ShipType.Corvette) return Fail("ship_type " + got.ShipType);
        if (got.Position.X != 1.5) return Fail("position.x " + got.Position.X);
        if (got.Position.Y != -2.25) return Fail("position.y " + got.Position.Y);
        if (got.Position.Z != 3.0) return Fail("position.z " + got.Position.Z);
        if (got.Health != 777) return Fail("health " + got.Health);
        if (got.Alive != true) return Fail("alive " + got.Alive);

        Console.WriteLine("happy " + Lang + ": ok (" + wire.Length + " bytes)");
        return 0;
    }
}
