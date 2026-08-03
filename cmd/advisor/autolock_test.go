package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// A second instance must not start collecting into a cache someone else is
// already filling: they share one daily STRATZ quota and would spend it fetching
// the same drafts twice.
func TestAutoLockRefusesASecondHolder(t *testing.T) {
	db := filepath.Join(t.TempDir(), "cmnet.db")

	first, held := acquireAutoLock(db)
	if first == nil {
		t.Fatalf("first acquire failed: %s", held)
	}
	defer first.Release()

	// The parent process stands in for the other instance: it is genuinely running
	// (a lock naming a dead pid is taken over on purpose) and it is not us (a lock
	// written by our own pid is deliberately re-acquirable, so restarting the app
	// does not lock it out).
	writeLockAs(t, lockPathFor(db), os.Getppid(), time.Now())

	second, why := acquireAutoLock(db)
	if second != nil {
		second.Release()
		t.Fatal("two instances acquired the collection lock at once")
	}
	if why == "" {
		t.Error("refusing to collect must come with a reason to show the user")
	}
}

// A lock left behind by a killed process must expire. On Windows the normal way
// to quit this app is closing the console window, so a lock that outlived its
// owner would silently disable collection for good.
func TestAutoLockIsTakenOverWhenStale(t *testing.T) {
	db := filepath.Join(t.TempDir(), "cmnet.db")
	// A live pid, so it is the stale heartbeat being tested and not a dead owner.
	writeLockAs(t, lockPathFor(db), os.Getppid(), time.Now().Add(-lockStale-time.Minute))

	lock, why := acquireAutoLock(db)
	if lock == nil {
		t.Fatalf("a stale lock was not taken over: %s", why)
	}
	lock.Release()
}

// Closing the window and starting again is the normal way this app restarts. The
// lock its predecessor left behind names a dead pid, so it must be taken over at
// once rather than costing the user lockStale minutes of no collection.
//
// Windows-only: os.FindProcess cannot tell a dead pid from a live one on Unix, so
// there the staleness timeout is the only fallback and this check does not apply.
func TestAutoLockIsTakenOverFromADeadProcess(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("dead-pid detection is only accurate on Windows")
	}
	db := filepath.Join(t.TempDir(), "cmnet.db")
	// Fresh heartbeat, so only the dead pid can justify taking it over.
	writeLockAs(t, lockPathFor(db), deadPID, time.Now())

	lock, why := acquireAutoLock(db)
	if lock == nil {
		t.Fatalf("a lock held by a dead process was not taken over: %s", why)
	}
	lock.Release()
}

// deadPID is a pid no process can have (Windows pids are multiples of 4 and well
// below this).
const deadPID = 999999

// Releasing removes the file, so a restart is not made to wait out the staleness
// window.
func TestAutoLockReleaseRemovesTheFile(t *testing.T) {
	db := filepath.Join(t.TempDir(), "cmnet.db")
	lock, _ := acquireAutoLock(db)
	if lock == nil {
		t.Fatal("acquire failed")
	}
	lock.Release()
	if _, err := os.Stat(lockPathFor(db)); !os.IsNotExist(err) {
		t.Errorf("lock file still present after Release (err = %v)", err)
	}
}

// A garbled lock file must not disable collection forever — treat it as absent.
func TestAutoLockIgnoresAnUnreadableFile(t *testing.T) {
	db := filepath.Join(t.TempDir(), "cmnet.db")
	if err := os.WriteFile(lockPathFor(db), []byte("not a lock"), 0o644); err != nil {
		t.Fatal(err)
	}
	lock, why := acquireAutoLock(db)
	if lock == nil {
		t.Fatalf("a corrupt lock file blocked collection: %s", why)
	}
	lock.Release()
}

func writeLockAs(t *testing.T, path string, pid int, at time.Time) {
	t.Helper()
	body := fmt.Sprintf("%d\n%d\n", pid, at.Unix())
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
