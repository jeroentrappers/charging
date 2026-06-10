// Command chargingctl drives the system entirely through the HTTP API (read +
// admin) — it never touches the database directly, exactly like the web client.
//
// Config (env): CHARGING_API (default http://localhost:8080), ADMIN_TOKEN (for
// admin commands). Most commands accept --json for raw output.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	c := &client{
		base:       envOr("CHARGING_API", "http://localhost:8080"),
		adminToken: os.Getenv("ADMIN_TOKEN"),
		http:       &http.Client{Timeout: 30 * time.Second},
	}

	var err error
	switch os.Args[1] {
	case "chargers":
		err = cmdChargers(c, os.Args[2:])
	case "sessions":
		err = cmdSessions(c, os.Args[2:])
	case "stats":
		err = cmdStats(c, os.Args[2:])
	case "sources":
		err = cmdSources(c, os.Args[2:])
	case "ingest":
		err = cmdIngest(c, os.Args[2:])
	case "runs":
		err = cmdRuns(c, os.Args[2:])
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Print(`chargingctl — drive the charging API/CLI

Read:
  chargers cheapest --lat L --lon L [--radius m --session key --available --min-power kW --plug P --limit N] [--json]
  sessions [--json]
  stats overview|sessions|regions|price-trend [--by city|postal] [--months N] [--json]

Admin (needs ADMIN_TOKEN):
  sources list [--json]
  sources add --id ID --name N --url U [--version V --type ocpi|datex --token-env E --poll-cron C --status-cron C]
  sources enable|disable|rm ID
  sources set-token ID TOKEN
  ingest run ID [--kind price|availability]
  runs [--cpo ID --limit N] [--json]

Env: CHARGING_API (default http://localhost:8080), ADMIN_TOKEN
`)
}

// ---- HTTP client ----

type client struct {
	base       string
	adminToken string
	http       *http.Client
}

