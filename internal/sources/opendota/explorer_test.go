package opendota

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// explorerServing builds a client pointed at a server that answers every request
// with body.
func explorerServing(t *testing.T, body string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	c := New()
	c.base = srv.URL
	return c
}

// An answer with no rows array is malformed, not empty. The distinction matters
// because the rank fill marks every id it asked about as looked up: reading a
// truncated 200 as "none of these exist" would retire those matches for good.
func TestExplorerRejectsAnAnswerWithoutARowsArray(t *testing.T) {
	for name, body := range map[string]string{
		"field absent": `{"command":"SELECT"}`,
		"rows null":    `{"rows":null}`,
		"rows object":  `{"rows":{}}`,
	} {
		t.Run(name, func(t *testing.T) {
			c := explorerServing(t, body)
			got, err := c.RankTiers(context.Background(), []int64{1, 2})
			if err == nil {
				t.Fatalf("RankTiers returned %v and no error; a body with no rows array "+
					"must not read as an empty result", got)
			}
		})
	}
}

// An empty array is a real answer: a match_id band with nothing in it is what the
// bulk scan expects to see, and it must not turn into an error.
func TestExplorerAcceptsAnEmptyRowsArray(t *testing.T) {
	c := explorerServing(t, `{"rows":[]}`)
	got, err := c.RankTiers(context.Background(), []int64{1, 2})
	if err != nil {
		t.Fatalf("empty result returned an error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %v, want no tiers", got)
	}
}

// A row whose tier is NULL carries no answer and must be dropped rather than
// decoded into a zero, which would read as a rank below Herald.
func TestRankTiersDropsRowsWithoutATier(t *testing.T) {
	c := explorerServing(t,
		`{"rows":[{"match_id":10,"avg_rank_tier":41},{"match_id":11,"avg_rank_tier":null}]}`)
	got, err := c.RankTiers(context.Background(), []int64{10, 11})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[10] != 41 {
		t.Errorf("got %v, want only match 10 at tier 41", got)
	}
}

// A server-side SQL error is an error, not an empty result.
func TestExplorerReportsASQLError(t *testing.T) {
	c := explorerServing(t, `{"err":"syntax error at or near \"selct\""}`)
	if _, err := c.RankTiers(context.Background(), []int64{1}); err == nil {
		t.Fatal("a rejected query returned no error")
	}
}

// No ids means no request at all: an empty IN () list is not valid SQL, and asking
// about nothing would spend a request to learn nothing.
func TestRankTiersWithNoIdsMakesNoRequest(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write([]byte(`{"rows":[]}`))
	}))
	defer srv.Close()
	c := New()
	c.base = srv.URL

	got, err := c.RankTiers(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
	if hits != 0 {
		t.Errorf("made %d requests for an empty id list, want none", hits)
	}
}
