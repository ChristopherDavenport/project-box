package git

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"syscall"
)

// HTTPError is a non-2xx answer from the GitHub REST API: the forge asked
// and GitHub answered, which is a fact about the forge's own request, not
// about the change being landed. Method and Endpoint identify the call;
// StatusCode, Status and Body carry what GitHub said, so a caller deciding
// whether to retry never has to re-derive them from a formatted string.
type HTTPError struct {
	Method     string
	Endpoint   string
	StatusCode int
	Status     string
	Body       string
}

// Error preserves the message shape callers and tests already depend on:
// "github: METHOD endpoint: status: body".
func (e *HTTPError) Error() string {
	return fmt.Sprintf("github: %s %s: %s: %s", e.Method, e.Endpoint, e.Status, e.Body)
}

// Transient reports whether err is positively known to be the forge's own
// trouble reaching GitHub — a GitHub-side 5xx, or a transport failure from
// the underlying round trip — rather than a fact about the change or the
// request itself. Everything else, including every 4xx and every error this
// function cannot place, answers false: this repository's rule is that
// unknown never reads as zero, so an error nobody can classify parks as
// today rather than being retried in place.
func Transient(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		return httpErr.StatusCode >= 500
	}
	// context.Canceled is excluded above regardless of how deep it is
	// wrapped — *url.Error's Unwrap surfaces it through the same
	// errors.Is(err, context.Canceled) check, so nothing below re-tests it.
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		if urlErr.Timeout() {
			return true
		}
		if errors.Is(urlErr.Err, syscall.ECONNRESET) {
			return true
		}
		if errors.Is(urlErr.Err, io.ErrUnexpectedEOF) {
			return true
		}
		var netErr net.Error
		if errors.As(urlErr.Err, &netErr) && netErr.Timeout() {
			return true
		}
	}
	return false
}
