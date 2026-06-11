package ocpi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Credentials is the OCPI credentials object exchanged during the registration
// handshake. `Token` is the token the *receiver* of this object must use to
// call the *sender*; `URL` is the sender's versions endpoint.
type Credentials struct {
	Token string            `json:"token"`
	URL   string            `json:"url"`
	Roles []CredentialsRole `json:"roles"`
}

type CredentialsRole struct {
	Role            string          `json:"role"` // CPO | EMSP
	BusinessDetails BusinessDetails `json:"business_details"`
	PartyID         string          `json:"party_id"`
	CountryCode     string          `json:"country_code"`
}

// Version is one entry of the OCPI versions list.
type Version struct {
	Version string `json:"version"`
	URL     string `json:"url"`
}

// RegisterResult is what we keep after a successful handshake with a CPO.
type RegisterResult struct {
	TokenC            string            // token we use for all future calls to them
	Version           string            // negotiated version (e.g. "2.2.1")
	VersionDetailsURL string            // their version-details URL (use as client BaseURL)
	Endpoints         map[string]string // their module identifier -> URL
}

// EncodeToken applies the OCPI token-transport rule: 2.2+ base64-encodes the
// token; 2.1.1 sends it raw.
func EncodeToken(token, version string) string {
	if strings.HasPrefix(version, "2.2") {
		return base64.StdEncoding.EncodeToString([]byte(token))
	}
	return token
}

// Register performs the eMSP-initiated OCPI credentials handshake: given the
// CPO's versions URL and the Token A they shared out-of-band, it discovers the
// version + endpoints, POSTs our credentials (carrying the token they'll use to
// call us) to their credentials endpoint, and returns the Token C they issue.
func Register(ctx context.Context, versionsURL, tokenA, wantVersion string, ours Credentials) (RegisterResult, error) {
	hc := &http.Client{Timeout: 30 * time.Second}
	enc := func(v string) string { return EncodeToken(tokenA, v) }

	// 1) versions list (the token-transport version is the one we want).
	var versions Envelope[Version]
	if err := getJSON(ctx, hc, versionsURL, "Token "+enc(wantVersion), &versions); err != nil {
		return RegisterResult{}, fmt.Errorf("versions: %w", err)
	}
	ver := pickVersion(versions.Data, wantVersion)
	if ver.URL == "" {
		return RegisterResult{}, fmt.Errorf("no usable OCPI version offered (wanted %s)", wantVersion)
	}

	// 2) version details -> module endpoints (incl. credentials).
	var details ObjectEnvelope[VersionDetails]
	if err := getJSON(ctx, hc, ver.URL, "Token "+enc(ver.Version), &details); err != nil {
		return RegisterResult{}, fmt.Errorf("version details: %w", err)
	}
	eps := map[string]string{}
	for _, e := range details.Data.Endpoints {
		if cur, ok := eps[e.Identifier]; ok && cur != "" && e.Role != "" && e.Role != "SENDER" {
			continue // prefer SENDER interface
		}
		eps[e.Identifier] = e.URL
	}
	credURL := eps["credentials"]
	if credURL == "" {
		return RegisterResult{}, fmt.Errorf("CPO exposes no credentials endpoint")
	}

	// 3) POST our credentials -> receive theirs (with Token C).
	body, _ := json.Marshal(ours)
	var theirs ObjectEnvelope[Credentials]
	if err := sendJSON(ctx, hc, http.MethodPost, credURL, "Token "+enc(ver.Version), body, &theirs); err != nil {
		return RegisterResult{}, fmt.Errorf("post credentials: %w", err)
	}
	if theirs.Data.Token == "" {
		return RegisterResult{}, fmt.Errorf("CPO returned no token")
	}
	return RegisterResult{TokenC: theirs.Data.Token, Version: ver.Version, VersionDetailsURL: ver.URL, Endpoints: eps}, nil
}

func pickVersion(vs []Version, want string) Version {
	for _, v := range vs {
		if v.Version == want {
			return v
		}
	}
	// else highest version string offered (2.2.1 > 2.1.1)
	sort.Slice(vs, func(i, j int) bool { return vs[i].Version > vs[j].Version })
	if len(vs) > 0 {
		return vs[0]
	}
	return Version{}
}

func getJSON(ctx context.Context, hc *http.Client, url, auth string, out any) error {
	return sendJSON(ctx, hc, http.MethodGet, url, auth, nil, out)
}

func sendJSON(ctx context.Context, hc *http.Client, method, url, auth string, body []byte, out any) error {
	var rdr *bytes.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", auth)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("http %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
