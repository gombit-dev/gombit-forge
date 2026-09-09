package cloudclient

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// cloudStub is a minimal stand-in for the §51 build endpoints. Each handler
// asserts the request shape the client is supposed to send.
func cloudStub(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("POST /api/v1/projects/{projectID}/builds", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer tok-123" {
			t.Errorf("create build auth = %q, want Bearer tok-123", got)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("create build content-type = %q", ct)
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"source_commit":"rev-abc"`) {
			t.Errorf("create build body = %s", body)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"data":{"id":"bld_1","status":"queued"}}`))
	})

	mux.HandleFunc("PUT /api/v1/projects/{projectID}/builds/{id}/source", func(w http.ResponseWriter, r *http.Request) {
		if ct := r.Header.Get("Content-Type"); ct != "application/gzip" {
			t.Errorf("upload content-type = %q, want application/gzip", ct)
		}
		body, _ := io.ReadAll(r.Body)
		if string(body) != "tar-gz-bytes" {
			t.Errorf("upload body = %q", body)
		}
		if r.PathValue("id") != "bld_1" {
			t.Errorf("upload build id = %q", r.PathValue("id"))
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"data":{"id":"bld_1","status":"queued"}}`))
	})

	mux.HandleFunc("GET /api/v1/projects/{projectID}/builds/{id}", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"id":"bld_1","status":"succeeded","artifact_digest":"sha256:cafe"}}`))
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestClientBuildLifecycle(t *testing.T) {
	srv := cloudStub(t)
	c := &Client{BaseURL: srv.URL, Token: "tok-123"}
	ctx := context.Background()

	b, err := c.CreateBuild(ctx, "prj_9", "rev-abc")
	if err != nil {
		t.Fatalf("CreateBuild: %v", err)
	}
	if b.ID != "bld_1" || b.Status != "queued" {
		t.Fatalf("created build = %+v", b)
	}

	if err := c.UploadSource(ctx, "prj_9", b.ID, strings.NewReader("tar-gz-bytes")); err != nil {
		t.Fatalf("UploadSource: %v", err)
	}

	got, err := c.GetBuild(ctx, "prj_9", b.ID)
	if err != nil {
		t.Fatalf("GetBuild: %v", err)
	}
	if got.Status != "succeeded" || got.ArtifactDigest != "sha256:cafe" {
		t.Fatalf("fetched build = %+v", got)
	}
}

func TestClientGetBuildLogs(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/builds/{id}/logs", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("id") != "bld_1" {
			t.Errorf("build id = %q", r.PathValue("id"))
		}
		// The since filter is forwarded for tailing.
		if got := r.URL.Query().Get("since"); got != "2026-01-01T00:00:00Z" {
			t.Errorf("since = %q", got)
		}
		_, _ = w.Write([]byte(`{"data":{"logs":[{"timestamp":"2026-01-01T00:00:01Z","stream":"build","message":"cloning"}]}}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c := &Client{BaseURL: srv.URL, Token: "tok"}
	logs, err := c.GetBuildLogs(context.Background(), "bld_1", "2026-01-01T00:00:00Z")
	if err != nil {
		t.Fatalf("GetBuildLogs: %v", err)
	}
	if len(logs) != 1 || logs[0].Message != "cloning" || logs[0].Stream != "build" {
		t.Fatalf("logs = %+v", logs)
	}
}

func TestClientErrorEnvelope(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/v1/projects/{projectID}/builds/{id}/source", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":{"code":"build_not_queued","message":"source can only be submitted to a queued build"}}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c := &Client{BaseURL: srv.URL, Token: "t"}
	err := c.UploadSource(context.Background(), "p", "b", strings.NewReader("x"))
	var ce *Error
	if !errors.As(err, &ce) {
		t.Fatalf("error = %v, want *cloudclient.Error", err)
	}
	if ce.Status != http.StatusConflict || ce.Code != "build_not_queued" {
		t.Fatalf("error = %+v, want 409 build_not_queued", ce)
	}
}

func TestClientNonEnvelopeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	c := &Client{BaseURL: srv.URL, Token: "t"}
	_, err := c.GetBuild(context.Background(), "p", "b")
	var ce *Error
	if !errors.As(err, &ce) || ce.Status != http.StatusInternalServerError || ce.Code != "unexpected" {
		t.Fatalf("error = %v, want 500 unexpected", err)
	}
}
