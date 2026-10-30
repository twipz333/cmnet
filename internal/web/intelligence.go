package web

import (
 "net/http"
 "strconv"
 "time"
)

// Cache-first evidence endpoint: absence of evidence is never treated as proof
// that a source account is private. The server owns all network restrictions.
func (s *Server) handleIntelligenceObservation(w http.ResponseWriter,r *http.Request) {
 if r.Method!=http.MethodGet {
  w.Header().Set("Allow","GET")
  http.Error(w,"method not allowed",http.StatusMethodNotAllowed)
  return
 }
 id,err:=strconv.ParseInt(r.URL.Query().Get("accountId"),10,64)
 if err!=nil||id<=0 {http.Error(w,"invalid accountId",http.StatusBadRequest);return}
 provider:=r.URL.Query().Get("provider")
 if provider=="" {provider="opendota"}
 if provider!="opendota" {http.Error(w,"unsupported provider",http.StatusBadRequest);return}
 s.mu.Lock()
 store:=s.localCache
 s.mu.Unlock()
 if store==nil {http.Error(w,"local cache unavailable",http.StatusServiceUnavailable);return}
 result,found,err:=store.LatestObservation(r.Context(),id,provider)
 if err!=nil {http.Error(w,"cache read failed",http.StatusInternalServerError);return}
 if !found {
  writeJSON(w,http.StatusOK,map[string]any{
   "schemaVersion":1,"accountId":id,"provider":provider,
   "status":"unavailable","observedAt":nil,"evidence":nil,
  })
  return
 }
 age:=time.Since(time.Unix(result.ObservedAt,0))
 status:=result.Status
 if status=="fresh"&&age>24*time.Hour {status="stale"}
 writeJSON(w,http.StatusOK,map[string]any{
  "schemaVersion":1,"accountId":result.AccountID,"provider":result.Provider,
  "status":status,"observedAt":result.ObservedAt,
  "ageSeconds":max(int64(0),int64(age.Seconds())),
  "evidence":result.Evidence,
 })
}
