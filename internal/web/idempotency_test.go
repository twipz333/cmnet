package web

import (
 "net/http"
 "testing"
)

func TestPostIdempotencyRejectsReplayedOrConflictingCommands(t *testing.T) {
 s:=testServer(t)
 do(t,s,http.MethodPost,"/api/apply",`{"heroId":1}`,nil)
 do(t,s,http.MethodPost,"/api/apply",`{"heroId":2}`,nil)
 headers:=map[string]string{"Idempotency-Key":"replay-undo-0001"}
 first:=do(t,s,http.MethodPost,"/api/v1/undo","",headers)
 if first.Code!=http.StatusOK {t.Fatal(first.Code,first.Body.String())}
 duplicate:=do(t,s,http.MethodPost,"/api/v1/undo","",headers)
 if duplicate.Code!=http.StatusOK {t.Fatal(duplicate.Code,duplicate.Body.String())}
 current:=decodeState(t,do(t,s,http.MethodGet,"/api/state","",nil))
 if len(current.Board.RadiantBans)!=1 {t.Fatalf("duplicate command undid twice: %+v",current.Board)}
 applied:=do(t,s,http.MethodPost,"/api/v1/apply",`{"heroId":2}`,headers)
 if applied.Code!=http.StatusConflict {t.Fatalf("key reused across commands: got %d",applied.Code)}
 current=decodeState(t,do(t,s,http.MethodGet,"/api/state","",nil))
 if len(current.Board.RadiantBans)!=1 {t.Fatal("conflicting request mutated board")}
}
