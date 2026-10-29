package main

import (
	"testing"

	"cmnet/internal/scout"
)

func TestSteamAvatarURLAllowlist(t *testing.T) {
	cases := []struct{ url string; allow bool }{
		{"https://avatars.steamstatic.com/a_full.jpg", true},
		{"https://steamcdn-a.akamaihd.net/steamcommunity/public/images/avatars/a.jpg", true},
		{"https://steamstatic.com.evil.test/a.jpg", false},
		{"http://avatars.steamstatic.com/a.jpg", false},
		{"https://localhost/admin", false},
		{"https://127.0.0.1/a.jpg", false},
		{"file:///etc/passwd", false},
		{"https://steamstatic.com:8080/a.jpg", false},
	}
	for _, tc := range cases {
		got := safeSteamAvatarURL(tc.url)
		if (got != "") != tc.allow { t.Errorf("safeSteamAvatarURL(%q)=%q, allowed=%v", tc.url, got, tc.allow) }
	}
}

func TestMergeScoutRetainsSuccessfulDataOnFailedRefresh(t *testing.T) {
	old := &scout.Layer2{WindowDays: 30, Players: []scout.PlayerAffinity{
		{AccountID: 42, Resolved: true, Status: "ok", Heroes: []scout.HeroAffinity{{HeroID: 1, Games: 12}}},
	}}
	failed := &scout.Layer2{WindowDays: 30, Players: []scout.PlayerAffinity{
		{AccountID: 42, Resolved: true, Status: "error"},
	}}
	merged := mergeLayer2(old, failed)
	if len(merged.Players) != 1 || len(merged.Players[0].Heroes) != 1 ||
		merged.Players[0].Status != "stale" {
		t.Fatalf("failed refresh discarded cached scouting: %+v", merged.Players)
	}

	updated := &scout.Layer2{WindowDays: 30, Players: []scout.PlayerAffinity{
		{AccountID: 42, Resolved: true, Status: "ok", Heroes: []scout.HeroAffinity{{HeroID: 3, Games: 5}}},
	}}
	merged = mergeLayer2(merged, updated)
	if merged.Players[0].Status != "ok" || merged.Players[0].Heroes[0].HeroID != 3 {
		t.Fatalf("successful refresh did not replace stale stats: %+v", merged.Players[0])
	}
}
