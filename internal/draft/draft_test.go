package draft

import "testing"

func TestCMSequenceBalanced(t *testing.T) {
	for _, fp := range []Team{Radiant, Dire} {
		seq := CMSequence(fp)
		if len(seq) != 24 {
			t.Fatalf("firstPick=%v: got %d steps, want 24", fp, len(seq))
		}
		bans := map[Team]int{}
		picks := map[Team]int{}
		for _, s := range seq {
			if s.Action == Ban {
				bans[s.Team]++
			} else {
				picks[s.Team]++
			}
		}
		for _, tm := range []Team{Radiant, Dire} {
			if bans[tm] != 7 {
				t.Errorf("firstPick=%v team=%v: %d bans, want 7", fp, tm, bans[tm])
			}
			if picks[tm] != 5 {
				t.Errorf("firstPick=%v team=%v: %d picks, want 5", fp, tm, picks[tm])
			}
		}
		if seq[0].Team != fp {
			t.Errorf("firstPick=%v: first step team=%v, want first-pick team", fp, seq[0].Team)
		}
	}
}

func TestApplyAndQueries(t *testing.T) {
	d := New(Radiant)
	// First four steps are bans: R, D, R, D.
	want := []Step{{Radiant, Ban}, {Dire, Ban}, {Radiant, Ban}, {Dire, Ban}}
	for i, w := range want {
		s, ok := d.Current()
		if !ok || s != w {
			t.Fatalf("step %d: got %+v ok=%v, want %+v", i, s, ok, w)
		}
		if err := d.Apply(i + 1); err != nil {
			t.Fatalf("apply step %d: %v", i, err)
		}
	}
	if got := d.Bans(Radiant); len(got) != 2 || got[0] != 1 || got[1] != 3 {
		t.Errorf("Radiant bans = %v, want [1 3]", got)
	}
	if got := d.Bans(Dire); len(got) != 2 || got[0] != 2 || got[1] != 4 {
		t.Errorf("Dire bans = %v, want [2 4]", got)
	}
}

func TestApplyRejectsDuplicateAndInvalid(t *testing.T) {
	d := New(Radiant)
	if err := d.Apply(NoHero); err == nil {
		t.Error("expected error for NoHero id")
	}
	if err := d.Apply(42); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	if err := d.Apply(42); err == nil {
		t.Error("expected error applying duplicate hero")
	}
	if !d.IsTaken(42) {
		t.Error("hero 42 should be taken")
	}
}

func TestUndo(t *testing.T) {
	d := New(Dire)
	if err := d.Undo(); err == nil {
		t.Error("undo on empty draft should error")
	}
	_ = d.Apply(10)
	_ = d.Apply(11)
	if err := d.Undo(); err != nil {
		t.Fatalf("undo: %v", err)
	}
	if d.IsTaken(11) {
		t.Error("hero 11 should be free after undo")
	}
	if d.Cursor != 1 {
		t.Errorf("cursor = %d, want 1", d.Cursor)
	}
}

func TestCompleteAfterFullDraft(t *testing.T) {
	d := New(Radiant)
	for i := 0; i < len(d.Seq); i++ {
		if err := d.Apply(100 + i); err != nil {
			t.Fatalf("apply %d: %v", i, err)
		}
	}
	if !d.Complete() {
		t.Error("draft should be complete")
	}
	if _, ok := d.Current(); ok {
		t.Error("Current should report not-ok when complete")
	}
	if err := d.Apply(999); err == nil {
		t.Error("apply on complete draft should error")
	}
}
