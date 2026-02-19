# Agent guide

Instructions for AI agents working in this repository.

## Repository overview

This repo contains a tool that computes week-over-week PR throughput metrics for GitHub repositories. The implementation is in Go (`cmd/throughput/`). There is also a legacy bash script (`throughput DEPRECATED.sh`) which is no longer maintained.

## Build and run

```sh
go build ./cmd/throughput/
./throughput --repo gitpod-io/gitpod-next --weeks 12
```

Or without a build step:

```sh
go run ./cmd/throughput/ --repo gitpod-io/gitpod-next --weeks 12
```

To run with the interactive chart visualization:

```sh
./throughput --repo gitpod-io/gitpod-next --weeks 52 --min-prs 10 --serve
```

## Code layout

All Go source lives in `cmd/throughput/`:

- `main.go` — Entry point, CLI flag parsing, week range computation, orchestration. Flags: `--repo`, `--branch`, `--weeks`, `--output`, `--exclude`, `--stats-output`, `--html`, `--serve`, `--port`, `--min-prs`, `--exclude-bottom-contributor-pct`, `--granularity`, `--compare-window-pct`, `--compare-ona-threshold`, `--top-contributors`, `--cycle-time`.
- `monthly.go` — Aggregates weekly stats into calendar months. Uses medians for rate metrics (PRs/engineer, review speed, Ona %, revert %) and sums for counts (PRs merged). Drops the last incomplete month.
- `token.go` — Resolves GitHub token from env vars or git credential helper.
- `graphql.go` — GraphQL HTTP client with retry (3 attempts, backoff) and rate-limit handling.
- `fetch.go` — Concurrent PR fetching. Uses a bounded goroutine pool (10 workers). Each worker fetches one week's PRs with pagination.
- `metrics.go` — Filters out bots, excluded users, and draft PRs. Computes two cycle time metrics (see below), review turnaround (PR created to first review), Ona co-authorship, revert detection. Percentile calculation (median, p90) uses linear interpolation matching the bash awk implementation.
- `csv.go` — Buckets enriched PRs into week ranges and formats CSV output. Also returns `weekStats` for use by stats and HTML generation.
- `stats.go` — Statistical analysis: trend windows (first 5% vs last 5% of weeks), Pearson correlation, Mann-Whitney U test. Returns `consolidatedRow` structs used by both the stats CSV and the HTML summary cards.
- `html.go` — Generates a self-contained HTML file with Chart.js. Includes summary stat cards (before/after with % change), quarterly averages table, and a dual-axis line chart.
- `contributors.go` — Per-contributor before/after Ona analysis. Ranks authors by total PR count, splits each author's PRs at their first Ona-involved PR, computes PRs/active-week for each period.
- `serve.go` — Local HTTP server that serves the HTML file with live reload via Server-Sent Events. File watcher uses modtime + size + content hash to detect changes.

## Key design decisions

- **Concurrency model**: Weeks are fetched in parallel, pagination within a week is serial. This saturates the API without hitting rate limits.
- **Percentile calculation**: Uses 1-based linear interpolation to match the awk implementation in `throughput.sh`. Do not change this without verifying output parity.
- **Ona co-authorship regex**: `(?i)Co-authored-by:.*[Oo]na.*@ona\.com` — matches the bash `jq` pattern. Case-insensitive.
- **Bot detection**: Uses the GraphQL `__typename` field. PRs from authors with `__typename == "Bot"` are excluded.
- **Default exclusions**: Hardcoded in `main.go` as `defaultExclude` (`dependabot[bot],renovate[bot]`). Additional exclusions come from the `--exclude` flag.
- **Min-PRs filtering**: `--min-prs` drops low-activity weeks (e.g. holidays) from CSV, stats, and chart output after aggregation.
- **Bottom contributor exclusion**: `--exclude-bottom-contributor-pct N` ranks all authors by total PR count across the full time range, excludes the bottom N% by headcount (ties at the boundary included), and drops their PRs entirely before aggregation.
- **HTML visualization**: Chart.js loaded from CDN, data embedded inline as JSON. The `--serve` flag injects a live-reload script via SSE. File watcher polls every 500ms using modtime + size + FNV-1a content hash.
- **Stats window**: Two mutually exclusive modes. `--compare-window-pct N` (default 5) compares first N% vs last N% of valid weeks (min 1 week per side). `--compare-ona-threshold N` splits weeks by Ona usage percentage (below vs above N%). The `windowSize` is stored on `consolidatedRow` so the HTML can display actual date ranges.
- **Quarterly averages**: Splits weeks into 4 equal groups (not calendar quarters). Last group absorbs remainder.
- **Monthly aggregation**: `--granularity monthly` groups weekly data into calendar months for stats and HTML output. CSV output remains weekly. Rate metrics (PRs/engineer, review speed, Ona %, revert %) use the median of weekly values; PR counts are summed. The last incomplete month is automatically dropped.
- **Cycle time metrics**: Two cycle time metrics are computed per PR, using the `ReadyForReviewEvent` timestamp from the GitHub GraphQL API as the split point:
  - **Coding time** (`codingTimeHours`): First commit `authoredDate` to `ReadyForReviewEvent.createdAt`. Measures pre-review development work. Only computed for PRs that were drafts and have a `ReadyForReviewEvent`; set to -1 for non-draft PRs.
  - **Review time** (`reviewTimeHours`): `ReadyForReviewEvent.createdAt` to merged (`mergedAt`). Measures time in review. Same availability constraint as coding time.
  - When `--cycle-time` is enabled, both metrics appear in stats analysis, HTML stat cards, and the chart. Without the flag, the Speed category has no time-based metrics.
  - GitHub's GraphQL `PullRequest.commits` connection returns original branch commits with real `authoredDate` values regardless of merge strategy (squash, merge, rebase). For PRs with >50 commits, a follow-up query fetches the true first commit.
- **Draft PR exclusion**: Draft PRs (`isDraft == true`) are excluded from all metrics. This matches GetDX's behavior and avoids inflating cycle times with WIP PRs that were opened early.
- **Top contributors**: `--top-contributors N` shows the top N contributors by total PR count in the HTML visualization, with before/after Ona PR throughput rates. The before/after split is per-contributor, based on the merge date of their first Ona-involved PR. PR/week is computed as total PRs / active weeks (weeks with at least one PR) in each period. Disabled by default (0). Stat card colors are context-aware: review speed and revert increases are red, all other metric increases are green.

## Testing changes

After modifying the Go code:

1. Verify it compiles: `go build ./cmd/throughput/`
2. Run a short test: `./throughput --repo gitpod-io/gitpod-next --weeks 2`
3. Test visualization: `./throughput --repo gitpod-io/gitpod-next --weeks 4 --serve`
4. Compare CSV output against the reference CSV (`throughput-gitpod-next.csv`) — the last N weeks should match if run on the same date the reference was generated.

## Reference data

`throughput-gitpod-next.csv` contains a 52-week run against `gitpod-io/gitpod-next` generated by the bash script. Use it to validate output parity when making changes to metric computation or CSV formatting. Note: the reference CSV has an older column set (no `unique_authors`, `prs_per_engineer`, `revert_count`, `pct_reverts`).

## Environment

The devcontainer includes Go 1.23 (configured in `.devcontainer/devcontainer.json`). No external dependencies beyond the Go standard library.