func (c *client) do(method, path string, query url.Values, body any, admin bool) ([]byte, error) {
	u := strings.TrimRight(c.base, "/") + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, u, r)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if admin {
		if c.adminToken == "" {
			return nil, fmt.Errorf("ADMIN_TOKEN is required for this command")
		}
		req.Header.Set("Authorization", "Bearer "+c.adminToken)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("API %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return data, nil
}

// ---- commands ----

func cmdChargers(c *client, args []string) error {
	if len(args) == 0 || args[0] != "cheapest" {
		return fmt.Errorf("usage: chargers cheapest --lat L --lon L [...]")
	}
	fs := newFlags("chargers cheapest")
	lat := fs.String("lat", "", "latitude (required)")
	lon := fs.String("lon", "", "longitude (required)")
	radius := fs.String("radius", "", "radius in metres")
	session := fs.String("session", "", "session profile key")
	plug := fs.String("plug", "", "plug type filter")
	minPower := fs.String("min-power", "", "minimum power kW")
	avail := fs.Bool("available", false, "only available")
	limit := fs.String("limit", "", "max results")
	jsonOut := fs.Bool("json", false, "raw JSON")
	fs.Parse(args[1:])
	if *lat == "" || *lon == "" {
		return fmt.Errorf("--lat and --lon are required")
	}

	q := url.Values{"lat": {*lat}, "lon": {*lon}}
	setIf(q, "radius", *radius)
	setIf(q, "session", *session)
	setIf(q, "plug", *plug)
	setIf(q, "min_power", *minPower)
	setIf(q, "limit", *limit)
	if *avail {
		q.Set("available", "true")
	}
	data, err := c.do("GET", "/chargers/cheapest", q, nil, false)
	if err != nil {
		return err
	}
	if *jsonOut {
		return printRaw(data)
	}
	var res struct {
		Results []struct {
			Name         string   `json:"name"`
			PlugType     string   `json:"plug_type"`
			DistanceM    float64  `json:"distance_m"`
			PowerKW      float64  `json:"power_kw"`
			Available    int      `json:"available_count"`
			PriceEUR     *float64 `json:"comparable_price_eur"`
			SessionPrice *float64 `json:"session_price_eur"`
			Stale        bool     `json:"availability_stale"`
		} `json:"results"`
	}
	if err := json.Unmarshal(data, &res); err != nil {
		return err
	}
	tw := tab()
	fmt.Fprintln(tw, "PRICE\tDIST\tkW\tPLUG\tAVAIL\tNAME")
	for _, r := range res.Results {
		p := r.PriceEUR
		if r.SessionPrice != nil {
			p = r.SessionPrice
		}
		fmt.Fprintf(tw, "%s\t%dm\t%.0f\t%s\t%s\t%s\n",
			eur(p), int(r.DistanceM), r.PowerKW, shorten(r.PlugType, 16), avComment(r.Available, r.Stale), r.Name)
	}
	return tw.Flush()
}

func cmdSessions(c *client, args []string) error {
	jsonOut := hasFlag(args, "--json")
	data, err := c.do("GET", "/sessions", nil, nil, false)
	if err != nil {
		return err
	}
	if jsonOut {
		return printRaw(data)
	}
	var res struct {
		Sessions []struct {
			Key, Label, Current string
			MeteredKW           float64 `json:"metered_kwh"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(data, &res); err != nil {
		return err
	}
	tw := tab()
	fmt.Fprintln(tw, "KEY\tCURRENT\tkWh\tLABEL")
	for _, s := range res.Sessions {
		fmt.Fprintf(tw, "%s\t%s\t%.1f\t%s\n", s.Key, s.Current, s.MeteredKW, s.Label)
	}
	return tw.Flush()
}

func cmdStats(c *client, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: stats overview|sessions|regions|price-trend")
	}
	sub := args[0]
	fs := newFlags("stats " + sub)
	by := fs.String("by", "city", "region grouping (city|postal)")
	months := fs.String("months", "", "trend months")
	jsonOut := fs.Bool("json", false, "raw JSON")
	fs.Parse(args[1:])

	q := url.Values{}
	switch sub {
	case "overview", "sessions":
	case "regions":
		setIf(q, "by", *by)
	case "price-trend":
		setIf(q, "months", *months)
	default:
		return fmt.Errorf("unknown stats subcommand %q", sub)
	}
	data, err := c.do("GET", "/stats/"+sub, q, nil, false)
	if err != nil {
		return err
	}
	// Stats payloads vary; print raw (pretty) unless caller wants compact.
	_ = jsonOut
	return printRaw(data)
}

func cmdSources(c *client, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: sources list|add|enable|disable|set-token|rm ...")
	}
	switch args[0] {
	case "list":
		data, err := c.do("GET", "/admin/sources", nil, nil, true)
		if err != nil {
			return err
		}
		if hasFlag(args, "--json") {
			return printRaw(data)
		}
		var res struct {
			Sources []struct {
				ID          string `json:"id"`
				SourceType  string `json:"source_type"`
				OCPIVersion string `json:"ocpi_version"`
				HasToken    bool   `json:"has_token"`
				Enabled     bool   `json:"enabled"`
			} `json:"sources"`
		}
		if err := json.Unmarshal(data, &res); err != nil {
			return err
		}
		tw := tab()
		fmt.Fprintln(tw, "ID\tTYPE\tVERSION\tENABLED\tTOKEN")
		for _, s := range res.Sources {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", s.ID, s.SourceType, dash(s.OCPIVersion), yn(s.Enabled), yn(s.HasToken))
		}
		return tw.Flush()
	case "add":
		fs := newFlags("sources add")
		id := fs.String("id", "", "")
		name := fs.String("name", "", "")
		u := fs.String("url", "", "OCPI base / DATEX feed URL")
		ver := fs.String("version", "2.1.1", "")
		typ := fs.String("type", "ocpi", "ocpi|datex")
		tokenEnv := fs.String("token-env", "", "")
		poll := fs.String("poll-cron", "", "")
		status := fs.String("status-cron", "", "")
		fs.Parse(args[1:])
		if *id == "" || *u == "" {
			return fmt.Errorf("--id and --url are required")
		}
		body := map[string]any{
			"id": *id, "name": *name, "ocpi_base_url": *u, "ocpi_version": *ver,
			"source_type": *typ, "token_env": *tokenEnv, "poll_cron": *poll, "status_cron": *status,
		}
		data, err := c.do("POST", "/admin/sources", nil, body, true)
		if err != nil {
			return err
		}
		return printRaw(data)
	case "enable", "disable":
		if len(args) < 2 {
			return fmt.Errorf("usage: sources %s ID", args[0])
		}
		_, err := c.do("POST", "/admin/sources/"+args[1]+"/"+args[0], nil, nil, true)
		if err == nil {
			fmt.Printf("%s %sd\n", args[1], args[0])
		}
		return err
	case "set-token":
		if len(args) < 3 {
			return fmt.Errorf("usage: sources set-token ID TOKEN")
		}
		_, err := c.do("PUT", "/admin/sources/"+args[1]+"/token", nil, map[string]string{"token": args[2]}, true)
		if err == nil {
			fmt.Printf("token set for %s\n", args[1])
		}
		return err
	case "rm":
		if len(args) < 2 {
			return fmt.Errorf("usage: sources rm ID")
		}
		_, err := c.do("DELETE", "/admin/sources/"+args[1], nil, nil, true)
		if err == nil {
			fmt.Printf("%s deleted\n", args[1])
		}
		return err
	}
	return fmt.Errorf("unknown sources subcommand %q", args[0])
}

func cmdIngest(c *client, args []string) error {
	if len(args) < 2 || args[0] != "run" {
		return fmt.Errorf("usage: ingest run ID [--kind price|availability]")
	}
	id := args[1]
	fs := newFlags("ingest run")
	kind := fs.String("kind", "price", "price|availability")
	fs.Parse(args[2:])
	q := url.Values{"kind": {*kind}}
	_, err := c.do("POST", "/admin/ingest/"+id+"/run", q, nil, true)
	if err == nil {
		fmt.Printf("%s ingestion (%s) started — check `chargingctl runs --cpo %s`\n", id, *kind, id)
	}
	return err
}

func cmdRuns(c *client, args []string) error {
	fs := newFlags("runs")
	cpo := fs.String("cpo", "", "filter by CPO id")
	limit := fs.String("limit", "", "max rows")
	jsonOut := fs.Bool("json", false, "raw JSON")
	fs.Parse(args)
	q := url.Values{}
	setIf(q, "cpo", *cpo)
	setIf(q, "limit", *limit)
	data, err := c.do("GET", "/admin/runs", q, nil, true)
	if err != nil {
		return err
	}
	if *jsonOut {
		return printRaw(data)
	}
	var res struct {
		Runs []struct {
			CPOID     string    `json:"cpo_id"`
			Kind      string    `json:"kind"`
			StartedAt time.Time `json:"started_at"`
			RowsSeen  int       `json:"rows_seen"`
			Changes   int       `json:"changes"`
			Error     *string   `json:"error"`
		} `json:"runs"`
	}
	if err := json.Unmarshal(data, &res); err != nil {
		return err
	}
	tw := tab()
	fmt.Fprintln(tw, "STARTED\tCPO\tKIND\tROWS\tCHANGES\tRESULT")
	for _, r := range res.Runs {
		result := "ok"
		if r.Error != nil {
			result = "ERR: " + shorten(*r.Error, 40)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%d\t%s\n",
			r.StartedAt.Format("01-02 15:04"), r.CPOID, r.Kind, r.RowsSeen, r.Changes, result)
	}
	return tw.Flush()
}

// ---- helpers ----

func newFlags(name string) *flag.FlagSet { return flag.NewFlagSet(name, flag.ContinueOnError) }

func tab() *tabwriter.Writer { return tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0) }

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func setIf(q url.Values, key, val string) {
	if val != "" {
		q.Set(key, val)
	}
}

func hasFlag(args []string, f string) bool {
	for _, a := range args {
		if a == f {
			return true
		}
	}
	return false
}

func printRaw(data []byte) error {
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		os.Stdout.Write(data)
		return nil
	}
	b, _ := json.MarshalIndent(v, "", "  ")
	fmt.Println(string(b))
	return nil
}

func eur(p *float64) string {
	if p == nil {
		return "—"
	}
	return "€" + strconv.FormatFloat(*p, 'f', 2, 64)
}

func avComment(available int, stale bool) string {
	switch {
	case stale:
		return "stale"
	case available > 0:
		return "free"
	default:
		return "in use"
	}
}

func yn(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func dash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func shorten(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
