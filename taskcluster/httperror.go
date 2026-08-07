package taskcluster

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// maxErrorDetailChars bounds how much of a failed response's body is quoted
// back in an error message — enough for a Taskcluster error's own message,
// short enough to fit a footer warning line.
const maxErrorDetailChars = 200

// maxErrorBodyBytes bounds how much of a failed response's body is read
// before summarizing it. An error document is small; anything larger is a
// server misbehaving, and reading it in full would defeat the point of
// failing fast.
const maxErrorBodyBytes = 8 * 1024

// HTTPStatusError reports a non-2xx HTTP response. Artifact fetches go
// straight to storage rather than through the generated Taskcluster client,
// so nothing else turns their failures into errors: without this, a 403's
// "InsufficientScopes" JSON blob or an S3 "AccessDenied" XML document is
// indistinguishable from the artifact's own content, and gets rendered as a
// log or written to disk under the artifact's name.
type HTTPStatusError struct {
	StatusCode int
	// Status is the response's own status line (e.g. "403 Forbidden").
	Status string
	// Detail is a short one-line summary of the response body, or "" when
	// the body says nothing useful.
	Detail string
}

func (e *HTTPStatusError) Error() string {
	if e.Detail == "" {
		return e.Status
	}
	return fmt.Sprintf("%s: %s", e.Status, e.Detail)
}

// isSuccessStatus reports whether a response carries content rather than an
// error document — only the status decides.
func isSuccessStatus(statusCode int) bool {
	return statusCode >= 200 && statusCode < 300
}

// statusError builds the error for a response isSuccessStatus rejected,
// consuming a bounded prefix of its body for the detail.
func statusError(response *http.Response) *HTTPStatusError {
	body, _ := io.ReadAll(io.LimitReader(response.Body, maxErrorBodyBytes))

	return &HTTPStatusError{
		StatusCode: response.StatusCode,
		Status:     strings.TrimSpace(response.Status),
		Detail:     summarizeErrorBody(body),
	}
}

// summarizeErrorBody renders a failed response's body as a short one-line
// detail, understanding the two shapes a Taskcluster artifact fetch can fail
// with — the services' own JSON errors and the cloud storage XML errors a
// redirected fetch lands on — and falling back to the body's plain text.
func summarizeErrorBody(body []byte) string {
	text := strings.TrimSpace(string(body))
	if text == "" {
		return ""
	}

	if detail := jsonErrorDetail(text); detail != "" {
		return truncateDetail(detail)
	}
	if detail := xmlErrorDetail(text); detail != "" {
		return truncateDetail(detail)
	}
	if isHTMLPage(text) {
		// Markup, not a message — quoting it back is noise.
		return ""
	}

	return truncateDetail(strings.Join(strings.Fields(text), " "))
}

// jsonErrorDetail reads a Taskcluster service error — {"code":…,"message":…}.
func jsonErrorDetail(text string) string {
	if !strings.HasPrefix(text, "{") {
		return ""
	}

	var parsed struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		return ""
	}

	return joinCodeAndMessage(parsed.Code, parsed.Message)
}

// xmlErrorDetail reads an S3/GCS storage error — <Error><Code>…</Code>…
func xmlErrorDetail(text string) string {
	if !strings.HasPrefix(text, "<") {
		return ""
	}

	var parsed struct {
		Code    string `xml:"Code"`
		Message string `xml:"Message"`
	}
	if err := xml.Unmarshal([]byte(text), &parsed); err != nil {
		return ""
	}

	return joinCodeAndMessage(parsed.Code, parsed.Message)
}

func joinCodeAndMessage(code, message string) string {
	code = strings.Join(strings.Fields(code), " ")
	message = strings.Join(strings.Fields(dropRequestContext(message)), " ")

	switch {
	case code != "" && message != "":
		return code + ": " + message
	case code != "":
		return code
	default:
		return message
	}
}

// dropRequestContext cuts a Taskcluster error message at the "---" line the
// services append their request context after (method, errorCode, time) —
// several lines of detail that dwarf the message itself and duplicate what
// the status already says.
func dropRequestContext(message string) string {
	lines := strings.Split(message, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) == "---" {
			return strings.Join(lines[:i], "\n")
		}
	}
	return message
}

func isHTMLPage(text string) bool {
	lower := strings.ToLower(text)
	return strings.HasPrefix(lower, "<!doctype html") || strings.HasPrefix(lower, "<html")
}

func truncateDetail(detail string) string {
	runes := []rune(detail)
	if len(runes) <= maxErrorDetailChars {
		return detail
	}
	return strings.TrimSpace(string(runes[:maxErrorDetailChars])) + "…"
}
