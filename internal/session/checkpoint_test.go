package session

import (
 "reflect"
 "testing"

 "cmnet/internal/draft"
)

func TestDraftCheckpointReplayAndValidation(t *testing.T) {
 cfg:=Config{Heroes:[]Hero{{ID:1},{ID:2},{ID:3}},
  OurTeam:draft.Radiant,FirstPick:draft.Radiant,
  BundleVersion:"pinned",Rec:testRecommender(t)}
 first:=New(cfg)
 if first.DraftInProgress(){t.Fatal("fresh draft is active")}
 if err:=first.Apply(1);err!=nil {t.Fatal(err)}
 if _,err:=first.ToggleStepAction();err!=nil {t.Fatal(err)}
 if err:=first.Apply(2);err!=nil {t.Fatal(err)}
 saved,ok:=first.ExportDraft()
 if !ok {t.Fatal("export failed")}
 after:=New(cfg)
 if err:=after.RestoreDraft(saved);err!=nil {t.Fatal(err)}
 if !after.DraftInProgress(){t.Fatal("restored draft is inactive")}
 for _,team:=range []draft.Team{draft.Radiant,draft.Dire} {
  if !reflect.DeepEqual(after.View().Bans(team),first.View().Bans(team)) ||
    !reflect.DeepEqual(after.View().Picks(team),first.View().Picks(team)) {
   t.Fatalf("board changed on replay, team=%v",team)
  }
 }
 invalid:=saved
 invalid.Heroes=append([]int{},saved.Heroes...)
 invalid.Heroes[0]=999999
 if err:=after.RestoreDraft(invalid);err==nil {t.Fatal("invalid hero accepted")}
 if !reflect.DeepEqual(after.View().Bans(draft.Radiant),first.View().Bans(draft.Radiant)){
  t.Fatal("invalid replay modified live board")
 }
 if err:=after.NewDraft();err!=nil {t.Fatal(err)}
 if after.DraftInProgress(){t.Fatal("new draft retains active flag")}
}
