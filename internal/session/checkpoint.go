package session

import (
	"fmt"

	"cmnet/internal/draft"
)

// DraftCheckpoint is the exact sequence the captain has already played.
// The recommender is not serialized: it is resolved by the pinned bundle version.
type DraftCheckpoint struct {
	Format int `json:"format"`
	FirstPick draft.Team `json:"firstPick"`
	OurTeam draft.Team `json:"ourTeam"`
	Steps []draft.Step `json:"steps"`
	Heroes []int `json:"heroes"`
}

// ExportDraft copies the board under the session lock. GSI state is owned by
// the game and should not be replayed as a manual board on restart.
func (s *Session) ExportDraft() (DraftCheckpoint, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gsiMode || s.drafts == nil { return DraftCheckpoint{}, false }
	n := s.drafts.Cursor
	d := DraftCheckpoint{
		Format:1, FirstPick:s.cfg.FirstPick, OurTeam:s.cfg.OurTeam,
		Steps:append([]draft.Step{},s.drafts.Seq[:n]...),
		Heroes:append([]int{},s.drafts.Selections[:n]...),
	}
	return d,true
}

// RestoreDraft validates a checkpoint before changing any live state. Replaying
// the immutable captured steps preserves patch-specific manual step overrides,
// without logging historical actions a second time.
func (s *Session) RestoreDraft(saved DraftCheckpoint) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gsiMode { return fmt.Errorf("cannot restore manual board in GSI mode") }
	if saved.Format!=1 || len(saved.Steps)!=len(saved.Heroes) ||
		(saved.FirstPick!=draft.Radiant && saved.FirstPick!=draft.Dire) ||
		(saved.OurTeam!=draft.Radiant && saved.OurTeam!=draft.Dire) {
		return fmt.Errorf("invalid or incompatible draft checkpoint")
	}
	next:=draft.NewWithOrder(saved.FirstPick,s.cmOrder())
	if len(saved.Heroes)>len(next.Seq) { return fmt.Errorf("draft checkpoint exceeds CM step count") }
	allowed:=make(map[int]bool,len(s.cfg.Heroes))
	for _,h:=range s.cfg.Heroes { allowed[h.ID]=true }
	for i,hero:=range saved.Heroes {
		step:=saved.Steps[i]
		if !allowed[hero] || (step.Team!=draft.Radiant && step.Team!=draft.Dire) ||
			(step.Action!=draft.Ban && step.Action!=draft.Pick) {
			return fmt.Errorf("invalid hero or action at checkpoint step %d",i)
		}
		next.SetCurrentStep(step.Team,step.Action)
		if err:=next.Apply(hero);err!=nil { return fmt.Errorf("checkpoint step %d: %w",i,err) }
	}
	s.cfg.FirstPick=saved.FirstPick
	s.cfg.OurTeam=saved.OurTeam
	s.drafts=next
	s.view=next
	s.recompute()
	return nil
}
