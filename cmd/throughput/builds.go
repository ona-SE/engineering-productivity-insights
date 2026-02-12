package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"
)

type buildWeekStats struct {
	runs         int
	successCount int
}

type workflowRun struct {
	ID         int64     `json:"id"`
	Event      string    `json:"event"`
	Status     string    `json:"status"`
	Conclusion string    `json:"conclusion"`
	CreatedAt  time.Time `json:"created_at"`
}

type workflowRunsResponse struct {
	TotalCount   int           `json:"total_count"`
	WorkflowRuns []workflowRun `json:"workflow_runs"`
}

// fetchBuildRuns fetches GitHub Actions workflow runs per week concurrently.
// Uses total_count for run counts and a sample page for success rate.
// Returns nil if Actions data is unavailable.
func fetchBuildRuns(cfg config, weeks []weekRange) []buildWeekStats {
	if len(weeks) == 0 {
		return nil
	}

	fmt.Fprintf(os.Stderr, "Fetching GitHub Actions workflow runs...\n")

	// Probe first week to check if Actions is accessible
	probe := weeks[0]
	_, _, err := restGetPage(cfg.token, cfg.owner, cfg.repo,
		probe.start.Format("2006-01-02"),
		probe.end.AddDate(0, 0, 1).Format("2006-01-02"),
		"push", 1)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  Skipping build metrics: %v\n", err)
		return nil
	}

	stats := make([]buildWeekStats, len(weeks))
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, maxConcurrency)

	for i, wr := range weeks {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, wr weekRange) {
			defer wg.Done()
			defer func() { <-sem }()

			rangeStart := wr.start.Format("2006-01-02")
			rangeEnd := wr.end.AddDate(0, 0, 1).Format("2006-01-02")

			ws := fetchWeekBuildStats(cfg.token, cfg.owner, cfg.repo, rangeStart, rangeEnd)

			mu.Lock()
			stats[idx] = ws
			mu.Unlock()
		}(i, wr)
	}
	wg.Wait()

	var totalRuns int
	for _, s := range stats {
		totalRuns += s.runs
	}

	if totalRuns == 0 {
		fmt.Fprintf(os.Stderr, "  No workflow runs found (push/PR triggers)\n")
		return nil
	}

	fmt.Fprintf(os.Stderr, "  %d workflow runs total (push/PR triggers)\n", totalRuns)
	return stats
}

// fetchWeekBuildStats gets run count and success rate for one week.
// Queries push and pull_request events separately, using total_count for
// the run count and a sample of up to 100 runs for the success rate.
func fetchWeekBuildStats(token, owner, repo, rangeStart, rangeEnd string) buildWeekStats {
	var totalRuns, totalSuccess, sampleSize int

	for _, event := range []string{"push", "pull_request"} {
		runs, count, err := restGetPage(token, owner, repo, rangeStart, rangeEnd, event, 1)
		if err != nil {
			continue
		}
		totalRuns += count

		// Compute success rate from the sample
		for _, r := range runs {
			sampleSize++
			if r.Conclusion == "success" {
				totalSuccess++
			}
		}
	}

	ws := buildWeekStats{runs: totalRuns}
	if sampleSize > 0 {
		// Extrapolate success count from sample rate
		rate := float64(totalSuccess) / float64(sampleSize)
		ws.successCount = int(rate*float64(totalRuns) + 0.5)
	}
	return ws
}

// restGetPage fetches one page of workflow runs from the GitHub REST API.
func restGetPage(token, owner, repo, rangeStart, rangeEnd, event string, page int) ([]workflowRun, int, error) {
	url := fmt.Sprintf(
		"https://api.github.com/repos/%s/%s/actions/runs?status=completed&event=%s&created=%s..%s&per_page=100&page=%d",
		owner, repo, event, rangeStart, rangeEnd, page,
	)

	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return nil, 0, fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("Authorization", "bearer "+token)
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

		resp, err := httpClient.Do(req)
		if err != nil {
			lastErr = err
			time.Sleep(time.Duration(attempt*5) * time.Second)
			continue
		}

		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = err
			time.Sleep(time.Duration(attempt*5) * time.Second)
			continue
		}

		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusForbidden {
			return nil, 0, fmt.Errorf("Actions API returned %d (no access or not enabled)", resp.StatusCode)
		}

		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("REST API returned %d: %s", resp.StatusCode, string(data[:min(200, len(data))]))
			time.Sleep(time.Duration(attempt*5) * time.Second)
			continue
		}

		var result workflowRunsResponse
		if err := json.Unmarshal(data, &result); err != nil {
			lastErr = fmt.Errorf("unmarshal response: %w", err)
			time.Sleep(time.Duration(attempt*5) * time.Second)
			continue
		}

		return result.WorkflowRuns, result.TotalCount, nil
	}
	return nil, 0, fmt.Errorf("REST query failed after 3 attempts: %v", lastErr)
}
