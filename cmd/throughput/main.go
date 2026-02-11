package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

const defaultExclude = "dependabot[bot],renovate[bot]"

type config struct {
	owner      string
	repo       string
	branch     string
	weeks      int
	output     string
	excludeSet map[string]bool
	token      string
}

func main() {
	repoFlag := flag.String("repo", "", "owner/repo (default: detect from git remote)")
	branch := flag.String("branch", "main", "target branch")
	weeks := flag.Int("weeks", 12, "number of weeks to analyze")
	output := flag.String("output", "", "output CSV file (default: stdout)")
	exclude := flag.String("exclude", "", "additional usernames to exclude (comma-separated)")
	statsOutput := flag.String("stats-output", "", "output CSV file for statistical analysis (optional)")
	htmlOutput := flag.String("html", "", "output HTML file with interactive chart (optional)")
	serve := flag.Bool("serve", false, "start a local server to view the HTML chart (implies --html)")
	servePort := flag.Int("port", 8080, "port for the local server (used with --serve)")
	minPRs := flag.Int("min-prs", 0, "exclude weeks with fewer than N merged PRs (e.g. holiday weeks)")
	excludeBottomPct := flag.Int("exclude-bottom-contributor-pct", 0, "exclude bottom N% of contributors by total PR count (0-99)")
	flag.Parse()

	// --serve implies --html with a default filename
	if *serve && *htmlOutput == "" {
		defaultHTML := "chart.html"
		htmlOutput = &defaultHTML
	}

	cfg := config{
		branch: *branch,
		weeks:  *weeks,
		output: *output,
	}

	// Resolve owner/repo
	if *repoFlag != "" {
		cfg.owner, cfg.repo = parseRepo(*repoFlag)
	} else {
		cfg.owner, cfg.repo = detectRepo()
	}
	if cfg.owner == "" || cfg.repo == "" {
		fatal("Could not determine owner/repo. Use --repo owner/repo.")
	}

	// Build exclude set (case-insensitive)
	excludeList := defaultExclude
	if *exclude != "" {
		excludeList += "," + *exclude
	}
	cfg.excludeSet = make(map[string]bool)
	for _, u := range strings.Split(excludeList, ",") {
		u = strings.TrimSpace(u)
		if u != "" {
			cfg.excludeSet[strings.ToLower(u)] = true
		}
	}

	// Resolve token
	cfg.token = resolveToken()
	if cfg.token == "" {
		fatal("No GitHub token found. Tried: GH_TOKEN, GITHUB_TOKEN, git credential helper.")
	}

	fmt.Fprintf(os.Stderr, "Repository: %s/%s (branch: %s)\n", cfg.owner, cfg.repo, cfg.branch)

	// Compute week ranges
	now := time.Now()
	weekRanges := computeWeekRanges(now, cfg.weeks)

	startDate := weekRanges[0].start.Format("2006-01-02")
	today := now.Format("2006-01-02")
	fmt.Fprintf(os.Stderr, "Analyzing PRs merged from %s to %s (%d weeks)\n", startDate, today, cfg.weeks)
	fmt.Fprintf(os.Stderr, "Exclude list: %s\n", excludeList)

	// Fetch PRs concurrently
	fmt.Fprintf(os.Stderr, "Fetching merged PRs via GraphQL...\n")
	allPRs := fetchAllPRs(cfg, weekRanges)

	// Filter and compute metrics
	fmt.Fprintf(os.Stderr, "Processing PRs...\n")
	filtered := filterPRs(allPRs, cfg.excludeSet)
	fmt.Fprintf(os.Stderr, "Processed: %d PRs (%d excluded)\n", len(filtered), len(allPRs)-len(filtered))

	// Exclude bottom N% of contributors by total PR count
	if *excludeBottomPct > 0 && *excludeBottomPct < 100 {
		// Count PRs per author
		authorCounts := make(map[string]int)
		for _, pr := range filtered {
			authorCounts[pr.authorLogin]++
		}

		// Sort authors by PR count ascending
		type authorEntry struct {
			login string
			count int
		}
		authors := make([]authorEntry, 0, len(authorCounts))
		for login, count := range authorCounts {
			authors = append(authors, authorEntry{login, count})
		}
		sort.Slice(authors, func(i, j int) bool {
			return authors[i].count < authors[j].count
		})

		// Compute cutoff: bottom N% of authors by headcount
		cutoffIdx := len(authors) * *excludeBottomPct / 100
		if cutoffIdx > 0 {
			excludeSet := make(map[string]bool)
			for i := 0; i < cutoffIdx; i++ {
				excludeSet[authors[i].login] = true
			}
			// Also exclude anyone tied with the last excluded author
			thresholdCount := authors[cutoffIdx-1].count
			for i := cutoffIdx; i < len(authors); i++ {
				if authors[i].count <= thresholdCount {
					excludeSet[authors[i].login] = true
				} else {
					break
				}
			}

			// Log excluded authors
			var excluded []string
			for i := 0; i < len(authors) && excludeSet[authors[i].login]; i++ {
				excluded = append(excluded, fmt.Sprintf("%s (%d)", authors[i].login, authors[i].count))
			}
			fmt.Fprintf(os.Stderr, "Excluded %d bottom contributors (<=%d PRs): %s\n",
				len(excludeSet), thresholdCount, strings.Join(excluded, ", "))

			// Filter PRs
			var kept []enrichedPR
			for _, pr := range filtered {
				if !excludeSet[pr.authorLogin] {
					kept = append(kept, pr)
				}
			}
			fmt.Fprintf(os.Stderr, "After contributor filter: %d PRs (%d removed)\n", len(kept), len(filtered)-len(kept))
			filtered = kept
		}
	}

	// Aggregate and output CSV
	fmt.Fprintf(os.Stderr, "Aggregating by week...\n")
	csv, allWeekStats := aggregateCSV(filtered, weekRanges)

	// Filter out low-activity weeks (e.g. holidays)
	if *minPRs > 0 {
		var filteredRanges []weekRange
		var filteredStats []weekStats
		var filteredCSVLines []string
		csvLines := strings.Split(csv, "\n")
		// Keep header
		if len(csvLines) > 0 {
			filteredCSVLines = append(filteredCSVLines, csvLines[0])
		}
		var dropped int
		for i, ws := range allWeekStats {
			if ws.prsMerged >= *minPRs {
				filteredRanges = append(filteredRanges, weekRanges[i])
				filteredStats = append(filteredStats, ws)
				if i+1 < len(csvLines) {
					filteredCSVLines = append(filteredCSVLines, csvLines[i+1])
				}
			} else {
				dropped++
			}
		}
		if dropped > 0 {
			fmt.Fprintf(os.Stderr, "Excluded %d week(s) with fewer than %d PRs\n", dropped, *minPRs)
		}
		weekRanges = filteredRanges
		allWeekStats = filteredStats
		csv = strings.Join(filteredCSVLines, "\n")
		if !strings.HasSuffix(csv, "\n") {
			csv += "\n"
		}
	}

	if cfg.output != "" {
		if err := os.WriteFile(cfg.output, []byte(csv), 0644); err != nil {
			fatal("Failed to write output: %v", err)
		}
		fmt.Fprintf(os.Stderr, "CSV written to %s\n", cfg.output)
	} else {
		fmt.Print(csv)
	}

	// Statistical analysis (always compute if we have enough data, for HTML summary)
	fmt.Fprintf(os.Stderr, "Computing statistical analysis...\n")
	statsCSV, statsRows := generateStats(allWeekStats)

	if *statsOutput != "" {
		if statsCSV != "" {
			if err := os.WriteFile(*statsOutput, []byte(statsCSV), 0644); err != nil {
				fatal("Failed to write stats output: %v", err)
			}
			fmt.Fprintf(os.Stderr, "Stats CSV written to %s\n", *statsOutput)
		} else {
			fmt.Fprintf(os.Stderr, "No stats generated (insufficient data).\n")
		}
	}

	// HTML visualization (optional)
	if *htmlOutput != "" {
		fmt.Fprintf(os.Stderr, "Generating HTML chart...\n")
		title := fmt.Sprintf("%s/%s — %s to %s", cfg.owner, cfg.repo, startDate, today)
		htmlContent, err := generateHTML(title, weekRanges, allWeekStats, statsRows)
		if err != nil {
			fatal("Failed to generate HTML: %v", err)
		}
		if err := os.WriteFile(*htmlOutput, []byte(htmlContent), 0644); err != nil {
			fatal("Failed to write HTML output: %v", err)
		}
		fmt.Fprintf(os.Stderr, "HTML chart written to %s\n", *htmlOutput)
	}

	fmt.Fprintf(os.Stderr, "Done.\n")

	// Start local server (blocks forever)
	if *serve {
		serveHTML(*htmlOutput, *servePort)
	}
}

