package recommender

import "cmnet/internal/draft"

// openingBans is the length of the first ban phase of the built-in schedule;
// applying that many dummy heroes lands the board on the first-pick team's
// first pick.
var openingBans = draft.DefaultOrder().Phases()[0]
