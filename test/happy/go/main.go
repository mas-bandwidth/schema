// Per-PR happy path for the Go leg (ci-fast.yml): one ShipCreate written,
// read back, every field compared. The heavy Go gates run nightly.
package main

import (
	"fmt"
	"os"

	"github.com/mas-bandwidth/serialize.go/v2"
	"happy"
)

func fail(why string, args ...any) {
	fmt.Fprintf(os.Stderr, "happy go: FAIL: "+why+"\n", args...)
	os.Exit(1)
}

func main() {
	in := happy.ShipCreate{
		ShipType: happy.ShipTypeCorvette,
		Position: happy.Vector3{X: 1.5, Y: -2.25, Z: 3.0},
		Health:   777,
		Alive:    true,
	}

	buffer := make([]byte, happy.ShipCreateMaxBytes)
	w := serialize.NewWriteStream(buffer)
	if err := happy.WriteShipCreate(w, &in); err != nil {
		fail("write ShipCreate: %v", err)
	}
	w.Flush()
	if err := w.Err(); err != nil {
		fail("write stream: %v", err)
	}
	bytes := w.BytesProcessed()

	var out happy.ShipCreate
	if err := happy.ReadShipCreate(serialize.NewReadStream(buffer[:bytes]), &out); err != nil {
		fail("read ShipCreate: %v", err)
	}

	switch {
	case out.ShipType != happy.ShipTypeCorvette:
		fail("ship_type mismatch: %v", out.ShipType)
	case out.Position.X != 1.5:
		fail("position.x mismatch: %v", out.Position.X)
	case out.Position.Y != -2.25:
		fail("position.y mismatch: %v", out.Position.Y)
	case out.Position.Z != 3.0:
		fail("position.z mismatch: %v", out.Position.Z)
	case out.Health != 777:
		fail("health mismatch: %v", out.Health)
	case out.Alive != true:
		fail("alive mismatch: %v", out.Alive)
	}

	fmt.Printf("happy go: ok (%d bytes)\n", bytes)
}
