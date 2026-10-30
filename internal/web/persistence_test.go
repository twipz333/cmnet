package web

import (
 "context"
 "path/filepath"
 "testing"

 "cmnet/internal/cache"
 "cmnet/internal/draft"
 "cmnet/internal/session"
)

func TestCheckpointPersistsAcrossRestart(t *testing.T) {
 store,err:=cache.Open(filepath.Join(t.TempDir(),"resume.db"))
 if err!=nil {t.Fatal(err)}
 defer store.Close()
 old:=testServer(t)
 old.localCache=store
 // Synthetic test bundles are unversioned; persistence requires a stable model identity.
 seeded:=old.sess.Config()
 seeded.BundleVersion="checkpoint-fixture-v1"
 old.sess=session.New(seeded)
 if err:=old.sess.Apply(1);err!=nil {t.Fatal(err)}
 if err:=old.saveCheckpoint();err!=nil {t.Fatal(err)}
 saved,found,err:=store.LatestDraftCheckpoint(context.Background())
 if err!=nil||!found||saved.Revision!=1 {t.Fatalf("checkpoint: %+v %v %v",saved,found,err)}
 cfg:=old.sess.Config()
 next:=NewForSession(session.New(cfg),"127.0.0.1:0")
 next.localCache=store
 next.restoreCheckpoint(context.Background())
 if !next.sess.DraftInProgress() {t.Fatal("did not resume draft")}
 if len(next.sess.View().Bans(draft.Radiant))!=1{t.Fatal("lost first action")}
 cfg.BundleVersion="other-model"
 wrong:=NewForSession(session.New(cfg),"127.0.0.1:0")
 wrong.localCache=store
 wrong.restoreCheckpoint(context.Background())
 if wrong.sess.DraftInProgress(){t.Fatal("restored with incompatible model")}
}
