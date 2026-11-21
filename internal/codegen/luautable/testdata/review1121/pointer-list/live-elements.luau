--!strict
-- The control: a list of pointers holding only live elements, which every
-- spelling of the exported type admits.
local G = require("./GraphTable")
local n: G.Node = G.NewNode()
n.Kids = { G.NewNode(), G.NewNode() }
local first = n.Kids[1]
assert(first and first.V == 0, "a live element")
print("ok")
