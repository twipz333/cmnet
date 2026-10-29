package cache

import (
	"database/sql"
	"fmt"
	"time"
)

// The job queue is the cache's persistence layer for background work.
//
// Before it, everything long-running was a goroutine plus an in-memory slot: one
// operation at a time, no history, and a killed process lost both the work and any
// record that it happened. The queue turns those into rows, which is what makes
// "collection keeps running while you draft" and "show me what the background
// worker did an hour ago" possible at all.

// JobState is where a job is in its lifecycle.
type JobState string

const (
	JobPending   JobState = "pending"
	JobRunning   JobState = "running"
	JobDone      JobState = "done"
	JobFailed    JobState = "failed"
	JobCancelled JobState = "cancelled"
)

// Job priorities. A manual action always outranks background upkeep: the user is
// sitting in front of the screen waiting for it.
const (
	PriorityBackground = 0
	PriorityManual     = 100
)

// Job is one unit of queued work.
type Job struct {
	ID        int64
	Kind      string
	Priority  int
	State     JobState
	Payload   string // opaque to the queue; the handler decides what it means
	Attempts  int
	NextRunAt int64 // unix seconds; a retry is not eligible before this
	CreatedAt int64
	StartedAt int64 // 0 until first claimed
	UpdatedAt int64
	Result    string
	Error     string
}

// JobLogEntry is one line a job emitted.
//
// Structured rather than a formatted string because the browser filters on these
// fields: the old front end had one flat []string for everything, so "show me only
// what failed" was not expressible.
type JobLogEntry struct {
	JobID int64
	Seq   int64
	At    int64 // unix millis; sub-second ordering matters within a chatty phase
	Level string
	Phase string
	Text  string
}

// Log levels. Kept to three: anything finer invites a taxonomy nobody maintains.
const (
	LogInfo  = "info"
	LogWarn  = "warn"
	LogError = "error"
)

