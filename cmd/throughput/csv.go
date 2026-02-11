package main

import (
	"fmt"
	"strings"
	"time"
)

const csvHeader = "week_start,week_end,prs_merged,total_additions,total_deletions,total_files_changed,median_cycle_time_hours,p90_cycle_time_hours,median_review_turnaround_hours,p90_review_turnaround_hours,avg_pr_size_lines,pct_ona_coauthored"

// aggregateCSV buckets PRs into weeks and produces CSV output.
func aggregateCSV(prs []enrichedPR, weeks []weekRange) string {
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
		count      int
		additions  int
		deletions  int
		files      int
		onaCount   int
		cycleTimes []float64
		reviewTimes []float64
	}
	buckets := make([]weekBucket, len(weeks))

	for _, pr := range prs {
		for i := range weeks {
			if pr.mergedEpoch >= bounds[i].startEpoch && pr.mergedEpoch <= bounds[i].endEpoch {
				buckets[i].count++
				buckets[i].additions += pr.additions
				buckets[i].deletions += pr.deletions
				buckets[i].files += pr.changedFiles
				if pr.onaCoauthored {
					buckets[i].onaCount++
				}
				if pr.cycleTimeHours >= 0 {
					buckets[i].cycleTimes = append(buckets[i].cycleTimes, pr.cycleTimeHours)
				}
				if pr.reviewTurnaround >= 0 {
					buckets[i].reviewTimes = append(buckets[i].reviewTimes, pr.reviewTurnaround)
				}
				break
			}
		}
	}

	// Build CSV
	var sb strings.Builder
	sb.WriteString(csvHeader)
	sb.WriteByte('\n')

	for i, wr := range weeks {
		b := buckets[i]
		ws := wr.start.Format("2006-01-02")
		we := wr.end.Format("2006-01-02")

		medCycle := formatPercentile(median(b.cycleTimes))
		p90Cycle := formatPercentile(p90(b.cycleTimes))
		medReview := formatPercentile(median(b.reviewTimes))
		p90Review := formatPercentile(p90(b.reviewTimes))

		var avgSize string
		var pctOna string
		if b.count > 0 {
			avgSize = fmt.Sprintf("%.2f", float64(b.additions+b.deletions)/float64(b.count))
			pctOna = fmt.Sprintf("%.1f", float64(b.onaCount)/float64(b.count)*100)
		} else {
			avgSize = "0.00"
			pctOna = "0.0"
		}

		fmt.Fprintf(&sb, "%s,%s,%d,%d,%d,%d,%s,%s,%s,%s,%s,%s\n",
			ws, we, b.count, b.additions, b.deletions, b.files,
			medCycle, p90Cycle, medReview, p90Review, avgSize, pctOna)
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
