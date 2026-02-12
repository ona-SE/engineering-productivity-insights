package main

import (
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
)

// --- Metric definitions ---

// metricDef defines how to extract a metric from weekly data.
type metricDef struct {
	name    string
	extract func(ws weekStats) float64
	valid   func(ws weekStats) bool
}

// allMetrics defines the 6 rows in the consolidated stats CSV.
var allMetrics = []metricDef{
	{
		name:    "prs_merged",
		extract: func(ws weekStats) float64 { return float64(ws.prsMerged) },
		valid:   func(ws weekStats) bool { return ws.prsMerged > 0 },
	},
	{
		name:    "unique_authors",
		extract: func(ws weekStats) float64 { return float64(ws.uniqueAuthors) },
		valid:   func(ws weekStats) bool { return ws.prsMerged > 0 },
	},
	{
		name:    "prs_per_engineer",
		extract: func(ws weekStats) float64 { return ws.prsPerEngineer },
		valid:   func(ws weekStats) bool { return ws.prsMerged > 0 },
	},
	{
		name:    "median_review_speed_hours",
		extract: func(ws weekStats) float64 { return ws.medianReviewSpeed },
		valid:   func(ws weekStats) bool { return ws.prsMerged > 0 && ws.medianReviewSpeed >= 0 },
	},
	{
		name:    "pct_reverts",
		extract: func(ws weekStats) float64 { return ws.pctReverts },
		valid:   func(ws weekStats) bool { return ws.prsMerged > 0 },
	},
	{
		name:    "pct_ona_involved",
		extract: func(ws weekStats) float64 { return ws.pctOnaInvolved },
		valid:   func(ws weekStats) bool { return ws.prsMerged > 0 },
	},
}

// Extractor for the Ona attribution variable.
func extractOna(ws weekStats) float64 { return ws.pctOnaInvolved }

// --- Consolidated stats row ---

type consolidatedRow struct {
	metric          string
	n               int
	windowSize      int // for positional windows (same on both sides)
	firstWindowSize int // for threshold windows (may differ)
	lastWindowSize  int
	firstAvg        float64
	lastAvg         float64
	absChange       float64
	pctChange       string // formatted, or "N/A"
	window          string
	r2Ona           string // empty if self-referential
	pPearsonOna     string
	pMannWhitneyOna string
	sigOna          string
}

const consolidatedHeader = "metric,n,first_avg,last_avg,abs_change,pct_change,window,r2_ona,p_pearson_ona,p_mann_whitney_ona,significance_ona"

// --- Main entry point ---

// generateStats produces the consolidated 6-row stats CSV.
// It returns both the CSV string and the parsed rows for use by the HTML generator.
func generateStats(allStats []weekStats, windowPct int, onaThreshold float64, periodLabel string) (string, []consolidatedRow) {
	// Compute overall average PRs/week (across all non-zero weeks)
	var totalPRs int
	var nonZeroCount int
	for _, ws := range allStats {
		if ws.prsMerged > 0 {
			totalPRs += ws.prsMerged
			nonZeroCount++
		}
	}
	if nonZeroCount == 0 {
		fmt.Fprintf(os.Stderr, "WARNING: No non-empty weeks. Skipping stats.\n")
		return "", nil
	}
	avgPRs := float64(totalPRs) / float64(nonZeroCount)
	threshold := avgPRs * 0.10

	// Filter out weeks below 10% of overall average PRs/week
	var valid []weekStats
	var excluded int
	for _, ws := range allStats {
		if ws.prsMerged > 0 && float64(ws.prsMerged) >= threshold {
			valid = append(valid, ws)
		} else if ws.prsMerged > 0 {
			excluded++
		}
	}
	if excluded > 0 {
		fmt.Fprintf(os.Stderr, "Stats: excluded %d week(s) below %.0f PRs (10%% of avg %.1f)\n", excluded, threshold, avgPRs)
	}

	if len(valid) < 4 {
		fmt.Fprintf(os.Stderr, "WARNING: Only %d weeks after filtering — need at least 4 for stats. Skipping.\n", len(valid))
		return "", nil
	}

	// Extract Ona values (independent variable)
	onaVals := make([]float64, len(valid))
	for i, ws := range valid {
		onaVals[i] = extractOna(ws)
	}

	var rows []consolidatedRow

	for _, md := range allMetrics {
		row := buildRow(md, valid, onaVals, windowPct, onaThreshold, periodLabel)
		if row != nil {
			rows = append(rows, *row)
		}
	}

	if len(rows) == 0 {
		return "", nil
	}

	// Build CSV
	var sb strings.Builder
	sb.WriteString(consolidatedHeader)
	sb.WriteByte('\n')
	for _, r := range rows {
		fmt.Fprintf(&sb, "%s,%d,%s,%s,%s,%s,%s,%s,%s,%s,%s\n",
			r.metric, r.n,
			r.fmtFirstAvg(), r.fmtLastAvg(), r.fmtAbsChange(), r.pctChange, r.window,
			r.r2Ona, r.pPearsonOna, r.pMannWhitneyOna,
			r.sigOna)
	}
	return sb.String(), rows
}

