package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"cmnet/internal/declog"
)

// newLogCmd reads the prospective decision log (§6.1) back out.
//
// A log nobody can read is a log nobody will keep. This is the offline half of
// the acceptance criterion — one played draft must produce a record that can be
// inspected without the app running — plus the two operations the privacy rules
// require: erase one player, and export a pseudonymised copy for teammates.
func newLogCmd() *cobra.Command {
	var (
		dbPath   string
		path     string
		asJSON   bool
		export   string
		forgetID int64
		purge    bool
		last     int
	)
	cmd := &cobra.Command{
		Use:   "log",
		Short: "Read the local decision log: what was recommended, what was chosen, and why not",
		Long: "The decision log records live drafts: the board, the recommendations shown, the " +
			"enemy prediction, what the captain took, and any reason given for going against " +
			"the advice.\n\n" +
			"It never leaves this machine. --export writes a copy with account ids replaced by " +
			"salted pseudonyms and names removed, for sharing; --forget erases one player from " +
			"it entirely.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if path == "" {
				path = declog.PathFor(dbPath)
			}
			switch {
			case forgetID != 0:
				if purge {
					n, err := declog.Purge(path, forgetID)
					if err != nil {
						return err
					}
					cmd.Printf("deleted %d logged drafts involving account %d\n", n, forgetID)
					return nil
				}
				n, err := declog.ForgetIn(path, forgetID)
				if err != nil {
					return err
				}
				cmd.Printf("account %d redacted from %d logged drafts (ids, names, profiles and "+
					"match ids removed; free-text reasons are left as written — use --purge to "+
					"delete those drafts outright)\n", forgetID, n)
				return nil
			case export != "":
				data, err := declog.Export(path)
				if err != nil {
					return err
				}
				if err := os.WriteFile(export, data, 0o600); err != nil {
					return err
				}
				cmd.Printf("exported pseudonymised log to %s\n", export)
				return nil
			}

			drafts, err := declog.Load(path)
			if err != nil {
				return err
			}
			if len(drafts) == 0 {
				cmd.Printf("no drafts logged yet (%s)\n", path)
				return nil
			}
			if last > 0 && len(drafts) > last {
				drafts = drafts[len(drafts)-last:]
			}
			if asJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(drafts)
			}
			cmd.Print(formatLog(drafts))
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&dbPath, "db", "cmnet.db", "cache path; the log lives beside it")
	f.StringVar(&path, "file", "", "read this log file instead of the one next to --db")
	f.BoolVar(&asJSON, "json", false, "dump the assembled drafts as JSON")
	f.StringVar(&export, "export", "", "write a pseudonymised copy to this path and exit")
	f.Int64Var(&forgetID, "forget", 0, "erase this account id from the log and exit")
	f.BoolVar(&purge, "purge", false, "with --forget: delete the whole drafts they appear in, not just their identifiers")
	f.IntVar(&last, "last", 0, "show only the newest N drafts (0 = all)")
	return cmd
}

// formatLog renders the log for reading, leading with the numbers that say
// whether the advice is being used: how often the top recommendation was taken,
// and how often the captain went somewhere we never suggested.
func formatLog(drafts []declog.Draft) string {
	var b strings.Builder
	var total, tookTop, offList, withReason int

	for _, d := range drafts {
		when := time.Unix(d.TS, 0).Format("2006-01-02 15:04")
		outcome := "—"
		if d.Outcome != nil {
			outcome = "loss"
			if d.Outcome.Win {
				outcome = "win"
			}
		}
		fmt.Fprintf(&b, "\n%s  %s  our side: %s, first pick: %s, source: %s, outcome: %s\n",
			when, d.ID, orDash(d.Header.OurTeam), orDash(d.Header.FirstPick), orDash(d.Header.Source), outcome)
		if n := len(d.Header.Enemies); n > 0 {
			fmt.Fprintf(&b, "  opponents profiled: %d, allies: %d, snapshot: %s\n",
				n, len(d.Header.Allies), shortHash(d.Header.SnapshotHash))
		}
		for _, dec := range d.Decisions {
			total++
			mark := "·"
			switch dec.ChosenRank {
			case 1:
				tookTop++
				mark = "="
			case 0:
				offList++
				mark = "!"
			}
			top := 0
			if len(dec.Recs) > 0 {
				top = dec.Recs[0].HeroID
			}
			fmt.Fprintf(&b, "  %s step %2d %-4s chose %3d, top %3d, rank %d",
				mark, dec.Seq, dec.Action, dec.Chosen, top, dec.ChosenRank)
			if dec.Strength != nil {
				fmt.Fprintf(&b, ", board %.3f", *dec.Strength)
			}
			b.WriteByte('\n')
			if dec.Reason != "" {
				withReason++
				fmt.Fprintf(&b, "      why not: %s\n", dec.Reason)
			}
		}
	}

	fmt.Fprintf(&b, "\n%d drafts, %d logged decisions\n", len(drafts), total)
	if total > 0 {
		fmt.Fprintf(&b, "  took the top recommendation: %d (%.0f%%)\n", tookTop, 100*float64(tookTop)/float64(total))
		fmt.Fprintf(&b, "  chose something not shown:   %d (%.0f%%)\n", offList, 100*float64(offList)/float64(total))
		fmt.Fprintf(&b, "  reasons recorded:            %d\n", withReason)
		if withReason == 0 {
			b.WriteString("  (reasons are the point of this log — without them a refusal says only that the\n" +
				"   advice was not taken, not which feature the model is missing)\n")
		}
	}
	return b.String()
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func shortHash(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	if s == "" {
		return "—"
	}
	return s
}
