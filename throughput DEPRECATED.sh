#!/usr/bin/env bash
#
# Engineering throughput analysis for current repo.
# Produces a CSV with week-over-week PR-based metrics for the last 12 weeks.
#
# Uses GitHub GraphQL API for efficient batch fetching.
#
# Dependencies: curl, jq, awk, date (GNU coreutils or compatible)
# Auth: GH_TOKEN or GITHUB_TOKEN environment variable
#
# Usage:
#   ./throughput.sh                              # CSV to stdout (uses current repo)
#   ./throughput.sh --repo owner/repo            # analyze a different repo
#   ./throughput.sh --output report.csv          # CSV to file
#   ./throughput.sh --exclude "user1,user2"      # additional usernames to exclude
#   ./throughput.sh --branch develop             # target branch (default: main)
#   ./throughput.sh --weeks 52                   # number of weeks to analyze (default: 12)

set -euo pipefail

EXPLICIT_REPO=""
BASE_BRANCH="main"
WEEKS=12
DEFAULT_EXCLUDE="ona-automations,ona-gha-automations[bot],dependabot[bot],renovate[bot]"
API_BASE="https://api.github.com"

# --- Argument parsing -----------------------------------------------------------

OUTPUT=""
EXTRA_EXCLUDE=""

while [[ $# -gt 0 ]]; do
    case "$1" in
        --output)
            OUTPUT="$2"
            shift 2
            ;;
        --exclude)
            EXTRA_EXCLUDE="$2"
            shift 2
            ;;
        --repo)
            EXPLICIT_REPO="$2"
            shift 2
            ;;
        --branch)
            BASE_BRANCH="$2"
            shift 2
            ;;
        --weeks)
            WEEKS="$2"
            shift 2
            ;;
        --help|-h)
            echo "Usage: $0 [--repo owner/repo] [--branch BRANCH] [--weeks N] [--output FILE] [--exclude user1,user2]" >&2
            exit 0
            ;;
        *)
            echo "Unknown argument: $1" >&2
            exit 1
            ;;
    esac
done

# --- Resolve owner/repo ---------------------------------------------------------

if [[ -n "$EXPLICIT_REPO" ]]; then
    # Parse owner/repo from explicit argument (accepts "owner/repo" or full GitHub URL)
    OWNER_REPO=$(echo "$EXPLICIT_REPO" | sed -E 's#https?://github\.com/##; s#\.git$##; s#/$##; s#/tree/.*$##')
    OWNER=$(echo "$OWNER_REPO" | cut -d'/' -f1)
    REPO=$(echo "$OWNER_REPO" | cut -d'/' -f2)
else
    # Detect owner/repo from the git remote of the current directory
    REMOTE_URL=$(git remote get-url origin 2>/dev/null) || {
        echo "ERROR: Not in a git repository or no 'origin' remote found." >&2
        echo "  Use --repo owner/repo to specify a repository explicitly." >&2
        exit 1
    }
    OWNER_REPO=$(echo "$REMOTE_URL" | sed -E 's#(https?://[^/]+/|git@[^:]+:)##; s#\.git$##')
    OWNER=$(echo "$OWNER_REPO" | cut -d'/' -f1)
    REPO=$(echo "$OWNER_REPO" | cut -d'/' -f2)
fi

if [[ -z "$OWNER" || -z "$REPO" ]]; then
    echo "ERROR: Could not parse owner/repo from: ${EXPLICIT_REPO:-$REMOTE_URL}" >&2
    exit 1
fi

echo "Repository: ${OWNER}/${REPO} (branch: ${BASE_BRANCH})" >&2

EXCLUDE_LIST="${DEFAULT_EXCLUDE}"
if [[ -n "$EXTRA_EXCLUDE" ]]; then
    EXCLUDE_LIST="${EXCLUDE_LIST},${EXTRA_EXCLUDE}"
fi
EXCLUDE_LIST_LOWER=$(echo "$EXCLUDE_LIST" | tr '[:upper:]' '[:lower:]')

# --- Prerequisite checks -------------------------------------------------------


if ! command -v curl &>/dev/null; then
    echo "ERROR: curl not found." >&2
    exit 1
fi

