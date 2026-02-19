package main

import (
	"fmt"
	"strings"
	"time"
)

const csvHeader = "week_start,week_end,prs_merged,unique_authors,prs_per_engineer,total_additions,total_deletions,total_files_changed,median_coding_time_hours,p90_coding_time_hours,median_review_time_hours,p90_review_time_hours,median_review_turnaround_hours,p90_review_turnaround_hours,avg_pr_size_lines,pct_ona_involved,revert_count,pct_reverts"

// weekStats holds the computed per-week values needed by the stats analysis.
type weekStats struct {
	prsMerged            int
	uniqueAuthors        int
	prsPerEngineer       float64
	medianCodingTime     float64 // first commit to ready-for-review; -1 if no data
	medianReviewTime     float64 // ready-for-review to merged; -1 if no data
	pctOnaInvolved       float64
	pctReverts           float64
	buildRuns            int
	buildSuccessPct      float64
}

// aggregateCSV buckets PRs into weeks and produces CSV output.
// It also returns per-week stats for use by the statistical analysis.
func aggregateCSV(prs []enrichedPR, weeks []weekRange) (string, []weekStats) {
	// Precompute week epoch boundaries
	type weekBounds struct {
		startEpoch int64
		endEpoch   int64
	}
	bounds := make([]weekBounds, len(weeks))
	for i, wr := range weeks {
		bounds[i] = weekBounds{
			startEpoch: wr.start.Unix(),
			// End of day: 23:59:59
			endEpoch: time.Date(wr.end.Year(), wr.end.Month(), wr.end.Day(), 23, 59, 59, 0, time.UTC).Unix(),
		}
	}

	// Bucket PRs into weeks
	type weekBucket struct {
		count            int
		additions        int
		deletions        int
		files            int
		onaCount         int
		revertCount      int
		codingTimes      []float64 // first commit to ready-for-review
		reviewTimes      []float64 // ready-for-review to merged
		turnaroundTimes  []float64 // PR created to first review
		authors          map[string]bool
	}
	buckets := make([]weekBucket, len(weeks))
	for i := range buckets {
		buckets[i].authors = make(map[string]bool)
	}

	for _, pr := range prs {
		for i := range weeks {
			if pr.mergedEpoch >= bounds[i].startEpoch && pr.mergedEpoch <= bounds[i].endEpoch {
				buckets[i].count++
				buckets[i].additions += pr.additions
				buckets[i].deletions += pr.deletions
				buckets[i].files += pr.changedFiles
				buckets[i].authors[pr.authorLogin] = true
				if pr.onaInvolved {
					buckets[i].onaCount++
				}
				if pr.isRevert {
					buckets[i].revertCount++
				}
				if pr.codingTimeHours >= 0 {
					buckets[i].codingTimes = append(buckets[i].codingTimes, pr.codingTimeHours)
				}
				if pr.reviewTimeHours >= 0 {
					buckets[i].reviewTimes = append(buckets[i].reviewTimes, pr.reviewTimeHours)
				}
				if pr.reviewTurnaround >= 0 {
					buckets[i].turnaroundTimes = append(buckets[i].turnaroundTimes, pr.reviewTurnaround)
				}
				break
			}
		}
	}

	// Build CSV and collect stats
	var sb strings.Builder
	sb.WriteString(csvHeader)
	sb.WriteByte('\n')

	allStats := make([]weekStats, len(weeks))

	for i, wr := range weeks {
		b := buckets[i]
		ws := wr.start.Format("2006-01-02")
		we := wr.end.Format("2006-01-02")

		uniqueAuthors := len(b.authors)
		var prsPerEng float64
		if uniqueAuthors > 0 {
			prsPerEng = float64(b.count) / float64(uniqueAuthors)
		}

		medCoding := formatPercentile(median(b.codingTimes))
		p90Coding := formatPercentile(p90(b.codingTimes))
		medReviewTime := formatPercentile(median(b.reviewTimes))
		p90ReviewTime := formatPercentile(p90(b.reviewTimes))
		medTurnaround := formatPercentile(median(b.turnaroundTimes))
		p90Turnaround := formatPercentile(p90(b.turnaroundTimes))

		var avgSize string
		var pctOna float64
		var pctReverts float64
		if b.count > 0 {
			avgSize = fmt.Sprintf("%.2f", float64(b.additions+b.deletions)/float64(b.count))
			pctOna = float64(b.onaCount) / float64(b.count) * 100
			pctReverts = float64(b.revertCount) / float64(b.count) * 100
		} else {
			avgSize = "0.00"
		}

		fmt.Fprintf(&sb, "%s,%s,%d,%d,%.2f,%d,%d,%d,%s,%s,%s,%s,%s,%s,%s,%.1f,%d,%.1f\n",
			ws, we, b.count, uniqueAuthors, prsPerEng,
			b.additions, b.deletions, b.files,
			medCoding, p90Coding, medReviewTime, p90ReviewTime,
			medTurnaround, p90Turnaround, avgSize, pctOna,
			b.revertCount, pctReverts)

		allStats[i] = weekStats{
			prsMerged:         b.count,
			uniqueAuthors:     uniqueAuthors,
			prsPerEngineer:    prsPerEng,
			medianCodingTime:  median(b.codingTimes),
			medianReviewTime:  median(b.reviewTimes),
			pctOnaInvolved:    pctOna,
			pctReverts:        pctReverts,
		}
	}

	return sb.String(), allStats
}

// appendBuildColumns appends build_runs and build_success_pct columns to existing CSV.
func appendBuildColumns(csv string, stats []weekStats) string {
	lines := strings.Split(strings.TrimRight(csv, "\n"), "\n")
	if len(lines) == 0 {
		return csv
	}

	var sb strings.Builder
	// Header
	sb.WriteString(lines[0])
	sb.WriteString(",build_runs,build_success_pct\n")

	// Data rows
	for i, line := range lines[1:] {
		sb.WriteString(line)
		if i < len(stats) {
			fmt.Fprintf(&sb, ",%d,%.1f", stats[i].buildRuns, stats[i].buildSuccessPct)
		} else {
			sb.WriteString(",0,0.0")
		}
		sb.WriteByte('\n')
	}

	return sb.String()
}

// formatPercentile formats a percentile value, returning empty string for no data.
func formatPercentile(v float64) string {
	if v < 0 {
		return ""
	}
	return fmt.Sprintf("%.2f", v)
}
