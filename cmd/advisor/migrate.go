package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"cmnet/internal/datamigrate"
)

func newMigrateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Electron→native data backup, copy, and rollback",
		Long: "Detect Electron vs native data folders, create a timestamped consistent " +
			"SQLite backup (VACUUM INTO), copy local artifacts without inventing data, and " +
			"restore from backup. Refuses when cmnet.db is locked by Electron or another " +
			"advisor — quit the other client first.\n\n" +
			"Typical path:\n" +
			"  1. Close Electron\n" +
			"  2. advisor migrate status\n" +
			"  3. advisor migrate run --update-desktop\n" +
			"  4. Open native WPF\n" +
			"  5. On trouble: advisor migrate rollback --backup <path>",
	}
	cmd.AddCommand(
		newMigrateStatusCmd(),
		newMigrateBackupCmd(),
		newMigrateRunCmd(),
		newMigrateRollbackCmd(),
	)
	return cmd
}

func migrateOptsFromFlags(from, to string) datamigrate.Options {
	return datamigrate.Options{Source: from, Dest: to}
}

func newMigrateStatusCmd() *cobra.Command {
	var from, to string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show Electron vs native dirs, lock state, and backups",
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := datamigrate.Inspect(migrateOptsFromFlags(from, to))
			if err != nil {
				return err
			}
			if asJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(st)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "source (Electron/prior): %s\n", st.Source.Dir)
			fmt.Fprintf(cmd.OutOrStdout(), "  db=%v bundle=%v artifacts=%v\n", st.Source.HasDB, st.Source.HasBundle, st.Source.Artifacts)
			fmt.Fprintf(cmd.OutOrStdout(), "dest (native):           %s\n", st.Destination.Dir)
			fmt.Fprintf(cmd.OutOrStdout(), "  db=%v bundle=%v artifacts=%v\n", st.Destination.HasDB, st.Destination.HasBundle, st.Destination.Artifacts)
			if st.DesktopDir != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "desktop.json dataDir:    %s\n", st.DesktopDir)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "sameFolder=%v needsMigrate=%v dbLocked=%v\n", st.SameFolder, st.NeedsMigrate, st.DBLocked)
			if st.LockError != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "lock: %s\n", st.LockError)
			}
			if len(st.Backups) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "backups: (none)")
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "backups:")
				for _, b := range st.Backups {
					fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", b)
				}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "advice: %s\n", st.Advice)
			return nil
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "source data dir (default: desktop.json dataDir or %%APPDATA%%/cmnet/data)")
	cmd.Flags().StringVar(&to, "to", "", "native data dir (default: %%APPDATA%%/cmnet/data)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print machine-readable JSON")
	return cmd
}

func newMigrateBackupCmd() *cobra.Command {
	var from, to, stamp string
	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Create a timestamped backup only (no copy)",
		RunE: func(cmd *cobra.Command, args []string) error {
			opt := migrateOptsFromFlags(from, to)
			if err := datamigrate.ResolveDefaults(&opt); err != nil {
				return err
			}
			if stamp != "" {
				opt.Stamp = stamp
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Minute)
			defer cancel()
			parent := opt.Dest
			src := opt.Source
			if opt.Source == opt.Dest {
				parent = opt.Source
			}
			man, err := datamigrate.CreateBackup(ctx, src, parent, opt.Stamp, "manual")
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "backup ok: %s\n", datamigrate.BackupDir(parent, man.Stamp))
			fmt.Fprintf(cmd.OutOrStdout(), "files: %v\n", man.Files)
			fmt.Fprintf(cmd.OutOrStdout(), "rollback: advisor migrate rollback --backup %s --to %s\n",
				datamigrate.BackupDir(parent, man.Stamp), parent)
			return nil
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "folder to snapshot")
	cmd.Flags().StringVar(&to, "to", "", "parent for migration-backup/ (default: native data dir)")
	cmd.Flags().StringVar(&stamp, "stamp", "", "optional fixed stamp (tests)")
	return cmd
}

func newMigrateRunCmd() *cobra.Command {
	var from, to, stamp string
	var updateDesktop, skipAssets bool
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Backup then copy Electron artifacts into the native data dir",
		RunE: func(cmd *cobra.Command, args []string) error {
			opt := datamigrate.Options{
				Source:        from,
				Dest:          to,
				Stamp:         stamp,
				UpdateDesktop: updateDesktop,
				SkipAssets:    skipAssets,
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Minute)
			defer cancel()
			res, err := datamigrate.Run(ctx, opt)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), res.Message)
			fmt.Fprintf(cmd.OutOrStdout(), "copied: %v\n", res.Copied)
			backupParent := res.Dest
			if res.SameDir {
				backupParent = res.Source
			}
			fmt.Fprintf(cmd.OutOrStdout(), "backup: %s\n", datamigrate.BackupDir(backupParent, res.Backup.Stamp))
			return nil
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "Electron/source data dir")
	cmd.Flags().StringVar(&to, "to", "", "native destination data dir")
	cmd.Flags().StringVar(&stamp, "stamp", "", "optional fixed stamp (tests)")
	cmd.Flags().BoolVar(&updateDesktop, "update-desktop", false, "point %%APPDATA%%/cmnet/desktop.json dataDir at --to")
	cmd.Flags().BoolVar(&skipAssets, "skip-assets", false, "do not copy assets/ image cache")
	return cmd
}

func newMigrateRollbackCmd() *cobra.Command {
	var backup, to string
	cmd := &cobra.Command{
		Use:   "rollback",
		Short: "Restore a migration-backup folder into a data dir",
		RunE: func(cmd *cobra.Command, args []string) error {
			if backup == "" {
				return fmt.Errorf("нужен --backup путь к migration-backup/<stamp>")
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Minute)
			defer cancel()
			man, err := datamigrate.Rollback(ctx, backup, to)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "rollback ok → %s\n", man.Note)
			fmt.Fprintf(cmd.OutOrStdout(), "restored files: %v\n", man.Files)
			return nil
		},
	}
	cmd.Flags().StringVar(&backup, "backup", "", "path to migration-backup/<stamp>")
	cmd.Flags().StringVar(&to, "to", "", "destination data dir (default: manifest destDir)")
	return cmd
}
