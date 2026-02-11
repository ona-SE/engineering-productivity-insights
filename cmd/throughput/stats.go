package main

import (
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
)

const statsCSVHeader = "test,metric,ona_metric,n,value,p_value,interpretation"

// statRow represents one row in the stats CSV output.
type statRow struct {
	test           string
	metric         string
	onaMetric      string
	n              int
	value          float64
	pValue         float64
	interpretation string
}

// metricPair defines a correlation pair to test.
type metricPair struct {
	name   string                    // column name for the dependent variable
	extract func(ws weekStats) float64 // extracts the metric value from a weekStats
	valid   func(ws weekStats) bool    // whether this week has valid data for this metric
}

var metricPairs = []metricPair{
	{
		name:    "prs_per_engineer",
		extract: func(ws weekStats) float64 { return ws.prsPerEngineer },
		valid:   func(ws weekStats) bool { return ws.prsMerged > 0 },
	},
	{
		name:    "median_cycle_time_hours",
		extract: func(ws weekStats) float64 { return ws.medianCycleTime },
		valid:   func(ws weekStats) bool { return ws.prsMerged > 0 && ws.medianCycleTime >= 0 },
	},
	{
		name:    "pct_reverts",
		extract: func(ws weekStats) float64 { return ws.pctReverts },
		valid:   func(ws weekStats) bool { return ws.prsMerged > 0 },
	},
}

// generateStats runs all correlation tests and summary calculations, returning
// the stats CSV content. Returns empty string if insufficient data.
func generateStats(allStats []weekStats) string {
	// Filter to weeks with >0 PRs
	var valid []weekStats
	for _, ws := range allStats {
		if ws.prsMerged > 0 {
			valid = append(valid, ws)
		}
	}

	if len(valid) < 4 {
		fmt.Fprintf(os.Stderr, "WARNING: Only %d non-empty weeks — need at least 4 for stats. Skipping.\n", len(valid))
		return ""
	}

	// Extract Ona values (independent variable for all tests)
	onaValues := make([]float64, len(valid))
	for i, ws := range valid {
		onaValues[i] = ws.pctOnaInvolved
	}

	var rows []statRow

	// For each metric pair, run Pearson and Mann-Whitney
	for _, mp := range metricPairs {
		// Collect paired data (ona, metric) for weeks where this metric is valid
		var onaX, metricY []float64
		for _, ws := range valid {
			if mp.valid(ws) {
				onaX = append(onaX, ws.pctOnaInvolved)
				metricY = append(metricY, mp.extract(ws))
			}
		}

		n := len(onaX)
		if n < 4 {
			fmt.Fprintf(os.Stderr, "WARNING: Only %d valid weeks for %s — skipping.\n", n, mp.name)
			continue
		}

		// Pearson correlation
		r, pValuePearson := pearsonCorrelation(onaX, metricY)
		rows = append(rows, statRow{
			test:           "pearson",
			metric:         mp.name,
			onaMetric:      "pct_ona_involved",
			n:              n,
			value:          r,
			pValue:         pValuePearson,
			interpretation: interpretPValue(pValuePearson),
		})

		// Mann-Whitney U
		uStat, pValueMW, ok := mannWhitneyU(onaX, metricY)
		if ok {
			rows = append(rows, statRow{
				test:           "mann_whitney_u",
				metric:         mp.name,
				onaMetric:      "pct_ona_involved",
				n:              n,
				value:          uStat,
				pValue:         pValueMW,
				interpretation: interpretPValue(pValueMW),
			})
		}

		// Summary row
		summaryRow := computeSummary(mp, valid, r, pValuePearson)
		if summaryRow != nil {
			rows = append(rows, *summaryRow)
		}
	}

	if len(rows) == 0 {
		return ""
	}

	// Build CSV
	var sb strings.Builder
	sb.WriteString(statsCSVHeader)
	sb.WriteByte('\n')
	for _, row := range rows {
		// Quote interpretation field since summary rows contain commas
		interp := row.interpretation
		if strings.ContainsAny(interp, ",\"") {
			interp = "\"" + strings.ReplaceAll(interp, "\"", "\"\"") + "\""
		}
		fmt.Fprintf(&sb, "%s,%s,%s,%d,%.2f,%.6f,%s\n",
			row.test, row.metric, row.onaMetric, row.n,
			row.value, row.pValue, interp)
	}
	return sb.String()
}

