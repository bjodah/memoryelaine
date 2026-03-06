package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"memoryelaine/internal/config"
	"memoryelaine/internal/database"

	"github.com/spf13/cobra"
)

var (
	logFormat string
	logLimit  int
	logOffset int
	logStatus int
	logPath   string
	logSince  string
	logUntil  string
	logQuery  string
	logID     int64
)

var logCmd = &cobra.Command{
	Use:   "log",
	Short: "Query proxy logs",
	RunE:  runLog,
}

func init() {
	logCmd.Flags().StringVarP(&logFormat, "format", "f", "json", "Output format: json, jsonl, table")
	logCmd.Flags().IntVarP(&logLimit, "limit", "n", 20, "Number of records")
	logCmd.Flags().IntVar(&logOffset, "offset", 0, "Pagination offset")
	logCmd.Flags().IntVar(&logStatus, "status", 0, "Filter by status code")
	logCmd.Flags().StringVar(&logPath, "path", "", "Filter by request path")
	logCmd.Flags().StringVar(&logSince, "since", "", "Since (ISO 8601 or relative: 1h, 30m, 7d)")
	logCmd.Flags().StringVar(&logUntil, "until", "", "Until (ISO 8601 or relative)")
	logCmd.Flags().StringVarP(&logQuery, "query", "q", "", "Search body content")
	logCmd.Flags().Int64Var(&logID, "id", 0, "Show single record by ID")
	rootCmd.AddCommand(logCmd)
}

func runLog(cmd *cobra.Command, args []string) (err error) {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return err
	}

	db, err := database.OpenReader(cfg.Database.Path)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("closing database: %w", closeErr)
		}
	}()

	reader := database.NewLogReader(db)

	if logID > 0 {
		entry, err := reader.GetByID(logID)
		if err != nil {
			return fmt.Errorf("entry not found: %w", err)
		}
		return outputEntries([]database.LogEntry{*entry})
	}

	filter := database.DefaultQueryFilter()
	filter.Limit = logLimit
	filter.Offset = logOffset

	if logStatus > 0 {
		filter.StatusCode = &logStatus
	}
	if logPath != "" {
		filter.Path = &logPath
	}
	if logSince != "" {
		ts, err := parseTimeArg(logSince)
		if err != nil {
			return fmt.Errorf("invalid --since: %w", err)
		}
		filter.Since = &ts
	}
	if logUntil != "" {
		ts, err := parseTimeArg(logUntil)
		if err != nil {
			return fmt.Errorf("invalid --until: %w", err)
		}
		filter.Until = &ts
	}
	if logQuery != "" {
		filter.Search = &logQuery
	}

	entries, err := reader.Query(filter)
	if err != nil {
		return err
	}

	return outputEntries(entries)
}

func outputEntries(entries []database.LogEntry) error {
	switch logFormat {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(entries)
	case "jsonl":
		enc := json.NewEncoder(os.Stdout)
		for _, e := range entries {
			if err := enc.Encode(e); err != nil {
				return err
			}
		}
		return nil
	case "table":
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		if _, err := fmt.Fprintln(w, "ID\tTIME\tMETHOD\tPATH\tSTATUS\tDURATION\tREQ SIZE\tRESP SIZE"); err != nil {
			return err
		}
		for _, e := range entries {
			status := "—"
			if e.StatusCode != nil {
				status = strconv.Itoa(*e.StatusCode)
			}
			dur := "—"
			if e.DurationMs != nil {
				dur = fmt.Sprintf("%dms", *e.DurationMs)
			}
			t := time.UnixMilli(e.TsStart).Format("15:04:05")
			if _, err := fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\t%d\t%d\n",
				e.ID, t, e.RequestMethod, e.RequestPath,
				status, dur, e.ReqBytes, e.RespBytes); err != nil {
				return err
			}
		}
		return w.Flush()
	default:
		return fmt.Errorf("unknown format %q", logFormat)
	}
}

// parseTimeArg parses ISO 8601 or relative durations like "1h", "30m", "7d".
func parseTimeArg(s string) (int64, error) {
	// Try ISO 8601
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UnixMilli(), nil
	}

	// Try relative duration
	s = strings.TrimSpace(s)
	if len(s) < 2 {
		return 0, fmt.Errorf("invalid time %q", s)
	}
	unit := s[len(s)-1]
	numStr := s[:len(s)-1]
	num, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid time %q", s)
	}

	var d time.Duration
	switch unit {
	case 's':
		d = time.Duration(num * float64(time.Second))
	case 'm':
		d = time.Duration(num * float64(time.Minute))
	case 'h':
		d = time.Duration(num * float64(time.Hour))
	case 'd':
		d = time.Duration(num * 24 * float64(time.Hour))
	default:
		return 0, fmt.Errorf("unknown time unit %q", string(unit))
	}

	return time.Now().Add(-d).UnixMilli(), nil
}
