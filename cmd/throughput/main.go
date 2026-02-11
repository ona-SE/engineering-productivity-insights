package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
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
	flag.Parse()

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

	// Aggregate and output CSV
	fmt.Fprintf(os.Stderr, "Aggregating by week...\n")
	csv := aggregateCSV(filtered, weekRanges)

	if cfg.output != "" {
		if err := os.WriteFile(cfg.output, []byte(csv), 0644); err != nil {
			fatal("Failed to write output: %v", err)
		}
		fmt.Fprintf(os.Stderr, "CSV written to %s\n", cfg.output)
	} else {
		fmt.Print(csv)
	}
	fmt.Fprintf(os.Stderr, "Done.\n")
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
