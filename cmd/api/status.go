package main

import (
	"fmt"
	"html/template"
	"net/http"
	"time"
)

// statusDashboard renders a small ops page: per-source health, staleness and
// availability for every CPO (polled and push). Served at /api/status.
func (s *server) statusDashboard(w http.ResponseWriter, r *http.Request) {
	hs, err := s.st.SourceHealthAll(r.Context())
	if err != nil {
		http.Error(w, "cannot load source health", http.StatusServiceUnavailable)
		return
	}
	now := time.Now()
	v := statusView{Generated: now.UTC().Format("2006-01-02 15:04 UTC")}
	for _, h := range hs {
		row := statusRow{
			ID: h.ID, Name: h.Name, Type: h.SourceType, Country: h.Country,
			Chargers: h.Chargers, Priced: h.Priced, Available: h.Available,
		}
		switch {
		case h.SourceType == "mobilithek":
			row.Mode = "push"
		case h.Enabled:
			row.Mode = "poll"
		default:
			row.Mode = "off"
		}
		if h.Chargers > 0 {
			row.PricedPct = fmt.Sprintf("%.0f%%", 100*float64(h.Priced)/float64(h.Chargers))
		}
		// Location-only registers never carry availability/price — don't flag
		// their missing freshness as a fault.
		locationOnly := h.SourceType == "bnetza" || h.SourceType == "irve"
		row.StatusAgo, row.StatusClass = ago(now, h.NewestStatus, locationOnly)
		row.PriceAgo, _ = ago(now, h.NewestPrice, locationOnly || h.Priced == 0)
		v.Totals.Sources++
		v.Totals.Chargers += h.Chargers
		v.Totals.Priced += h.Priced
		v.Totals.Available += h.Available
		v.Rows = append(v.Rows, row)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = statusTmpl.Execute(w, v)
}

type statusRow struct {
	ID, Name, Type, Country, Mode    string
	Chargers, Priced, Available      int
	PricedPct                        string
	StatusAgo, PriceAgo, StatusClass string
}

type statusView struct {
	Generated string
	Totals    struct{ Sources, Chargers, Priced, Available int }
	Rows      []statusRow
}

// ago renders a compact age + a freshness class. "expected empty" (e.g. a
// location-only register has no availability) reads as neutral, not stale.
func ago(now time.Time, t *time.Time, expectedEmpty bool) (string, string) {
	if t == nil {
		if expectedEmpty {
			return "—", "na"
		}
		return "never", "old"
	}
	d := now.Sub(*t)
	cls := "ok"
	switch {
	case d > 24*time.Hour:
		cls = "old"
	case d > time.Hour:
		cls = "stale"
	}
	switch {
	case d < time.Minute:
		return "just now", cls
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes())), cls
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours())), cls
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24)), cls
	}
}

var statusTmpl = template.Must(template.New("status").Parse(`<!doctype html>
<html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<meta http-equiv="refresh" content="30"><title>Charging — source health</title>
<style>
 body{font:14px/1.4 system-ui,sans-serif;margin:20px;color:#0f172a;background:#f8fafc}
 h1{font-size:18px;margin:0 0 4px} .sub{color:#64748b;margin:0 0 16px;font-size:12px}
 .totals{display:flex;gap:18px;margin:0 0 16px;flex-wrap:wrap}
 .totals div{background:#fff;border:1px solid #e2e8f0;border-radius:10px;padding:8px 14px}
 .totals b{font-size:18px;display:block}
 table{border-collapse:collapse;width:100%;background:#fff;border:1px solid #e2e8f0;border-radius:10px;overflow:hidden}
 th,td{text-align:left;padding:7px 10px;border-bottom:1px solid #f1f5f9} th{background:#f1f5f9;font-size:12px;color:#475569}
 td.num{text-align:right;font-variant-numeric:tabular-nums}
 .pill{font-size:11px;padding:1px 7px;border-radius:999px;border:1px solid #cbd5e1;color:#475569}
 .ok{color:#15803d;font-weight:600} .stale{color:#b45309;font-weight:600} .old{color:#b91c1c;font-weight:600} .na{color:#94a3b8}
</style></head><body>
<h1>Source health</h1>
<p class="sub">{{.Totals.Sources}} sources · generated {{.Generated}} · auto-refresh 30s · <a href="metrics">/metrics</a></p>
<div class="totals">
 <div>Chargers<b>{{.Totals.Chargers}}</b></div>
 <div>Priced<b>{{.Totals.Priced}}</b></div>
 <div>Available now<b>{{.Totals.Available}}</b></div>
</div>
<table>
<tr><th>Source</th><th>Type</th><th>Mode</th><th>CC</th><th class="num">Chargers</th><th class="num">Priced</th><th class="num">Avail</th><th>Status</th><th>Price</th></tr>
{{range .Rows}}<tr>
 <td>{{.Name}}<br><span class="na">{{.ID}}</span></td>
 <td>{{.Type}}</td><td><span class="pill">{{.Mode}}</span></td><td>{{.Country}}</td>
 <td class="num">{{.Chargers}}</td><td class="num">{{.Priced}} {{if .PricedPct}}<span class="na">({{.PricedPct}})</span>{{end}}</td>
 <td class="num">{{.Available}}</td>
 <td class="{{.StatusClass}}">{{.StatusAgo}}</td><td class="na">{{.PriceAgo}}</td>
</tr>{{end}}
</table></body></html>`))
