// Copyright 2026 The Taisce Authors
// SPDX-License-Identifier: Apache-2.0

package taisce

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client speaks the v1 contract to one deployment with one credential. It is safe for concurrent
// use.
type Client struct {
	base  string
	token string
	http  *http.Client
}

// Option configures a Client.
type Option func(*Client)

// WithHTTPClient replaces the HTTP client. The replacement is used as given: a caller who supplies
// one that follows redirects takes back the decision this package made not to.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.http = h }
}

// New builds a client for the deployment at baseURL, authenticated by token.
//
// An address that is not https is refused unless its host is loopback (localhost, 127.0.0.0/8,
// ::1). The token travels with every request and a project holds every end user's memory, so one
// mistyped scheme would hand the credential to anyone on the path. There is no switch to allow
// more: nothing has needed one, and a switch is what gets left on.
func New(baseURL, token string, opts ...Option) (*Client, error) {
	if strings.TrimSpace(baseURL) == "" {
		return nil, errors.New("taisce: a deployment address is required")
	}
	if strings.TrimSpace(token) == "" {
		return nil, errors.New("taisce: a credential is required")
	}
	if err := requireTransport(baseURL); err != nil {
		return nil, err
	}
	c := &Client{
		base:  strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		token: strings.TrimSpace(token),
		// No redirect is followed. The standard client keeps the Authorization header on a redirect
		// to the same host, and a deployment never answers with one, so a redirect is either a
		// misconfigured proxy or somebody else's server: neither should see the credential again.
		http: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

func requireTransport(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err == nil && u.Host != "" {
		switch {
		case u.Scheme == "https" && u.Hostname() != "":
			return nil
		case u.Scheme == "http" && isLoopback(u.Hostname()):
			return nil
		}
	}
	return errors.New("taisce: a deployment address must be https, or http to a loopback host: " +
		"the credential travels with every request and would otherwise cross the network in clear")
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Error is a refusal from the deployment, carrying the contract's own code. Branch on Code, or use
// errors.Is with ErrForbidden or ErrExportTooLarge.
type Error struct {
	Status  int
	Code    string
	Message string
}

func (e *Error) Error() string { return fmt.Sprintf("taisce: %d %s: %s", e.Status, e.Code, e.Message) }

var (
	// ErrForbidden is a credential whose access does not cover the operation: a read-only key
	// changing memory, or an agent key erasing a person. Retrying with the same key cannot succeed.
	ErrForbidden = errors.New("taisce: the credential does not permit this operation")
	// ErrExportTooLarge is a subject holding more rows than one export returns. The deployment
	// refuses rather than truncates, because a partial export is a false answer to an access request.
	ErrExportTooLarge = errors.New("taisce: the export is larger than one response")
)

// Is matches the sentinel a refusal stands for, by status and code together.
func (e *Error) Is(target error) bool {
	switch target {
	case ErrForbidden:
		return e.Status == http.StatusForbidden && e.Code == "forbidden"
	case ErrExportTooLarge:
		return e.Status == http.StatusUnprocessableEntity && e.Code == "export_too_large"
	case ErrArtifactNotFound:
		return e.Status == http.StatusNotFound && e.Code == "not_found"
	case ErrArtifactConflict:
		return e.Status == http.StatusConflict && e.Code == "artifact_conflict"
	}
	return false
}

func (c *Client) post(ctx context.Context, path string, body any, success int, into any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req, success, into)
}

func (c *Client) get(ctx context.Context, path string, success int, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return err
	}
	return c.do(req, success, into)
}

func (c *Client) do(req *http.Request, success int, into any) error {
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	// A bound on what is read, because an answer the client never asked for is not trusted to be
	// small. Sixty-four megabytes is above any export the deployment returns in one response.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != success {
		refusal := &Error{Status: resp.StatusCode, Code: "unexpected_response", Message: "the deployment answered " + resp.Status}
		var body struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(raw, &body) == nil && body.Error.Code != "" {
			refusal.Code, refusal.Message = body.Error.Code, body.Error.Message
		}
		return refusal
	}
	if into == nil {
		return nil
	}
	return json.Unmarshal(raw, into)
}
