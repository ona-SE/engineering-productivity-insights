package main

import (
	"sort"
	"time"
)

// monthRange represents a calendar month period.
type monthRange struct {
	start time.Time // first day of month
	end   time.Time // last day of month
}

// monthlyStats aggregates weekly stats into calendar months.
// PRs merged, unique authors, and revert counts are summed.
// PRs/engineer, review speed, Ona involvement, and revert % use the median of weekly values.
// Weeks with 0 PRs are excluded from median calculations.
func aggregateMonthly(weeks []weekRange, stats []weekStats) ([]weekRange, []weekStats) {
	if len(weeks) == 0 {
		return nil, nil
	}

	// Group week indices by calendar month (YYYY-MM)
	type monthGroup struct {
		month string
		start time.Time
		end   time.Time
		weeks []int
	}

	groups := make(map[string]*monthGroup)
	var order []string

	for i, wr := range weeks {
		key := wr.start.Format("2006-01")
		g, ok := groups[key]
		if !ok {
			firstOfMonth := time.Date(wr.start.Year(), wr.start.Month(), 1, 0, 0, 0, 0, time.UTC)
			lastOfMonth := firstOfMonth.AddDate(0, 1, -1)
			g = &monthGroup{month: key, start: firstOfMonth, end: lastOfMonth}
			groups[key] = g
			order = append(order, key)
		}
		g.weeks = append(g.weeks, i)
		// Extend end to cover the last week's end date
		if wr.end.After(g.end) {
			g.end = wr.end
		}
	}

	// Drop the last month if it's incomplete (doesn't contain a week
	// starting in the last 7 days of that calendar month).
	if len(order) > 0 {
		lastKey := order[len(order)-1]
		lg := groups[lastKey]
		lastOfMonth := lg.start.AddDate(0, 1, -1)
		lastWeekStart := lg.weeks[len(lg.weeks)-1]
		if weeks[lastWeekStart].start.Before(lastOfMonth.AddDate(0, 0, -6)) {
			order = order[:len(order)-1]
		}
	}

	var outRanges []weekRange
	var outStats []weekStats

	for _, key := range order {
		g := groups[key]

		var totalPRs int
		var prsPerEngVals, reviewSpeedVals, onaVals, revertPctVals []float64

		for _, wi := range g.weeks {
			ws := stats[wi]
			totalPRs += ws.prsMerged

			if ws.prsMerged > 0 {
				prsPerEngVals = append(prsPerEngVals, ws.prsPerEngineer)
				onaVals = append(onaVals, ws.pctOnaInvolved)
				revertPctVals = append(revertPctVals, ws.pctReverts)
			}
			if ws.medianReviewSpeed >= 0 && ws.prsMerged > 0 {
				reviewSpeedVals = append(reviewSpeedVals, ws.medianReviewSpeed)
			}
		}

		// For unique authors at the monthly level, we need to re-count from
		// the weekly unique_authors. Since we don't have per-PR author data
		// at this stage, sum unique_authors across weeks as an approximation
		// (this overcounts if the same author appears in multiple weeks).
		// Use the max weekly unique_authors as a lower bound, and average as
		// the displayed value. The most accurate approach: use median of
		// weekly unique_authors.
		var authorCountVals []float64
		for _, wi := range g.weeks {
			ws := stats[wi]
			if ws.prsMerged > 0 {
				authorCountVals = append(authorCountVals, float64(ws.uniqueAuthors))
			}
		}

		medianAuthors := medianFloat(authorCountVals)
		medianPrsPerEng := medianFloat(prsPerEngVals)
		medianReviewSpeed := medianFloat(reviewSpeedVals)
		medianOna := medianFloat(onaVals)
		medianRevertPct := medianFloat(revertPctVals)

		if len(reviewSpeedVals) == 0 {
			medianReviewSpeed = -1
		}

		outRanges = append(outRanges, weekRange{start: g.start, end: g.end})
		outStats = append(outStats, weekStats{
			prsMerged:         totalPRs,
			uniqueAuthors:     int(medianAuthors),
			prsPerEngineer:    medianPrsPerEng,
			medianReviewSpeed: medianReviewSpeed,
			medianCycleTime:   -1, // not meaningful at monthly level
			pctOnaInvolved:    medianOna,
			pctReverts:        medianRevertPct,
		})
	}

	return outRanges, outStats
}

// medianFloat returns the median of a float64 slice, or 0 if empty.
func medianFloat(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	sorted := make([]float64, len(vals))
	copy(sorted, vals)
	sort.Float64s(sorted)
	n := len(sorted)
	if n%2 == 0 {
		return (sorted[n/2-1] + sorted[n/2]) / 2
	}
	return sorted[n/2]
}
