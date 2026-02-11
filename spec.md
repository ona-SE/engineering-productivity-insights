# Spec: Statistical Significance of Ona Uptake Correlations

## Problem Statement

The throughput tool tracks Ona co-authorship percentage per week but provides no
statistical analysis of whether Ona adoption correlates with changes in
engineering productivity or quality. Users must manually eyeball CSV columns to
guess at trends. We need automated statistical tests that quantify the strength
and significance of correlations between Ona uptake and key metrics.

## Current State

The weekly CSV output contains these columns:
`week_start, week_end, prs_merged, total_additions, total_deletions,
total_files_changed, median_cycle_time_hours, p90_cycle_time_hours,
median_review_turnaround_hours, p90_review_turnaround_hours, avg_pr_size_lines,
pct_ona_coauthored, revert_count, pct_reverts`

Missing:
- No per-engineer throughput metric (unique authors, PRs per engineer).
- Ona uptake only tracks co-authorship via commit trailers, not PRs where Ona is
  the primary author.
- No statistical analysis of any kind.

## Requirements

### 1. New CSV Columns

Add two columns to the weekly CSV output, placed after `prs_merged`:

| Column | Definition |
|---|---|
| `unique_authors` | Count of distinct PR author logins in that week (after bot/exclusion filtering) |
| `prs_per_engineer` | `prs_merged / unique_authors`, formatted to 2 decimal places. `0.00` when no PRs. |

### 2. Expanded Ona Uptake Detection

Broaden `pct_ona_coauthored` (rename to `pct_ona_involved`) to also count PRs
where Ona is the **primary author**. A PR counts as Ona-involved if either:

- **Co-authored**: Any commit message matches the existing regex
  `(?i)Co-authored-by:.*[Oo]na.*@ona\.com`
- **Primary author**: The PR `author.login` has the prefix `ona-`
  (case-insensitive match via `strings.HasPrefix(lower(login), "ona-")`)

A PR matching both conditions is counted once (union, not sum).

### 3. Statistical Analysis Output

Add a `--stats-output <file>` CLI flag. When provided, compute and write a CSV
file with correlation analysis results. When omitted, no stats are computed.

#### Stats CSV Format

```
test,metric,ona_metric,n,value,p_value,interpretation
```

| Column | Description |
|---|---|
| `test` | `pearson` or `mann_whitney_u` |
| `metric` | The dependent variable: `prs_per_engineer`, `median_cycle_time_hours`, or `pct_reverts` |
| `ona_metric` | Always `pct_ona_involved` (the independent variable) |
| `n` | Number of data points (weeks) used |
| `value` | Pearson r coefficient, or Mann-Whitney U statistic |
| `p_value` | Two-tailed p-value, formatted to 6 decimal places |
| `interpretation` | Human-readable label: `significant` (p < 0.05), `marginal` (p < 0.10), or `not_significant` |

#### Correlation Pairs (3 pairs x 2 tests = 6 rows)

For each pair, the independent variable is `pct_ona_involved`:

1. **pct_ona_involved vs prs_per_engineer** — Does Ona uptake correlate with
   higher per-engineer throughput?
2. **pct_ona_involved vs median_cycle_time_hours** — Does Ona uptake correlate
   with faster cycle times?
3. **pct_ona_involved vs pct_reverts** — Does Ona uptake correlate with lower
   revert rates (quality)?

#### Statistical Methods

**Pearson correlation coefficient (r):**
- Measures linear relationship strength between two variables.
- Computed from weekly aggregated data points.
- p-value derived from t-distribution with n-2 degrees of freedom.
- Implemented in pure Go (no external dependencies).

**Mann-Whitney U test:**
- Non-parametric test comparing two independent groups.
- Split weeks into two groups: "high Ona" (pct_ona_involved > median
  pct_ona_involved) and "low Ona" (≤ median).
- Tests whether the metric distributions differ between groups.
- p-value approximated via normal approximation (valid for n ≥ 8 per group;
  for smaller samples, note this in stderr).
- Implemented in pure Go.

#### Edge Cases

- Weeks with 0 PRs: Exclude from correlation analysis (no meaningful metrics).
- Fewer than 4 data points: Skip analysis, write no stats file, print warning
  to stderr.
