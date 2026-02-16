---
name: throughput-cli
description: Run the throughput CLI tool to compute PR metrics for GitHub repositories. Use when asked to analyze PR throughput, generate metrics, produce charts, compare engineering productivity, or run the throughput command.
---

# Throughput CLI

Compute week-over-week PR throughput metrics for GitHub repositories.

## Build and run

```sh
go build ./cmd/throughput/
./throughput --repo owner/repo --weeks 12
```

Or without building:

```sh
go run ./cmd/throughput/ --repo owner/repo --weeks 12
```

## Flags

### Required

| Flag | Default | Description |
|------|---------|-------------|
| `--repo` | Detected from git remote | `owner/repo` to analyze. Accepts full GitHub URLs, SSH URLs, or `owner/repo` shorthand. |

### Time range

| Flag | Default | Description |
|------|---------|-------------|
| `--weeks` | `12` | Number of weeks to analyze. |
| `--branch` | `main` | Target branch for merged PRs. |

### Output

| Flag | Default | Description |
|------|---------|-------------|
| `--output` | stdout | Output CSV file path. |
| `--stats-output` | (none) | Output CSV file for statistical analysis (trend windows, correlations). |
| `--html` | (none) | Output HTML file with Chart.js interactive chart, summary cards, and quarterly table. |
| `--serve` | `false` | Start a local HTTP server to view the HTML chart with live reload via SSE. Implies `--html` with default `chart.html`. |
| `--port` | `8080` | Port for the local server. Only used with `--serve`. |

### Filtering

| Flag | Default | Description |
|------|---------|-------------|
| `--exclude` | (none) | Additional usernames to exclude, comma-separated. Added to the default exclusions (`dependabot[bot]`, `renovate[bot]`). |
| `--min-prs` | `0` | Exclude periods with fewer than N merged PRs (filters holiday/low-activity periods). Applied after aggregation. |
| `--exclude-bottom-contributor-pct` | `0` | Exclude the bottom N% of contributors by total PR count across the full time range (0–99). Ties at the boundary are included in the exclusion. |

### Aggregation

| Flag | Default | Description |
|------|---------|-------------|
| `--granularity` | `weekly` | Aggregation granularity for stats and chart: `weekly` or `monthly`. CSV output remains weekly regardless. Monthly uses medians for rate metrics and sums for counts; drops the last incomplete month. |

### Comparison modes

These two flags are mutually exclusive.

| Flag | Default | Description |
|------|---------|-------------|
| `--compare-window-pct` | `5` | Compare first N% vs last N% of valid periods (1–49). Used for before/after trend analysis. |
| `--compare-ona-threshold` | `0` | Split periods by Ona usage percentage: below vs above N%. For example, `--compare-ona-threshold 70` compares weeks with <70% Ona usage against weeks with >=70%. |

## Common recipes

**Basic 12-week analysis to stdout:**
```sh
./throughput --repo owner/repo
```

**52-week analysis with chart, filtering low-activity weeks:**
```sh
./throughput --repo owner/repo --weeks 52 --min-prs 10 --serve
```

**Monthly granularity with stats export:**
```sh
./throughput --repo owner/repo --weeks 52 --granularity monthly --output data.csv --stats-output stats.csv --html report.html
```

**Exclude specific users and bottom contributors:**
```sh
./throughput --repo owner/repo --exclude "user1,user2" --exclude-bottom-contributor-pct 10
```

**Compare weeks by Ona adoption threshold:**
```sh
./throughput --repo owner/repo --weeks 52 --compare-ona-threshold 70 --serve
```

**Analyze a non-default branch:**
```sh
./throughput --repo owner/repo --branch develop --weeks 24
```

## Environment

Requires a GitHub token. Resolved in order: `GH_TOKEN`, `GITHUB_TOKEN`, git credential helper.

## Anti-patterns

- Don't use `--compare-window-pct` and `--compare-ona-threshold` together — they are mutually exclusive and the tool will error.
- Don't set `--granularity` to anything other than `weekly` or `monthly`.
- Don't set `--exclude-bottom-contributor-pct` to 100 or above — valid range is 0–99.
- Don't rely on commit-to-merge cycle time (`median_commit_to_merge_hours` in CSV) for repos using squash-and-merge — it will be nearly identical to review speed. Use review speed instead.
- Don't forget `--min-prs` when analyzing long time ranges — holiday weeks with 1–2 PRs skew medians.
