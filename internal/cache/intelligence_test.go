package cache

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestProvenanceAndRevisionedCheckpointRoundTrip(t *testing.T) {
	s,err:=Open(filepath.Join(t.TempDir(),"cmnet.db"))
	if err!=nil {t.Fatal(err)}
	defer s.Close()
	ctx:=context.Background()
	if err:=s.RecordObservation(ctx,SourceObservation{AccountID:42,Provider:"opendota",Status:"rate_limited",Evidence:json.RawMessage(`{"httpStatus":429}`)});err!=nil {t.Fatal(err)}
	obs,found,err:=s.LatestObservation(ctx,42,"opendota")
	if err!=nil || !found || obs.Status!="rate_limited" {t.Fatalf("provenance mismatch: %+v %v %v",obs,found,err)}
	if err:=s.RecordObservation(ctx,SourceObservation{AccountID:42,Provider:"opendota",Status:"not-really-private"});err==nil {t.Fatal("accepted invalid status")}
	a:=DraftCheckpoint{ID:"draft-a",ModelVersion:"bundle-v1",Revision:10,State:json.RawMessage(`{"board":[1,2]}`)}
	if err:=s.SaveDraftCheckpoint(ctx,a);err!=nil {t.Fatal(err)}
	a.Revision=9
	a.State=json.RawMessage(`{"board":[]}`)
	if err:=s.SaveDraftCheckpoint(ctx,a);err!=nil {t.Fatal(err)}
	got,found,err:=s.LoadDraftCheckpoint(ctx,"draft-a")
	if err!=nil||!found||got.Revision!=10||string(got.State)!=`{"board":[1,2]}` {
		t.Fatalf("stale overwrite: %+v %v %v",got,found,err)
	}
}

func TestBackupIncludesUncheckpointedWALRows(t *testing.T) {
	root:=t.TempDir()
	s,err:=Open(filepath.Join(root,"cmnet.db"))
	if err!=nil {t.Fatal(err)}
	defer s.Close()
	if err:=s.SetMeta("latest","visible");err!=nil {t.Fatal(err)}
	backup:=filepath.Join(root,"snapshot","cmnet-backup.db")
	if err:=s.Backup(context.Background(),backup);err!=nil {t.Fatal(err)}
	if _,err:=os.Stat(backup);err!=nil {t.Fatal(err)}
	copy,err:=Open(backup)
	if err!=nil {t.Fatal(err)}
	defer copy.Close()
	v,ok,err:=copy.GetMeta("latest")
	if err!=nil||!ok||v!="visible" {t.Fatalf("missing WAL row from backup: %q %v %v",v,ok,err)}
	if err:=s.Backup(context.Background(),backup);err==nil {t.Fatal("backup silently overwrote an existing file")}
}
