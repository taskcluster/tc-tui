package taskcluster

import (
	"strings"
	"testing"
)

func TestHTTPStatusErrorMessageIncludesStatusAndDetail(t *testing.T) {
	err := &HTTPStatusError{StatusCode: 403, Status: "403 Forbidden", Detail: "InsufficientScopes: missing scope"}
	got := err.Error()
	if !strings.Contains(got, "403 Forbidden") || !strings.Contains(got, "InsufficientScopes: missing scope") {
		t.Fatalf("unexpected message: %q", got)
	}
}

func TestHTTPStatusErrorMessageWithoutDetailIsJustTheStatus(t *testing.T) {
	err := &HTTPStatusError{StatusCode: 404, Status: "404 Not Found"}
	if got := err.Error(); got != "404 Not Found" {
		t.Fatalf("unexpected message: %q", got)
	}
}

func TestSummarizeErrorBodyPrefersTaskclusterCodeAndMessage(t *testing.T) {
	body := []byte(`{"code":"InsufficientScopes","message":"You do not have sufficient scopes.","requestInfo":{"method":"getArtifact"}}`)
	got := summarizeErrorBody(body)
	if got != "InsufficientScopes: You do not have sufficient scopes." {
		t.Fatalf("unexpected summary: %q", got)
	}
}

func TestSummarizeErrorBodyDropsTaskclusterRequestContextTrailer(t *testing.T) {
	body := []byte(`{"code":"ResourceNotFound","message":"Artifact not found\n\n---\n\n* method:     getArtifact\n* errorCode:  ResourceNotFound\n* statusCode: 404\n* time:       2026-08-07T12:13:56.014Z"}`)
	got := summarizeErrorBody(body)
	if got != "ResourceNotFound: Artifact not found" {
		t.Fatalf("unexpected summary: %q", got)
	}
}

func TestSummarizeErrorBodyKeepsAMultilineMessageWithNoTrailer(t *testing.T) {
	body := []byte(`{"code":"InvalidRequestArguments","message":"Invalid URL patterns:\nURL parameter 'taskId' is malformed"}`)
	got := summarizeErrorBody(body)
	if got != "InvalidRequestArguments: Invalid URL patterns: URL parameter 'taskId' is malformed" {
		t.Fatalf("unexpected summary: %q", got)
	}
}

func TestSummarizeErrorBodyUsesMessageWhenThereIsNoCode(t *testing.T) {
	got := summarizeErrorBody([]byte(`{"message":"Artifact not found"}`))
	if got != "Artifact not found" {
		t.Fatalf("unexpected summary: %q", got)
	}
}

func TestSummarizeErrorBodyExtractsXMLErrorCodeAndMessage(t *testing.T) {
	body := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<Error><Code>AccessDenied</Code><Message>Access Denied</Message></Error>`)
	got := summarizeErrorBody(body)
	if got != "AccessDenied: Access Denied" {
		t.Fatalf("unexpected summary: %q", got)
	}
}

func TestSummarizeErrorBodyCollapsesPlainTextWhitespace(t *testing.T) {
	got := summarizeErrorBody([]byte("  something\n   went    wrong\n\n"))
	if got != "something went wrong" {
		t.Fatalf("unexpected summary: %q", got)
	}
}

func TestSummarizeErrorBodyTruncatesLongDetail(t *testing.T) {
	got := summarizeErrorBody([]byte(strings.Repeat("x", maxErrorDetailChars*2)))
	if len([]rune(got)) > maxErrorDetailChars+1 {
		t.Fatalf("expected detail capped near %d runes, got %d", maxErrorDetailChars, len([]rune(got)))
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("expected a truncation marker, got %q", got)
	}
}

func TestSummarizeErrorBodyIgnoresHTMLPage(t *testing.T) {
	got := summarizeErrorBody([]byte("<!DOCTYPE html>\n<html><body><h1>403 Forbidden</h1></body></html>"))
	if got != "" {
		t.Fatalf("expected no detail for an HTML page, got %q", got)
	}
}

func TestSummarizeErrorBodyOfEmptyBodyIsEmpty(t *testing.T) {
	if got := summarizeErrorBody(nil); got != "" {
		t.Fatalf("expected empty detail, got %q", got)
	}
}
