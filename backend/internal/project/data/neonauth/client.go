// Package neonauth is the outbound adapter that verifies dashboard sessions with Neon Auth
// (Better Auth). It implements repository.SessionVerifier and fails closed: anything other than
// a clear "this session is valid" or "there is no such session" is ErrAuthUnavailable.
package neonauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"hookrelay/internal/project/domain/entity"
	domainerrors "hookrelay/internal/project/domain/errors"
)

const (
	getSessionPath = "/get-session"
	maxBodyBytes   = 64 << 10

	headerCookie  = "Cookie"
	headerOrigin  = "Origin"
	headerAccept  = "Accept"
	mediaTypeJSON = "application/json"
)

// uuidPattern is the shape of a Neon Auth user ID, which the projects table stores as uuid.
var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Options configure a Client. HTTP must carry the timeout and should not follow redirects.
type Options struct {
	HTTP       *http.Client
	BaseURL    string
	CookieName string
	Origin     string
}

// Client calls GET {base}/get-session.
type Client struct {
	http       *http.Client
	url        string
	cookieName string
	origin     string
}

// NewClient builds a Client. A trailing slash on BaseURL is ignored.
func NewClient(o Options) *Client {
	return &Client{
		http:       o.HTTP,
		url:        strings.TrimRight(o.BaseURL, "/") + getSessionPath,
		cookieName: o.CookieName,
		origin:     o.Origin,
	}
}

// sessionResponse is the part of the get-session body we use. The body is JSON null when
// there is no session, which decodes to a nil pointer.
type sessionResponse struct {
	Session struct {
		ExpiresAt time.Time `json:"expiresAt"`
	} `json:"session"`
	User struct {
		ID string `json:"id"`
	} `json:"user"`
}

// Verify asks Neon who the session belongs to. Errors never contain the token.
func (c *Client) Verify(ctx context.Context, token entity.SessionToken) (entity.SessionUser, error) {
	res, err := c.send(ctx, token)
	if err != nil {
		return entity.SessionUser{}, fmt.Errorf("get-session request: %w: %w", domainerrors.ErrAuthUnavailable, err)
	}
	// A failed close after the body was read leaves nothing to recover.
	defer func() { _ = res.Body.Close() }()
	return readSession(res)
}

func (c *Client) send(ctx context.Context, token entity.SessionToken) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set(headerCookie, c.cookieName+"="+token.Value())
	req.Header.Set(headerOrigin, c.origin)
	req.Header.Set(headerAccept, mediaTypeJSON)
	res, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	return res, nil
}

// readSession maps a get-session response to a user or a domain error.
func readSession(res *http.Response) (entity.SessionUser, error) {
	if res.StatusCode == http.StatusUnauthorized {
		return entity.SessionUser{}, domainerrors.ErrUnauthorized
	}
	if res.StatusCode != http.StatusOK {
		return entity.SessionUser{}, fmt.Errorf("get-session answered status %d: %w", res.StatusCode, domainerrors.ErrAuthUnavailable)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, maxBodyBytes))
	if err != nil {
		return entity.SessionUser{}, fmt.Errorf("read get-session body: %w: %w", domainerrors.ErrAuthUnavailable, err)
	}
	return parseSession(body)
}

func parseSession(body []byte) (entity.SessionUser, error) {
	var parsed *sessionResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return entity.SessionUser{}, fmt.Errorf("decode get-session body: %w: %w", domainerrors.ErrAuthUnavailable, err)
	}
	if parsed == nil {
		return entity.SessionUser{}, domainerrors.ErrUnauthorized
	}
	if !uuidPattern.MatchString(parsed.User.ID) {
		return entity.SessionUser{}, fmt.Errorf("get-session returned an unusable user id: %w", domainerrors.ErrAuthUnavailable)
	}
	return entity.SessionUser{ID: parsed.User.ID, ExpiresAt: parsed.Session.ExpiresAt.UTC()}, nil
}
