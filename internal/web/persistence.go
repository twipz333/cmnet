package web

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"cmnet/internal/cache"
	"cmnet/internal/session"
)

// saveCheckpoint runs after a successful command, not after a read or SSE poll.
// Its short independent timeout survives an HTTP client closing after applying
// a move: the committed board must still be durable.
func (s *Server) saveCheckpoint() error {
	s.mu.Lock()
	store, sess, id := s.localCache, s.sess, s.draftID
	if store == nil || sess == nil {
		s.mu.Unlock()
		return nil
	}
	s.draftRevision++
	revision := s.draftRevision
	s.mu.Unlock()
	state, ok := sess.ExportDraft()
	if !ok { return nil } // GSI is owned by Dota, never replayed manually.
	version := sess.Config().BundleVersion
	if version == "" { return nil }
	data, err := json.Marshal(state)
	if err != nil { return fmt.Errorf("marshal draft checkpoint: %w", err) }
	ctx,cancel:=context.WithTimeout(context.Background(),5*time.Second)
	defer cancel()
	return store.SaveDraftCheckpoint(ctx,cache.DraftCheckpoint{
		ID:id,Revision:revision,ModelVersion:version,State:data,
	})
}

// restoreCheckpoint replays an existing manual draft only with the EXACT model
// version it was created with. Applying an unrelated model silently is worse
// than explaining why a safe resume is unavailable.
func (s *Server) restoreCheckpoint(ctx context.Context) {
	s.mu.Lock()
	store, sess := s.localCache, s.sess
	s.mu.Unlock()
	if store==nil || sess==nil || sess.GSIMode() { return }
	readCtx,cancel:=context.WithTimeout(ctx,5*time.Second)
	defer cancel()
	saved,exists,err:=store.LatestDraftCheckpoint(readCtx)
	if err!=nil { s.setStatus("cannot load saved draft: "+err.Error());return }
	if !exists { return }
	if saved.ModelVersion!=sess.Config().BundleVersion {
		s.setStatus("saved draft uses a different model version; board left untouched for safety")
		return
	}
	var previous session.DraftCheckpoint
	if err:=json.Unmarshal(saved.State,&previous);err!=nil {
		s.setStatus("saved draft JSON is invalid: "+err.Error());return
	}
	if err:=sess.RestoreDraft(previous);err!=nil {
		s.setStatus("saved draft cannot be replayed: "+err.Error());return
	}
	s.mu.Lock()
	s.draftID=saved.ID
	s.draftRevision=saved.Revision
	s.mu.Unlock()
}
