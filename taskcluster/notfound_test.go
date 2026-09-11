package taskcluster

import (
	"errors"
	"net/http"
	"testing"

	tcclient "github.com/taskcluster/taskcluster/v109/clients/client-go"
)

func TestIsNotFoundTrueFor404(t *testing.T) {
	err := &tcclient.APICallException{
		CallSummary: &tcclient.CallSummary{
			HTTPResponse: &http.Response{StatusCode: 404},
		},
	}
	if !isNotFound(err) {
		t.Fatalf("expected isNotFound(404) to be true")
	}
}

func TestIsNotFoundFalseForOtherStatusCode(t *testing.T) {
	err := &tcclient.APICallException{
		CallSummary: &tcclient.CallSummary{
			HTTPResponse: &http.Response{StatusCode: 500},
		},
	}
	if isNotFound(err) {
		t.Fatalf("expected isNotFound(500) to be false")
	}
}

func TestIsNotFoundFalseForNonAPICallException(t *testing.T) {
	if isNotFound(errors.New("boom")) {
		t.Fatalf("expected isNotFound to be false for a plain error")
	}
}
