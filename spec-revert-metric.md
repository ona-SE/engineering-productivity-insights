# Spec: Add revert/rollback quality metric columns

## Problem Statement

The CSV output currently has no signal for code quality regressions. Reverts and rollbacks are a proxy for changes that broke something and had to be undone. Adding columns that count and percentage these PRs per week gives visibility into stability trends.

## Requirements

1. Add a `title` field to the GraphQL query and PR struct (not currently fetched).
2. Detect revert/rollback PRs by matching the **PR title only** (case-insensitive) against these variations:
   - `revert`
   - `reverting`
   - `rollback`
   - `roll back`
   - `rolled back`
3. Add two new CSV columns at the end of each row:
   - `revert_count` — raw number of revert/rollback PRs merged that week (integer).
   - `pct_reverts` — percentage of that week's merged PRs that are reverts, formatted as `%.1f` (e.g. `2.5`). `0.0` when no PRs exist.
4. The new columns appear after the existing `pct_ona_coauthored` column.
5. The `enrichedPR` struct gains an `isRevert bool` field, set during `filterPRs`.

## Files Changed

| File | Change |
|---|---|
| `cmd/throughput/fetch.go` | Add `Title string` to `PR` struct; add `title` to GraphQL query |
| `cmd/throughput/metrics.go` | Add revert regex; set `isRevert` on `enrichedPR` during filtering |
| `cmd/throughput/csv.go` | Add `revertCount` to `weekBucket`; append `revert_count,pct_reverts` to header and each row |

## Acceptance Criteria

- [ ] `go build ./cmd/throughput/` compiles without errors.
- [ ] CSV header ends with `...,pct_ona_coauthored,revert_count,pct_reverts`.
- [ ] A PR titled "Revert: fix login bug" is counted as a revert.
- [ ] A PR titled "Rolling back deploy" is **not** counted (does not match the specified variations).
- [ ] A PR titled "reverted the change" is **not** counted (`reverted` is not in the variation list).
- [ ] A PR titled "Rolled back feature flag" **is** counted.
- [ ] Weeks with 0 PRs show `0,0.0` for the two new columns.
- [ ] Running `--weeks 2` against `gitpod-io/gitpod-next` produces valid output with the new columns.

## Implementation Plan

1. Add `Title string` field to the `PR` struct in `fetch.go` and add `title` to the GraphQL `... on PullRequest` fragment.
2. Add a revert-detection regex to `metrics.go`: `(?i)\b(revert|reverting|rollback|roll\s+back|rolled\s+back)\b`.
3. Add `isRevert bool` to `enrichedPR` and set it in `filterPRs` by matching the PR title.
4. Add `revertCount int` to `weekBucket` in `csv.go`; increment it when `isRevert` is true.
5. Update `csvHeader` to append `,revert_count,pct_reverts`.
6. Update the CSV row formatting to append the two new values.
7. Build and test with `--weeks 2` against `gitpod-io/gitpod-next`.
