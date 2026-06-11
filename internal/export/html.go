package export

import (
	"fmt"
	"html"
	"io"
	"sort"
	"strings"
)

// RenderIndexHTML writes a human-readable listing of the published dataset
// files (the manifest, but browsable), grouped by country then region. Served
// as index.html at the export root. Links are relative so they work under any
// base path.
func RenderIndexHTML(w io.Writer, m Manifest) error {
	var b strings.Builder
	b.WriteString(`<!doctype html><html lang="en"><head><meta charset="utf-8">`)
	b.WriteString(`<meta name="viewport" content="width=device-width, initial-scale=1">`)
	b.WriteString(`<title>Charging — open EV charging dataset</title><style>`)
	b.WriteString(`body{font:15px/1.5 system-ui,sans-serif;max-width:1000px;margin:24px auto;padding:0 16px;color:#0f172a}` +
		`h1{font-size:22px}h2{font-size:16px;margin-top:28px;border-bottom:1px solid #e2e8f0;padding-bottom:4px}` +
		`.muted{color:#64748b}.sum{background:#f6f7f9;border-radius:10px;padding:12px 14px;margin:12px 0}` +
		`table{border-collapse:collapse;width:100%;font-size:13px;margin:8px 0 4px}` +
		`th,td{text-align:left;padding:4px 8px;border-bottom:1px solid #eef1f4}` +
		`td.n,th.n{text-align:right;font-variant-numeric:tabular-nums}a{color:#15803d;text-decoration:none}a:hover{text-decoration:underline}` +
		`code{background:#f1f5f9;padding:1px 5px;border-radius:5px}details{margin:6px 0}summary{cursor:pointer;font-weight:600}</style>`)
	b.WriteString(`</head><body>`)

	b.WriteString(`<h1>Charging — open EV charging dataset</h1>`)
	fmt.Fprintf(&b, `<div class="sum"><b>%s</b> chargers · <b>%s</b> priced · <b>%d</b> regions`,
		commas(m.Chargers), commas(m.PricedChargers), len(m.Regions))
	if !m.GeneratedAt.IsZero() {
		fmt.Fprintf(&b, ` · generated <span class="muted">%s UTC</span>`, m.GeneratedAt.UTC().Format("2006-01-02 15:04"))
	}
	b.WriteString(`<br><span class="muted">License: ` + html.EscapeString(m.License) + " — " + html.EscapeString(m.Attribution) + `</span></div>`)

	b.WriteString(`<p>Each region (country + postal prefix) is published in four formats. ` +
		`Machine-readable manifest: <a href="index.json">index.json</a>` +
		fileLink(m, "availability.json", ` · live availability delta: <a href="availability.json">availability.json</a>`) +
		`</p>`)
	b.WriteString(`<p class="muted">Formats: <b>.ndjson</b> one normalized record per line · ` +
		`<b>.geojson</b> a point layer for maps · <b>OCPI</b> Locations + Tariffs (roaming-shaped).</p>`)

	// Group regions by country (prefix before the first '-').
	byCountry := map[string][]string{}
	for _, r := range m.Regions {
		c := r
		if i := strings.IndexByte(r, '-'); i >= 0 {
			c = r[:i]
		}
		byCountry[c] = append(byCountry[c], r)
	}
	countries := make([]string, 0, len(byCountry))
	for c := range byCountry {
		countries = append(countries, c)
	}
	sort.Strings(countries)

	for _, c := range countries {
		regions := byCountry[c]
		sort.Strings(regions)
		fmt.Fprintf(&b, `<h2>%s <span class="muted">(%d regions)</span></h2>`, html.EscapeString(countryName(c)), len(regions))
		b.WriteString(`<table><thead><tr><th>Region</th><th>NDJSON</th><th>GeoJSON</th><th>OCPI Locations</th><th>OCPI Tariffs</th></tr></thead><tbody>`)
		for _, r := range regions {
			fmt.Fprintf(&b, `<tr><td><code>%s</code></td>`, html.EscapeString(r))
			cell(&b, m, "ndjson/"+r+".ndjson")
			cell(&b, m, "geojson/"+r+".geojson")
			cell(&b, m, "ocpi/"+r+"-locations.json")
			cell(&b, m, "ocpi/"+r+"-tariffs.json")
			b.WriteString(`</tr>`)
		}
		b.WriteString(`</tbody></table>`)
	}

	b.WriteString(`</body></html>`)
	_, err := io.WriteString(w, b.String())
	return err
}

// cell renders a download link + size for one file, or a dash if absent.
func cell(b *strings.Builder, m Manifest, name string) {
	if f, ok := m.Files[name]; ok {
		fmt.Fprintf(b, `<td class="n"><a href="%s">%s</a></td>`, html.EscapeString(name), humanBytes(f.Bytes))
		return
	}
	b.WriteString(`<td class="n muted">—</td>`)
}

// fileLink returns the suffix only when the file exists in the manifest.
func fileLink(m Manifest, name, suffix string) string {
	if _, ok := m.Files[name]; ok {
		return suffix
	}
	return ""
}

func countryName(code string) string {
	switch code {
	case "BE":
		return "🇧🇪 Belgium"
	case "NL":
		return "🇳🇱 Netherlands"
	case "DE":
		return "🇩🇪 Germany"
	case "FR":
		return "🇫🇷 France"
	case "XX":
		return "Other / unknown"
	}
	return code
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

func commas(n int) string {
	s := fmt.Sprintf("%d", n)
	if n < 0 {
		return s
	}
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	return string(out)
}
