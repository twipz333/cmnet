package main

import (
 "context"
 "encoding/json"
 "fmt"
 "time"

 "cmnet/internal/cache"
 "cmnet/internal/scout"
)

// Store source outcomes independent of model predictions. A network error
// is not evidence that a Steam profile is private.
func recordScoutEvidence(path string, layer *scout.Layer2, side string, progress logFunc) {
 if layer == nil || len(layer.Players)==0 { return }
 store,err:=cache.Open(path)
 if err!=nil { progress("observation cache: "+err.Error());return }
 defer store.Close()
 ctx,cancel:=context.WithTimeout(context.Background(),3*time.Second)
 defer cancel()
 for _,p:=range layer.Players {
  if p.AccountID<=0 { continue }
  status:="error"
  switch p.Status {
  case "ok":status="fresh"
  case "no_stats":status="partial"
  case "stale":status="stale"
  case "unresolved":status="unavailable"
  }
  evidence,err:=json.Marshal(map[string]any{
   "windowDays":layer.WindowDays,"heroCount":len(p.Heroes),
   "side":side,"sourceStatus":p.Status,
  })
  if err!=nil {progress(fmt.Sprintf("observation %d: %v",p.AccountID,err));continue}
  err=store.RecordObservation(ctx,cache.SourceObservation{
   AccountID:p.AccountID,Provider:"opendota",Status:status,Evidence:evidence,
  })
  if err!=nil {progress(fmt.Sprintf("observation %d: %v",p.AccountID,err))}
 }
}
