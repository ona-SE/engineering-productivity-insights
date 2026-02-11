package main

import (
	"math"
	"regexp"
	"sort"
	"strings"
)

var onaCoauthorRe = regexp.MustCompile(`(?i)Co-authored-by:.*[Oo]na.*@ona\.com`)
var revertRe = regexp.MustCompile(`(?i)\b(revert|reverting|rollback|roll\s+back|rolled\s+back)\b`)

// enrichedPR holds a PR with computed metrics.
type enrichedPR struct {
	mergedEpoch       int64
	cycleTimeHours    float64 // -1 means not available
	reviewTurnaround  float64 // -1 means not available
	additions         int
	deletions         int
	changedFiles      int
	number            int
	onaCoauthored     bool
	isRevert          bool
}

// filterPRs filters out bots and excluded users, computes metrics.
func filterPRs(prs []PR, excludeSet map[string]bool) []enrichedPR {
	var result []enrichedPR

	for _, pr := range prs {
		// Skip bots
		if pr.Author.Typename == "Bot" {
			continue
		}

		// Skip excluded users (case-insensitive)
		login := strings.ToLower(pr.Author.Login)
		if excludeSet[login] {
			continue
		}

		// Skip PRs without mergedAt
		if pr.MergedAt.IsZero() {
			continue
		}

		mergedEpoch := pr.MergedAt.Unix()
		createdEpoch := pr.CreatedAt.Unix()

		// Cycle time: first commit authored date to merged
		cycleHours := -1.0
		if len(pr.Commits.Nodes) > 0 {
			firstCommitTime := pr.Commits.Nodes[0].Commit.AuthoredDate
			if !firstCommitTime.IsZero() {
				fcEpoch := firstCommitTime.Unix()
				if mergedEpoch >= fcEpoch {
					cycleHours = float64(mergedEpoch-fcEpoch) / 3600.0
					// Match bash: * 100 | round / 100
					cycleHours = math.Round(cycleHours*100) / 100
				}
			}
		}

		// Review turnaround: PR created to first review submitted
		reviewHours := -1.0
		if len(pr.Reviews.Nodes) > 0 && pr.Reviews.Nodes[0].SubmittedAt != nil {
			revEpoch := pr.Reviews.Nodes[0].SubmittedAt.Unix()
			if revEpoch >= createdEpoch {
				reviewHours = float64(revEpoch-createdEpoch) / 3600.0
				reviewHours = math.Round(reviewHours*100) / 100
			}
		}

		// Ona co-authorship: check all commit messages
		onaCoauthored := false
		for _, cn := range pr.Commits.Nodes {
			if onaCoauthorRe.MatchString(cn.Commit.Message) {
				onaCoauthored = true
				break
			}
		}

		isRevert := revertRe.MatchString(pr.Title)

		result = append(result, enrichedPR{
			mergedEpoch:      mergedEpoch,
			cycleTimeHours:   cycleHours,
			reviewTurnaround: reviewHours,
			additions:        pr.Additions,
			deletions:        pr.Deletions,
			changedFiles:     pr.ChangedFiles,
			number:           pr.Number,
			onaCoauthored:    onaCoauthored,
			isRevert:         isRevert,
		})
	}

	return result
}

// percentile computes the p-th percentile using linear interpolation.
// Matches the bash awk implementation.
func percentile(values []float64, pct float64) float64 {
	n := len(values)
	if n == 0 {
		return -1 // sentinel for "no data"
	}

	sorted := make([]float64, n)
	copy(sorted, values)
	sort.Float64s(sorted)

	if n == 1 {
		return sorted[0]
	}

	// awk formula: idx = (pct / 100.0) * (n - 1) + 1 (1-based)
	// Convert to 0-based: idx0 = (pct / 100.0) * (n - 1)
	idx := (pct / 100.0) * float64(n-1)
	lower := int(idx)
	if lower < 0 {
		lower = 0
	}
	if lower >= n-1 {
		return sorted[n-1]
	}
	frac := idx - float64(lower)
	return sorted[lower] + frac*(sorted[lower+1]-sorted[lower])
}

func median(values []float64) float64 {
	return percentile(values, 50)
}

func p90(values []float64) float64 {
	return percentile(values, 90)
}
