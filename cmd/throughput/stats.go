package main

import (
	"fmt"
	"math"
	"os"
)

// --- Metric definitions ---

// metricDef defines how to extract a metric from weekly data.
type metricDef struct {
	name    string
	extract func(ws weekStats) float64
	valid   func(ws weekStats) bool
}

// allMetrics defines the rows in the consolidated stats CSV.
var allMetrics = []metricDef{
	{
		name:    "prs_merged",
		extract: func(ws weekStats) float64 { return float64(ws.prsMerged) },
		valid:   func(ws weekStats) bool { return ws.prsMerged > 0 },
	},
	{
		name:    "unique_authors",
		extract: func(ws weekStats) float64 { return float64(ws.uniqueAuthors) },
		valid:   func(ws weekStats) bool { return ws.prsMerged > 0 },
	},
	{
		name:    "prs_per_engineer",
		extract: func(ws weekStats) float64 { return ws.prsPerEngineer },
		valid:   func(ws weekStats) bool { return ws.prsMerged > 0 },
	},
	{
		name:    "commits_per_engineer",
		extract: func(ws weekStats) float64 { return ws.commitsPerEngineer },
		valid:   func(ws weekStats) bool { return ws.prsMerged > 0 },
	},
	{
		name:    "pct_reverts",
		extract: func(ws weekStats) float64 { return ws.pctReverts },
		valid:   func(ws weekStats) bool { return ws.prsMerged > 0 },
	},
	{
		name:    "pct_ona_involved",
		extract: func(ws weekStats) float64 { return ws.pctOnaInvolved },
		valid:   func(ws weekStats) bool { return ws.prsMerged > 0 },
	},
	{
		name:    "build_runs",
		extract: func(ws weekStats) float64 { return float64(ws.buildRuns) },
		valid:   func(ws weekStats) bool { return ws.buildRuns > 0 },
	},
	{
		name:    "build_success_pct",
		extract: func(ws weekStats) float64 { return ws.buildSuccessPct },
		valid:   func(ws weekStats) bool { return ws.buildRuns > 0 },
	},
}

// --- Consolidated stats row ---

type consolidatedRow struct {
	metric          string
	n               int
	windowSize      int // for positional windows (same on both sides)
	firstWindowSize int // for threshold windows (may differ)
	lastWindowSize  int
	firstAvg        float64
	lastAvg         float64
	absChange       float64
	pctChange       string // formatted, or "N/A"
	window          string
}

// --- Main entry point ---

// generateStats computes before/after aggregation rows used by the HTML stat cards.
func generateStats(allStats []weekStats, windowPct int, onaThreshold float64, periodLabel string) []consolidatedRow {
	// Compute overall average PRs/week (across all non-zero weeks)
	var totalPRs int
	var nonZeroCount int
	for _, ws := range allStats {
		if ws.prsMerged > 0 {
			totalPRs += ws.prsMerged
			nonZeroCount++
		}
	}
	if nonZeroCount == 0 {
		fmt.Fprintf(os.Stderr, "WARNING: No non-empty weeks. Skipping stats.\n")
		return nil
	}
	avgPRs := float64(totalPRs) / float64(nonZeroCount)
	threshold := avgPRs * 0.10

	// Filter out weeks below 10% of overall average PRs/week
	var valid []weekStats
	var excluded int
	for _, ws := range allStats {
		if ws.prsMerged > 0 && float64(ws.prsMerged) >= threshold {
			valid = append(valid, ws)
		} else if ws.prsMerged > 0 {
			excluded++
		}
	}
	if excluded > 0 {
		fmt.Fprintf(os.Stderr, "Stats: excluded %d week(s) below %.0f PRs (10%% of avg %.1f)\n", excluded, threshold, avgPRs)
	}

	if len(valid) < 4 {
		fmt.Fprintf(os.Stderr, "WARNING: Only %d weeks after filtering — need at least 4 for stats. Skipping.\n", len(valid))
		return nil
	}

	// Build metrics list including coding/review time
	metrics := append(allMetrics,
		metricDef{
			name:    "median_coding_time_hours",
			extract: func(ws weekStats) float64 { return ws.medianCodingTime },
			valid:   func(ws weekStats) bool { return ws.prsMerged > 0 && ws.medianCodingTime >= 0 },
		},
		metricDef{
			name:    "median_review_time_hours",
			extract: func(ws weekStats) float64 { return ws.medianReviewTime },
			valid:   func(ws weekStats) bool { return ws.prsMerged > 0 && ws.medianReviewTime >= 0 },
		},
	)

	var rows []consolidatedRow

	for _, md := range metrics {
		row := buildRow(md, valid, windowPct, onaThreshold, periodLabel)
		if row != nil {
			rows = append(rows, *row)
		}
	}

	if len(rows) == 0 {
		return nil
	}

	return rows
}

