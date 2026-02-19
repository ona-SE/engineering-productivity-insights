# Engineering Insights

Week-over-week PR throughput metrics for any GitHub repository. Fetches merged PRs via the GitHub GraphQL API and produces a CSV with review speed (open-to-merge), review turnaround, PR size, revert tracking, and Ona co-authorship stats. Includes a built-in interactive chart visualization.

Fetches all weeks concurrently -- a 52-week analysis completes in ~1-2 minutes.

## Quick start

```sh
# CSV output
go run ./cmd/throughput/ --repo gitpod-io/gitpod-next --weeks 12 --output report.csv

# Interactive chart with live reload
go run ./cmd/throughput/ --repo gitpod-io/gitpod-next --weeks 52 --min-prs 10 --serve
```

## Usage

```
go run ./cmd/throughput/ [flags]
```

### Flags

| Flag | Default | Description |
|---|---|---|
| `--repo` | auto-detect from git remote | Repository as `owner/repo` |
| `--branch` | `main` | Target branch to scope merged PRs |
| `--weeks` | `12` | Number of weeks to analyze |
| `--output` | stdout | Write CSV to a file instead of stdout |
| `--exclude` | — | Additional usernames to exclude (comma-separated) |
| `--stats-output` | — | Write statistical analysis CSV to a file |
| `--html` | — | Write interactive HTML chart to a file |
| `--serve` | `false` | Start a local server to view the chart (implies `--html chart.html`) |
| `--port` | `8080` | Port for the local server (used with `--serve`) |
| `--min-prs` | `0` | Exclude weeks with fewer than N merged PRs (e.g. holiday weeks) |
| `--exclude-bottom-contributor-pct` | `0` | Exclude bottom N% of contributors by total PR count (0-99) |
| `--granularity` | `weekly` | Aggregation for stats and chart: `weekly` or `monthly` |
| `--compare-window-pct` | `5` | Compare first/last N% of periods (1-49) |
| `--compare-ona-threshold` | `0` | Compare periods below vs above N% Ona usage (e.g. 70) |
| `--top-contributors` | `0` | Show top N contributors with before/after Ona PR rates in HTML (0 = disabled) |
| `--cycle-time` | `false` | Show coding time (first commit → ready for review) and review time (ready for review → merged) in stats, chart, and stat cards |

`--compare-window-pct` and `--compare-ona-threshold` are mutually exclusive.

When `--granularity monthly` is used, weekly data is grouped into calendar months for the stats analysis and HTML chart. The CSV output remains weekly. Rate metrics (PRs/engineer, review speed, Ona %, revert %) use the median of weekly values; PR counts are summed. The last incomplete month is automatically dropped.

### Examples

```sh
# Auto-detect repo from git remote, output CSV to stdout
go run ./cmd/throughput/

# 52-week analysis with chart, excluding holiday weeks
go run ./cmd/throughput/ --repo gitpod-io/gitpod-next --weeks 52 --min-prs 10 --serve

# Exclude bottom 25% of contributors by PR count
go run ./cmd/throughput/ --repo gitpod-io/gitpod-next --weeks 52 --exclude-bottom-contributor-pct 25 --serve

# CSV + HTML + stats all at once
go run ./cmd/throughput/ --repo gitpod-io/gitpod-next --weeks 26 \
  --output report.csv --html chart.html --stats-output stats.csv

# Monthly aggregation for smoother trends
go run ./cmd/throughput/ --repo gitpod-io/gitpod-next --weeks 52 --min-prs 10 --granularity monthly --serve

# Show top 5 contributors with before/after Ona throughput
go run ./cmd/throughput/ --repo gitpod-io/gitpod-next --weeks 52 --top-contributors 5 --serve

# Exclude additional users
go run ./cmd/throughput/ --repo gitpod-io/gitpod-next --exclude "staging-bot,test-user"

# Show coding time and review time breakdown in stats and chart
go run ./cmd/throughput/ --repo gitpod-io/gitpod-next --weeks 52 --cycle-time --serve
```

### Building a binary (optional)

```sh
go build ./cmd/throughput/
./throughput --repo gitpod-io/gitpod-next --weeks 12 --serve
```

## Visualization

