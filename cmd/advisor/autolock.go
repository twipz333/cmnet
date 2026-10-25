package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"cmnet/internal/atomicfile"
)

// autoLock is an advisory single-writer lock for background collection.
//
// Two copies of the app collecting into the same cache is not a race — SQLite in
// WAL mode handles the writes — it is a waste: they share one STRATZ daily quota
// and would spend it fetching the same drafts twice, halving a collection that
// already takes days. So the second copy comes up with collection off and says so,
// rather than silently competing.
//
// Advisory on purpose. A hard OS lock would strand the file when the process is
// killed (which on Windows, closing the console window, is the normal way this app
// exits), and a stranded lock is worse than a duplicated sync: it turns collection
// off permanently with no obvious way to fix it. Instead the holder writes its pid
// and refreshes a heartbeat; a lock whose heartbeat has gone stale is taken over.
type autoLock struct {
	path string
	// token identifies THIS holder. Two instances starting at the same second can
	// both find the lock absent or stale, and both write; the token is what lets the
	// loser notice at its first heartbeat that the file is no longer its own, and
	// what stops either from deleting or refreshing a lock it does not hold.
	token string
	stop  chan struct{}
	// lost is closed when a heartbeat finds someone else's token in the file.
	lost chan struct{}
	// beatDone closes when the heartbeat goroutine has returned, so Release can be
	// sure no write will follow the file it is about to remove.
	beatDone chan struct{}
	once     sync.Once // guards closing lost
	stopped  sync.Once // guards closing stop
}

// lockStale is how long a lock survives without a heartbeat. Comfortably longer
// than lockBeat so an app that is merely busy is not evicted, short enough that
// restarting after a kill does not leave the user without collection for long.
const (
	lockBeat  = 30 * time.Second
	lockStale = 3 * time.Minute
)

// acquireAutoLock claims the collection lock for dbPath. It returns nil (and the
// reason) when another live instance holds it.
func acquireAutoLock(dbPath string) (*autoLock, string) {
	path := lockPathFor(dbPath)
	l := &autoLock{
		path: path, token: newLockToken(),
		stop: make(chan struct{}), lost: make(chan struct{}), beatDone: make(chan struct{}),
	}

	// Create exclusively first, so the common case (no lock file) cannot be won by
	// two starters at once: O_EXCL makes exactly one of them succeed.
	err := l.create()
	if os.IsExist(err) {
		holder, ok := readLock(path)
		if ok && holder.pid != os.Getpid() && time.Since(holder.at) < lockStale && processAlive(holder.pid) {
			return nil, fmt.Sprintf("another advisor (pid %d) is collecting into %s", holder.pid, dbPath)
		}
		// Stale, ours from a previous run, or unreadable: take it over. The write is
		// still a race between two would-be holders, which is what the token in the
		// file is for — the loser sees a foreign token at its next heartbeat and
		// stands down.
		err = l.write()
	}
	if err != nil {
		// Not being able to write the lock file is not a reason to refuse to collect:
		// the lock is an optimisation, and the failure mode it prevents (two syncs) is
		// milder than the one refusing would cause (no sync at all).
		l.path = ""
		close(l.beatDone)
		return l, ""
	}
	go l.beat()
	return l, ""
}

// Lost closes when another instance has taken this lock over — which can happen
// when two of them decide at the same moment that an old lock is stale and both
// write. The caller stops collecting when it fires; otherwise the loser would
// keep spending the same daily quota, which is the whole thing the lock prevents.
func (l *autoLock) Lost() <-chan struct{} { return l.lost }

// beat refreshes the lock, and gives it up if it turns out to belong to someone
// else now.
func (l *autoLock) beat() {
	defer close(l.beatDone)
	t := time.NewTicker(lockBeat)
	defer t.Stop()
	for {
		select {
		case <-l.stop:
			return
		case <-t.C:
			if holder, ok := readLock(l.path); ok && holder.token != l.token {
				l.once.Do(func() { close(l.lost) })
				return
			}
			_ = l.write()
		}
	}
}

// Release stops the heartbeat and removes the lock file, but only while it is
// still ours: a lock taken over after we were considered stale belongs to the new
// holder, and deleting it would let a third instance in.
func (l *autoLock) Release() {
	if l == nil {
		return
	}
	l.stopOnce()
	// Wait for the heartbeat to be done before deleting, or it can rewrite the file
	// straight after the removal and leave a lock nobody owns.
	<-l.beatDone
	if l.path == "" {
		return
	}
	if holder, ok := readLock(l.path); ok && holder.token != l.token {
		return
	}
	_ = os.Remove(l.path)
}

// stopOnce signals the heartbeat exactly once, so a second Release does not panic
// on a closed channel.
func (l *autoLock) stopOnce() { l.stopped.Do(func() { close(l.stop) }) }

// create writes the lock file only if it does not exist yet.
func (l *autoLock) create() error {
	f, err := os.OpenFile(l.path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	_, werr := f.WriteString(l.body())
	cerr := f.Close()
	if werr != nil {
		return werr
	}
	return cerr
}

// write replaces the lock file's contents with ours.
//
// Atomically, so a reader never sees half a record: a truncated line would parse
// as a lock with no owner token, and every holder that read it would conclude the
// lock is someone else's and stand down — all of them, leaving nobody collecting.
func (l *autoLock) write() error {
	if l.path == "" {
		return nil
	}
	return atomicfile.Write(l.path, []byte(l.body()), 0o644)
}

func (l *autoLock) body() string {
	return fmt.Sprintf("%d\n%d\n%s\n", os.Getpid(), time.Now().Unix(), l.token)
}

// newLockToken returns a value unique to this holder. Time plus pid is enough:
// the only thing it has to distinguish is two processes writing the same file,
// and two processes cannot share a pid at the same instant.
func newLockToken() string {
	return fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UnixNano())
}

// lockHolder is what a lock file says about whoever wrote it.
type lockHolder struct {
	pid   int
	at    time.Time
	token string
}

// readLock parses a lock file. ok is false when it is missing or unreadable,
// which is treated as "not locked".
func readLock(path string) (h lockHolder, ok bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return lockHolder{}, false
	}
	fields := strings.Fields(string(data))
	if len(fields) < 2 {
		return lockHolder{}, false
	}
	pid, err := strconv.Atoi(fields[0])
	if err != nil {
		return lockHolder{}, false
	}
	unix, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		return lockHolder{}, false
	}
	h = lockHolder{pid: pid, at: time.Unix(unix, 0)}
	if len(fields) > 2 {
		h.token = fields[2] // absent in files written by an older build
	}
	return h, true
}

// processAlive reports whether a process with this pid still exists.
//
// This is what makes "close the window, start it again" not cost the user three
// minutes of no collection: the lock its predecessor left behind names a pid that
// is gone, so it can be taken over immediately instead of waiting out lockStale.
//
// Windows-accurate, best-effort elsewhere: os.FindProcess opens a real handle on
// Windows and fails for a dead pid, while on Unix it always succeeds — there this
// returns true and the staleness timeout remains the only fallback, which is the
// behaviour we had anyway.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	if _, err := os.FindProcess(pid); err != nil {
		return false
	}
	return true
}

// lockPathFor puts the lock next to the database it guards, so a second copy of
// the app pointed at the same cache finds it regardless of working directory.
func lockPathFor(dbPath string) string { return filepath.Clean(dbPath) + ".autolock" }
