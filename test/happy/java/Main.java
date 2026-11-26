// The per-PR happy path, Java leg (make/happy-path-java.mk): one ShipCreate
// written with the generated packet writer, read back into a fresh value,
// every field compared. Run under -ea so the write contracts are live.

import happy.Happy;

public final class Main {
    private static int fail(String why) {
        System.err.println("happy java: FAILED: " + why);
        return 1;
    }

    private static int run() {
        Happy.ShipCreate sent = new Happy.ShipCreate();
        sent.shipType = Happy.ShipType.CORVETTE;
        sent.position.x = 1.5;
        sent.position.y = -2.25;
        sent.position.z = 3.0;
        sent.health = 777;
        sent.alive = true;

        byte[] buffer = new byte[Happy.SHIP_CREATE_MAX_BYTES];
        int bytes = Happy.writeShipCreate(sent, buffer);
        if (bytes <= 0 || bytes > Happy.SHIP_CREATE_MAX_BYTES) {
            return fail("writeShipCreate returned " + bytes + " bytes");
        }
        byte[] wire = java.util.Arrays.copyOf(buffer, bytes);

        Happy.ShipCreate got = new Happy.ShipCreate();
        if (!Happy.readShipCreate(got, wire, bytes * 8)) {
            return fail("readShipCreate refused the wire");
        }

        if (got.shipType != Happy.ShipType.CORVETTE) return fail("ship_type " + got.shipType);
        if (got.position.x != 1.5) return fail("position.x " + got.position.x);
        if (got.position.y != -2.25) return fail("position.y " + got.position.y);
        if (got.position.z != 3.0) return fail("position.z " + got.position.z);
        if (got.health != 777) return fail("health " + got.health);
        if (!got.alive) return fail("alive " + got.alive);

        System.out.println("happy java: ok (" + bytes + " bytes)");
        return 0;
    }

    public static void main(String[] args) {
        System.exit(run());
    }
}
