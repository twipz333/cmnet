package scout

import "testing"

func TestParseAccountID(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"123456789", 123456789},
		{"  123456789 ", 123456789},
		{"https://www.opendota.com/players/123456789", 123456789},
		{"https://www.opendota.com/players/123456789/matches", 123456789},
		{"https://stratz.com/players/123456789", 123456789},
		{"https://www.dotabuff.com/players/123456789", 123456789},
		{"[U:1:200000]", 200000},
		{"U:1:200000", 200000},
	}
	for _, c := range cases {
		got, err := ParseAccountID(c.in)
		if err != nil {
			t.Errorf("ParseAccountID(%q) error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseAccountID(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestParseAccountIDSteamID64Conversion(t *testing.T) {
	// 76561198083722517 = base + 123456789
	got, err := ParseAccountID("76561198083722517")
	if err != nil {
		t.Fatal(err)
	}
	if got != 123456789 {
		t.Errorf("SteamID64 conversion = %d, want 123456789", got)
	}
}

func TestParseAccountIDSteamCommunityProfile(t *testing.T) {
	got, err := ParseAccountID("https://steamcommunity.com/profiles/76561198083722517")
	if err != nil {
		t.Fatal(err)
	}
	if got != 123456789 {
		t.Errorf("steam profile = %d, want 123456789", got)
	}
}

func TestParseAccountIDErrors(t *testing.T) {
	for _, in := range []string{"", "garbage", "https://opendota.com/players/", "0"} {
		if _, err := ParseAccountID(in); err == nil {
			t.Errorf("ParseAccountID(%q): expected error", in)
		}
	}
}

func TestParsePlayers(t *testing.T) {
	ids, errs := ParsePlayers([]string{"123", "garbage"})
	if len(ids) != 2 || len(errs) != 2 {
		t.Fatalf("unexpected lengths: %d %d", len(ids), len(errs))
	}
	if ids[0] != 123 || errs[0] != nil {
		t.Errorf("first: id=%d err=%v", ids[0], errs[0])
	}
	if errs[1] == nil {
		t.Error("second should error")
	}
}
