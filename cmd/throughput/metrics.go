package main

import (
	"math"
	"regexp"
	"sort"
	"strings"
	"time"
)

var onaCoauthorRe = regexp.MustCompile(`(?i)Co-authored-by:.*[Oo]na.*@ona\.com`)
var revertRe = regexp.MustCompile(`(?i)\b(revert|reverting|rollback|roll\s+back|rolled\s+back)\b`)

// enrichedPR holds a PR with computed metrics.
type enrichedPR struct {
	mergedEpoch          int64
	codingTimeHours      float64 // first commit to ready-for-review; -1 means not available
	reviewTimeHours      float64 // ready-for-review to merged; -1 means not available
	reviewTurnaround     float64 // PR created to first review submitted; -1 means not available
	additions            int
	deletions            int
	changedFiles         int
	number               int
	authorLogin          string
	onaInvolved          bool
	isRevert             bool
	commitsByAuthor      map[string]int // GitHub login → commit count (unlinked commits excluded)
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

		// Skip draft PRs (matching GetDX behavior)
		if pr.IsDraft {
			continue
		}

		mergedEpoch := pr.MergedAt.Unix()
		createdEpoch := pr.CreatedAt.Unix()

		// Determine ready-for-review timestamp.
		// PRs that were drafts have a ReadyForReviewEvent in timelineItems.
		// PRs that were never drafts have no event — coding/review time
		// are set to -1 (not available) for those.
		var readyForReviewEpoch int64
		hasReadyEvent := len(pr.TimelineItems.Nodes) > 0 && pr.TimelineItems.Nodes[0].CreatedAt != nil
		if hasReadyEvent {
			readyForReviewEpoch = pr.TimelineItems.Nodes[0].CreatedAt.Unix()
		}

		// Coding time: earliest commit → ready-for-review.
		// Review time: ready-for-review → merged.
		// Both only available for PRs with a ReadyForReviewEvent.
		codingHours := -1.0
		reviewTimeHours := -1.0
		if hasReadyEvent {
			// Review time: ready-for-review to merged
			if mergedEpoch >= readyForReviewEpoch {
				reviewTimeHours = float64(mergedEpoch-readyForReviewEpoch) / 3600.0
				reviewTimeHours = math.Round(reviewTimeHours*100) / 100
			}

			// Coding time: earliest commit to ready-for-review
			if len(pr.Commits.Nodes) > 0 {
				var earliest time.Time
				for _, cn := range pr.Commits.Nodes {
					ad := cn.Commit.AuthoredDate
					if !ad.IsZero() && (earliest.IsZero() || ad.Before(earliest)) {
						earliest = ad
					}
				}
				if !earliest.IsZero() {
					fcEpoch := earliest.Unix()
					if readyForReviewEpoch >= fcEpoch {
						codingHours = float64(readyForReviewEpoch-fcEpoch) / 3600.0
						codingHours = math.Round(codingHours*100) / 100
					} else {
						// Earliest commit postdates ready event (shouldn't happen, but clamp)
						codingHours = 0
					}
				}
			}
		}

		// Review turnaround: PR created to first review submitted
		reviewTurnaroundHours := -1.0
		if len(pr.Reviews.Nodes) > 0 && pr.Reviews.Nodes[0].SubmittedAt != nil {
			revEpoch := pr.Reviews.Nodes[0].SubmittedAt.Unix()
			if revEpoch >= createdEpoch {
				reviewTurnaroundHours = float64(revEpoch-createdEpoch) / 3600.0
				reviewTurnaroundHours = math.Round(reviewTurnaroundHours*100) / 100
			}
		}

		// Ona involvement: co-authored OR primary author (login prefix "ona-")
		onaInvolved := strings.HasPrefix(login, "ona-")
		if !onaInvolved {
			for _, cn := range pr.Commits.Nodes {
				if onaCoauthorRe.MatchString(cn.Commit.Message) {
					onaInvolved = true
					break
				}
			}
		}

		isRevert := revertRe.MatchString(pr.Title)

		// Per-author commit counting: attribute commits to their actual author.
		commitsByAuthor := make(map[string]int)
		for _, cn := range pr.Commits.Nodes {
			if cn.Commit.Author.User != nil && cn.Commit.Author.User.Login != "" {
				commitsByAuthor[strings.ToLower(cn.Commit.Author.User.Login)]++
			}
		}

		result = append(result, enrichedPR{
			mergedEpoch:      mergedEpoch,
			codingTimeHours:  codingHours,
			reviewTimeHours:  reviewTimeHours,
			reviewTurnaround: reviewTurnaroundHours,
			additions:        pr.Additions,
			deletions:        pr.Deletions,
			changedFiles:     pr.ChangedFiles,
			number:           pr.Number,
			authorLogin:      login,
			onaInvolved:      onaInvolved,
			isRevert:         isRevert,
			commitsByAuthor:  commitsByAuthor,
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
