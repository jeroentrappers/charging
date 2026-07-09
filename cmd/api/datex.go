package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
)

// registerDatex wires the self-service DATEX II push subscription endpoints.
// Registration is open, but a callback is only activated after it proves
// ownership by echoing a challenge nonce (webhook-style verification), so the
// endpoint can't be turned into a blind push amplifier. Deletion requires the
// manage_secret handed back at registration.
func (s *server) registerDatex(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "datex-subscribe", Method: http.MethodPost, Path: "/datex/subscriptions",
		Summary:       "Register a DATEX II push callback (verified by challenge echo)",
		Tags:          []string{"DATEX II"},
		DefaultStatus: http.StatusCreated,
	}, s.opDatexSubscribe)

	huma.Register(api, huma.Operation{
		OperationID: "datex-unsubscribe", Method: http.MethodDelete, Path: "/datex/subscriptions/{id}",
		Summary:       "Remove a DATEX II push callback (requires the manage secret)",
		Tags:          []string{"DATEX II"},
		DefaultStatus: http.StatusNoContent,
	}, s.opDatexUnsubscribe)
}

type datexSubscribeIn struct {
	XForwardedFor string `header:"X-Forwarded-For"`
	XRealIP       string `header:"X-Real-IP"`
	Body          struct {
		CallbackURL string `json:"callback_url" doc:"HTTPS endpoint we POST each publication to"`
		Encoding    string `json:"encoding,omitempty" doc:"'xml' (default) or 'json'"`
		PushToken   string `json:"push_token,omitempty" doc:"Optional bearer we send on each push and on the verification request"`
	}
}

type datexSubscribeOut struct {
	Body struct {
		ID           int64  `json:"id"`
		Status       string `json:"status"`
		Encoding     string `json:"encoding"`
		ManageSecret string `json:"manage_secret" doc:"Keep this: required to delete the subscription"`
	}
}

func (s *server) opDatexSubscribe(ctx context.Context, in *datexSubscribeIn) (*datexSubscribeOut, error) {
	ip := clientIP(in.XForwardedFor, in.XRealIP)
	if !s.datexLimiter.allow(ip) {
		return nil, huma.Error429TooManyRequests("too many registration attempts; please slow down")
	}

	encoding := strings.ToLower(strings.TrimSpace(in.Body.Encoding))
	if encoding == "" {
		encoding = "xml"
	}
	if encoding != "xml" && encoding != "json" {
		return nil, huma.Error400BadRequest("encoding must be 'xml' or 'json'")
	}

	target, err := validateCallbackURL(in.Body.CallbackURL)
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}

	// Ownership + reachability proof: the callback must echo our challenge.
	challenge := randHex(16)
	if err := s.verifyCallback(ctx, target, in.Body.PushToken, challenge); err != nil {
		return nil, huma.Error422UnprocessableEntity("callback verification failed: " + err.Error())
	}

	secret := randHex(24)
	id, err := s.st.UpsertDatexSubscription(ctx, target, encoding, in.Body.PushToken, secret, time.Now().UTC())
	if err != nil {
		s.log.Error("datex subscribe: store", "err", err)
		return nil, huma.Error500InternalServerError("could not record subscription")
	}
	s.log.Info("datex subscription registered", "id", id, "url", target, "encoding", encoding)

	out := &datexSubscribeOut{}
	out.Body.ID = id
	out.Body.Status = "active"
	out.Body.Encoding = encoding
	out.Body.ManageSecret = secret
	return out, nil
}

type datexUnsubscribeIn struct {
	ID            int64  `path:"id"`
	Authorization string `header:"Authorization" doc:"Bearer <manage_secret> returned at registration"`
}

type datexUnsubscribeOut struct{}

func (s *server) opDatexUnsubscribe(ctx context.Context, in *datexUnsubscribeIn) (*datexUnsubscribeOut, error) {
	secret := strings.TrimSpace(strings.TrimPrefix(in.Authorization, "Bearer "))
	if secret == "" {
		return nil, huma.Error401Unauthorized("missing manage secret")
	}
	ok, err := s.st.DeleteDatexSubscription(ctx, in.ID, secret)
	if err != nil {
		s.log.Error("datex unsubscribe: store", "err", err)
		return nil, huma.Error500InternalServerError("could not remove subscription")
	}
	if !ok {
		return nil, huma.Error404NotFound("no such subscription, or wrong manage secret")
	}
	return &datexUnsubscribeOut{}, nil
}

// validateCallbackURL parses and vets a subscriber callback: it must be http(s)
// with a resolvable host that does not point at a loopback, private, or
// link-local address (SSRF guard). Returns the normalized URL.
func validateCallbackURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", errors.New("callback_url is not a valid URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", errors.New("callback_url must be http or https")
	}
	host := u.Hostname()
	if host == "" {
		return "", errors.New("callback_url is missing a host")
	}
	ips, err := net.LookupIP(host)
	if err != nil || len(ips) == 0 {
		return "", errors.New("callback_url host does not resolve")
	}
	for _, ip := range ips {
		if isInternalIP(ip) {
			return "", errors.New("callback_url resolves to a disallowed internal address")
		}
	}
	return u.String(), nil
}

func isInternalIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified()
}

// verifyCallback POSTs a challenge to the callback and requires it back. The
// endpoint must respond 2xx with the challenge echoed as the trimmed body or as
// a JSON {"challenge": "..."} object.
func (s *server) verifyCallback(ctx context.Context, target, token, challenge string) error {
	payload, _ := json.Marshal(map[string]string{"type": "verification", "challenge": challenge})
	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, target, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Datex-Hook-Challenge", challenge)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := s.datexVerifyClient.Do(req)
	if err != nil {
		return errors.New("callback unreachable")
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("callback returned http %d", resp.StatusCode)
	}
	if strings.TrimSpace(string(body)) == challenge {
		return nil
	}
	var echoed struct {
		Challenge string `json:"challenge"`
	}
	if json.Unmarshal(body, &echoed) == nil && echoed.Challenge == challenge {
		return nil
	}
	return errors.New("challenge not echoed")
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
