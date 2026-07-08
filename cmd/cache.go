package cmd

import (
	"fmt"
	"io"
	"strconv"
	"text/tabwriter"
	"time"
	"unicode"

	"github.com/deveshctl/layerx/image"
	"github.com/spf13/cobra"
)

// nowFn lets relativeTime tests freeze the clock without DI plumbing.
// Production code never overwrites it. Same shape as image/cache.go's
// nowFn; the two are independent — image/ deals in mtime math, cmd/ in
// human-readable rendering.
var nowFn = time.Now

var cacheCmd = &cobra.Command{
	Use:   "cache",
	Short: "Manage the layerx analysis cache",
	Long: `Manage the layerx analysis cache.

The cache stores parsed layer data per image digest under a per-user
directory. layerx self-prunes the cache by age (default 30 days) and
total size (default 1 GiB) at the end of every successful analyze; use
"layerx cache list" to inspect it and "layerx cache prune" to evict
entries explicitly.

Cache directory:
  By default, layerx writes the cache under your platform's user cache
  directory (e.g. ~/.cache/layerx on Linux, ~/Library/Caches/layerx on
  macOS, %LOCALAPPDATA%\layerx on Windows). Override with the
  LAYERX_CACHE_DIR environment variable.

  Auto-prune limits are configurable via LAYERX_CACHE_TTL_DAYS and
  LAYERX_CACHE_MAX_BYTES; set either to 0 to disable that limit.`,
}

var cacheListCmd = &cobra.Command{
	Use:   "list",
	Short: "List cached image digests with size and timestamp",
	Long: `List cached image digests with size and timestamp.

Reports the original image reference, size on disk, and the cached-at
time of every entry under the layerx cache directory, plus a totals
footer. Rows are ordered newest-first so a freshly-cached entry is at
the top of the table. The cached-at time is the file's modification
time, which is fixed when the cache entry is written; layerx does not
bump it on cache hits. The IMAGE column shows whatever reference was
passed to layerx when the entry was first written; entries from older
versions of layerx that pre-date this metadata appear as "<unknown>".

Cache directory: $LAYERX_CACHE_DIR overrides the default location.`,
	Example: `  # See what's in the cache
  layerx cache list`,
	Args: cobra.NoArgs,
	RunE: runCacheList,
}

var (
	flagCacheOlderThan string
	flagCacheAll       bool
	flagCacheDryRun    bool
)

var cachePruneCmd = &cobra.Command{
	Use:   "prune",
	Short: "Remove cached entries by age or in full",
	Long: `Remove cached entries by age or in full.

By default (no flags), prune is a DRY RUN — it prints what would be
removed and exits without touching disk. Use --older-than DURATION or
--all to actually remove entries.

Duration syntax accepts an integer followed by a unit:
  s   seconds
  m   minutes
  h   hours
  d   days
  w   weeks
Examples: 30d, 12h, 2w, 90m. "mo" and "y" are not accepted.

Cache directory: $LAYERX_CACHE_DIR overrides the default location.`,
	Example: `  # Show what would be evicted by a 7-day policy (no removal)
  layerx cache prune --older-than 7d --dry-run

  # Actually evict entries older than 7 days
  layerx cache prune --older-than 7d

  # Empty the cache
  layerx cache prune --all`,
	Args: cobra.NoArgs,
	RunE: runCachePrune,
}

func init() {
	cachePruneCmd.Flags().StringVar(&flagCacheOlderThan, "older-than", "",
		"remove entries older than DURATION (e.g. 7d, 12h, 2w)")
	cachePruneCmd.Flags().BoolVar(&flagCacheAll, "all", false,
		"remove every entry under the cache directory")
	cachePruneCmd.Flags().BoolVar(&flagCacheDryRun, "dry-run", false,
		"preview removals without deleting (implicit when no other flag is given)")
	cachePruneCmd.MarkFlagsMutuallyExclusive("older-than", "all")

	cacheCmd.AddCommand(cacheListCmd)
	cacheCmd.AddCommand(cachePruneCmd)
	rootCmd.AddCommand(cacheCmd)
}