if ! command -v jq &>/dev/null; then
    echo "ERROR: jq not found." >&2
    exit 1
fi

AUTH_TOKEN="${GH_TOKEN:-${GITHUB_TOKEN:-}}"

# If no token in environment, try to extract from git credential helper
if [[ -z "$AUTH_TOKEN" ]]; then
    CRED_HELPER=$(git config --get credential.github.com.helper 2>/dev/null | sed "s/^!f() { cat '//; s/'; }; f$//")
    if [[ -n "$CRED_HELPER" ]] && [[ -f "$CRED_HELPER" ]]; then
        AUTH_TOKEN=$(grep '^password=' "$CRED_HELPER" 2>/dev/null | cut -d'=' -f2)
    fi
fi

if [[ -z "$AUTH_TOKEN" ]]; then
    echo "ERROR: No GitHub token found." >&2
    echo "  Tried: GH_TOKEN, GITHUB_TOKEN environment variables, and git credential helper" >&2
    exit 1
fi

# --- GraphQL helper -------------------------------------------------------------

graphql() {
    local query="$1"
    local attempt
    for attempt in 1 2 3; do
        local resp
        resp=$(curl -sS -X POST \
            -H "Authorization: bearer ${AUTH_TOKEN}" \
            -H "Content-Type: application/json" \
            "${API_BASE}/graphql" \
            -d "$query" 2>/dev/null) || { sleep 5; continue; }

        # Check for rate limit errors
        local has_errors
        has_errors=$(echo "$resp" | jq 'has("errors")' 2>/dev/null)
        if [[ "$has_errors" == "true" ]]; then
            local error_type
            error_type=$(echo "$resp" | jq -r '.errors[0].type // ""' 2>/dev/null)
            if [[ "$error_type" == "RATE_LIMITED" ]]; then
                echo "  Rate limited, waiting 60s (attempt $attempt)..." >&2
                sleep 60
                continue
            fi
        fi

        echo "$resp"
        return 0
    done
    return 1
}

# --- Date range calculation -----------------------------------------------------

TODAY=$(date +%Y-%m-%d)
DOW=$(date +%u)
DAYS_SINCE_MONDAY=$(( DOW - 1 ))
CURRENT_MONDAY=$(date -d "$TODAY - $DAYS_SINCE_MONDAY days" +%Y-%m-%d)
START_DATE=$(date -d "$CURRENT_MONDAY - $WEEKS weeks" +%Y-%m-%d)

echo "Analyzing PRs merged from $START_DATE to $TODAY ($WEEKS weeks)" >&2
echo "Exclude list: $EXCLUDE_LIST" >&2

declare -a WEEK_STARTS
declare -a WEEK_ENDS
for (( i=0; i<WEEKS; i++ )); do
    ws=$(date -d "$START_DATE + $((i)) weeks" +%Y-%m-%d)
    we=$(date -d "$ws + 6 days" +%Y-%m-%d)
    WEEK_STARTS+=("$ws")
    WEEK_ENDS+=("$we")
done

# --- Fetch all merged PRs via GraphQL search (paginated) ------------------------

echo "Fetching merged PRs via GraphQL..." >&2

ALL_PRS_FILE=$(mktemp)
ENRICHED_DATA=$(mktemp)
trap 'rm -f "$ALL_PRS_FILE" "$ENRICHED_DATA" 2>/dev/null' EXIT

# Query per-week to avoid the 1000-result search cap
TOTAL_FETCHED=0

