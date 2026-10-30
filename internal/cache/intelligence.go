package cache

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

//go:embed migration_008.sql
var migration008SQL string

func migrateLocalIntelligence(tx *sql.Tx) error {
	_, err := tx.Exec(migration008SQL)
	return err
}

// SourceObservation records the source and freshness independently from any
// model-inferred hypothesis. A timeout is never evidence of a private profile.
type SourceObservation struct {
	AccountID int64 `json:"accountId"`
	Provider string `json:"provider"`
	ObservedAt int64 `json:"observedAt"`
	Status string `json:"status"`
	Evidence json.RawMessage `json:"evidence"`
}

var allowedObservationStatus = map[string]bool{
	"fresh":true, "stale":true, "loading":true, "partial":true,
	"private":true, "rate_limited":true, "unavailable":true, "error":true,
}

func (s *Store) RecordObservation(ctx context.Context, v SourceObservation) error {
	if v.AccountID <= 0 || len(v.Provider) < 1 || len(v.Provider) > 64 ||
		strings.TrimSpace(v.Provider) != v.Provider || !allowedObservationStatus[v.Status] {
		return errors.New("invalid source observation identity or status")
	}
	if v.ObservedAt <= 0 { v.ObservedAt = time.Now().Unix() }
	if len(v.Evidence) == 0 { v.Evidence = json.RawMessage("{}") }
	if !json.Valid(v.Evidence) { return errors.New("observation evidence is not JSON") }
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO source_observations(account_id,provider,observed_at,status,evidence_json)
		 VALUES(?,?,?,?,?) ON CONFLICT(account_id,provider,observed_at)
		 DO UPDATE SET status=excluded.status,evidence_json=excluded.evidence_json`,
		v.AccountID,v.Provider,v.ObservedAt,v.Status,string(v.Evidence))
	return err
}

func (s *Store) LatestObservation(ctx context.Context, id int64, provider string) (SourceObservation, bool, error) {
	var v SourceObservation
	var raw string
	err := s.rd.QueryRowContext(ctx,
		`SELECT account_id,provider,observed_at,status,evidence_json
		  FROM source_observations WHERE account_id=? AND provider=?
		  ORDER BY observed_at DESC LIMIT 1`,id,provider).
		Scan(&v.AccountID,&v.Provider,&v.ObservedAt,&v.Status,&raw)
	if errors.Is(err,sql.ErrNoRows) { return v,false,nil }
	if err != nil { return v,false,err }
	v.Evidence = json.RawMessage(raw)
	return v,true,nil
}

// DraftCheckpoint is a revisioned local copy of the captain's visible board.
// It is not a recomputed recommendation: it records the model actually used.
type DraftCheckpoint struct {
	ID string `json:"draftId"`
	CreatedAt int64 `json:"createdAt"`
	UpdatedAt int64 `json:"updatedAt"`
	Revision uint64 `json:"revision"`
	ModelVersion string `json:"modelVersion"`
	State json.RawMessage `json:"state"`
}

// SaveDraftCheckpoint is idempotent: late/out-of-order frames cannot roll the
// persisted board backwards. A later revision updates, same/older is ignored.
func (s *Store) SaveDraftCheckpoint(ctx context.Context, v DraftCheckpoint) error {
	if v.ID == "" || len(v.ID) > 128 || v.ModelVersion == "" || len(v.ModelVersion) > 256 ||
		!json.Valid(v.State) || v.Revision > uint64(1<<63-1) {
		return errors.New("invalid draft checkpoint")
	}
	now := time.Now().Unix()
	if v.CreatedAt == 0 { v.CreatedAt = now }
	if v.UpdatedAt == 0 { v.UpdatedAt = now }
	_,err := s.db.ExecContext(ctx,`INSERT INTO draft_sessions(draft_id,created_at,updated_at,revision,model_version,state_json)
	VALUES(?,?,?,?,?,?)
	ON CONFLICT(draft_id) DO UPDATE SET
	 updated_at=excluded.updated_at, revision=excluded.revision,
	 model_version=excluded.model_version,state_json=excluded.state_json
	WHERE excluded.revision > draft_sessions.revision`,
		v.ID,v.CreatedAt,v.UpdatedAt,v.Revision,v.ModelVersion,string(v.State))
	return err
}

func (s *Store) LoadDraftCheckpoint(ctx context.Context, id string) (DraftCheckpoint, bool, error) {
	var v DraftCheckpoint
	var body string
	err := s.rd.QueryRowContext(ctx,
		`SELECT draft_id,created_at,updated_at,revision,model_version,state_json
		FROM draft_sessions WHERE draft_id=?`,id).
		Scan(&v.ID,&v.CreatedAt,&v.UpdatedAt,&v.Revision,&v.ModelVersion,&body)
	if errors.Is(err,sql.ErrNoRows) { return v,false,nil }
	if err != nil { return v,false,err }
	v.State = json.RawMessage(body)
	return v,true,nil
}

// Backup produces a consistent snapshot of WAL-backed SQLite, not a raw copy
// of cmnet.db (which would silently lose uncheckpointed WAL transactions).
func (s *Store) Backup(ctx context.Context, destination string) error {
	if destination == "" { return errors.New("backup path is empty") }
	if _,err := os.Stat(destination); err == nil { return fmt.Errorf("backup exists: %s",destination) } else if !errors.Is(err,os.ErrNotExist) { return err }
	if err := os.MkdirAll(filepath.Dir(destination),0700); err != nil { return err }
	if _,err := s.db.ExecContext(ctx,"VACUUM INTO ?",destination); err != nil {
		_ = os.Remove(destination)
		return fmt.Errorf("SQLite consistent backup: %w",err)
	}
	// Verify the snapshot before reporting success to the migration UI.
	db,err := sql.Open("sqlite",dsnFor(destination))
	if err != nil { return err }
	defer db.Close()
	var integrity string
	if err := db.QueryRowContext(ctx,"PRAGMA integrity_check").Scan(&integrity); err != nil { return err }
	if integrity != "ok" { return fmt.Errorf("backup integrity check failed: %s",integrity) }
	return nil
}