- All pct_ona_involved values identical (zero variance): Report r=0, p=1.0,
  `not_significant`. Skip Mann-Whitney (can't split into groups).

### 4. Summary Statistics Rows

In addition to the 6 correlation test rows, the stats CSV includes 3 **summary
rows** (one per metric) that describe the observed trend and attribute it to Ona
uptake.

#### Summary Row Format

Same CSV columns as correlation rows, with these semantics:

| Column | Value for summary rows |
|---|---|
| `test` | `summary` |
| `metric` | `prs_per_engineer`, `median_cycle_time_hours`, or `pct_reverts` |
| `ona_metric` | `pct_ona_involved` |
| `n` | Number of weeks used (total non-zero weeks) |
| `value` | Absolute change: `last_avg - first_avg` (formatted to 2 decimal places) |
| `p_value` | Pearson p-value (reused from the correlation test for this metric) |
| `interpretation` | Human-readable: e.g. `+15.3% change (r²=0.18, p=0.032, significant)` |

#### Trend Calculation: First 5% vs Last 5%

To measure the change over the analysis period:

1. Take all weeks with > 0 PRs merged (same filter as correlation analysis).
2. Compute window size: `floor(n * 0.05)`, minimum 1 week.
3. **First window**: average of the metric over the first `window_size` weeks
   (chronologically earliest).
4. **Last window**: average of the metric over the last `window_size` weeks
   (chronologically latest).
5. **Absolute change**: `last_avg - first_avg`.
6. **Percentage change**: `((last_avg - first_avg) / first_avg) * 100`. If
   `first_avg` is 0, report percentage as `N/A`.

#### Attribution to Ona

Each summary row includes:
- **R-squared** (`r²`): The square of the Pearson r coefficient for that metric
  pair. Represents the fraction of variance in the metric that is explained by
  Ona uptake. E.g. r²=0.18 means 18% of the variation is associated with Ona
  usage.
- **p-value**: Reused from the Pearson correlation test row for the same metric.
- **interpretation label**: Same thresholds (`significant`, `marginal`,
  `not_significant`).

These are combined in the `interpretation` column as:
`{+/-}{pct_change}% change (r²={r_squared}, p={p_value}, {significance})`

Example: `+23.5% change (r²=0.31, p=0.004, significant)`

If percentage is N/A: `+2.40 absolute change (r²=0.31, p=0.004, significant)`

### 5. Enriched PR Data Model

Add `authorLogin string` field to `enrichedPR` struct so that unique author
counting can happen during CSV aggregation.

## Acceptance Criteria

1. `go build ./cmd/throughput/` compiles without errors.
2. Running with `--weeks 12` produces CSV with the two new columns
   (`unique_authors`, `prs_per_engineer`) in every row.
3. The `pct_ona_coauthored` column (renamed to `pct_ona_involved`) counts both
   co-authored and primary-author PRs.
4. Running with `--stats-output stats.csv` produces a valid CSV with 9 rows:
   6 correlation test rows (3 metrics x 2 tests) + 3 summary rows.
5. Each summary row contains the absolute change, percentage change, r², and
   p-value in the `interpretation` column.
6. The trend window uses first 5% vs last 5% of weeks (min 1 week each).
7. Running without `--stats-output` produces no stats file and behaves
   identically to current behavior (aside from new columns).
8. All statistical computations use only the Go standard library.
9. Existing CSV column order is preserved for all pre-existing columns (new
   columns inserted after `prs_merged`).

## Implementation Approach

1. **Add `authorLogin` to `enrichedPR`** in `metrics.go`. Populate it in
   `filterPRs` from `pr.Author.Login`.

2. **Expand Ona detection** in `metrics.go`: rename `onaCoauthored` to
   `onaInvolved`, add primary-author check
   (`strings.HasPrefix(strings.ToLower(login), "ona-")`), combine with existing
   co-author regex via OR.

3. **Add unique author tracking** in `csv.go`: use a `map[string]bool` per week
   bucket to collect distinct logins. Compute `prs_per_engineer`.

4. **Update CSV header and row format** in `csv.go`: insert `unique_authors`
   and `prs_per_engineer` after `prs_merged`. Rename `pct_ona_coauthored` to
   `pct_ona_involved`.

5. **Create `stats.go`**: Implement Pearson correlation (r, p-value via
   t-distribution) and Mann-Whitney U (U statistic, p-value via normal
   approximation). Include a helper for the t-distribution CDF (regularized
   incomplete beta function or approximation). Also implement R-squared
   (simply `r * r` from Pearson).

6. **Add summary statistics** in `stats.go`: Implement the first-5%-vs-last-5%
   trend calculation. Compute window size as `max(1, floor(n * 0.05))`. Compute
   absolute and percentage change. Combine with Pearson r² and p-value into the
   interpretation string.

7. **Add `--stats-output` flag** in `main.go`. After CSV aggregation, if flag
   is set, run correlation analysis on the weekly buckets, compute summary
   stats, and write the stats CSV (6 correlation rows + 3 summary rows).

8. **Verify**: Build, run with `--weeks 2` to confirm new columns appear. Run
   with `--stats-output` to confirm stats file has 9 data rows.
