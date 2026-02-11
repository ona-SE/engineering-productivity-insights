package main

import (
	"bytes"
	"fmt"
	"html/template"
)

type htmlData struct {
	Title string
	Weeks []htmlWeek
	Stats []htmlStat
}

type htmlWeek struct {
	WeekStart       string
	PRsMerged       int
	PRsPerEngineer  float64
	MedianCycleTime float64
	PctOnaInvolved  float64
	PctReverts      float64
}

type htmlStat struct {
	Label     string
	LastAvg   string
	Change    string
	IsUp      bool // true = positive change
	PctChange string
	Unit      string
}

func generateHTML(title string, weeks []weekRange, weeklyStats []weekStats, summaryRows []consolidatedRow) (string, error) {
	data := htmlData{Title: title}
	for i, wr := range weeks {
		s := weeklyStats[i]
		ct := s.medianCycleTime
		if ct < 0 {
			ct = 0
		}
		data.Weeks = append(data.Weeks, htmlWeek{
			WeekStart:       wr.start.Format("2006-01-02"),
			PRsMerged:       s.prsMerged,
			PRsPerEngineer:  s.prsPerEngineer,
			MedianCycleTime: ct,
			PctOnaInvolved:  s.pctOnaInvolved,
			PctReverts:      s.pctReverts,
		})
	}

	// Map metric names to display labels and units
	labelMap := map[string]string{
		"prs_merged":             "PRs Merged",
		"unique_authors":         "Unique Authors",
		"prs_per_engineer":       "PRs / Engineer",
		"median_cycle_time_hours": "Median Cycle Time",
		"pct_reverts":            "Reverts",
		"pct_ona_involved":       "Ona Involved",
	}
	unitMap := map[string]string{
		"prs_merged":             "",
		"unique_authors":         "",
		"prs_per_engineer":       "",
		"median_cycle_time_hours": "hrs",
		"pct_reverts":            "%",
		"pct_ona_involved":       "%",
	}

	for _, r := range summaryRows {
		label := labelMap[r.metric]
		if label == "" {
			label = r.metric
		}
		unit := unitMap[r.metric]
		lastAvg := fmt.Sprintf("%.1f", r.lastAvg)
		change := fmt.Sprintf("%.1f", r.absChange)
		if r.absChange >= 0 {
			change = "+" + change
		}
		data.Stats = append(data.Stats, htmlStat{
			Label:     label,
			LastAvg:   lastAvg,
			Change:    change,
			IsUp:      r.absChange >= 0,
			PctChange: r.pctChange,
			Unit:      unit,
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
  .stats-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(170px, 1fr)); gap: 12px; margin-bottom: 20px; }
  .stat-card { background: #fff; border-radius: 8px; padding: 16px; box-shadow: 0 1px 3px rgba(0,0,0,0.1); }
  .stat-label { font-size: 0.75rem; color: #6b7280; text-transform: uppercase; letter-spacing: 0.05em; margin-bottom: 4px; }
  .stat-value { font-size: 1.75rem; font-weight: 700; color: #1a1a2e; }
  .stat-unit { font-size: 0.875rem; font-weight: 400; color: #6b7280; }
  .stat-change { font-size: 0.8rem; margin-top: 4px; }
  .stat-change.up { color: #16a34a; }
  .stat-change.down { color: #dc2626; }
  .chart-container { background: #fff; border-radius: 8px; padding: 24px; box-shadow: 0 1px 3px rgba(0,0,0,0.1); }
  canvas { width: 100% !important; }
</style>
</head>
<body>
<div class="container">
  <h1>{{.Title}}</h1>
  {{if .Stats}}
  <div class="stats-grid">
    {{range .Stats}}
    <div class="stat-card">
      <div class="stat-label">{{.Label}}</div>
      <div class="stat-value">{{.LastAvg}}{{if .Unit}} <span class="stat-unit">{{.Unit}}</span>{{end}}</div>
      <div class="stat-change {{if .IsUp}}up{{else}}down{{end}}">{{.Change}}{{if .Unit}} {{.Unit}}{{end}} ({{.PctChange}})</div>
    </div>
    {{end}}
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
  medianCycleTime: {{$w.MedianCycleTime}},
  pctOna: {{$w.PctOnaInvolved}},
  pctReverts: {{$w.PctReverts}}
}{{end}}];

const labels = weeks.map(w => w.week);

new Chart(document.getElementById("chart"), {
  type: "line",
  data: {
    labels: labels,
    datasets: [
      {
        label: "PRs Merged",
        data: weeks.map(w => w.prsMerged),
        borderColor: "#2563eb",
        backgroundColor: "rgba(37,99,235,0.1)",
        yAxisID: "y",
        tension: 0.3,
        pointRadius: 4,
        pointHoverRadius: 6
      },
      {
        label: "PRs per Engineer",
        data: weeks.map(w => w.prsPerEngineer),
        borderColor: "#16a34a",
        backgroundColor: "rgba(22,163,74,0.1)",
        yAxisID: "y2",
        tension: 0.3,
        pointRadius: 4,
        pointHoverRadius: 6
      },
      {
        label: "Median Cycle Time (hrs)",
        data: weeks.map(w => w.medianCycleTime),
        borderColor: "#ea580c",
        backgroundColor: "rgba(234,88,12,0.1)",
        yAxisID: "y2",
        tension: 0.3,
        pointRadius: 4,
        pointHoverRadius: 6
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
        borderColor: "#dc2626",
        backgroundColor: "rgba(220,38,38,0.1)",
        yAxisID: "y1",
        tension: 0.3,
        borderDash: [6, 3],
        pointRadius: 4,
        pointHoverRadius: 6
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
        title: { display: true, text: "PRs/Engineer · Cycle Time (hrs)" },
        beginAtZero: true,
        grid: { drawOnChartArea: false }
      }
    }
  }
});
</script>
</body>
</html>
`
