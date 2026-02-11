# Spec: Rewrite throughput script in Go

## Problem Statement

The bash throughput script takes ~35 minutes to analyze 52 weeks of `gitpod-io/gitpod-next` (7,111 PRs, ~95 API calls). The goal is to reduce execution time by rewriting in Go.

## Performance Analysis

### Where time is spent today (52-week run, gitpod-next)

| Phase | Time | % of total |
|---|---|---|
| GraphQL fetching (95 sequential API calls) | ~30 min | ~86% |
| jq query construction per API call (subprocess overhead) | ~4 min | ~11% |
| jq PR enrichment + awk aggregation | ~10s | ~1% |
| Sleeps (0.3s x 95 calls) | ~30s | ~1% |

Each API call takes 4-7s round-trip (GitHub GraphQL response time for 100 PRs with commits/reviews). The calls are fully sequential — each week waits for the previous to finish.

### Go vs parallel bash — concrete comparison

| Approach | Estimated time (52 weeks) | How |
|---|---|---|
| Current bash (sequential) | ~35 min | 95 serial API calls + per-call jq overhead |
| Parallel bash (background jobs per week) | ~3-5 min | 52 weeks fetched concurrently, but each week's pagination still serial. jq subprocess overhead per call remains. Temp file coordination adds complexity. |
| Go (concurrent) | ~1-2 min | All 52 weeks fetched concurrently with goroutines. Pagination within each week also concurrent where possible. Native JSON unmarshalling — no subprocess overhead. Single-pass in-memory processing. |

### Why Go is faster than parallel bash

1. **No subprocess overhead.** Bash spawns `jq` and `curl` as child processes for every API call. Each spawn costs ~5-10ms, and jq query construction adds ~2-3s per call. Go uses `net/http` and `encoding/json` natively — zero process spawning.

2. **Finer-grained concurrency.** Bash `&` parallelizes at the week level, but pagination within a week is still serial. Go goroutines can overlap pagination cursors across weeks with a worker pool, saturating the API rate limit more efficiently.

3. **No intermediate files.** Bash pipes data through temp files (`$ALL_PRS_FILE`, `$ENRICHED_DATA`). Go processes everything in memory with typed structs.

4. **Connection reuse.** Go's `http.Client` reuses TCP connections (HTTP keep-alive) across requests. Bash's `curl` opens a new connection per call (~2ms overhead each, minor but adds up).

5. **Rate limit awareness.** Go can implement a token-bucket rate limiter that fires requests as fast as GitHub allows, rather than using fixed sleeps.

### What Go does NOT improve

- **GitHub API response time.** Each call still takes 4-7s server-side. This is the floor — no language change can reduce it.
- **GitHub rate limits.** 5,000 points/hour for GraphQL. Each search query costs 1 point. With 95 calls we're well under the limit, so this isn't a constraint for this workload.

### Expected outcome

52-week run drops from **~35 min to ~1-2 min** (roughly 20-30x faster). The improvement comes from concurrent fetching, not from Go being a "faster language."

## Requirements

### Functional (1:1 parity with bash script)

1. Accept the same CLI flags: `--repo`, `--branch`, `--weeks`, `--output`, `--exclude`
2. Default to detecting owner/repo from the current git remote when `--repo` is not provided
3. Resolve GitHub token from `GH_TOKEN`, `GITHUB_TOKEN`, or the git credential helper (same fallback chain)
4. Fetch merged PRs via GitHub GraphQL API, scoped to the target branch
5. Exclude bots by `__typename` and users by the exclude list (case-insensitive)
6. Compute per-week metrics: PRs merged, total additions/deletions/files changed, median and p90 cycle time, median and p90 review turnaround, average PR size, % Ona co-authored
7. Output identical CSV format (same columns, same header, same precision)
8. Print progress to stderr (repo info, per-week fetch counts, processing summary)

### Non-functional

9. Fetch all weeks concurrently (bounded goroutine pool to avoid rate limits)
10. Retry failed API calls up to 3 times with backoff
11. Handle rate limiting (wait and retry on `RATE_LIMITED` errors)
12. Single binary with no external dependencies (no curl, jq, awk needed)
13. Go module in the repo root, source in a `cmd/throughput/` directory

### DevContainer

14. Add Go to the devcontainer so the binary can be built and run in-environment

## Acceptance Criteria

- [ ] `go build ./cmd/throughput` produces a working binary
- [ ] Running against `gitpod-io/gitpod-next --weeks 12` produces CSV output matching the bash script (same column values, allowing for rounding differences of +/- 0.01)
- [ ] Running against `gitpod-io/gitpod-next --weeks 52` completes in under 5 minutes
- [ ] `--repo`, `--branch`, `--weeks`, `--output`, `--exclude` flags all work
- [ ] Token resolution from git credential helper works
- [ ] DevContainer rebuilds successfully with Go available

## Implementation Plan

1. Add Go feature to `.devcontainer/devcontainer.json` and rebuild
2. Initialize Go module (`go mod init`) and create `cmd/throughput/` directory structure
3. Implement CLI flag parsing (match bash interface exactly)
4. Implement GitHub token resolution (env vars + git credential helper)
5. Implement GraphQL client with retry/rate-limit logic
6. Implement concurrent week-fetching with bounded worker pool
7. Implement PR filtering (bot exclusion, username exclusion)
8. Implement metric computation (cycle time, review turnaround, Ona co-authorship)
9. Implement weekly aggregation with percentile calculations (median, p90)
10. Implement CSV output (matching existing format exactly)
11. Test against `gitpod-io/gitpod-next --weeks 12`, diff output against bash script's CSV
12. Run full `--weeks 52` and verify it completes under 5 minutes
