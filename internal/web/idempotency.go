package web

import (
 "bytes"
 "crypto/sha256"
 "io"
 "net/http"
 "strings"
)

// checkIdempotency compares request bodies too: reusing one key for a different
// command is a conflict, never a silent acceptance. Guarded by commandMu.
func (s *Server) checkIdempotency(w http.ResponseWriter,r *http.Request) (string,[32]byte,bool,bool) {
 key:=r.Header.Get("Idempotency-Key")
 if key=="" {return "",[32]byte{},false,true}
 if len(key)>128 || len(key)<8 || strings.TrimSpace(key)!=key {
  http.Error(w,"invalid idempotency key",http.StatusBadRequest)
  return "",[32]byte{},false,false
 }
 for _,c:=range key {
  if c < 33 || c > 126 {
   http.Error(w,"invalid idempotency key",http.StatusBadRequest)
   return "",[32]byte{},false,false
  }
 }
 body,err:=io.ReadAll(io.LimitReader(r.Body,(1<<20)+1))
 if err!=nil || len(body)>(1<<20) {
  http.Error(w,"request body too large or unreadable",http.StatusRequestEntityTooLarge)
  return "",[32]byte{},false,false
 }
 r.Body=io.NopCloser(bytes.NewReader(body))
 hashInput:=make([]byte,0,len(r.URL.Path)+1+len(body))
 hashInput=append(hashInput,r.URL.Path...)
 hashInput=append(hashInput,0)
 hashInput=append(hashInput,body...)
 fingerprint:=sha256.Sum256(hashInput)
 if before,ok:=s.receipts[key];ok {
  if before!=fingerprint {
   http.Error(w,"idempotency key reused for another command",http.StatusConflict)
   return "",[32]byte{},false,false
  }
  return key,fingerprint,true,true
 }
 return key,fingerprint,false,true
}

// Bounded in-memory replay window. The same-key POST is at most once while the
// Go process lives; persistence of request receipts is a separate guarantee.
func (s *Server) rememberIdempotency(key string,hash [32]byte) {
 if s.receipts==nil {s.receipts=make(map[string][32]byte)}
 if len(s.receiptOrder)>=512 {
  delete(s.receipts,s.receiptOrder[0])
  s.receiptOrder=s.receiptOrder[1:]
 }
 s.receipts[key]=hash
 s.receiptOrder=append(s.receiptOrder,key)
}