func (r *consolidatedRow) fmtFirstAvg() string  { return fmt.Sprintf("%.2f", r.firstAvg) }
func (r *consolidatedRow) fmtLastAvg() string   { return fmt.Sprintf("%.2f", r.lastAvg) }
func (r *consolidatedRow) fmtAbsChange() string { return fmt.Sprintf("%.2f", r.absChange) }

// buildRow constructs one consolidated row for a metric.
func buildRow(md metricDef, valid []weekStats, onaVals []float64, windowPct int, onaThreshold float64, periodLabel string) *consolidatedRow {
	var firstAvg, lastAvg float64
	var n, firstWinSize, lastWinSize int
	var window string
	var ok bool

	if onaThreshold > 0 {
		firstAvg, lastAvg, n, firstWinSize, lastWinSize, ok = thresholdWindow(valid, md, onaThreshold)
		if !ok {
			return nil
		}
		abbrev := "w"
		if periodLabel == "month" {
			abbrev = "mo"
		}
		window = fmt.Sprintf("below %.0f%% Ona (%d%s) vs above %.0f%% Ona (%d%s)", onaThreshold, firstWinSize, abbrev, onaThreshold, lastWinSize, abbrev)
	} else {
		var winSize int
		firstAvg, lastAvg, n, winSize, ok = trendWindow(valid, md, windowPct)
		if !ok {
			return nil
		}
		firstWinSize = winSize
		lastWinSize = winSize
		abbrev := "w"
		if periodLabel == "month" {
			abbrev = "mo"
		}
		window = fmt.Sprintf("first %d%s vs last %d%s avg", winSize, abbrev, winSize, abbrev)
	}

	absChange := lastAvg - firstAvg
	var pctChange string
	if firstAvg != 0 {
		pct := (absChange / math.Abs(firstAvg)) * 100
		sign := "+"
		if pct < 0 {
			sign = ""
		}
		pctChange = fmt.Sprintf("%s%.1f%%", sign, pct)
	} else {
		pctChange = "N/A"
	}

	// Extract metric values aligned with valid weeks
	metricVals := make([]float64, 0, len(valid))
	onaAligned := make([]float64, 0, len(valid))
	for i, ws := range valid {
		if md.valid(ws) {
			metricVals = append(metricVals, md.extract(ws))
			onaAligned = append(onaAligned, onaVals[i])
		}
	}

	row := &consolidatedRow{
		metric:          md.name,
		windowSize:      firstWinSize,
		firstWindowSize: firstWinSize,
		lastWindowSize:  lastWinSize,
		n:               n,
		firstAvg:        firstAvg,
		lastAvg:         lastAvg,
		absChange:       absChange,
		pctChange:       pctChange,
		window:          window,
	}

	// Ona correlation (skip if metric IS pct_ona_involved)
	if md.name != "pct_ona_involved" && len(metricVals) >= 4 {
		r, pPearson := pearsonCorrelation(onaAligned, metricVals)
		_, pMW, mwOK := mannWhitneyU(onaAligned, metricVals)
		row.r2Ona = fmt.Sprintf("%.4f", r*r)
		row.pPearsonOna = fmt.Sprintf("%.6f", pPearson)
		if mwOK {
			row.pMannWhitneyOna = fmt.Sprintf("%.6f", pMW)
		}
		row.sigOna = interpretPValue(pPearson)
	}

	return row
}

