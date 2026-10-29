package session

import "testing"

func TestTeamSheetSeatSwapsAndClears(t *testing.T) {
	var ts TeamSheet
	must := func(side string, pos int, acct int64) {
		t.Helper()
		if err := ts.Seat(side, pos, acct); err != nil {
			t.Fatal(err)
		}
	}
	must("our", 1, 11)
	must("our", 2, 22)
	must("our", 2, 11) // 11 moves to mid: 22 takes carry
	if ts.Our != [5]int64{22, 11, 0, 0, 0} {
		t.Fatalf("swap: %v", ts.Our)
	}
	must("our", 3, 22) // moving into an empty seat leaves the old one empty
	if ts.Our != [5]int64{0, 11, 22, 0, 0} {
		t.Fatalf("move: %v", ts.Our)
	}
	must("our", 2, 0)
	if ts.Our != [5]int64{0, 0, 22, 0, 0} {
		t.Fatalf("clear: %v", ts.Our)
	}
	must("enemy", 5, 22) // sides are independent
	if ts.Enemy[4] != 22 || ts.Our[2] != 22 {
		t.Fatalf("sides mixed: %+v", ts)
	}
	if err := ts.Seat("our", 6, 1); err == nil {
		t.Fatal("position 6 accepted")
	}
	if err := ts.Seat("both", 1, 1); err == nil {
		t.Fatal("bad side accepted")
	}
}
