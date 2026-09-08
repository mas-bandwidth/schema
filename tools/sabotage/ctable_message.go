package main

func init() {
	sabotages["message-c-wrong-slot"] = []edit{{old: "ind, g.messageSlot(e))", new: "ind, g.messageSlot(e)+1) // SABOTAGED: a different vocabulary slot"}}
}
