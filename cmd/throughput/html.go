package main

import (
	"bytes"
	"fmt"
	"html/template"
)

type htmlData struct {
	Title        string
	WindowDesc   string
	FilterNotes  []string
	Weeks        []htmlWeek
	Categories   []htmlCategory
	ActivityLine []htmlActivity
}

type htmlWeek struct {
	WeekStart         string
	PRsMerged         int
	PRsPerEngineer    float64
	MedianReviewSpeed float64
	PctOnaInvolved    float64
	PctReverts        float64
	BuildRuns         int
}

type htmlCategory struct {
	Name        string // e.g. "Speed"
	AccentColor string // e.g. "#2563eb"
	TintColor   string // e.g. "rgba(37,99,235,0.06)"
	Stats       []htmlStat
}

type htmlStat struct {
	Label       string
	FirstAvg    string
	LastAvg     string
	IsPositive  bool   // true = change is in the "good" direction (accounts for inversion)
	PctChange   string
	Unit        string
	InvertColor bool // true = lower is better (e.g. reverts)
}

type htmlActivity struct {
	Label     string // e.g. "PRs merged"
	FirstAvg  string // e.g. "134"
	LastAvg   string // e.g. "207"
	PctChange string // e.g. "+8.2%"
	IsUp      bool
}

func generateHTML(title string, weeks []weekRange, weeklyStats []weekStats, summaryRows []consolidatedRow, periodLabel string, filterNotes []string) (string, error) {
	data := htmlData{Title: title, FilterNotes: filterNotes}
	for i, wr := range weeks {
		s := weeklyStats[i]
		rs := s.medianReviewSpeed
		if rs < 0 {
			rs = 0
		}
		data.Weeks = append(data.Weeks, htmlWeek{
			WeekStart:         wr.start.Format("2006-01-02"),
			PRsMerged:         s.prsMerged,
			PRsPerEngineer:    s.prsPerEngineer,
			MedianReviewSpeed: rs,
			PctOnaInvolved:    s.pctOnaInvolved,
			PctReverts:        s.pctReverts,
			BuildRuns:         s.buildRuns,
		})
	}

	// Metric display config
	type metricConfig struct {
		label       string
		unit        string
		category    string // "Speed", "Quality", "Ona Uptake", or "activity"
		invertColor bool   // true = lower is better
	}
	metricCfg := map[string]metricConfig{
		"prs_per_engineer": {label: "PRs / Engineer", unit: "", category: "Speed", invertColor: false},
		"pct_reverts":      {label: "Reverts", unit: "%", category: "Quality", invertColor: true},
		"pct_ona_involved": {label: "Ona Involved", unit: "%", category: "Ona Uptake", invertColor: false},
		"prs_merged":        {label: "PRs merged", unit: "", category: "activity"},
		"unique_authors":    {label: "Unique authors", unit: "", category: "activity"},
		"build_runs":        {label: "Builds", unit: "", category: "activity"},
		"build_success_pct": {label: "Build success", unit: "%", category: "activity"},
	}

	// Category definitions in display order
	type catDef struct {
		name  string
		accent string
		tint   string
	}
	catOrder := []catDef{
		{name: "Speed", accent: "#2563eb", tint: "#f0f4ff"},
		{name: "Quality", accent: "#16a34a", tint: "#f0fdf4"},
		{name: "Ona Uptake", accent: "#9333ea", tint: "#faf5ff"},
	}

	// Compute window description from the first summary row
	if len(summaryRows) > 0 && len(weeks) > 0 {
		r := summaryRows[0]
		n := len(weeks)
		if r.firstWindowSize != r.lastWindowSize {
			data.WindowDesc = "Comparing " + r.window
		} else {
			ws := r.windowSize
			if ws < 1 {
				ws = 1
			}
			firstStart := weeks[0].start
			firstEnd := weeks[ws-1].end
			lastStart := weeks[n-ws].start
			lastEnd := weeks[n-1].end
			data.WindowDesc = fmt.Sprintf("Comparing first %d %s(s) (%s – %s) vs last %d %s(s) (%s – %s)",
				ws, periodLabel, firstStart.Format("Jan 2, 2006"), firstEnd.Format("Jan 2, 2006"),
				ws, periodLabel, lastStart.Format("Jan 2, 2006"), lastEnd.Format("Jan 2, 2006"))
		}
	}

	// Route metrics into categories and activity line
	catStats := make(map[string][]htmlStat)
	for _, r := range summaryRows {
		cfg, ok := metricCfg[r.metric]
		if !ok {
			continue // skip median_review_speed_hours and any unknown metrics
		}

		firstAvg := fmt.Sprintf("%.1f", r.firstAvg)
		lastAvg := fmt.Sprintf("%.1f", r.lastAvg)
		if cfg.unit != "" {
			firstAvg += cfg.unit
			lastAvg += cfg.unit
		}

		isUp := r.absChange >= 0
		// IsPositive: for inverted metrics, down is good
		isPositive := isUp
		if cfg.invertColor {
			isPositive = !isUp
		}

		// When first window is 0, show absolute change instead of N/A
		pctChange := r.pctChange
		if pctChange == "N/A" && r.firstAvg == 0 && r.lastAvg != 0 {
			sign := "+"
			if r.absChange < 0 {
				sign = ""
			}
			pctChange = fmt.Sprintf("%s%.1f%s", sign, r.absChange, cfg.unit)
		}

		if cfg.category == "activity" {
			actFirst := fmt.Sprintf("%.0f", r.firstAvg)
			actLast := fmt.Sprintf("%.0f", r.lastAvg)
			if cfg.unit != "" {
				actFirst += cfg.unit
				actLast += cfg.unit
			}
			data.ActivityLine = append(data.ActivityLine, htmlActivity{
				Label:     cfg.label,
				FirstAvg:  actFirst,
				LastAvg:   actLast,
				PctChange: pctChange,
				IsUp:      isUp,
			})
		} else {
			catStats[cfg.category] = append(catStats[cfg.category], htmlStat{
				Label:       cfg.label,
				FirstAvg:    firstAvg,
				LastAvg:     lastAvg,
				IsPositive:  isPositive,
				PctChange:   pctChange,
				Unit:        cfg.unit,
				InvertColor: cfg.invertColor,
			})
		}
	}

	// Build categories in display order
	for _, cd := range catOrder {
		stats := catStats[cd.name]
		if len(stats) == 0 {
			continue
		}
		data.Categories = append(data.Categories, htmlCategory{
			Name:        cd.name,
			AccentColor: cd.accent,
			TintColor:   cd.tint,
			Stats:       stats,
		})
	}

	tmpl, err := template.New("chart").Parse(htmlTemplate)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}
	return buf.String(), nil
}

const htmlTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>{{.Title}}</title>
<script src="https://cdn.jsdelivr.net/npm/chart.js"></script>
<style>
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; background: #f8f9fa; color: #1a1a2e; padding: 24px; }
  h1 { font-size: 1.25rem; font-weight: 600; margin-bottom: 16px; }
  .container { max-width: 1200px; margin: 0 auto; }
  .filter-notes { background: #f3f4f6; border: 1px solid #e5e7eb; border-radius: 8px; padding: 12px 16px; margin-bottom: 16px; font-size: 0.82rem; color: #4b5563; }
  .filter-notes ul { margin: 4px 0 0 0; padding-left: 20px; }
  .filter-notes li { margin: 2px 0; }
  .filter-notes .filter-title { font-weight: 600; color: #374151; }
  .window-desc { font-size: 0.85rem; color: #6b7280; text-align: center; margin-bottom: 16px; }

  .banner-strip { display: flex; align-items: center; gap: 16px; border-radius: 8px; padding: 16px 20px; margin-bottom: 10px; border-left: 5px solid; box-shadow: 0 1px 3px rgba(0,0,0,0.06); }
  .banner-category { font-size: 0.7rem; font-weight: 700; text-transform: uppercase; letter-spacing: 0.08em; min-width: 90px; }
  .banner-metric { font-size: 0.9rem; color: #374151; min-width: 120px; }
  .banner-pct { font-size: 1.5rem; font-weight: 700; }
  .banner-pct.positive { color: #16a34a; }
  .banner-pct.negative { color: #dc2626; }
  .banner-detail { font-size: 0.85rem; color: #6b7280; margin-left: 8px; }
  .banner-arrow { color: #9ca3af; margin: 0 4px; }

  .activity-line { font-size: 0.8rem; color: #6b7280; margin-bottom: 20px; padding: 0 4px; }
  .activity-line .activity-label { font-weight: 600; color: #9ca3af; text-transform: uppercase; font-size: 0.7rem; letter-spacing: 0.05em; margin-right: 8px; }
  .activity-line .activity-sep { margin: 0 10px; color: #d1d5db; }
  .activity-line .activity-pct { font-weight: 600; }
  .activity-line .activity-pct.up { color: #16a34a; }
  .activity-line .activity-pct.down { color: #dc2626; }

  .chart-container { background: #fff; border-radius: 8px; padding: 24px; box-shadow: 0 1px 3px rgba(0,0,0,0.1); }
  canvas { width: 100% !important; }
</style>
</head>
<body>
<div class="container">
  <h1>{{.Title}}</h1>
  {{if .FilterNotes}}
  <div class="filter-notes">
    <span class="filter-title">Data filters applied:</span>
    <ul>
    {{range .FilterNotes}}<li>{{.}}</li>
    {{end}}</ul>
  </div>
  {{end}}
  {{if .Categories}}
  <div class="window-desc">{{.WindowDesc}}</div>
  {{range .Categories}}
  <div class="banner-strip" style="border-left-color: {{.AccentColor}}; background: {{.TintColor}};">
    <span class="banner-category" style="color: {{.AccentColor}};">{{.Name}}</span>
    {{range .Stats}}
    <span class="banner-metric">{{.Label}}</span>
    <span class="banner-pct {{if .IsPositive}}positive{{else}}negative{{end}}">{{.PctChange}}</span>
    <span class="banner-detail">{{.FirstAvg}} <span class="banner-arrow">&rarr;</span> {{.LastAvg}}</span>
    {{end}}
  </div>
  {{end}}
  {{end}}
  {{if .ActivityLine}}
  <div class="activity-line">
    <span class="activity-label">Activity</span>
    {{range $i, $a := .ActivityLine}}{{if $i}}<span class="activity-sep">&middot;</span>{{end}}{{$a.Label}}: {{$a.FirstAvg}} <span class="banner-arrow">&rarr;</span> {{$a.LastAvg}} <span class="activity-pct {{if $a.IsUp}}up{{else}}down{{end}}">({{$a.PctChange}})</span>{{end}}
  </div>
  {{end}}
  <div class="chart-container">
    <canvas id="chart"></canvas>
  </div>
</div>
<script>
const weeks = [{{range $i, $w := .Weeks}}{{if $i}},{{end}}{
  week: "{{$w.WeekStart}}",
  prsMerged: {{$w.PRsMerged}},
  prsPerEngineer: {{$w.PRsPerEngineer}},
  reviewSpeed: {{$w.MedianReviewSpeed}},
  pctOna: {{$w.PctOnaInvolved}},
  pctReverts: {{$w.PctReverts}},
  buildRuns: {{$w.BuildRuns}}
}{{end}}];

const labels = weeks.map(w => w.week);

// Linear regression for PRs per Engineer trendline
const ppeData = weeks.map(w => w.prsPerEngineer);
const n = ppeData.length;
let sumX = 0, sumY = 0, sumXY = 0, sumXX = 0;
for (let i = 0; i < n; i++) {
  sumX += i; sumY += ppeData[i]; sumXY += i * ppeData[i]; sumXX += i * i;
}
const slope = (n * sumXY - sumX * sumY) / (n * sumXX - sumX * sumX);
const intercept = (sumY - slope * sumX) / n;
const trendData = ppeData.map((_, i) => Math.round((slope * i + intercept) * 100) / 100);

new Chart(document.getElementById("chart"), {
  type: "line",
  data: {
    labels: labels,
    datasets: [
      {
        label: "PRs Merged",
        data: weeks.map(w => w.prsMerged),
        borderColor: "#6b7280",
        backgroundColor: "rgba(107,114,128,0.1)",
        yAxisID: "y",
        tension: 0.3,
        pointRadius: 4,
        pointHoverRadius: 6,
        hidden: true
      },
      {
        label: "PRs per Engineer",
        data: weeks.map(w => w.prsPerEngineer),
        borderColor: "#2563eb",
        backgroundColor: "rgba(37,99,235,0.1)",
        yAxisID: "y2",
        tension: 0.3,
        pointRadius: 4,
        pointHoverRadius: 6
      },
      {
        label: "PRs/Eng Trend",
        data: trendData,
        borderColor: "rgba(37,99,235,0.5)",
        backgroundColor: "transparent",
        yAxisID: "y2",
        borderDash: [6, 4],
        borderWidth: 2,
        pointRadius: 0,
        pointHoverRadius: 0,
        tension: 0
      },
      {
        label: "Review Speed (hrs)",
        data: weeks.map(w => w.reviewSpeed),
        borderColor: "#ea580c",
        backgroundColor: "rgba(234,88,12,0.1)",
        yAxisID: "y2",
        tension: 0.3,
        pointRadius: 4,
        pointHoverRadius: 6,
        hidden: true
      },
      {
        label: "% Ona Involved",
        data: weeks.map(w => w.pctOna),
        borderColor: "#9333ea",
        backgroundColor: "rgba(147,51,234,0.1)",
        yAxisID: "y1",
        tension: 0.3,
        borderDash: [6, 3],
        pointRadius: 4,
        pointHoverRadius: 6
      },
      {
        label: "% Reverts",
        data: weeks.map(w => w.pctReverts),
        borderColor: "#16a34a",
        backgroundColor: "rgba(22,163,74,0.1)",
        yAxisID: "y1",
        tension: 0.3,
        borderDash: [6, 3],
        pointRadius: 4,
        pointHoverRadius: 6
      },
      {
        label: "Builds",
        data: weeks.map(w => w.buildRuns),
        borderColor: "#f59e0b",
        backgroundColor: "rgba(245,158,11,0.1)",
        yAxisID: "y3",
        tension: 0.3,
        pointRadius: 4,
        pointHoverRadius: 6,
        hidden: true
      }
    ]
  },
  options: {
    responsive: true,
    interaction: {
      mode: "index",
      intersect: false
    },
    plugins: {
      tooltip: {
        callbacks: {
          label: function(ctx) {
            let v = ctx.parsed.y;
            if (ctx.dataset.yAxisID === "y1") return ctx.dataset.label + ": " + v.toFixed(1) + "%";
            if (ctx.dataset.yAxisID === "y2") return ctx.dataset.label + ": " + v.toFixed(2);
            if (ctx.dataset.yAxisID === "y3") return ctx.dataset.label + ": " + v.toLocaleString();
            if (ctx.dataset.label === "PRs Merged") return ctx.dataset.label + ": " + v;
            return ctx.dataset.label + ": " + v.toFixed(2);
          }
        }
      },
      legend: {
        position: "bottom",
        labels: { usePointStyle: true, padding: 16 }
      }
    },
    scales: {
      x: {
        title: { display: true, text: "Week Starting" },
        ticks: { maxRotation: 45 }
      },
      y: {
        type: "linear",
        position: "left",
        title: { display: true, text: "PRs Merged" },
        beginAtZero: true,
        grid: { color: "rgba(0,0,0,0.06)" }
      },
      y1: {
        type: "linear",
        position: "right",
        title: { display: true, text: "Percentage (%)" },
        min: 0,
        max: 100,
        grid: { drawOnChartArea: false }
      },
      y2: {
        type: "linear",
        position: "right",
        title: { display: true, text: "PRs/Engineer · Review Speed (hrs)" },
        beginAtZero: true,
        grid: { drawOnChartArea: false }
      },
      y3: {
        type: "linear",
        position: "left",
        display: false,
        title: { display: true, text: "Builds" },
        beginAtZero: true,
        grid: { drawOnChartArea: false }
      }
    }
  },
  plugins: [{
    // Show/hide y3 axis when Builds dataset is toggled
    id: "y3Toggle",
    afterUpdate(chart) {
      const ds = chart.data.datasets.find(d => d.yAxisID === "y3");
      if (ds && chart.scales.y3) {
        const meta = chart.getDatasetMeta(chart.data.datasets.indexOf(ds));
        chart.scales.y3.options.display = !meta.hidden;
      }
    }
  }]
});
</script>
</body>
</html>
`
