// Per-PR happy path for the Rust leg (ci-fast.yml): one ShipCreate written,
// read back, every field compared. The heavy Rust gates run nightly.

use happy::{ShipCreate, ShipType, Vector3};
use serialize::{ReadStream, Stream, WriteStream};
use std::process::ExitCode;

fn fail(why: &str) -> ExitCode {
    eprintln!("happy rust: FAIL: {why}");
    ExitCode::FAILURE
}

fn main() -> ExitCode {
    let input = ShipCreate {
        ship_type: ShipType::CORVETTE,
        position: Vector3 { x: 1.5, y: -2.25, z: 3.0 },
        health: 777,
        alive: true,
    };

    // + 8: a read buffer must extend 8 bytes past the data (the reader loads 64-bit windows)
    let mut buffer = [0u8; happy::SHIP_CREATE_MAX_BYTES + 8];
    let bytes = {
        let mut ws = WriteStream::new(&mut buffer[..happy::SHIP_CREATE_MAX_BYTES]);
        if let Err(e) = happy::write_ship_create(&mut ws, &input) {
            return fail(&format!("write ShipCreate: {e:?}"));
        }
        ws.flush();
        ws.bytes_processed() as usize
    };

    let mut out = ShipCreate::default();
    let mut rs = ReadStream::new(&buffer, bytes);
    if let Err(e) = happy::read_ship_create(&mut rs, &mut out) {
        return fail(&format!("read ShipCreate: {e:?}"));
    }

    if out.ship_type != ShipType::CORVETTE {
        return fail(&format!("ship_type mismatch: {:?}", out.ship_type));
    }
    if out.position.x != 1.5 {
        return fail(&format!("position.x mismatch: {}", out.position.x));
    }
    if out.position.y != -2.25 {
        return fail(&format!("position.y mismatch: {}", out.position.y));
    }
    if out.position.z != 3.0 {
        return fail(&format!("position.z mismatch: {}", out.position.z));
    }
    if out.health != 777 {
        return fail(&format!("health mismatch: {}", out.health));
    }
    if !out.alive {
        return fail("alive mismatch: false");
    }

    println!("happy rust: ok ({bytes} bytes)");
    ExitCode::SUCCESS
}