func runCacheList(cmd *cobra.Command, _ []string) error {
	cmd.SilenceUsage = true
	root, _ := image.CacheDir()
	entries, warns, err := image.ListCache(root)
	if err != nil {
		return fmt.Errorf("cache list failed: %w", err)
	}
	for _, w := range warns {
		fmt.Fprintln(cmd.ErrOrStderr(), w)
	}
	renderListTable(cmd.OutOrStdout(), root, entries)
	return nil
}

func runCachePrune(cmd *cobra.Command, _ []string) error {
	cmd.SilenceUsage = true

	opts := image.PruneOptions{}
	bareDryRun := false
	switch {
	case flagCacheOlderThan != "":
		ttl, err := parseOlderThan(flagCacheOlderThan)
		if err != nil {
			return fmt.Errorf("invalid value %q for --older-than: %w", flagCacheOlderThan, err)
		}
		opts.TTL = ttl
		opts.DryRun = flagCacheDryRun
	case flagCacheAll:
		opts.All = true
		opts.DryRun = flagCacheDryRun
	default:
		opts.All = true
		opts.DryRun = true
		bareDryRun = true
	}

	root, _ := image.CacheDir()
	res, err := image.PruneCache(root, opts)
	if err != nil {
		return fmt.Errorf("cache prune failed: %w", err)
	}
	for _, w := range res.Warnings {
		fmt.Fprintln(cmd.ErrOrStderr(), w)
	}
	renderPruneResult(cmd.OutOrStdout(), res, opts.DryRun)

	// On a bare `prune` (no flags), nothing was actually removed. Hint at
	// the commands that would, so users don't think the dry-run did the job.
	if bareDryRun && len(res.Removed) > 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "\nThis was a dry run. To remove entries, use:")
		fmt.Fprintln(cmd.OutOrStdout(), "  layerx cache prune --all                      remove all entries")
		fmt.Fprintln(cmd.OutOrStdout(), "  layerx cache prune --older-than DURATION      remove entries older than e.g. 7d, 12h, 2w")
		fmt.Fprintln(cmd.OutOrStdout(), "  layerx cache prune --older-than 7d --dry-run  preview what --older-than would remove")
	}

	// Exit 1 only when the prune accomplished nothing AND a partial
	// failure was reported. If the cache was simply empty, exit 0.
	if !opts.DryRun && len(res.Removed) == 0 && len(res.Warnings) > 0 {
		// At least one Warning means at least one RemoveAll failed.
		return fmt.Errorf("cache prune failed to remove any entries")
	}
	return nil
}

// parseOlderThan accepts integer + single unit suffix:
//
//	s   seconds
//	m   minutes
//	h   hours
//	d   days
//	w   weeks
//
// Negative or zero values are rejected. "mo", "y", and any compound
// (e.g. "1d2h") are rejected; users running cache prune do not need
// calendar-month or leap-year math, and a single integer + single unit
// is unambiguous to test and document.
func parseOlderThan(s string) (time.Duration, error) {
	if s == "" {
		return 0, fmt.Errorf("empty duration")
	}
	i := 0
	for i < len(s) && (s[i] == '-' || s[i] == '+' || unicode.IsDigit(rune(s[i]))) {
		i++
	}
	if i == 0 {
		return 0, fmt.Errorf("missing leading number")
	}
	if i == len(s) {
		return 0, fmt.Errorf("missing unit; supported: s, m, h, d, w")
	}
	num, err := strconv.ParseInt(s[:i], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid number %q: %w", s[:i], err)
	}
	if num <= 0 {
		return 0, fmt.Errorf("must be positive")
	}
	unit := s[i:]
	var mult time.Duration
	switch unit {
	case "s":
		mult = time.Second
	case "m":
		mult = time.Minute
	case "h":
		mult = time.Hour
	case "d":
		mult = 24 * time.Hour
	case "w":
		mult = 7 * 24 * time.Hour
	default:
		return 0, fmt.Errorf("unsupported unit %q; supported: s, m, h, d, w", unit)
	}
	// Overflow check: time.Duration is int64 nanoseconds.
	if num > int64(time.Duration(1<<63-1)/mult) {
		return 0, fmt.Errorf("duration overflow")
	}
	return time.Duration(num) * mult, nil
}

