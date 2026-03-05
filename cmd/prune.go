package cmd

import (
	"fmt"
	"time"

	"memoryelaine/internal/config"
	"memoryelaine/internal/database"

	"github.com/spf13/cobra"
)

var (
	pruneKeepDays int
	pruneVacuum   bool
	pruneDryRun   bool
)

var pruneCmd = &cobra.Command{
	Use:   "prune",
	Short: "Delete old log entries",
	RunE:  runPrune,
}

func init() {
	pruneCmd.Flags().IntVar(&pruneKeepDays, "keep-days", 0, "Delete records older than this many days (required)")
	pruneCmd.Flags().BoolVar(&pruneVacuum, "vacuum", false, "Run VACUUM after deletion (may be slow)")
	pruneCmd.Flags().BoolVar(&pruneDryRun, "dry-run", false, "Print count without deleting")
	pruneCmd.MarkFlagRequired("keep-days")
	rootCmd.AddCommand(pruneCmd)
}

func runPrune(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return err
	}

	db, err := database.OpenWriter(cfg.Database.Path)
	if err != nil {
		return err
	}
	defer db.Close()

	cutoff := time.Now().AddDate(0, 0, -pruneKeepDays).UnixMilli()
	reader := database.NewLogReader(db)

	if pruneDryRun {
		since := int64(0)
		filter := database.QueryFilter{Limit: 1, Until: &cutoff, Since: &since}
		count, err := reader.Count(filter)
		if err != nil {
			return err
		}
		fmt.Printf("Would delete %d records older than %d days\n", count, pruneKeepDays)
		return nil
	}

	deleted, err := reader.DeleteBefore(cutoff)
	if err != nil {
		return err
	}
	fmt.Printf("Deleted %d records older than %d days\n", deleted, pruneKeepDays)

	if pruneVacuum {
		fmt.Println("Running VACUUM (this may take a while for large databases)...")
		if _, err := db.Exec("VACUUM"); err != nil {
			return fmt.Errorf("vacuum failed: %w", err)
		}
		fmt.Println("VACUUM complete")
	}

	return nil
}
