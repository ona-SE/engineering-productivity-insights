package main

import (
	"bytes"
	"fmt"
	"html/template"
)

type htmlData struct {
	Title      string
	WindowDesc string
	Weeks      []htmlWeek
	Stats      []htmlStat
	Quarters   []htmlQuarter
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
	FirstAvg  string
	LastAvg   string
	IsUp      bool // true = positive change
	PctChange string
	Unit      string
}

type htmlQuarter struct {
	Label           string
	DateRange       string
	PRsMerged       string
	PRsPerEngineer  string
	MedianCycleTime string
	PctOnaInvolved  string
	PctReverts      string
}

func computeQuarters(weeks []weekRange, stats []weekStats) []htmlQuarter {
	n := len(weeks)
	if n < 4 {
		return nil
	}
	qSize := n / 4
	var quarters []htmlQuarter
	for q := 0; q < 4; q++ {
		start := q * qSize
		end := start + qSize
		if q == 3 {
			end = n // last quarter absorbs remainder
		}
		var totalPRs, count int
		var sumPrsPerEng, sumCycleTime, sumOna, sumReverts float64
		var cycleCount int
		for i := start; i < end; i++ {
			s := stats[i]
			if s.prsMerged > 0 {
				count++
				totalPRs += s.prsMerged
				sumPrsPerEng += s.prsPerEngineer
				sumOna += s.pctOnaInvolved
				sumReverts += s.pctReverts
				if s.medianCycleTime >= 0 {
					sumCycleTime += s.medianCycleTime
					cycleCount++
				}
			}
		}
		label := fmt.Sprintf("Q%d", q+1)
		dateRange := fmt.Sprintf("%s – %s",
			weeks[start].start.Format("Jan 2, 2006"),
			weeks[end-1].end.Format("Jan 2, 2006"))

		qr := htmlQuarter{Label: label, DateRange: dateRange}
		if count > 0 {
			qr.PRsMerged = fmt.Sprintf("%.0f", float64(totalPRs)/float64(count))
			qr.PRsPerEngineer = fmt.Sprintf("%.1f", sumPrsPerEng/float64(count))
			qr.PctOnaInvolved = fmt.Sprintf("%.1f%%", sumOna/float64(count))
			qr.PctReverts = fmt.Sprintf("%.1f%%", sumReverts/float64(count))
		} else {
			qr.PRsMerged = "–"
			qr.PRsPerEngineer = "–"
			qr.PctOnaInvolved = "–"
			qr.PctReverts = "–"
		}
		if cycleCount > 0 {
			qr.MedianCycleTime = fmt.Sprintf("%.1f hrs", sumCycleTime/float64(cycleCount))
		} else {
			qr.MedianCycleTime = "–"
		}
		quarters = append(quarters, qr)
	}
	return quarters
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

	// Compute window description from the first summary row
	if len(summaryRows) > 0 && len(weeks) > 0 {
		r := summaryRows[0]
		n := len(weeks)
		if r.firstWindowSize != r.lastWindowSize {
			// Threshold-based split — use the window string directly
			data.WindowDesc = "Comparing " + r.window
		} else {
			// Positional window — show date ranges
			ws := r.windowSize
			if ws < 1 {
				ws = 1
			}
			firstStart := weeks[0].start
			firstEnd := weeks[ws-1].end
			lastStart := weeks[n-ws].start
			lastEnd := weeks[n-1].end
			data.WindowDesc = fmt.Sprintf("Comparing first %d week(s) (%s – %s) vs last %d week(s) (%s – %s)",
				ws, firstStart.Format("Jan 2, 2006"), firstEnd.Format("Jan 2, 2006"),
				ws, lastStart.Format("Jan 2, 2006"), lastEnd.Format("Jan 2, 2006"))
		}
	}

	for _, r := range summaryRows {
		label := labelMap[r.metric]
		if label == "" {
			label = r.metric
		}
		unit := unitMap[r.metric]
		firstAvg := fmt.Sprintf("%.1f", r.firstAvg)
		lastAvg := fmt.Sprintf("%.1f", r.lastAvg)
		if unit != "" {
			firstAvg += " " + unit
			lastAvg += " " + unit
		}
		data.Stats = append(data.Stats, htmlStat{
			Label:     label,
			FirstAvg:  firstAvg,
			LastAvg:   lastAvg,
			IsUp:      r.absChange >= 0,
			PctChange: r.pctChange,
			Unit:      unit,
		})
	}

	// Compute quarterly averages (split into 4 equal groups)
	data.Quarters = computeQuarters(weeks, weeklyStats)

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
  .window-desc { font-size: 0.85rem; color: #6b7280; text-align: center; margin-bottom: 16px; }
  .stats-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(280px, 1fr)); gap: 12px; margin-bottom: 20px; }
  .stat-card { background: #fff; border-radius: 8px; padding: 14px 18px; box-shadow: 0 1px 3px rgba(0,0,0,0.1); }
  .stat-label { font-size: 0.7rem; color: #6b7280; text-transform: uppercase; letter-spacing: 0.05em; margin-bottom: 6px; }
  .stat-row { display: flex; align-items: baseline; gap: 8px; font-size: 1.25rem; font-weight: 600; }
  .stat-arrow { color: #9ca3af; }
  .stat-pct { margin-left: auto; }
  .stat-pct.up { color: #16a34a; }
  .stat-pct.down { color: #dc2626; }
  .section-title { font-size: 0.85rem; font-weight: 600; color: #6b7280; text-transform: uppercase; letter-spacing: 0.05em; margin-bottom: 10px; }
  .quarter-table { width: 100%; border-collapse: collapse; background: #fff; border-radius: 8px; box-shadow: 0 1px 3px rgba(0,0,0,0.1); margin-bottom: 20px; overflow: hidden; }
  .quarter-table th { font-size: 0.7rem; color: #6b7280; text-transform: uppercase; letter-spacing: 0.05em; padding: 10px 14px; text-align: left; border-bottom: 1px solid #e5e7eb; }
  .quarter-table td { font-size: 0.95rem; font-weight: 500; padding: 10px 14px; border-bottom: 1px solid #f3f4f6; }
  .quarter-table tr:last-child td { border-bottom: none; }
  .quarter-label { font-weight: 700; }
  .quarter-dates { font-size: 0.75rem; color: #9ca3af; font-weight: 400; }
  .chart-container { background: #fff; border-radius: 8px; padding: 24px; box-shadow: 0 1px 3px rgba(0,0,0,0.1); }
  canvas { width: 100% !important; }
</style>
</head>
<body>
<div class="container">
  <h1>{{.Title}}</h1>
  {{if .Stats}}
  <div class="window-desc">{{.WindowDesc}}</div>
  <div class="stats-grid">
    {{range .Stats}}
    <div class="stat-card">
      <div class="stat-label">{{.Label}}</div>
      <div class="stat-row">
        <span>{{.FirstAvg}}</span>
        <span class="stat-arrow">&rarr;</span>
        <span>{{.LastAvg}}</span>
        <span class="stat-pct {{if .IsUp}}up{{else}}down{{end}}">({{.PctChange}})</span>
      </div>
    </div>
    {{end}}
  </div>
  {{end}}
  {{if .Quarters}}
  <div class="section-title">Quarterly Averages</div>
  <table class="quarter-table">
    <thead>
      <tr>
        <th>Period</th>
        <th>PRs Merged / wk</th>
        <th>PRs / Engineer</th>
        <th>Cycle Time</th>
        <th>Ona Involved</th>
        <th>Reverts</th>
      </tr>
    </thead>
    <tbody>
      {{range .Quarters}}
      <tr>
        <td><span class="quarter-label">{{.Label}}</span> <span class="quarter-dates">{{.DateRange}}</span></td>
        <td>{{.PRsMerged}}</td>
        <td>{{.PRsPerEngineer}}</td>
        <td>{{.MedianCycleTime}}</td>
        <td>{{.PctOnaInvolved}}</td>
        <td>{{.PctReverts}}</td>
      </tr>
      {{end}}
    </tbody>
  </table>
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
