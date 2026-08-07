package taskcluster

import (
	"io"
	"net/http"
	"time"

	"github.com/taskcluster/tc-tui/crash"
)

// streamReadBufferBytes sizes streamHttpResponse's per-read buffer — small
// enough that a trickling live log surfaces lines promptly, large enough
// that a fast-scrolling one doesn't burn a call per few bytes.
const streamReadBufferBytes = 32 * 1024

// streamHttpResponse fetches url and delivers its body incrementally,
// calling onChunk as each read completes — unlike getHttpResponseCapped,
// which blocks until the whole body has arrived, this works on endpoints
// that hold the response open and keep writing (a live log). The chunk
// slice passed to onChunk is reused between calls, so onChunk must consume
// or copy it before returning.
//
// It returns when the server closes the stream (EOF), after delivering
// maxBytes in total (reported via truncated), or once stop is closed — a
// watcher goroutine then closes the response body, which unblocks the
// pending Read; that path returns whatever was delivered so far with a nil
// error, since an interrupted stream is an expected outcome (the caller
// navigated away), not a failure.
func streamHttpResponse(url string, maxBytes int64, stop <-chan struct{}, onChunk func(chunk []byte)) (contentType string, truncated bool, err error) {
	response, err := http.Get(url)
	if err != nil {
		return "", false, err
	}
	defer response.Body.Close()

	// Started before anything reads the body, including the error-document
	// read below: a server can send an error status and then stall, so that
	// read has to be interruptible too.
	watcherDone := make(chan struct{})
	defer close(watcherDone)
	crash.Go(func() {
		select {
		case <-stop:
			response.Body.Close()
		case <-watcherDone:
		}
	})

	// A non-2xx body is an error document, not log output — see HTTPStatusError.
	if !isSuccessStatus(response.StatusCode) {
		return "", false, statusError(response)
	}

	contentType = response.Header.Get("Content-Type")

	var total int64
	buf := make([]byte, streamReadBufferBytes)
	for {
		n, readErr := response.Body.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			if total+int64(n) > maxBytes {
				onChunk(chunk[:maxBytes-total])
				return contentType, true, nil
			}
			total += int64(n)
			onChunk(chunk)
		}
		if readErr != nil {
			if readErr == io.EOF {
				return contentType, false, nil
			}
			select {
			case <-stop:
				// The read failed because the watcher closed the body —
				// a clean interruption, not a fetch failure.
				return contentType, false, nil
			default:
			}
			return contentType, false, readErr
		}
	}
}

// StreamArtifactContent fetches one artifact's content as a live stream —
// see streamHttpResponse for the delivery/termination contract, and
// GetArtifactContent for the one-shot variant this exists alongside: a
// still-running task's live log holds its response open until the task
// finishes, which would block the one-shot fetch indefinitely.
func (tc *TC) StreamArtifactContent(taskID string, runID int64, name string, stop <-chan struct{}, onChunk func(chunk []byte)) (contentType string, truncated bool, err error) {
	fetchURL, err := tc.artifactURL(taskID, runID, name, 60*time.Second)
	if err != nil {
		return "", false, err
	}

	return streamHttpResponse(fetchURL, MaxArtifactContentBytes, stop, onChunk)
}