// --- Trend windowing ---

// trendWindow computes the first-N%-vs-last-N% averages for a metric.
func trendWindow(weeks []weekStats, md metricDef, windowPct int) (float64, float64, int, int, bool) {
	var values []float64
	for _, ws := range weeks {
		if md.valid(ws) {
			values = append(values, md.extract(ws))
		}
	}
	n := len(values)
	if n < 2 {
		return 0, 0, n, 0, false
	}

	windowSize := int(math.Floor(float64(n) * float64(windowPct) / 100.0))
	if windowSize < 1 {
		windowSize = 1
	}

	var firstSum float64
	for i := 0; i < windowSize; i++ {
		firstSum += values[i]
	}
	firstAvg := firstSum / float64(windowSize)

	var lastSum float64
	for i := n - windowSize; i < n; i++ {
		lastSum += values[i]
	}
	lastAvg := lastSum / float64(windowSize)

	return firstAvg, lastAvg, n, windowSize, true
}

// thresholdWindow splits weeks by Ona usage threshold and computes averages for each group.
func thresholdWindow(weeks []weekStats, md metricDef, threshold float64) (float64, float64, int, int, int, bool) {
	var belowVals, aboveVals []float64
	for _, ws := range weeks {
		if !md.valid(ws) {
			continue
		}
		v := md.extract(ws)
		if ws.pctOnaInvolved < threshold {
			belowVals = append(belowVals, v)
		} else {
			aboveVals = append(aboveVals, v)
		}
	}
	if len(belowVals) == 0 || len(aboveVals) == 0 {
		return 0, 0, 0, 0, 0, false
	}

	var belowSum float64
	for _, v := range belowVals {
		belowSum += v
	}
	var aboveSum float64
	for _, v := range aboveVals {
		aboveSum += v
	}

	n := len(belowVals) + len(aboveVals)
	return belowSum / float64(len(belowVals)), aboveSum / float64(len(aboveVals)),
		n, len(belowVals), len(aboveVals), true
}

// --- Significance interpretation ---

func interpretPValue(p float64) string {
	if p < 0.05 {
		return "significant"
	}
	if p < 0.10 {
		return "marginal"
	}
	return "not_significant"
}

// --- Pearson correlation ---

func pearsonCorrelation(x, y []float64) (float64, float64) {
	n := len(x)
	if n < 3 {
		return 0, 1.0
	}

	var sumX, sumY float64
	for i := 0; i < n; i++ {
		sumX += x[i]
		sumY += y[i]
	}
	meanX := sumX / float64(n)
	meanY := sumY / float64(n)

	var cov, varX, varY float64
	for i := 0; i < n; i++ {
		dx := x[i] - meanX
		dy := y[i] - meanY
		cov += dx * dy
		varX += dx * dx
		varY += dy * dy
	}

	if varX == 0 || varY == 0 {
		return 0, 1.0
	}

	r := cov / math.Sqrt(varX*varY)
	if r > 1 {
		r = 1
	}
	if r < -1 {
		r = -1
	}

	df := float64(n - 2)
	t := r * math.Sqrt(df/(1-r*r))
	p := 2 * tDistCDF(-math.Abs(t), df)

	return r, p
}

