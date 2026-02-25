package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// PR represents a pull request from the GraphQL response.
type PR struct {
	Number       int       `json:"number"`
	Title        string    `json:"title"`
	CreatedAt    time.Time `json:"createdAt"`
	MergedAt     time.Time `json:"mergedAt"`
	IsDraft      bool      `json:"isDraft"`
	Additions    int       `json:"additions"`
	Deletions    int       `json:"deletions"`
	ChangedFiles int       `json:"changedFiles"`
	Author       struct {
		Login    string `json:"login"`
		Typename string `json:"__typename"`
	} `json:"author"`
	Commits struct {
		TotalCount int `json:"totalCount"`
		Nodes      []struct {
			Commit struct {
				AuthoredDate time.Time `json:"authoredDate"`
				Message      string    `json:"message"`
				Author       struct {
					User *struct {
						Login string `json:"login"`
					} `json:"user"`
				} `json:"author"`
			} `json:"commit"`
		} `json:"nodes"`
	} `json:"commits"`
	Reviews struct {
		Nodes []struct {
			SubmittedAt *time.Time `json:"submittedAt"`
		} `json:"nodes"`
	} `json:"reviews"`
	TimelineItems struct {
		Nodes []struct {
			CreatedAt *time.Time `json:"createdAt"`
		} `json:"nodes"`
	} `json:"timelineItems"`
}

type searchResponse struct {
	Search struct {
		PageInfo struct {
			HasNextPage bool   `json:"hasNextPage"`
			EndCursor   string `json:"endCursor"`
		} `json:"pageInfo"`
		Nodes []json.RawMessage `json:"nodes"`
	} `json:"search"`
}

const maxConcurrency = 10

// fetchAllPRs fetches merged PRs for all weeks concurrently.
func fetchAllPRs(cfg config, weeks []weekRange) []PR {
	var (
		mu       sync.Mutex
		allPRs   []PR
		wg       sync.WaitGroup
		sem      = make(chan struct{}, maxConcurrency)
		totalFetched atomic.Int64
	)

	for i, wr := range weeks {
		wg.Add(1)
		sem <- struct{}{} // acquire semaphore
		go func(idx int, wr weekRange) {
			defer wg.Done()
			defer func() { <-sem }() // release semaphore

			prs := fetchWeekPRs(cfg, wr)
			weekCount := len(prs)
			total := totalFetched.Add(int64(weekCount))

			mu.Lock()
			allPRs = append(allPRs, prs...)
			mu.Unlock()

			fmt.Fprintf(os.Stderr, "  Week %s: %d PRs (total: %d)\n",
				wr.start.Format("2006-01-02"), weekCount, total)
		}(i, wr)
	}

	wg.Wait()

	fmt.Fprintf(os.Stderr, "Total PRs fetched: %d\n", len(allPRs))
	return allPRs
}

