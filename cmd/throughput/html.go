package main

import (
	"bytes"
	"fmt"
	"html/template"
)

type htmlData struct {
	Title            string
	WindowDesc       string
	FilterNotes      []string
	Weeks            []htmlWeek
	Stats            []htmlStat
	Categories       []htmlCategory
	ActivityLine     []htmlActivity
	Contributors     []htmlContributor
}

type htmlWeek struct {
	WeekStart           string
	PRsMerged           int
	PRsPerEngineer      float64
	CommitsPerEngineer  float64
	MedianCodingTime    float64
	MedianReviewTime    float64
	PctOnaInvolved      float64
	PctReverts          float64
	BuildRuns           int
}

type htmlCategory struct {
	Name           string // e.g. "Speed"
	AccentColor    string // e.g. "#2563eb"
	TintColor      string // e.g. "rgba(37,99,235,0.06)"
	Stats          []htmlStat
	CycleTimeStats []htmlStat // second row: coding time | review time
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

type htmlContributor struct {
	Login      string
	TotalPRs   int
	BeforeRate string
	AfterRate  string
	PctChange  string
	IsUp       bool
	HasOnaPRs  bool
}

func generateHTML(title string, weeks []weekRange, weeklyStats []weekStats, summaryRows []consolidatedRow, periodLabel string, filterNotes []string, topContributors []contributorStat) (string, error) {
	data := htmlData{Title: title, FilterNotes: filterNotes}
	for i, wr := range weeks {
		s := weeklyStats[i]
		ct := s.medianCodingTime
		if ct < 0 {
			ct = 0
		}
		rt := s.medianReviewTime
		if rt < 0 {
			rt = 0
		}
		data.Weeks = append(data.Weeks, htmlWeek{
			WeekStart:          wr.start.Format("2006-01-02"),
			PRsMerged:          s.prsMerged,
			PRsPerEngineer:     s.prsPerEngineer,
			CommitsPerEngineer: s.commitsPerEngineer,
			MedianCodingTime:   ct,
			MedianReviewTime:   rt,
			PctOnaInvolved:     s.pctOnaInvolved,
			PctReverts:         s.pctReverts,
			BuildRuns:          s.buildRuns,
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
		"prs_per_engineer":    {label: "Median PRs / Engineer / " + periodLabel, unit: "", category: "Speed", invertColor: false},
		"commits_per_engineer": {label: "Median Commits / Engineer / " + periodLabel, unit: "", category: "Speed", invertColor: false},
		"pct_reverts":      {label: "Reverts", unit: "%", category: "Quality", invertColor: true},
		"pct_ona_involved": {label: "Ona Involved", unit: "%", category: "Ona Uptake", invertColor: false},
		"prs_merged":        {label: "PRs merged / " + periodLabel, unit: "", category: "activity"},
		"unique_authors":    {label: "Unique authors / " + periodLabel, unit: "", category: "activity"},
		"build_runs":              {label: "Builds / " + periodLabel, unit: "", category: "activity"},
		"build_success_pct":       {label: "Build success", unit: "%", category: "activity"},
		"median_coding_time_hours": {label: "Median Time Spent Coding", unit: "hrs", category: "Cycle Time", invertColor: true},
		"median_review_time_hours": {label: "Median Time Spent Reviewing", unit: "hrs", category: "Cycle Time", invertColor: true},
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

	// Category definitions in display order
	type catDef struct {
		name   string
		accent string
		tint   string
	}
	catOrder := []catDef{
		{name: "Speed", accent: "#2563eb", tint: "#f0f4ff"},
		{name: "Quality", accent: "#16a34a", tint: "#f0fdf4"},
		{name: "Ona Uptake", accent: "#9333ea", tint: "#faf5ff"},
	}
	catStats := make(map[string][]htmlStat)

	for _, r := range summaryRows {
		cfg, ok := metricCfg[r.metric]
		if !ok {
			continue // skip unknown metrics
		}

		firstAvg := fmt.Sprintf("%.1f", r.firstAvg)
		lastAvg := fmt.Sprintf("%.1f", r.lastAvg)
		if cfg.unit != "" {
			firstAvg += cfg.unit
			lastAvg += cfg.unit
		}
		// For inverted metrics (review speed, reverts), a decrease is good.
		isGood := r.absChange >= 0
		if cfg.invertColor {
			isGood = r.absChange <= 0
		}

		stat := htmlStat{
			Label:       cfg.label,
			FirstAvg:    firstAvg,
			LastAvg:     lastAvg,
			IsPositive:  isGood,
			PctChange:   r.pctChange,
			Unit:        cfg.unit,
			InvertColor: cfg.invertColor,
		}

		if cfg.category == "activity" {
			data.ActivityLine = append(data.ActivityLine, htmlActivity{
				Label:     cfg.label,
				FirstAvg:  firstAvg,
				LastAvg:   lastAvg,
				PctChange: r.pctChange,
				IsUp:      r.absChange >= 0,
			})
		} else {
			catStats[cfg.category] = append(catStats[cfg.category], stat)
		}
		data.Stats = append(data.Stats, stat)
	}

	for _, c := range catOrder {
		stats, hasStats := catStats[c.name]
		ctStats := catStats["Cycle Time"] // attach to Speed category
		if !hasStats && (c.name != "Speed" || len(ctStats) == 0) {
			continue
		}
		cat := htmlCategory{
			Name:        c.name,
			AccentColor: c.accent,
			TintColor:   c.tint,
			Stats:       stats,
		}
		if c.name == "Speed" {
			cat.CycleTimeStats = ctStats
		}
		data.Categories = append(data.Categories, cat)
	}

	for _, c := range topContributors {
		pctStr := fmt.Sprintf("%+.1f%%", c.pctChange)
		if !c.hasOnaPRs {
			pctStr = "No Ona PRs"
		} else if c.beforeRate == 0 {
			pctStr = "N/A"
		}
		data.Contributors = append(data.Contributors, htmlContributor{
			Login:      c.login,
			TotalPRs:   c.totalPRs,
			BeforeRate: fmt.Sprintf("%.2f", c.beforeRate),
			AfterRate:  fmt.Sprintf("%.2f", c.afterRate),
			PctChange:  pctStr,
			IsUp:       c.afterRate >= c.beforeRate,
			HasOnaPRs:  c.hasOnaPRs,
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

  .banner-strip { display: flex; align-items: center; gap: 20px; border-radius: 8px; padding: 16px 20px; margin-bottom: 10px; border-left: 5px solid; box-shadow: 0 1px 3px rgba(0,0,0,0.06); }
  .banner-rows { display: flex; flex-direction: column; gap: 8px; flex: 1; }
  .banner-row { display: flex; align-items: center; gap: 16px; flex-wrap: wrap; }
  .banner-sep { color: #d1d5db; font-size: 1.2rem; font-weight: 300; margin: 0 4px; }
  .banner-sublabel { font-size: 0.7rem; font-weight: 600; text-transform: uppercase; letter-spacing: 0.06em; color: #6b7280; }
  .banner-category { font-size: 0.7rem; font-weight: 700; text-transform: uppercase; letter-spacing: 0.08em; min-width: 90px; }
  .banner-metric { font-size: 0.7rem; font-weight: 600; text-transform: uppercase; letter-spacing: 0.06em; color: #6b7280; min-width: 120px; }
  .banner-metric-sub { font-size: 0.7rem; font-weight: 400; text-transform: uppercase; letter-spacing: 0.06em; color: #6b7280; min-width: 120px; }
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

  .contributors-section { margin-top: 24px; }
  .contributors-section h2 { font-size: 1rem; font-weight: 600; margin-bottom: 12px; color: #374151; }
  .contributors-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(220px, 1fr)); gap: 12px; }
  .contrib-card { background: #fff; border-radius: 8px; padding: 14px 18px; box-shadow: 0 1px 3px rgba(0,0,0,0.1); }
  .contrib-login { font-size: 0.95rem; font-weight: 600; color: #1a1a2e; }
  .contrib-total { font-size: 0.75rem; color: #9ca3af; margin-bottom: 8px; }
  .contrib-rates { display: flex; align-items: baseline; gap: 6px; font-size: 1.1rem; font-weight: 600; }
  .contrib-rates .unit { font-size: 0.7rem; font-weight: 400; color: #9ca3af; }
  .contrib-pct { margin-top: 4px; font-size: 0.85rem; font-weight: 600; }
  .contrib-pct.up { color: #16a34a; }
  .contrib-pct.down { color: #dc2626; }
  .contrib-pct.neutral { color: #9ca3af; }

  .metric-defs { margin-top: 24px; }
  .metric-defs summary { font-size: 0.95rem; font-weight: 600; color: #374151; cursor: pointer; padding: 12px 0; }
  .metric-defs summary:hover { color: #1a1a2e; }
  .metric-defs-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(340px, 1fr)); gap: 12px; margin-top: 12px; }
  .metric-def-card { background: #fff; border-radius: 8px; padding: 16px 20px; box-shadow: 0 1px 3px rgba(0,0,0,0.08); border-left: 4px solid #d1d5db; }
  .metric-def-card h3 { font-size: 0.9rem; font-weight: 600; color: #1a1a2e; margin-bottom: 6px; }
  .metric-def-card p { font-size: 0.82rem; color: #4b5563; line-height: 1.5; margin-bottom: 6px; }
  .metric-def-card .def-label { font-size: 0.7rem; font-weight: 700; text-transform: uppercase; letter-spacing: 0.05em; color: #9ca3af; margin-bottom: 2px; }
  .metric-def-card .def-good { color: #16a34a; }
  .metric-def-card .def-warn { color: #b45309; }
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
    <div class="banner-rows">
      <div class="banner-row">
        {{range $i, $s := .Stats}}{{if $i}}<span class="banner-sep">|</span>{{end}}
        <span class="banner-metric">{{$s.Label}}</span>
        <span class="banner-pct {{if $s.IsPositive}}positive{{else}}negative{{end}}">{{$s.PctChange}}</span>
        <span class="banner-detail">{{$s.FirstAvg}} <span class="banner-arrow">&rarr;</span> {{$s.LastAvg}}</span>
        {{end}}
      </div>
      {{if .CycleTimeStats}}
      <div class="banner-row">
        <span class="banner-sublabel">Cycle Time:</span>
        {{range $i, $s := .CycleTimeStats}}{{if $i}}<span class="banner-sep">|</span>{{end}}
        <span class="banner-metric-sub">{{$s.Label}}</span>
        <span class="banner-pct {{if $s.IsPositive}}positive{{else}}negative{{end}}">{{$s.PctChange}}</span>
        <span class="banner-detail">{{$s.FirstAvg}} <span class="banner-arrow">&rarr;</span> {{$s.LastAvg}}</span>
        {{end}}
      </div>
      {{end}}
    </div>
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
  {{if .Contributors}}
  <div class="contributors-section">
    <h2>Top Contributors — Before &amp; After Ona</h2>
    <div class="contributors-grid">
      {{range .Contributors}}
      <div class="contrib-card">
        <div class="contrib-login">@{{.Login}}</div>
        <div class="contrib-total">{{.TotalPRs}} PRs total</div>
        <div class="contrib-rates">
          <span>{{.BeforeRate}}</span>
          <span class="stat-arrow">&rarr;</span>
          <span>{{.AfterRate}}</span>
          <span class="unit">PRs/week</span>
        </div>
        <div class="contrib-pct {{if not .HasOnaPRs}}neutral{{else if .IsUp}}up{{else}}down{{end}}">{{.PctChange}}</div>
      </div>
      {{end}}
    </div>
  </div>
  {{end}}
  <details class="metric-defs">
    <summary>Metric Definitions</summary>
    <div class="metric-defs-grid">
      <div class="metric-def-card">
        <h3>PRs per Engineer</h3>
        <p>Merged PRs divided by unique authors in the period. Measures individual throughput normalized by team size.</p>
        <div class="def-label def-good">Benefits</div>
        <p>Controls for team growth — a team doubling in size won't appear twice as productive. Useful for comparing periods with different headcounts.</p>
        <div class="def-label def-warn">Drawbacks</div>
        <p>Doesn't account for PR size or complexity. A week of small refactors scores the same as a week of large features. Infrequent contributors (1 PR) inflate the denominator.</p>
      </div>
      <div class="metric-def-card">
        <h3>Commits per Engineer</h3>
        <p>Total commits (with a resolved GitHub author) across all merged PRs, divided by unique commit authors in the period. Attributes commits to their actual author, not the PR opener.</p>
        <div class="def-label def-good">Benefits</div>
        <p>Captures per-author work granularity that PRs/engineer misses. Engineers who contribute commits to others' PRs are counted. Useful alongside PRs/engineer to understand collaboration and PR sizing patterns.</p>
        <div class="def-label def-warn">Drawbacks</div>
        <p>Capped at 50 commits per PR — large PRs may undercount. Commits without a linked GitHub account are excluded. Inflated by fixup commits or CI-triggered commits. Uses PR branch commit count from GitHub's API, not the squashed merge commit.</p>
      </div>
      <div class="metric-def-card">
        <h3>% Ona Involved</h3>
        <p>Percentage of PRs where Ona was a co-author (via <code>Co-authored-by</code> trailer) or the primary author (login prefix <code>ona-</code>).</p>
        <div class="def-label def-good">Benefits</div>
        <p>Tracks adoption of Ona-assisted development over time. Correlating with other metrics shows whether Ona usage coincides with throughput or quality changes.</p>
        <div class="def-label def-warn">Drawbacks</div>
        <p>Measures presence, not impact. A PR with a trivial Ona contribution counts the same as one where Ona wrote most of the code. Relies on the co-author trailer being present.</p>
      </div>
      <div class="metric-def-card">
        <h3>% Reverts</h3>
        <p>Percentage of PRs whose title matches revert/rollback patterns. A proxy for code quality and deployment stability.</p>
        <div class="def-label def-good">Benefits</div>
        <p>Captures production issues that required rolling back changes. Trending upward may signal quality regression or insufficient testing.</p>
        <div class="def-label def-warn">Drawbacks</div>
        <p>Title-based detection only — misses reverts with non-standard titles and may false-positive on PRs that mention "revert" without being one. Doesn't distinguish severity.</p>
      </div>
      <div class="metric-def-card">
        <h3>Coding Time</h3>
        <p>Time from first commit (<code>authoredDate</code>) to when the PR was marked ready for review (<code>ReadyForReviewEvent</code>). Measures pre-review development duration.</p>
        <div class="def-label def-good">Benefits</div>
        <p>Isolates the development phase from the review phase. Helps identify whether slowdowns are in coding or review. Not inflated by review wait times.</p>
        <div class="def-label def-warn">Drawbacks</div>
        <p>Only computed for PRs that were created as drafts and later marked ready. Non-draft PRs are excluded. Rebased or amended commits may shift the first commit timestamp. Median can be low if most PRs are opened shortly after the first commit.</p>
      </div>
      <div class="metric-def-card">
        <h3>Review Time</h3>
        <p>Time from when the PR was marked ready for review (<code>ReadyForReviewEvent</code>) to merged. Measures how long PRs spend in code review.</p>
        <div class="def-label def-good">Benefits</div>
        <p>Directly measures review bottlenecks. High review time may indicate reviewer availability issues, large PRs, or complex changes requiring multiple review rounds.</p>
        <div class="def-label def-warn">Drawbacks</div>
        <p>Only computed for PRs that were created as drafts. Includes time the author spends addressing feedback, not just reviewer wait time. Doesn't distinguish between active review and idle waiting.</p>
      </div>
      <div class="metric-def-card">
        <h3>PRs Merged</h3>
        <p>Total number of merged (non-draft, non-bot) pull requests per period. Raw volume metric.</p>
        <div class="def-label def-good">Benefits</div>
        <p>Simple, unambiguous count. Useful for spotting holidays, freezes, or unusual activity spikes.</p>
        <div class="def-label def-warn">Drawbacks</div>
        <p>Not normalized by team size. Conflates small fixes with large features. Higher isn't necessarily better — could indicate PR splitting or churn.</p>
      </div>
    </div>
  </details>
</div>
<script>
const weeks = [{{range $i, $w := .Weeks}}{{if $i}},{{end}}{
  week: "{{$w.WeekStart}}",
  prsMerged: {{$w.PRsMerged}},
  prsPerEngineer: {{$w.PRsPerEngineer}},
  commitsPerEngineer: {{$w.CommitsPerEngineer}},
  codingTime: {{$w.MedianCodingTime}},
  reviewTime: {{$w.MedianReviewTime}},
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
        label: "PRs per Engineer",
        data: weeks.map(w => w.prsPerEngineer),
        borderColor: "#2563eb",
        backgroundColor: "rgba(37,99,235,0.1)",
        yAxisID: "yPPE",
        tension: 0.3,
        pointRadius: 4,
        pointHoverRadius: 6
      },
      {
        label: "PRs/Eng Trend",
        data: trendData,
        borderColor: "rgba(37,99,235,0.5)",
        backgroundColor: "transparent",
        yAxisID: "yPPE",
        borderDash: [6, 4],
        borderWidth: 2,
        pointRadius: 0,
        pointHoverRadius: 0,
        tension: 0
      },
      {
        label: "Commits per Engineer",
        data: weeks.map(w => w.commitsPerEngineer),
        borderColor: "#d946ef",
        backgroundColor: "rgba(217,70,239,0.1)",
        yAxisID: "yPPE",
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
        yAxisID: "yPct",
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
        yAxisID: "yPct",
        tension: 0.3,
        borderDash: [6, 3],
        pointRadius: 4,
        pointHoverRadius: 6
      },
      {
        label: "Time Spent Coding (hrs)",
        data: weeks.map(w => w.codingTime),
        borderColor: "#0891b2",
        backgroundColor: "rgba(8,145,178,0.1)",
        yAxisID: "yHrs",
        tension: 0.3,
        borderDash: [6, 3],
        pointRadius: 4,
        pointHoverRadius: 6,
        hidden: true
      },
      {
        label: "Time Spent Reviewing (hrs)",
        data: weeks.map(w => w.reviewTime),
        borderColor: "#ea580c",
        backgroundColor: "rgba(234,88,12,0.1)",
        yAxisID: "yHrs",
        tension: 0.3,
        pointRadius: 4,
        pointHoverRadius: 6,
        hidden: true
      },
      {
        label: "PRs Merged",
        data: weeks.map(w => w.prsMerged),
        borderColor: "#6b7280",
        backgroundColor: "rgba(107,114,128,0.1)",
        yAxisID: "yCount",
        tension: 0.3,
        pointRadius: 4,
        pointHoverRadius: 6,
        hidden: true
      },
      {
        label: "Builds",
        data: weeks.map(w => w.buildRuns),
        borderColor: "#f59e0b",
        backgroundColor: "rgba(245,158,11,0.1)",
        yAxisID: "yBuilds",
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
            let lbl = ctx.dataset.label;
            let axis = ctx.dataset.yAxisID;
            if (axis === "yPct") return lbl + ": " + v.toFixed(1) + "%";
            if (axis === "yHrs") return lbl + ": " + v.toFixed(1) + "h";
            if (axis === "yCount" || axis === "yBuilds") return lbl + ": " + v.toLocaleString();
            return lbl + ": " + v.toFixed(2);
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
      yPPE: {
        type: "linear",
        position: "left",
        title: { display: true, text: "PRs / Engineer" },
        beginAtZero: true,
        grid: { color: "rgba(0,0,0,0.06)" }
      },
      yPct: {
        type: "linear",
        position: "right",
        weight: 1,
        title: { display: true, text: "%" },
        min: 0,
        max: 100,
        grid: { drawOnChartArea: false }
      },
      yHrs: {
        type: "linear",
        position: "right",
        weight: 2,
        display: false,
        title: { display: true, text: "Hours" },
        beginAtZero: true,
        grid: { drawOnChartArea: false }
      },
      yCount: {
        type: "linear",
        position: "right",
        weight: 3,
        display: false,
        title: { display: true, text: "PRs Merged" },
        beginAtZero: true,
        grid: { drawOnChartArea: false }
      },
      yBuilds: {
        type: "linear",
        position: "right",
        weight: 4,
        display: false,
        title: { display: true, text: "Builds" },
        beginAtZero: true,
        grid: { drawOnChartArea: false }
      }
    }
  },
  plugins: [{
    id: "axisToggle",
    beforeLayout(chart) {
      const axisIds = ["yPPE", "yPct", "yHrs", "yCount", "yBuilds"];
      for (const axisId of axisIds) {
        const scale = chart.options.scales[axisId];
        if (!scale) continue;
        let anyVisible = false;
        chart.data.datasets.forEach((ds, i) => {
          if (ds.yAxisID === axisId) {
            const meta = chart.getDatasetMeta(i);
            if (!meta.hidden && ds.hidden !== true) anyVisible = true;
          }
        });
        scale.display = anyVisible;
      }
    }
  }]
});
</script>
</body>
</html>
`