for (( w=0; w<WEEKS; w++ )); do
    RANGE_START="${WEEK_STARTS[$w]}"
    RANGE_END="${WEEK_ENDS[$w]}"

    HAS_NEXT="true"
    CURSOR=""
    WEEK_FETCHED=0

    while [[ "$HAS_NEXT" == "true" ]]; do
        AFTER_CLAUSE=""
        if [[ -n "$CURSOR" ]]; then
            AFTER_CLAUSE=", after: \"$CURSOR\""
        fi

        GQL_QUERY=$(jq -n --arg search "repo:${OWNER}/${REPO} is:pr is:merged base:${BASE_BRANCH} merged:${RANGE_START}..${RANGE_END}" \
            --arg after_clause "$AFTER_CLAUSE" \
            '{query: ("query { search(query: " + ($search | tojson) + ", type: ISSUE, first: 100" + $after_clause + ") { pageInfo { hasNextPage endCursor } nodes { ... on PullRequest { number createdAt mergedAt additions deletions changedFiles author { login ... on Bot { __typename } ... on User { __typename } } commits(first: 50) { nodes { commit { authoredDate message } } } reviews(first: 1) { nodes { submittedAt } } } } } }")}')

        RESULT=$(graphql "$GQL_QUERY") || {
            echo "ERROR: GraphQL query failed for week $RANGE_START" >&2
            exit 1
        }

        ERRORS=$(echo "$RESULT" | jq -r '.errors[0].message // empty' 2>/dev/null)
        if [[ -n "$ERRORS" ]]; then
            echo "  GraphQL error (week $RANGE_START): $ERRORS" >&2
        fi

        echo "$RESULT" | jq -c '.data.search.nodes[]' >> "$ALL_PRS_FILE" 2>/dev/null

        PAGE_COUNT=$(echo "$RESULT" | jq '.data.search.nodes | length' 2>/dev/null)
        WEEK_FETCHED=$((WEEK_FETCHED + PAGE_COUNT))

        HAS_NEXT=$(echo "$RESULT" | jq -r '.data.search.pageInfo.hasNextPage' 2>/dev/null)
        CURSOR=$(echo "$RESULT" | jq -r '.data.search.pageInfo.endCursor' 2>/dev/null)

        sleep 0.3
    done

    TOTAL_FETCHED=$((TOTAL_FETCHED + WEEK_FETCHED))
    echo "  Week $RANGE_START: $WEEK_FETCHED PRs (total: $TOTAL_FETCHED)" >&2
done

echo "Total PRs fetched: $TOTAL_FETCHED" >&2

# --- Filter and enrich ----------------------------------------------------------

echo "Processing PRs..." >&2

