package taskcluster

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetHttpResponseCappedReturnsFullBodyUnderCap(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("hello world"))
	}))
	defer server.Close()

	content, contentType, truncated, err := getHttpResponseCapped(server.URL, 1024)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(content) != "hello world" {
		t.Fatalf("unexpected content: %q", content)
	}
	if contentType != "text/plain" {
		t.Fatalf("unexpected content type: %q", contentType)
	}
	if truncated {
		t.Fatalf("expected truncated=false for a body under the cap")
	}
}

func TestGetHttpResponseCappedTruncatesOversizedBody(t *testing.T) {
	body := strings.Repeat("x", 1000)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body))
	}))
	defer server.Close()

	content, _, truncated, err := getHttpResponseCapped(server.URL, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(content) != 100 {
		t.Fatalf("expected content capped at 100 bytes, got %d", len(content))
	}
	if !truncated {
		t.Fatalf("expected truncated=true for a body over the cap")
	}
}

func TestGetHttpResponseCappedFailsOnErrorStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"code":"InsufficientScopes","message":"You do not have sufficient scopes."}`))
	}))
	defer server.Close()

	content, _, _, err := getHttpResponseCapped(server.URL, 1024)
	if err == nil {
		t.Fatalf("expected an error for a 403 response, got content %q", content)
	}
	if content != nil {
		t.Fatalf("expected no content alongside the error, got %q", content)
	}

	var statusErr *HTTPStatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("expected an *HTTPStatusError, got %T: %v", err, err)
	}
	if statusErr.StatusCode != http.StatusForbidden {
		t.Fatalf("unexpected status code: %d", statusErr.StatusCode)
	}
	if !strings.Contains(err.Error(), "InsufficientScopes") {
		t.Fatalf("expected the error to name the failure, got %q", err.Error())
	}
}

func TestGetHttpResponseCappedFailsOnErrorStatusWithXMLBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`<Error><Code>AccessDenied</Code><Message>Access Denied</Message></Error>`))
	}))
	defer server.Close()

	_, _, _, err := getHttpResponseCapped(server.URL, 1024)
	if err == nil {
		t.Fatalf("expected an error for a 403 response")
	}
	if !strings.Contains(err.Error(), "AccessDenied") {
		t.Fatalf("expected the error to name the failure, got %q", err.Error())
	}
}

func TestGetHttpResponseCappedAcceptsSuccessfulStatuses(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusPartialContent, http.StatusNoContent} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			w.Write([]byte(`{"code":"NotAnError"}`))
		}))

		_, _, _, err := getHttpResponseCapped(server.URL, 1024)
		server.Close()
		if err != nil {
			t.Fatalf("unexpected error for status %d: %v", status, err)
		}
	}
}

func TestGetHttpResponseCappedFollowsRedirect(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("redirected content"))
	}))
	defer target.Close()

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusSeeOther)
	}))
	defer redirector.Close()

	content, _, truncated, err := getHttpResponseCapped(redirector.URL, 1024)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(content) != "redirected content" {
		t.Fatalf("unexpected content: %q", content)
	}
	if truncated {
		t.Fatalf("expected truncated=false")
	}
}
