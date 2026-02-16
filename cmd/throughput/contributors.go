package main

import (
	"math"
	"sort"
)

// contributorStat holds before/after Ona metrics for a single contributor.
type contributorStat struct {
	login      string
	totalPRs   int
	beforeRate float64 // PRs per active week before first Ona PR
	afterRate  float64 // PRs per active week after first Ona PR
	pctChange  float64
	hasOnaPRs  bool
}

type contribWeekBound struct {
	startEpoch int64
	endEpoch   int64
}

// computeTopContributors ranks contributors by total PR count and computes
// before/after Ona PR throughput rates for the top N.
// The before/after split is per-contributor: "after" starts at the merge date
// of their first Ona-involved PR. PR/week = total PRs / active weeks in period.
func computeTopContributors(prs []enrichedPR, weekRanges []weekRange, n int) []contributorStat {
	if len(prs) == 0 || n <= 0 {
		return nil
	}

	// Group PRs by author
	byAuthor := make(map[string][]enrichedPR)
	for _, pr := range prs {
		byAuthor[pr.authorLogin] = append(byAuthor[pr.authorLogin], pr)
	}

	// Rank authors by total PR count descending
	type authorCount struct {
		login string
		count int
	}
	ranked := make([]authorCount, 0, len(byAuthor))
	for login, authorPRs := range byAuthor {
		ranked = append(ranked, authorCount{login, len(authorPRs)})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].count != ranked[j].count {
			return ranked[i].count > ranked[j].count
		}
		return ranked[i].login < ranked[j].login // stable tie-break
	})

	if n > len(ranked) {
		n = len(ranked)
	}

	// Precompute week boundaries for active-week counting
	wb := make([]contribWeekBound, len(weekRanges))
	for i, wr := range weekRanges {
		wb[i] = contribWeekBound{
			startEpoch: wr.start.Unix(),
			endEpoch:   wr.end.Unix() + 86399, // end of day
		}
	}

	results := make([]contributorStat, n)
	for idx := 0; idx < n; idx++ {
		login := ranked[idx].login
		authorPRs := byAuthor[login]

		// Find first Ona-involved PR (by merge epoch)
		var firstOnaEpoch int64
		hasOna := false
		for _, pr := range authorPRs {
			if pr.onaInvolved {
				if !hasOna || pr.mergedEpoch < firstOnaEpoch {
					firstOnaEpoch = pr.mergedEpoch
					hasOna = true
				}
			}
		}

		var beforePRs, afterPRs []enrichedPR
		if !hasOna {
			beforePRs = authorPRs
		} else {
			for _, pr := range authorPRs {
				if pr.mergedEpoch < firstOnaEpoch {
					beforePRs = append(beforePRs, pr)
				} else {
					afterPRs = append(afterPRs, pr)
				}
			}
		}

		beforeActive := countActiveWeeks(beforePRs, wb)
		afterActive := countActiveWeeks(afterPRs, wb)

		var beforeRate, afterRate float64
		if beforeActive > 0 {
			beforeRate = float64(len(beforePRs)) / float64(beforeActive)
			beforeRate = math.Round(beforeRate*100) / 100
		}
		if afterActive > 0 {
			afterRate = float64(len(afterPRs)) / float64(afterActive)
			afterRate = math.Round(afterRate*100) / 100
		}

		var pctChange float64
		if beforeRate > 0 {
			pctChange = ((afterRate - beforeRate) / beforeRate) * 100
			pctChange = math.Round(pctChange*10) / 10
		}

		results[idx] = contributorStat{
			login:      login,
			totalPRs:   len(authorPRs),
			beforeRate: beforeRate,
			afterRate:  afterRate,
			pctChange:  pctChange,
			hasOnaPRs:  hasOna,
		}
	}

	return results
}

// countActiveWeeks returns how many week ranges contain at least one PR.
func countActiveWeeks(prs []enrichedPR, wb []contribWeekBound) int {
	if len(prs) == 0 {
		return 0
	}
	active := 0
	for _, w := range wb {
		for _, pr := range prs {
			if pr.mergedEpoch >= w.startEpoch && pr.mergedEpoch <= w.endEpoch {
				active++
				break
			}
		}
	}
	return active
}
