package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var cfgFile string

var rootCmd = &cobra.Command{
	Use:   "memoryelaine",
	Short: "High-performance logging proxy for LLM inference APIs",
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: ./config.yaml)")
}

func Execute() error {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		return err
	}
	return nil
}