func parseRepo(s string) (string, string) {
	// Strip GitHub URL prefix and .git suffix
	s = strings.TrimPrefix(s, "https://github.com/")
	s = strings.TrimPrefix(s, "http://github.com/")
	s = strings.TrimSuffix(s, ".git")
	s = strings.TrimSuffix(s, "/")
	// Remove /tree/... suffix
	if idx := strings.Index(s, "/tree/"); idx != -1 {
		s = s[:idx]
	}
	parts := strings.SplitN(s, "/", 2)
	if len(parts) != 2 {
		return "", ""
	}
	return parts[0], parts[1]
}

func detectRepo() (string, string) {
	out, err := exec.Command("git", "remote", "get-url", "origin").Output()
	if err != nil {
		return "", ""
	}
	url := strings.TrimSpace(string(out))
	// Handle SSH URLs: git@github.com:owner/repo.git
	if strings.HasPrefix(url, "git@") {
		url = strings.TrimPrefix(url, "git@github.com:")
		url = strings.TrimSuffix(url, ".git")
		parts := strings.SplitN(url, "/", 2)
		if len(parts) == 2 {
			return parts[0], parts[1]
		}
		return "", ""
	}
	return parseRepo(url)
}

type weekRange struct {
	start time.Time
	end   time.Time
}

func computeWeekRanges(now time.Time, weeks int) []weekRange {
	// Find current Monday
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	daysSinceMonday := int(today.Weekday()+6) % 7 // Monday=0
	currentMonday := today.AddDate(0, 0, -daysSinceMonday)
	startDate := currentMonday.AddDate(0, 0, -7*weeks)

	ranges := make([]weekRange, weeks)
	for i := 0; i < weeks; i++ {
		ws := startDate.AddDate(0, 0, 7*i)
		we := ws.AddDate(0, 0, 6)
		ranges[i] = weekRange{start: ws, end: we}
	}
	return ranges
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "ERROR: "+format+"\n", args...)
	os.Exit(1)
}