// buildRow constructs one consolidated row for a metric.
func buildRow(md metricDef, valid []weekStats, windowPct int, onaThreshold float64, periodLabel string) *consolidatedRow {
	var firstAvg, lastAvg float64
	var n, firstWinSize, lastWinSize int
	var window string
	var ok bool

	if onaThreshold > 0 {
		firstAvg, lastAvg, n, firstWinSize, lastWinSize, ok = thresholdWindow(valid, md, onaThreshold)
		if !ok {
			return nil
		}
		abbrev := "w"
		if periodLabel == "month" {
			abbrev = "mo"
		}
		window = fmt.Sprintf("below %.0f%% Ona (%d%s) vs above %.0f%% Ona (%d%s)", onaThreshold, firstWinSize, abbrev, onaThreshold, lastWinSize, abbrev)
	} else {
		var winSize int
		firstAvg, lastAvg, n, winSize, ok = trendWindow(valid, md, windowPct)
		if !ok {
			return nil
		}
		firstWinSize = winSize
		lastWinSize = winSize
		abbrev := "w"
		if periodLabel == "month" {
			abbrev = "mo"
		}
		window = fmt.Sprintf("first %d%s vs last %d%s avg", winSize, abbrev, winSize, abbrev)
	}

	absChange := lastAvg - firstAvg
	var pctChange string
	if firstAvg != 0 {
		pct := (absChange / math.Abs(firstAvg)) * 100
		sign := "+"
		if pct < 0 {
			sign = ""
		}
		pctChange = fmt.Sprintf("%s%.1f%%", sign, pct)
	} else if lastAvg != 0 {
		// Starting from 0: show absolute change (e.g. "0 → 45.2" displays as "+45.2")
		sign := "+"
		if absChange < 0 {
			sign = ""
		}
		pctChange = fmt.Sprintf("%s%.1f", sign, absChange)
	} else {
		pctChange = "0.0%"
	}

	return &consolidatedRow{
		metric:          md.name,
		windowSize:      firstWinSize,
		firstWindowSize: firstWinSize,
		lastWindowSize:  lastWinSize,
		n:               n,
		firstAvg:        firstAvg,
		lastAvg:         lastAvg,
		absChange:       absChange,
		pctChange:       pctChange,
		window:          window,
	}
}

// --- Trend windowing ---

// trendWindow computes the first-N%-vs-last-N% averages for a metric.
func trendWindow(weeks []weekStats, md metricDef, windowPct int) (float64, float64, int, int, bool) {
	var values []float64
	for _, ws := range weeks {
		if md.valid(ws) {
			values = append(values, md.extract(ws))
		}
	}
	n := len(values)
	if n < 2 {
		return 0, 0, n, 0, false
	}

	windowSize := int(math.Floor(float64(n) * float64(windowPct) / 100.0))
	if windowSize < 1 {
		windowSize = 1
	}

	var firstSum float64
	for i := 0; i < windowSize; i++ {
		firstSum += values[i]
	}
	firstAvg := firstSum / float64(windowSize)

	var lastSum float64
	for i := n - windowSize; i < n; i++ {
		lastSum += values[i]
	}
	lastAvg := lastSum / float64(windowSize)

	return firstAvg, lastAvg, n, windowSize, true
}

// thresholdWindow splits weeks by Ona usage threshold and computes averages for each group.
func thresholdWindow(weeks []weekStats, md metricDef, threshold float64) (float64, float64, int, int, int, bool) {
	var belowVals, aboveVals []float64
	for _, ws := range weeks {
		if !md.valid(ws) {
			continue
		}
		v := md.extract(ws)
		if ws.pctOnaInvolved < threshold {
			belowVals = append(belowVals, v)
		} else {
			aboveVals = append(aboveVals, v)
		}
	}
	if len(belowVals) == 0 || len(aboveVals) == 0 {
		return 0, 0, 0, 0, 0, false
	}

	var belowSum float64
	for _, v := range belowVals {
		belowSum += v
	}
	var aboveSum float64
	for _, v := range aboveVals {
		aboveSum += v
	}

	n := len(belowVals) + len(aboveVals)
	return belowSum / float64(len(belowVals)), aboveSum / float64(len(aboveVals)),
		n, len(belowVals), len(aboveVals), true
}