When using `--serve` or `--html`, the tool generates a self-contained HTML file with:

- **Summary stat cards** showing before/after comparison with percentage change (first 5% vs last 5% of weeks). Colors are context-aware: review speed and revert increases are red.
- **Dual-axis line chart** with:
  - Left axis: PRs merged
  - Right axis 1: % Ona involved, % reverts (0-100%)
  - Right axis 2: PRs per engineer, review speed (hrs)

- **Top contributors** (with `--top-contributors N`): Shows the top N contributors ranked by total PR count, with per-contributor before/after Ona PR throughput rates. The split point is each contributor's first Ona-involved PR.

The `--serve` flag starts a local HTTP server with live reload — the browser automatically refreshes when the HTML file changes on disk.

## Authentication

The tool looks for a GitHub token in this order:

1. `GH_TOKEN` environment variable
2. `GITHUB_TOKEN` environment variable
3. Git credential helper for `github.com`

In Gitpod environments, the credential helper is configured automatically.

## Output format

The CSV contains one row per week with these columns:

| Column | Description |
|---|---|
| `week_start` | Monday of the week (YYYY-MM-DD) |
| `week_end` | Sunday of the week (YYYY-MM-DD) |
| `prs_merged` | Number of PRs merged that week |
| `unique_authors` | Number of distinct PR authors |
| `prs_per_engineer` | PRs merged / unique authors |
| `total_additions` | Sum of lines added |
| `total_deletions` | Sum of lines deleted |
| `total_files_changed` | Sum of files changed |
| `median_review_speed_hours` | Median hours from PR opened to merge |
| `p90_review_speed_hours` | 90th percentile review speed |
| `median_commit_to_merge_hours` | Median hours from first commit to merge (see note below) |
| `p90_commit_to_merge_hours` | 90th percentile commit-to-merge time |
| `median_review_turnaround_hours` | Median hours from PR creation to first review |
| `p90_review_turnaround_hours` | 90th percentile review turnaround |
| `avg_pr_size_lines` | Average PR size (additions + deletions) / PR count |
| `pct_ona_involved` | Percentage of PRs with Ona co-authorship |
| `revert_count` | Number of revert PRs |
| `pct_reverts` | Percentage of PRs that are reverts |

### Cycle time metrics

When `--cycle-time` is enabled, the tool splits the development cycle into two phases using the `ReadyForReviewEvent` from the GitHub GraphQL API:

- **Coding time** (`median_coding_time_hours`) — time from first commit `authoredDate` to when the PR was marked ready for review. Measures pre-review development work.
- **Review time** (`median_review_time_hours`) — time from ready for review to merged. Measures time spent in code review.

Only PRs that were created as drafts and later marked ready for review contribute to these metrics. Non-draft PRs (which have no `ReadyForReviewEvent`) are excluded from the coding/review split but still count toward PR volume, Ona %, and other metrics.

Both metrics appear in the stats analysis, HTML stat cards, and the chart when `--cycle-time` is enabled. Without the flag, no time-based speed metrics are shown.

Works for all repos including those using squash-and-merge — GitHub's GraphQL API returns the original branch commits on the PR object regardless of merge strategy. For PRs with more than 50 commits, a targeted follow-up query fetches the true first commit.

Draft PRs (still in draft at time of analysis) are excluded from all metrics.

## Default exclusions

These accounts are always excluded from metrics:

- `dependabot[bot]`
- `renovate[bot]`

Add more with `--exclude`.

## Project structure

```
cmd/throughput/
  main.go           CLI flags, repo detection, orchestration
  token.go          GitHub token resolution
  graphql.go        GraphQL client with retry/rate-limit handling
  fetch.go          Concurrent PR fetching with bounded worker pool
  metrics.go        PR filtering, cycle time, review turnaround, percentiles
  contributors.go   Per-contributor before/after Ona analysis
  csv.go            Weekly aggregation and CSV output
  monthly.go        Monthly aggregation of weekly stats (medians for rates, sums for counts)
  stats.go          Statistical analysis (trend windows, Pearson correlation, Mann-Whitney U)
  html.go           HTML chart generation (Chart.js template, summary cards, quarterly table)
  serve.go          Local HTTP server with file-watching live reload
```