// --- Mann-Whitney U test ---

// mannWhitneyU splits by median of the first array (grouping variable),
// compares distributions of the second array. Returns (U, p-value, ok).
func mannWhitneyU(groupVals, metricVals []float64) (float64, float64, bool) {
	n := len(groupVals)
	if n < 4 {
		return 0, 1.0, false
	}

	sorted := make([]float64, n)
	copy(sorted, groupVals)
	sort.Float64s(sorted)
	med := percentile(sorted, 50)

	var highGroup, lowGroup []float64
	for i := 0; i < n; i++ {
		if groupVals[i] > med {
			highGroup = append(highGroup, metricVals[i])
		} else {
			lowGroup = append(lowGroup, metricVals[i])
		}
	}

	n1 := len(lowGroup)
	n2 := len(highGroup)

	if n1 == 0 || n2 == 0 {
		return 0, 1.0, false
	}

	if n1 < 8 || n2 < 8 {
		fmt.Fprintf(os.Stderr, "NOTE: Mann-Whitney groups are small (n1=%d, n2=%d). Normal approximation may be imprecise.\n", n1, n2)
	}

	var u float64
	for _, v1 := range lowGroup {
		for _, v2 := range highGroup {
			if v1 < v2 {
				u += 1
			} else if v1 == v2 {
				u += 0.5
			}
		}
	}

	meanU := float64(n1*n2) / 2.0
	sigmaU := math.Sqrt(float64(n1*n2*(n1+n2+1)) / 12.0)

	if sigmaU == 0 {
		return u, 1.0, true
	}

	z := (u - meanU) / sigmaU
	p := 2 * normalCDF(-math.Abs(z))

	return u, p, true
}

// --- Distribution functions ---

func normalCDF(x float64) float64 {
	return 0.5 * math.Erfc(-x/math.Sqrt2)
}

func tDistCDF(t float64, df float64) float64 {
	if df <= 0 {
		return 0.5
	}
	x := df / (df + t*t)
	beta := regIncBeta(df/2.0, 0.5, x)
	if t >= 0 {
		return 1.0 - 0.5*beta
	}
	return 0.5 * beta
}

func regIncBeta(a, b, x float64) float64 {
	if x < 0 || x > 1 {
		return 0
	}
	if x == 0 {
		return 0
	}
	if x == 1 {
		return 1
	}

	if x > (a+1)/(a+b+2) {
		return 1.0 - regIncBeta(b, a, 1.0-x)
	}

	lnPrefix := a*math.Log(x) + b*math.Log(1-x) -
		math.Log(a) - lnBeta(a, b)

	const maxIter = 200
	const epsilon = 1e-14
	const tiny = 1e-30

	f := tiny
	c := f
	d := 0.0

	for i := 0; i <= maxIter; i++ {
		var an float64
		if i == 0 {
			an = 1.0
		} else {
			m := float64(i)
			if i%2 == 0 {
				k := m / 2.0
				an = (k * (b - k) * x) / ((a + 2*k - 1) * (a + 2*k))
			} else {
				k := (m - 1) / 2.0
				an = -((a + k) * (a + b + k) * x) / ((a + 2*k) * (a + 2*k + 1))
			}
		}

		d = 1.0 + an*d
		if math.Abs(d) < tiny {
			d = tiny
		}
		d = 1.0 / d

		c = 1.0 + an/c
		if math.Abs(c) < tiny {
			c = tiny
		}

		delta := c * d
		f *= delta

		if math.Abs(delta-1.0) < epsilon {
			break
		}
	}

	return math.Exp(lnPrefix) * f
}

func lnBeta(a, b float64) float64 {
	la, _ := math.Lgamma(a)
	lb, _ := math.Lgamma(b)
	lab, _ := math.Lgamma(a + b)
	return la + lb - lab
}
