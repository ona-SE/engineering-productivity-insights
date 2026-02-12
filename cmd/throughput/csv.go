package main

import (
	"fmt"
	"strings"
	"time"
)

const csvHeader = "week_start,week_end,prs_merged,unique_authors,prs_per_engineer,total_additions,total_deletions,total_files_changed,median_review_speed_hours,p90_review_speed_hours,median_commit_to_merge_hours,p90_commit_to_merge_hours,median_review_turnaround_hours,p90_review_turnaround_hours,avg_pr_size_lines,pct_ona_involved,revert_count,pct_reverts"

// weekStats holds the computed per-week values needed by the stats analysis.
type weekStats struct {
	prsMerged            int
	uniqueAuthors        int
	prsPerEngineer       float64
	medianReviewSpeed    float64 // PR opened to merge; -1 if no data
	medianCycleTime      float64 // first commit to merge (unreliable with squash merges); -1 if no data
	pctOnaInvolved       float64
	pctReverts           float64
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
		reviewSpeeds     []float64 // PR opened to merge
		cycleTimes       []float64 // first commit to merge
		reviewTimes      []float64
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
				if pr.reviewSpeedHours >= 0 {
					buckets[i].reviewSpeeds = append(buckets[i].reviewSpeeds, pr.reviewSpeedHours)
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

		medReviewSpeed := formatPercentile(median(b.reviewSpeeds))
		p90ReviewSpeed := formatPercentile(p90(b.reviewSpeeds))
		medCycle := formatPercentile(median(b.cycleTimes))
		p90Cycle := formatPercentile(p90(b.cycleTimes))
		medReview := formatPercentile(median(b.reviewTimes))
		p90Review := formatPercentile(p90(b.reviewTimes))

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
			medReviewSpeed, p90ReviewSpeed, medCycle, p90Cycle,
			medReview, p90Review, avgSize, pctOna,
			b.revertCount, pctReverts)

		allStats[i] = weekStats{
			prsMerged:         b.count,
			uniqueAuthors:     uniqueAuthors,
			prsPerEngineer:    prsPerEng,
			medianReviewSpeed: median(b.reviewSpeeds),
			medianCycleTime:   median(b.cycleTimes),
			pctOnaInvolved:    pctOna,
			pctReverts:        pctReverts,
		}
	}

	return sb.String(), allStats
}

// formatPercentile formats a percentile value, returning empty string for no data.
func formatPercentile(v float64) string {
	if v < 0 {
		return ""
	}
	return fmt.Sprintf("%.2f", v)
}
