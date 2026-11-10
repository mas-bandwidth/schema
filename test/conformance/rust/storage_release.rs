// The storage-bound refusal in a release build (docs/SPEC.md §5.1): a
// string(8) whose used length is 9 indexes past the storage it describes, and
// the release profile compiles every debug_assert! out while the refusal stays.

use storage_release::{root_measure_messages, root_save_messages, RootRow, TableReport};

fn main() {
    let mut value = RootRow::default();
    value.wide_length = 9; // the declared bound is 8
    value.wide[8] = 0x41;

    let mut report = TableReport::default();
    if root_measure_messages(std::slice::from_ref(&value), &mut report) != -1 {
        println!("FAIL storage release: root_measure_messages wrote a string past its storage");
        std::process::exit(1);
    }

    let mut report = TableReport::default();
    let mut buffer = [0u8; 4096];
    if root_save_messages(std::slice::from_ref(&value), &mut buffer, &mut report) != -1 {
        println!("FAIL storage release: root_save_messages wrote a string past its storage");
        std::process::exit(1);
    }

    println!("storage release gate: a string past its storage is refused with debug-assertions off");
}