// computeSummary computes the first-5%-vs-last-5% trend and builds a summary row.
func computeSummary(mp metricPair, valid []weekStats, pearsonR, pearsonP float64) *statRow {
	// Collect metric values in chronological order for valid weeks
	var values []float64
	for _, ws := range valid {
		if mp.valid(ws) {
			values = append(values, mp.extract(ws))
		}
	}

	n := len(values)
	if n < 2 {
		return nil
	}

	// Window size: floor(n * 0.05), min 1
	windowSize := int(math.Floor(float64(n) * 0.05))
	if windowSize < 1 {
		windowSize = 1
	}

	// First window average
	var firstSum float64
	for i := 0; i < windowSize; i++ {
		firstSum += values[i]
	}
	firstAvg := firstSum / float64(windowSize)

	// Last window average
	var lastSum float64
	for i := n - windowSize; i < n; i++ {
		lastSum += values[i]
	}
	lastAvg := lastSum / float64(windowSize)

	absChange := lastAvg - firstAvg
	rSquared := pearsonR * pearsonR
	sig := interpretPValue(pearsonP)

	// Build interpretation string
	var interp string
	if firstAvg != 0 {
		pctChange := (absChange / math.Abs(firstAvg)) * 100
		sign := "+"
		if pctChange < 0 {
			sign = ""
		}
		interp = fmt.Sprintf("%s%.1f%% change (r²=%.2f, p=%.6f, %s)",
			sign, pctChange, rSquared, pearsonP, sig)
	} else {
		sign := "+"
		if absChange < 0 {
			sign = ""
		}
		interp = fmt.Sprintf("%s%.2f absolute change (r²=%.2f, p=%.6f, %s)",
			sign, absChange, rSquared, pearsonP, sig)
	}

	return &statRow{
		test:           "summary",
		metric:         mp.name,
		onaMetric:      "pct_ona_involved",
		n:              n,
		value:          absChange,
		pValue:         pearsonP,
		interpretation: interp,
	}
}

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

// pearsonCorrelation computes Pearson r and two-tailed p-value.
// Returns (0, 1.0) if variance is zero.
func pearsonCorrelation(x, y []float64) (float64, float64) {
	n := len(x)
	if n < 3 {
		return 0, 1.0
	}

	// Means
	var sumX, sumY float64
	for i := 0; i < n; i++ {
		sumX += x[i]
		sumY += y[i]
	}
	meanX := sumX / float64(n)
	meanY := sumY / float64(n)

	// Covariance and standard deviations
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

	// Clamp r to [-1, 1] for numerical safety
	if r > 1 {
		r = 1
	}
	if r < -1 {
		r = -1
	}

	// t-statistic: t = r * sqrt((n-2) / (1-r²))
	df := float64(n - 2)
	t := r * math.Sqrt(df/(1-r*r))

	// Two-tailed p-value from t-distribution
	p := 2 * tDistCDF(-math.Abs(t), df)

	return r, p
}

// --- Mann-Whitney U test ---

// mannWhitneyU splits weeks into high/low Ona groups by median pct_ona_involved,
// then compares the metric distributions. Returns (U, p-value, ok).
// ok is false if groups can't be formed (e.g., all Ona values identical).
func mannWhitneyU(onaValues, metricValues []float64) (float64, float64, bool) {
	n := len(onaValues)
	if n < 4 {
		return 0, 1.0, false
	}

	// Find median of Ona values
	onaSorted := make([]float64, n)
	copy(onaSorted, onaValues)
	sort.Float64s(onaSorted)
	onaMedian := percentile(onaSorted, 50)

	// Split into two groups
	var highGroup, lowGroup []float64
	for i := 0; i < n; i++ {
		if onaValues[i] > onaMedian {
			highGroup = append(highGroup, metricValues[i])
		} else {
			lowGroup = append(lowGroup, metricValues[i])
		}
	}

	n1 := len(lowGroup)
	n2 := len(highGroup)

	if n1 == 0 || n2 == 0 {
		// Can't split — all values on one side of median
		return 0, 1.0, false
	}

	if n1 < 8 || n2 < 8 {
		fmt.Fprintf(os.Stderr, "NOTE: Mann-Whitney groups are small (n1=%d, n2=%d). Normal approximation may be imprecise.\n", n1, n2)
	}

	// Compute U statistic
	// U = number of times a low-group value precedes a high-group value
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

	// Normal approximation for p-value
	meanU := float64(n1*n2) / 2.0
	sigmaU := math.Sqrt(float64(n1*n2*(n1+n2+1)) / 12.0)

	if sigmaU == 0 {
		return u, 1.0, true
	}

	z := (u - meanU) / sigmaU
	// Two-tailed p-value
	p := 2 * normalCDF(-math.Abs(z))

	return u, p, true
}

// --- Distribution functions (pure Go, no external deps) ---

// normalCDF computes the cumulative distribution function of the standard normal
// distribution using the Abramowitz and Stegun approximation.
func normalCDF(x float64) float64 {
	return 0.5 * math.Erfc(-x/math.Sqrt2)
}

// tDistCDF computes the CDF of the Student's t-distribution with df degrees of
// freedom, using the regularized incomplete beta function.
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

// regIncBeta computes the regularized incomplete beta function I_x(a, b)
// using a continued fraction expansion (Lentz's method).
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

	// Use the symmetry relation if x > (a+1)/(a+b+2) for better convergence
	if x > (a+1)/(a+b+2) {
		return 1.0 - regIncBeta(b, a, 1.0-x)
	}

	// Log of the beta function prefix: x^a * (1-x)^b / (a * B(a,b))
	lnPrefix := a*math.Log(x) + b*math.Log(1-x) -
		math.Log(a) - lnBeta(a, b)

	// Continued fraction (Lentz's method)
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
				// Even term
				k := m / 2.0
				an = (k * (b - k) * x) / ((a + 2*k - 1) * (a + 2*k))
			} else {
				// Odd term
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

// lnBeta computes ln(B(a, b)) = lnGamma(a) + lnGamma(b) - lnGamma(a+b).
func lnBeta(a, b float64) float64 {
	la, _ := math.Lgamma(a)
	lb, _ := math.Lgamma(b)
	lab, _ := math.Lgamma(a + b)
	return la + lb - lab
}
