package ingest

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/appmire/charging/internal/datex"
	"github.com/appmire/charging/internal/model"
)

// mobilithekFeed consumes one German CPO/aggregator offering from the Mobilithek
// NAP (AFIR DATEX II Recharging profile) over mutual-TLS. OCPIBaseURL holds the
// two subscription pull URLs joined by "|":  "<static-url>|<status-url>". The
// static publication carries locations/power/plug + ad-hoc price; the status
// publication carries live availability + price updates. The client certificate
// (org-issued, see docs) comes from MOBILITHEK_CERT_FILE / _KEY_FILE / _CA_FILE.
type mobilithekFeed struct {
	cpoID     string
	staticURL string
	statusURL string
}

func newMobilithekFeed(cpoID, baseURL string) mobilithekFeed {
	st, dyn, _ := strings.Cut(baseURL, "|")
	return mobilithekFeed{cpoID: cpoID, staticURL: strings.TrimSpace(st), statusURL: strings.TrimSpace(dyn)}
}

func (f mobilithekFeed) Availability(ctx context.Context) ([]model.Connector, error) {
	conns, _, err := f.load(ctx)
	return conns, err
}

func (f mobilithekFeed) Full(ctx context.Context) ([]model.Connector, map[string]model.Tariff, error) {
	return f.load(ctx)
}

// load fetches the static publication (identity + ad-hoc price), then overlays
// the status publication (live availability + price updates) when present.
func (f mobilithekFeed) load(ctx context.Context) ([]model.Connector, map[string]model.Tariff, error) {
	cl, err := mobilithekClient()
	if err != nil {
		return nil, nil, err
	}
	staticXML, err := fetchGzipXML(ctx, cl, f.staticURL)
	if err != nil {
		return nil, nil, fmt.Errorf("mobilithek static %s: %w", f.cpoID, err)
	}
	conns, tariffs, err := datex.ParseAFIRStatic(f.cpoID, staticXML)
	if err != nil {
		return nil, nil, fmt.Errorf("parse afir static: %w", err)
	}
	if f.statusURL != "" {
		if statusXML, serr := fetchGzipXML(ctx, cl, f.statusURL); serr == nil && len(statusXML) > 0 {
			if st, perr := datex.ParseAFIRStatus(statusXML); perr == nil {
				for i := range conns {
					s, ok := st[conns[i].EVSEUID]
					if !ok {
						continue
					}
					if s.Status != "" {
						conns[i].EVSEStatus = s.Status
					}
					if s.Tariff != nil { // live price update wins
						if conns[i].TariffID == "" {
							conns[i].TariffID = conns[i].EVSEUID
						}
						tariffs[conns[i].TariffID] = *s.Tariff
					}
				}
			}
		}
	}
	return conns, tariffs, nil
}

// mobilithekClient builds the mutual-TLS HTTP client from the org client
// certificate. Returns a clear error if the cert env vars aren't configured.
func mobilithekClient() (*http.Client, error) {
	certFile, keyFile, caFile := os.Getenv("MOBILITHEK_CERT_FILE"), os.Getenv("MOBILITHEK_KEY_FILE"), os.Getenv("MOBILITHEK_CA_FILE")
	if certFile == "" || keyFile == "" {
		return nil, fmt.Errorf("mobilithek: set MOBILITHEK_CERT_FILE and MOBILITHEK_KEY_FILE (org client certificate)")
	}
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("mobilithek client cert: %w", err)
	}
	tlsCfg := &tls.Config{Certificates: []tls.Certificate{cert}}
	if caFile != "" {
		pem, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("mobilithek CA: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("mobilithek CA: no certs in %s", caFile)
		}
		tlsCfg.RootCAs = pool
	}
	return &http.Client{Timeout: 120 * time.Second, Transport: &http.Transport{TLSClientConfig: tlsCfg}}, nil
}

// fetchGzipXML GETs a Mobilithek subscription. Responses are always gzip (the
// broker rejects identity encoding), and because we set Accept-Encoding
// ourselves Go won't auto-decompress — so we gunzip by magic bytes. 204/304
// (no new packet / not modified) return empty, not an error.
func fetchGzipXML(ctx context.Context, cl *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/xml")
	req.Header.Set("Accept-Encoding", "gzip")
	resp, err := cl.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusNotModified {
		return nil, nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 512<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http %d", resp.StatusCode)
	}
	if len(body) >= 2 && body[0] == 0x1f && body[1] == 0x8b {
		zr, zerr := gzip.NewReader(bytes.NewReader(body))
		if zerr != nil {
			return nil, fmt.Errorf("gunzip: %w", zerr)
		}
		defer zr.Close()
		if body, err = io.ReadAll(io.LimitReader(zr, 1<<30)); err != nil {
			return nil, fmt.Errorf("gunzip read: %w", err)
		}
	}
	return body, nil
}
