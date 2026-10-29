package cache

import "testing"

func TestCachedSteamProfileRoundTrip(t *testing.T) {
	s := openTemp(t)
	want := Player{AccountID: 42, Persona: "old", AvatarURL: "https://avatars.steamstatic.com/old_full.jpg", FetchedAt: 100}
	if err := s.UpsertPlayers([]Player{want}); err != nil { t.Fatal(err) }
	got, err := s.Players([]int64{42})
	if err != nil { t.Fatal(err) }
	if got[42] != want { t.Fatalf("got %+v, want %+v", got[42], want) }

	fresh := Player{AccountID: 42, Persona: "new", AvatarURL: "https://avatars.steamstatic.com/new_full.jpg", FetchedAt: 200}
	if err := s.UpsertPlayers([]Player{fresh}); err != nil { t.Fatal(err) }
	got, err = s.Players([]int64{42})
	if err != nil { t.Fatal(err) }
	if got[42] != fresh { t.Fatalf("refresh got %+v, want %+v", got[42], fresh) }
}
