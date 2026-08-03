package web

import (
	"fmt"
	"testing"
	"time"
)

// The browser distinguishes "quietly working" from "hung" using these fields, so
// they have to keep moving even when the log itself stops growing.
func TestOpsDTOReportsLiveness(t *testing.T) {
	o := newOps()
	if d := o.dto(); d.Running != "" || d.RunningMs != 0 || d.QuietMs != 0 {
		t.Fatalf("idle ops should report nothing running, got %+v", d)
	}

	if !o.begin("sync", func() {}) {
		t.Fatal("begin should claim a free slot")
	}
	time.Sleep(20 * time.Millisecond)

	d := o.dto()
	if d.Running != "sync" {
		t.Errorf("running = %q, want sync", d.Running)
	}
	if d.RunningMs == 0 {
		t.Error("runningMs stayed 0; the page cannot show elapsed time")
	}
	if d.QuietMs == 0 {
		t.Error("quietMs stayed 0 despite no log lines")
	}

	o.append("first line")
	if d := o.dto(); d.QuietMs > 5 {
		t.Errorf("quietMs = %d after a fresh line, want ~0", d.QuietMs)
	}
}

// rev must keep counting past the log cap: len(log) saturates at maxProgressLines,
// so a UI watching the length would conclude a long sync had stopped producing
// output while it was in fact still working.
func TestOpsRevOutlivesTheLogCap(t *testing.T) {
	o := newOps()
	o.begin("sync", func() {})

	for i := range maxProgressLines + 50 {
		o.append(fmt.Sprintf("line %d", i))
	}
	d := o.dto()
	if len(d.Log) != maxProgressLines {
		t.Errorf("log length = %d, want it capped at %d", len(d.Log), maxProgressLines)
	}
	if d.Rev != uint64(maxProgressLines+50) {
		t.Errorf("rev = %d, want %d (it must not saturate with the log)", d.Rev, maxProgressLines+50)
	}

	before := d.Rev
	o.append("one more")
	if after := o.dto().Rev; after != before+1 {
		t.Errorf("rev did not advance past the cap: %d then %d", before, after)
	}
}

// begin must reset the clocks, or a second operation inherits the first one's
// start time and shows an elapsed time that never happened.
func TestOpsBeginResetsClocks(t *testing.T) {
	o := newOps()
	o.begin("sync", func() {})
	o.append("work")
	time.Sleep(30 * time.Millisecond)
	o.finish("done", nil)

	if !o.begin("train", func() {}) {
		t.Fatal("slot should be free after finish")
	}
	d := o.dto()
	if d.RunningMs > 15 {
		t.Errorf("runningMs = %d on a just-started operation; the previous run's clock leaked", d.RunningMs)
	}
	if d.Rev != 0 && len(d.Log) != 0 {
		t.Errorf("log was not cleared for the new operation: %+v", d)
	}
}

// A second operation must be refused while one is in flight, and the refusal must
// not disturb the running one's bookkeeping.
func TestOpsBeginRejectsConcurrentOperation(t *testing.T) {
	o := newOps()
	o.begin("sync", func() {})
	o.append("line")

	if o.begin("train", func() {}) {
		t.Fatal("a second operation was allowed to start")
	}
	if d := o.dto(); d.Running != "sync" || d.Rev != 1 {
		t.Errorf("the running operation was disturbed by the rejected one: %+v", d)
	}
}
