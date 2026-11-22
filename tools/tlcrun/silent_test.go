package main

import "testing"

// A states line whose counts do not fit an int is a line the judge cannot
// read, never 0 states left on the queue: read as 0, an unfinished search
// judged as finished.
func TestLastStatesRefusesACountItCannotRead(t *testing.T) {
	log := "1000 states generated, 900 distinct states found, 99999999999999999999 states left on queue.\n"
	if _, _, _, ok := lastStates(log); ok {
		t.Fatal("a queue count past an int was read as a states line")
	}
	g, d, q, ok := lastStates("1000 states generated, 900 distinct states found, 0 states left on queue.\n")
	if !ok || g != 1000 || d != 900 || q != 0 {
		t.Fatalf("the control line read %d %d %d %v", g, d, q, ok)
	}
}