func migrateJobs(tx *sql.Tx) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS jobs (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			kind        TEXT    NOT NULL,
			priority    INTEGER NOT NULL DEFAULT 0,   -- higher runs first
			state       TEXT    NOT NULL,             -- pending|running|done|failed|cancelled
			payload     TEXT    NOT NULL DEFAULT '',
			attempts    INTEGER NOT NULL DEFAULT 0,
			next_run_at INTEGER NOT NULL DEFAULT 0,   -- unix seconds; retry backoff
			created_at  INTEGER NOT NULL,
			started_at  INTEGER NOT NULL DEFAULT 0,
			updated_at  INTEGER NOT NULL,
			result      TEXT    NOT NULL DEFAULT '',
			error       TEXT    NOT NULL DEFAULT ''
		)`,
		// The claim query is "pending, due, best priority, oldest first"; this index
		// is that query. Without it every claim scans the whole history, which grows
		// without bound.
		`CREATE INDEX IF NOT EXISTS idx_jobs_claim ON jobs(state, next_run_at, priority DESC, id)`,
		`CREATE INDEX IF NOT EXISTS idx_jobs_recent ON jobs(created_at DESC)`,
		`CREATE TABLE IF NOT EXISTS job_log (
			job_id INTEGER NOT NULL,
			seq    INTEGER NOT NULL,
			at     INTEGER NOT NULL,            -- unix millis
			level  TEXT    NOT NULL DEFAULT 'info',
			phase  TEXT    NOT NULL DEFAULT '',
			text   TEXT    NOT NULL,
			PRIMARY KEY (job_id, seq),
			FOREIGN KEY (job_id) REFERENCES jobs(id) ON DELETE CASCADE
		)`,
	}
	for _, s := range stmts {
		if _, err := tx.Exec(s); err != nil {
			return fmt.Errorf("create job queue: %w", err)
		}
	}
	return nil
}

// EnqueueJob adds a job and returns its id.
//
// dedupe collapses a job onto an identical one already waiting: the background
// worker re-proposes the same upkeep every pass, and without this a queue that is
// busy for an hour accumulates sixty identical sync jobs that all run in sequence
// afterwards. A RUNNING job is not deduped against — that one is already underway,
// and the new request is about work that has arrived since it started.
func (s *Store) EnqueueJob(kind string, priority int, payload string, dedupe bool) (int64, error) {
	now := time.Now().Unix()
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	if dedupe {
		var id int64
		err := tx.QueryRow(
			`SELECT id FROM jobs WHERE kind = ? AND state = ? ORDER BY id LIMIT 1`,
			kind, string(JobPending)).Scan(&id)
		if err == nil {
			// Raise the priority if this request is more urgent than the waiting one:
			// a user pressing Sync must not inherit the background copy's place in line.
			if _, err := tx.Exec(
				`UPDATE jobs SET priority = MAX(priority, ?), updated_at = ? WHERE id = ?`,
				priority, now, id); err != nil {
				return 0, err
			}
			return id, tx.Commit()
		}
		if err != sql.ErrNoRows {
			return 0, err
		}
	}

	res, err := tx.Exec(
		`INSERT INTO jobs(kind, priority, state, payload, attempts, next_run_at, created_at, updated_at)
		 VALUES(?, ?, ?, ?, 0, 0, ?, ?)`,
		kind, priority, string(JobPending), payload, now, now)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

// ClaimJob takes the best eligible job whose kind is not in skip, marking it
// running. It returns (nil, nil) when nothing is eligible.
//
// skip carries the kinds whose resources are currently held by a running job. The
// exclusion is by resource rather than global — that is the whole point of the
// queue — but the resource map lives with the handlers, so the caller resolves it
// to a kind list and the storage layer stays ignorant of what a resource is.
//
// The claim is a single transaction: two workers racing for the last pending job
// must not both get it.
func (s *Store) ClaimJob(skip []string) (*Job, error) { return s.ClaimJobMin(skip, -1<<31) }

// ClaimJobMin is ClaimJob restricted to jobs of at least minPriority.
func (s *Store) ClaimJobMin(skip []string, minPriority int) (*Job, error) {
	now := time.Now().Unix()
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	q := `SELECT id, kind, priority, state, payload, attempts, next_run_at, created_at, started_at, updated_at, result, error
	      FROM jobs
	      WHERE state = ? AND next_run_at <= ? AND priority >= ?`
	args := []any{string(JobPending), now, minPriority}
	for _, k := range skip {
		q += " AND kind <> ?"
		args = append(args, k)
	}
	// Priority first, then insertion order: FIFO within a priority is what makes the
	// queue predictable to a user watching it.
	q += " ORDER BY priority DESC, id LIMIT 1"

	var j Job
	var state string
	err = tx.QueryRow(q, args...).Scan(&j.ID, &j.Kind, &j.Priority, &state, &j.Payload,
		&j.Attempts, &j.NextRunAt, &j.CreatedAt, &j.StartedAt, &j.UpdatedAt, &j.Result, &j.Error)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	j.State = JobState(state)

	if _, err := tx.Exec(
		`UPDATE jobs SET state = ?, attempts = attempts + 1, started_at = ?, updated_at = ?, error = ''
		 WHERE id = ?`,
		string(JobRunning), now, now, j.ID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	j.State = JobRunning
	j.Attempts++
	j.StartedAt = now
	return &j, nil
}

// FinishJob records a terminal state for a job.
func (s *Store) FinishJob(id int64, state JobState, result, errText string) error {
	now := time.Now().Unix()
	_, err := s.db.Exec(
		`UPDATE jobs SET state = ?, result = ?, error = ?, updated_at = ? WHERE id = ?`,
		string(state), result, errText, now, id)
	return err
}

// RetryJob puts a failed job back in the queue, eligible after delay.
func (s *Store) RetryJob(id int64, errText string, delay time.Duration) error {
	now := time.Now()
	_, err := s.db.Exec(
		`UPDATE jobs SET state = ?, error = ?, next_run_at = ?, updated_at = ? WHERE id = ?`,
		string(JobPending), errText, now.Add(delay).Unix(), now.Unix(), id)
	return err
}

// CancelJob marks a job cancelled. A running job is only marked here; stopping the
// goroutine is the queue's job.
func (s *Store) CancelJob(id int64) error {
	now := time.Now().Unix()
	_, err := s.db.Exec(
		`UPDATE jobs SET state = ?, updated_at = ? WHERE id = ? AND state IN (?, ?)`,
		string(JobCancelled), now, id, string(JobPending), string(JobRunning))
	return err
}

// RecoverJobs returns jobs left running by a process that died back to pending.
//
// Called once at startup. Without it a crash mid-sync leaves a row claiming to be
// running forever, and the resource it held is never released — the queue would
// deadlock itself across a restart.
func (s *Store) RecoverJobs() (int64, error) {
	now := time.Now().Unix()
	res, err := s.db.Exec(
		`UPDATE jobs SET state = ?, error = 'interrupted by shutdown', updated_at = ?
		 WHERE state = ?`,
		string(JobPending), now, string(JobRunning))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// AppendJobLog stores one log line, assigning the next seq for that job.
//
// Convenience for callers outside a running job (a queue-level note about a job
// that was never claimed). Anything on a hot path uses AppendJobLogBatch: a sync
// emits thousands of lines, and one transaction each is the difference between
// logging being free and logging being the bottleneck.
func (s *Store) AppendJobLog(jobID int64, level, phase, text string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var seq int64
	if err := tx.QueryRow(
		`SELECT COALESCE(MAX(seq), 0) + 1 FROM job_log WHERE job_id = ?`, jobID).Scan(&seq); err != nil {
		return err
	}
	if _, err := tx.Exec(
		`INSERT INTO job_log(job_id, seq, at, level, phase, text) VALUES(?, ?, ?, ?, ?, ?)`,
		jobID, seq, time.Now().UnixMilli(), level, phase, text); err != nil {
		return err
	}
	return tx.Commit()
}

// AppendJobLogBatch stores pre-sequenced lines in one transaction. The caller owns
// the seq counter, which is why this is cheap: no read-back per line.
func (s *Store) AppendJobLogBatch(entries []JobLogEntry) error {
	if len(entries) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(
		`INSERT OR REPLACE INTO job_log(job_id, seq, at, level, phase, text) VALUES(?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, e := range entries {
		if _, err := stmt.Exec(e.JobID, e.Seq, e.At, e.Level, e.Phase, e.Text); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// MaxJobLogSeq returns the highest seq stored for a job, so a queue restarting
// mid-job continues the sequence instead of colliding with what is already there.
func (s *Store) MaxJobLogSeq(jobID int64) (int64, error) {
	var seq int64
	err := s.rd.QueryRow(`SELECT COALESCE(MAX(seq), 0) FROM job_log WHERE job_id = ?`, jobID).Scan(&seq)
	return seq, err
}

// Jobs returns the most recent jobs, newest first.
func (s *Store) Jobs(limit int) ([]Job, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.rd.Query(
		`SELECT id, kind, priority, state, payload, attempts, next_run_at, created_at, started_at, updated_at, result, error
		 FROM jobs ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Job
	for rows.Next() {
		var j Job
		var state string
		if err := rows.Scan(&j.ID, &j.Kind, &j.Priority, &state, &j.Payload, &j.Attempts,
			&j.NextRunAt, &j.CreatedAt, &j.StartedAt, &j.UpdatedAt, &j.Result, &j.Error); err != nil {
			return nil, err
		}
		j.State = JobState(state)
		out = append(out, j)
	}
	return out, rows.Err()
}

// Job returns one job by id, or (nil, nil) if it no longer exists (trimmed away).
func (s *Store) Job(id int64) (*Job, error) {
	var j Job
	var state string
	err := s.rd.QueryRow(
		`SELECT id, kind, priority, state, payload, attempts, next_run_at, created_at, started_at, updated_at, result, error
		 FROM jobs WHERE id = ?`, id).
		Scan(&j.ID, &j.Kind, &j.Priority, &state, &j.Payload, &j.Attempts,
			&j.NextRunAt, &j.CreatedAt, &j.StartedAt, &j.UpdatedAt, &j.Result, &j.Error)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	j.State = JobState(state)
	return &j, nil
}

// ActiveJobs returns the jobs that are pending or running, oldest first — what the
// status bar reports as "in flight".
func (s *Store) ActiveJobs() ([]Job, error) {
	rows, err := s.rd.Query(
		`SELECT id, kind, priority, state, payload, attempts, next_run_at, created_at, started_at, updated_at, result, error
		 FROM jobs WHERE state IN (?, ?) ORDER BY id`,
		string(JobPending), string(JobRunning))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Job
	for rows.Next() {
		var j Job
		var state string
		if err := rows.Scan(&j.ID, &j.Kind, &j.Priority, &state, &j.Payload, &j.Attempts,
			&j.NextRunAt, &j.CreatedAt, &j.StartedAt, &j.UpdatedAt, &j.Result, &j.Error); err != nil {
			return nil, err
		}
		j.State = JobState(state)
		out = append(out, j)
	}
	return out, rows.Err()
}

// JobLog returns a job's lines after seq (0 for all), oldest first.
func (s *Store) JobLog(jobID, afterSeq int64, limit int) ([]JobLogEntry, error) {
	if limit <= 0 {
		limit = 500
	}
	rows, err := s.rd.Query(
		`SELECT job_id, seq, at, level, phase, text FROM job_log
		 WHERE job_id = ? AND seq > ? ORDER BY seq LIMIT ?`, jobID, afterSeq, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []JobLogEntry
	for rows.Next() {
		var e JobLogEntry
		if err := rows.Scan(&e.JobID, &e.Seq, &e.At, &e.Level, &e.Phase, &e.Text); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// LastJobLog returns a job's most recent line. ok is false when it logged nothing.
func (s *Store) LastJobLog(jobID int64) (JobLogEntry, bool, error) {
	var e JobLogEntry
	err := s.rd.QueryRow(
		`SELECT job_id, seq, at, level, phase, text FROM job_log
		 WHERE job_id = ? ORDER BY seq DESC LIMIT 1`, jobID).
		Scan(&e.JobID, &e.Seq, &e.At, &e.Level, &e.Phase, &e.Text)
	if err == sql.ErrNoRows {
		return JobLogEntry{}, false, nil
	}
	if err != nil {
		return JobLogEntry{}, false, err
	}
	return e, true, nil
}

// maxLinesPerJob bounds how much of one job's log is kept.
//
// A sync emits several hundred lines per pass and a long one thousands, so the
// history is dominated by a handful of chatty jobs. The tail is what anyone
// actually reads — the summary, and whatever went wrong near the end — so the
// oldest lines of an over-long job are dropped rather than the job itself.
const maxLinesPerJob = 2000

// TrimJobLog drops the oldest lines of a job that has produced too many.
func (s *Store) TrimJobLog(jobID int64, keep int) error {
	if keep <= 0 {
		keep = maxLinesPerJob
	}
	_, err := s.db.Exec(
		`DELETE FROM job_log WHERE job_id = ? AND seq <= (
			SELECT MAX(seq) - ? FROM job_log WHERE job_id = ?
		 )`, jobID, keep, jobID)
	return err
}

// TrimJobs deletes finished jobs beyond the newest keep, so a machine collecting
// for months does not accumulate an unbounded log. Cascades to job_log.
func (s *Store) TrimJobs(keep int) error {
	if keep <= 0 {
		keep = 200
	}
	_, err := s.db.Exec(
		`DELETE FROM jobs WHERE state NOT IN (?, ?) AND id NOT IN (
			SELECT id FROM jobs WHERE state NOT IN (?, ?) ORDER BY id DESC LIMIT ?
		 )`,
		string(JobPending), string(JobRunning), string(JobPending), string(JobRunning), keep)
	return err
}
