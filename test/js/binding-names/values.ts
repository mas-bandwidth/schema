import { number as a, bigint as b, symbol as c, unknown as d, never as e } from "./Values.js";

const values: number[] = [a, b, c, d, e];
if (values.join(",") !== "1,2,3,4,5") {
    throw new Error("generated value bindings changed: " + values);
}