// renderListTable writes the cache-list output to w. root is rendered in
// the header line so the user sees their resolved cache directory; pass
// the empty string to suppress the header (tests use this).
func renderListTable(w io.Writer, root string, entries []image.CacheEntry) {
	if root != "" {
		fmt.Fprintf(w, "Cache directory: %s\n\n", root)
	}
	if len(entries) == 0 {
		fmt.Fprintln(w, "Total: 0 entries, 0 B")
		return
	}
	// CACHED — column is mtime (write-time), not last-read. I-03
	// rejected bumping mtime on cache hits because of the read-path
	// I/O cost; we kept that decision and label honestly.
	//
	// ListCache returns oldest-first (the order PruneCache needs for
	// stale-eviction). For human display, walk in reverse so the most
	// recently cached entry — usually the one the user just created —
	// is at the top of the table without scrolling.
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "IMAGE\tDIGEST\tSIZE\tCACHED")
	var total int64
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		ref := e.ImageRef
		if ref == "" {
			// Entry was written by an older layerx version that didn't
			// persist the meta sidecar, or the sidecar was hand-deleted.
			// The digest still uniquely identifies the image — only the
			// human-readable label is missing.
			ref = "<unknown>"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
			ref,
			truncateDigest(e.Digest),
			image.FormatBytes(e.Size),
			relativeTime(e.CachedAt))
		total += e.Size
	}
	// tw is a tabwriter; Flush writes the column-aligned output. A failed
	// write to the underlying terminal stream has no useful recovery (we
	// were already writing to it), so the error is intentionally dropped.
	_ = tw.Flush()
	fmt.Fprintf(w, "\nTotal: %d entries, %s\n", len(entries), image.FormatBytes(total))
}

func renderPruneResult(w io.Writer, res image.PruneResult, dryRun bool) {
	if len(res.Removed) == 0 {
		fmt.Fprintln(w, "Nothing to prune.")
		return
	}
	verb := "Removed"
	footerVerb := "Removed"
	freedWord := "freed"
	if dryRun {
		verb = "Would remove"
		footerVerb = "Would remove"
		freedWord = "freeing"
	}
	var total int64
	for _, e := range res.Removed {
		fmt.Fprintf(w, "%s %s (%s)\n", verb, truncateDigest(e.Digest), image.FormatBytes(e.Size))
		total += e.Size
	}
	fmt.Fprintf(w, "%s %d entries, %s %s\n", footerVerb, len(res.Removed), freedWord, image.FormatBytes(total))
}

// truncateDigest returns the first 12 chars of d plus a horizontal
// ellipsis. Full digests are not useful at a glance; users who need
// the full string can ls the cache dir.
func truncateDigest(d string) string {
	if len(d) <= 12 {
		return d
	}
	return d[:12] + "…"
}

// relativeTime returns "just now", "5 minutes ago", "3 hours ago",
// "2 days ago", "3 weeks ago", "5 months ago", or "2 years ago".
// Months are 30 days, years are 365 days — approximations are fine
// for cache hygiene; we are not doing calendar math.
func relativeTime(t time.Time) string {
	d := nowFn().Sub(t)
	switch {
	case d < 60*time.Second:
		return "just now"
	case d < time.Hour:
		return pluralize(int(d/time.Minute), "minute")
	case d < 24*time.Hour:
		return pluralize(int(d/time.Hour), "hour")
	case d < 7*24*time.Hour:
		return pluralize(int(d/(24*time.Hour)), "day")
	case d < 30*24*time.Hour:
		return pluralize(int(d/(7*24*time.Hour)), "week")
	case d < 365*24*time.Hour:
		return pluralize(int(d/(30*24*time.Hour)), "month")
	default:
		return pluralize(int(d/(365*24*time.Hour)), "year")
	}
}

func pluralize(n int, unit string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s ago", unit)
	}
	return fmt.Sprintf("%d %ss ago", n, unit)
}
