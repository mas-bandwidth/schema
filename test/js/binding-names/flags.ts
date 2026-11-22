import { numbera, numberb, bigintc, symbold, unknowne, neverf, numberCount, Packet } from "./Flags.js";

const packet = new Packet();
packet.n = numbera | numberb;
packet.s = symbold;
const masks: bigint[] = [numbera, numberb, bigintc, symbold, unknowne, neverf];
if (masks.join(",") !== "1,2,1,1,1,1" || numberCount !== 2 || packet.n !== 3n) {
    throw new Error("generated flags masks changed: " + masks);
}
// every module of the unit, the table modules among them, loads
for (const module of ["./FlagsFlat.js", "./FlagsTable.js", "./BindingflagsTable.js", "./BindingflagsVariable.js", "./BindingflagsView.js"]) {
    await import(module);
}
