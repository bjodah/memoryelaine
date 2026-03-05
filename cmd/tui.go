package cmd

import (
	"memoryelaine/internal/config"
	"memoryelaine/internal/database"
	"memoryelaine/internal/tui"

	"github.com/spf13/cobra"
)

var tuiCmd = &cobra.Command{
	Use:   "tui",
	Short: "Interactive terminal UI for browsing logs",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(cfgFile)
		if err != nil {
			return err
		}
		db, err := database.OpenReader(cfg.Database.Path)
		if err != nil {
			return err
		}
		defer db.Close()
		return tui.Run(database.NewLogReader(db))
	},
}

func init() {
	rootCmd.AddCommand(tuiCmd)
}
