// Package cloudclient is the control plane's HTTP client for the Gombit Cloud
// build API (gombit-cloud §51): create a build for a Cloud project, upload the
// project's compiled source, and read the build's state. It is the Forge side of
// the M4 build integration (ADR-005 §4.4/D3) — Forge hands Cloud source and
// observes; it never builds. Cloud owns build execution (ADR-005 D2).
//
// The client is deliberately narrow — three calls against the concrete §51
// endpoints — and carries no ret/queue policy of its own; the build worker owns
// orchestration. Every call presents an API token as `Authorization: Bearer` and
// unwraps Cloud's D10 envelope ({"data": …} / {"error": {code, message}}).
package cloudclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Client talks to a Gombit Cloud control plane. The zero value is not usable;
// BaseURL and Token are required.
type Client struct {
	// BaseURL is the Cloud API root (scheme + host, no trailing slash), e.g.
	// "https://cloud.gombit.dev". The client appends the /api/v1 path itself.
	BaseURL string
	// Token is a Cloud API token, presented as `Authorization: Bearer <token>`.
	Token string
	// HTTP is the transport. Nil uses a client with a sane default timeout.
	HTTP *http.Client
}

// Build is the subset of a Cloud build's state the control plane tracks.
type Build struct {
	ID             string `json:"id"`
	Status         string `json:"status"`
	ArtifactDigest string `json:"artifact_digest,omitempty"`
}

// BuildLog is one build log line as Cloud returns it (§40). Forge reads these
// from Cloud and relays them; it never stores them (ADR-005 §24).
type BuildLog struct {
	Timestamp string `json:"timestamp"`
	Stream    string `json:"stream,omitempty"`
	Message   string `json:"message"`
}

// Error is a non-2xx Cloud response, carrying the D10 error envelope's code and
// message alongside the HTTP status, so a caller can branch on the code (e.g.
// "build_not_queued") rather than string-matching.
type Error struct {
	Status  int
	Code    string
	Message string
}

func (e *Error) Error() string {
	return fmt.Sprintf("cloud: %d %s: %s", e.Status, e.Code, e.Message)
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

// CreateBuild creates a build for cloudProjectID referencing sourceCommit
// (opaque provenance) and returns it in the "queued" state. §51
// POST /projects/{projectID}/builds.
func (c *Client) CreateBuild(ctx context.Context, cloudProjectID, sourceCommit string) (Build, error) {
	body, err := json.Marshal(map[string]string{"source_commit": sourceCommit})
	if err != nil {
		return Build{}, fmt.Errorf("cloud: encode create build: %w", err)
	}
	var b Build
	if err := c.do(ctx, http.MethodPost, "/projects/"+cloudProjectID+"/builds", "application/json", bytes.NewReader(body), &b); err != nil {
		return Build{}, err
	}
	return b, nil
}

// UploadSource uploads a `.tar.gz` source archive for a queued build, which
// starts the build asynchronously on Cloud. §51
// PUT /projects/{projectID}/builds/{buildID}/source. A build that is no longer
// queued yields an *Error with Code "build_not_queued".
func (c *Client) UploadSource(ctx context.Context, cloudProjectID, buildID string, archive io.Reader) error {
	return c.do(ctx, http.MethodPut, "/projects/"+cloudProjectID+"/builds/"+buildID+"/source", "application/gzip", archive, nil)
}

// GetBuild reads a build's current state. §51
// GET /projects/{projectID}/builds/{buildID}.
func (c *Client) GetBuild(ctx context.Context, cloudProjectID, buildID string) (Build, error) {
	var b Build
	if err := c.do(ctx, http.MethodGet, "/projects/"+cloudProjectID+"/builds/"+buildID, "", nil, &b); err != nil {
		return Build{}, err
	}
	return b, nil
}

// GetBuildLogs reads a build's log lines from Cloud, optionally only those after
// `since` (an RFC3339 timestamp) for tailing. §51 GET /builds/{buildID}/logs.
// The build id addresses the log owner directly, so no Cloud project id is
// needed. An empty `since` returns all lines.
func (c *Client) GetBuildLogs(ctx context.Context, buildID, since string) ([]BuildLog, error) {
	path := "/builds/" + buildID + "/logs"
	if since != "" {
		path += "?since=" + url.QueryEscape(since)
	}
	var wrap struct {
		Logs []BuildLog `json:"logs"`
	}
	if err := c.do(ctx, http.MethodGet, path, "", nil, &wrap); err != nil {
		return nil, err
	}
	return wrap.Logs, nil
}

// do issues a request against the §51 API and unwraps the D10 envelope. On a
// 2xx it decodes {"data": …} into out (when out is non-nil); on a non-2xx it
// decodes {"error": {code, message}} into an *Error. contentType is set only when
// a body is sent.
func (c *Client) do(ctx context.Context, method, path, contentType string, body io.Reader, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+"/api/v1"+path, body)
	if err != nil {
		return fmt.Errorf("cloud: build request: %w", err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("cloud: %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("cloud: read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return decodeError(resp.StatusCode, raw)
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("cloud: decode response: %w", err)
	}
	if err := json.Unmarshal(env.Data, out); err != nil {
		return fmt.Errorf("cloud: decode data: %w", err)
	}
	return nil
}

// decodeError turns a non-2xx response into an *Error, falling back to a generic
// code/message when the body is not the expected error envelope.
func decodeError(status int, raw []byte) error {
	var env struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &env); err != nil || env.Error.Code == "" {
		return &Error{Status: status, Code: "unexpected", Message: http.StatusText(status)}
	}
	return &Error{Status: status, Code: env.Error.Code, Message: env.Error.Message}
}