# Single jq pass over all PRs for performance (avoids per-PR subprocess overhead)
jq -r --arg exclude_list "$EXCLUDE_LIST_LOWER" '
    ($exclude_list | split(",")) as $excludes |
    (.author.__typename // "User") as $atype |
    (.author.login // "" | ascii_downcase) as $login |
    if $atype == "Bot" then empty
    elif ($excludes | any(. == $login)) then empty
    elif (.mergedAt // "") == "" then empty
    else
        def parse_ts: sub("\\.[0-9]+"; "") | sub("\\+00:00$"; "Z") | fromdate;
        (.mergedAt | parse_ts) as $merged_epoch |
        (.createdAt | parse_ts) as $created_epoch |
        (.commits.nodes[0].commit.authoredDate // null) as $first_commit |
        (if $first_commit != null then
            ($first_commit | parse_ts) as $fc_epoch |
            if $merged_epoch >= $fc_epoch then
                (($merged_epoch - $fc_epoch) / 3600 * 100 | round / 100 | tostring)
            else "" end
        else "" end) as $cycle_hours |
        (.reviews.nodes[0].submittedAt // null) as $first_review |
        (if $first_review != null then
            ($first_review | parse_ts) as $rev_epoch |
            if $rev_epoch >= $created_epoch then
                (($rev_epoch - $created_epoch) / 3600 * 100 | round / 100 | tostring)
            else "" end
        else "" end) as $review_hours |
        ([.commits.nodes[].commit.message] | any(test("Co-authored-by:.*[Oo]na.*@ona\\.com"; "i"))) as $ona |
        "\($merged_epoch)|\($cycle_hours)|\($review_hours)|\(.additions // 0)|\(.deletions // 0)|\(.changedFiles // 0)|\(.number)|\(if $ona then 1 else 0 end)"
    end
' "$ALL_PRS_FILE" > "$ENRICHED_DATA"

FILTERED_COUNT=$(wc -l < "$ENRICHED_DATA")
EXCLUDED_COUNT=$((TOTAL_FETCHED - FILTERED_COUNT))
echo "Processed: $FILTERED_COUNT PRs ($EXCLUDED_COUNT excluded)" >&2

# --- Bucketing and aggregation --------------------------------------------------

echo "Aggregating by week..." >&2

aggregate() {
    awk -v weeks="$WEEKS" \
        -v week_starts="$(IFS=,; echo "${WEEK_STARTS[*]}")" \
        -v week_ends="$(IFS=,; echo "${WEEK_ENDS[*]}")" \
    '
    BEGIN {
        FS="|"
        split(week_starts, ws, ",")
        split(week_ends, we, ",")

        for (i=1; i<=weeks; i++) {
            cmd = "date -d \"" ws[i] "\" +%s"
            cmd | getline ws_epoch[i]
            close(cmd)
            cmd = "date -d \"" we[i] " 23:59:59\" +%s"
            cmd | getline we_epoch[i]
            close(cmd)
        }
    }

    {
        merged_epoch = $1
        cycle_hours = $2
        review_hours = $3
        additions = $4 + 0
        deletions = $5 + 0
        changed_files = $6 + 0
        ona_flag = $8 + 0

        for (i=1; i<=weeks; i++) {
            if (merged_epoch >= ws_epoch[i] && merged_epoch <= we_epoch[i]) {
                count[i]++
                sum_add[i] += additions
                sum_del[i] += deletions
                sum_files[i] += changed_files
                ona_count[i] += ona_flag

                if (cycle_hours != "") {
                    cycle_n[i]++
                    cycle_vals[i, cycle_n[i]] = cycle_hours + 0
                }
                if (review_hours != "") {
                    review_n[i]++
                    review_vals[i, review_n[i]] = review_hours + 0
                }
                break
            }
        }
    }

    function sort_array(arr, n,    i, j, tmp) {
        for (i=2; i<=n; i++) {
            tmp = arr[i]
            j = i - 1
            while (j >= 1 && arr[j] > tmp) {
                arr[j+1] = arr[j]
                j--
            }
            arr[j+1] = tmp
        }
    }

    function percentile(bucket, vals, n, pct,    sorted, idx, lower, frac) {
        if (n == 0) return ""
        for (k=1; k<=n; k++) sorted[k] = vals[bucket, k]
        sort_array(sorted, n)

        if (n == 1) return sprintf("%.2f", sorted[1])

        idx = (pct / 100.0) * (n - 1) + 1
        lower = int(idx)
        if (lower < 1) lower = 1
        if (lower >= n) return sprintf("%.2f", sorted[n])
        frac = idx - lower
        return sprintf("%.2f", sorted[lower] + frac * (sorted[lower+1] - sorted[lower]))
    }

    function median(bucket, vals, n) {
        return percentile(bucket, vals, n, 50)
    }

    function p90(bucket, vals, n) {
        return percentile(bucket, vals, n, 90)
    }

    END {
        print "week_start,week_end,prs_merged,total_additions,total_deletions,total_files_changed,median_cycle_time_hours,p90_cycle_time_hours,median_review_turnaround_hours,p90_review_turnaround_hours,avg_pr_size_lines,pct_ona_coauthored"

        for (i=1; i<=weeks; i++) {
            c = count[i] + 0
            sa = sum_add[i] + 0
            sd = sum_del[i] + 0
            sf = sum_files[i] + 0
            oc = ona_count[i] + 0

            med_cycle = median(i, cycle_vals, cycle_n[i] + 0)
            p90_cycle = p90(i, cycle_vals, cycle_n[i] + 0)
            med_review = median(i, review_vals, review_n[i] + 0)
            p90_review = p90(i, review_vals, review_n[i] + 0)

            if (c > 0) {
                avg_size = sprintf("%.2f", (sa + sd) / c)
                pct_ona = sprintf("%.1f", (oc / c) * 100)
            } else {
                avg_size = "0.00"
                pct_ona = "0.0"
            }

            printf "%s,%s,%d,%d,%d,%d,%s,%s,%s,%s,%s,%s\n", \
                ws[i], we[i], c, sa, sd, sf, \
                med_cycle, p90_cycle, med_review, p90_review, avg_size, pct_ona
        }
    }
    ' "$ENRICHED_DATA"
}

# --- CSV output -----------------------------------------------------------------

if [[ -n "$OUTPUT" ]]; then
    aggregate > "$OUTPUT"
    echo "CSV written to $OUTPUT" >&2
else
    aggregate
fi

echo "Done." >&2
