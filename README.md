# Engineering Insights

Week-over-week PR throughput metrics for any GitHub repository. Fetches merged PRs via the GitHub GraphQL API and produces a CSV with cycle time, review turnaround, PR size, and Ona co-authorship stats.

Fetches all weeks concurrently -- a 52-week analysis completes in ~1-2 minutes.

## Quick start

```sh
go run ./cmd/throughput/ --repo gitpod-io/gitpod-next --weeks 12 --output productivity-analysis-output.csv
```

That's it. No build step needed. Results are saved to `output.csv`.

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

### Examples

**Analyze the current repo (auto-detects from git remote):**

```sh
go run ./cmd/throughput/
```

**Analyze a specific repo for the last 52 weeks:**

```sh
go run ./cmd/throughput/ --repo gitpod-io/gitpod-next --weeks 52
```

**Write output to a file:**

```sh
go run ./cmd/throughput/ --repo gitpod-io/gitpod-next --weeks 12 --output report.csv
```

**Target a different branch:**

```sh
go run ./cmd/throughput/ --repo myorg/myrepo --branch develop --weeks 8
```

**Exclude additional users:**

```sh
go run ./cmd/throughput/ --repo gitpod-io/gitpod-next --exclude "staging-bot,test-user"
```

**Combine flags:**

```sh
go run ./cmd/throughput/ \
  --repo gitpod-io/gitpod-next \
  --branch main \
  --weeks 26 \
  --exclude "staging-bot" \
  --output half-year-report.csv
```

### Building a binary (optional)

If you prefer a compiled binary:

```sh
go build ./cmd/throughput/
./throughput --repo gitpod-io/gitpod-next --weeks 12
```

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
| `total_additions` | Sum of lines added |
| `total_deletions` | Sum of lines deleted |
| `total_files_changed` | Sum of files changed |
| `median_cycle_time_hours` | Median hours from first commit to merge |
| `p90_cycle_time_hours` | 90th percentile cycle time |
| `median_review_turnaround_hours` | Median hours from PR creation to first review |
| `p90_review_turnaround_hours` | 90th percentile review turnaround |
| `avg_pr_size_lines` | Average PR size (additions + deletions) / PR count |
| `pct_ona_coauthored` | Percentage of PRs with an Ona co-author |

## Default exclusions

These accounts are always excluded from metrics:

- `ona-automations`
- `ona-gha-automations[bot]`
- `dependabot[bot]`
- `renovate[bot]`

Add more with `--exclude`.

## Project structure

```
cmd/throughput/     Go source for the throughput tool
  main.go           CLI flags, repo detection, orchestration
  token.go          GitHub token resolution
  graphql.go        GraphQL client with retry/rate-limit handling
  fetch.go          Concurrent PR fetching with bounded worker pool
  metrics.go        PR filtering, cycle time, review turnaround, percentiles
  csv.go            Weekly aggregation and CSV output
spec.md             Design spec
```