func fetchWeekPRs(cfg config, wr weekRange) []PR {
	rangeStart := wr.start.Format("2006-01-02")
	rangeEnd := wr.end.Format("2006-01-02")

	searchQuery := fmt.Sprintf(
		`repo:%s/%s is:pr is:merged base:%s merged:%s..%s`,
		cfg.owner, cfg.repo, cfg.branch, rangeStart, rangeEnd,
	)

	var prs []PR
	hasNext := true
	cursor := ""

	for hasNext {
		afterClause := ""
		if cursor != "" {
			afterClause = fmt.Sprintf(`, after: %q`, cursor)
		}

		query := fmt.Sprintf(`{
			search(query: %q, type: ISSUE, first: 100%s) {
				pageInfo { hasNextPage endCursor }
				nodes {
					... on PullRequest {
						number
						title
						createdAt
						mergedAt
						isDraft
						additions
						deletions
						changedFiles
						author {
							login
							... on Bot { __typename }
							... on User { __typename }
						}
						commits(first: 50) {
							totalCount
							nodes {
								commit {
									authoredDate
									message
									author { user { login } }
								}
							}
						}
						reviews(first: 1) {
							nodes {
								submittedAt
							}
						}
						timelineItems(itemTypes: READY_FOR_REVIEW_EVENT, first: 1) {
							nodes {
								... on ReadyForReviewEvent {
									createdAt
								}
							}
						}
					}
				}
			}
		}`, searchQuery, afterClause)

		resp, err := graphqlQuery(cfg.token, query)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: GraphQL query failed for week %s: %v\n", rangeStart, err)
			return prs
		}

		// Log non-fatal errors
		if len(resp.Errors) > 0 {
			fmt.Fprintf(os.Stderr, "  GraphQL error (week %s): %s\n", rangeStart, resp.Errors[0].Message)
		}

		var sr searchResponse
		if err := json.Unmarshal(resp.Data, &sr); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: Failed to parse search response for week %s: %v\n", rangeStart, err)
			return prs
		}

		for _, raw := range sr.Search.Nodes {
			var pr PR
			if err := json.Unmarshal(raw, &pr); err != nil {
				continue // skip malformed entries
			}
			// Skip entries with no number (empty search nodes)
			if pr.Number == 0 {
				continue
			}
			prs = append(prs, pr)
		}

		hasNext = sr.Search.PageInfo.HasNextPage
		cursor = sr.Search.PageInfo.EndCursor
	}

	return prs
}

// backfillFirstCommits fetches the first commit for PRs with >50 commits.
// This ensures accurate cycle time even for large PRs where commits(first:50)
// may not include the earliest commit.
func backfillFirstCommits(cfg config, prs []PR) {
	// Find PRs that need backfill
	type backfillItem struct {
		index int
		number int
	}
	var items []backfillItem
	for i, pr := range prs {
		if pr.Commits.TotalCount > 50 {
			items = append(items, backfillItem{index: i, number: pr.Number})
		}
	}
	if len(items) == 0 {
		return
	}

	fmt.Fprintf(os.Stderr, "Backfilling first commit for %d PRs with >50 commits...\n", len(items))

	var wg sync.WaitGroup
	sem := make(chan struct{}, maxConcurrency)

	for _, item := range items {
		wg.Add(1)
		sem <- struct{}{}
		go func(it backfillItem) {
			defer wg.Done()
			defer func() { <-sem }()

			query := fmt.Sprintf(`{
				repository(owner: %q, name: %q) {
					pullRequest(number: %d) {
						commits(first: 1) {
							nodes {
								commit {
									authoredDate
									message
									author { user { login } }
								}
							}
						}
					}
				}
			}`, cfg.owner, cfg.repo, it.number)

			resp, err := graphqlQuery(cfg.token, query)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  WARNING: Failed to backfill commits for PR #%d: %v\n", it.number, err)
				return
			}

			var result struct {
				Repository struct {
					PullRequest struct {
						Commits struct {
							Nodes []struct {
								Commit struct {
									AuthoredDate time.Time `json:"authoredDate"`
									Message      string    `json:"message"`
									Author       struct {
										User *struct {
											Login string `json:"login"`
										} `json:"user"`
									} `json:"author"`
								} `json:"commit"`
							} `json:"nodes"`
						} `json:"commits"`
					} `json:"pullRequest"`
				} `json:"repository"`
			}
			if err := json.Unmarshal(resp.Data, &result); err != nil {
				fmt.Fprintf(os.Stderr, "  WARNING: Failed to parse backfill response for PR #%d: %v\n", it.number, err)
				return
			}

			nodes := result.Repository.PullRequest.Commits.Nodes
			if len(nodes) > 0 {
				// Prepend the true first commit so the earliest-date scan in
				// metrics.go picks it up even if it wasn't in the original 50.
				prs[it.index].Commits.Nodes = append(nodes, prs[it.index].Commits.Nodes...)
			}
		}(item)
	}

	wg.Wait()
}
