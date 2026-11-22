--!strict
-- LUAU-3: a loaded list of pointers holds false for a null element and its
-- live length as n (the table.pack convention); strict code that handles
-- one type-checks against the exported type.
local G = require("./GraphTable")
local n: G.Node = G.NewNode()
n.Kids = { false, G.NewNode() }
n.Kids.n = 2
local first = n.Kids[1]
assert(first == false, "a null element is false")
local second = n.Kids[2]
assert(second ~= false and second ~= nil and second.V == 0, "a live element")
print("ok")
