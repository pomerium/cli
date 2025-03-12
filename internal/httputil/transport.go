package httputil

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/rs/zerolog"
)

type loggingRoundTripper struct {
	base      http.RoundTripper
	logger    zerolog.Logger
	customize []func(event *zerolog.Event) *zerolog.Event
}

func (l loggingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	start := time.Now()
	res, err := l.base.RoundTrip(req)
	statusCode := http.StatusInternalServerError
	if res != nil {
		statusCode = res.StatusCode
	}

	evt := l.logger.Debug().
		Str("method", req.Method).
		Str("authority", req.URL.Host).
		Str("path", req.URL.Path).
		Dur("duration", time.Since(start)).
		Int("response-code", statusCode)
	for _, f := range l.customize {
		f(evt)
	}

	// if status code is not 200, log the body
	if res != nil && statusCode/100 != 2 {
		responseBody := make([]byte, 16*1024)
		if n, r, e := peek(res.Body, responseBody); e == nil {
			// replace the body so that the peek'd bytes can be re-read
			res.Body = struct {
				io.Reader
				io.Closer
			}{r, res.Body}
			evt = evt.Str("response-body", string(responseBody[:n]))
		} else {
			panic(e)
		}
	}

	evt.Msg("http-request")
	return res, err
}

// NewLoggingRoundTripper creates a http.RoundTripper that will log requests.
func NewLoggingRoundTripper(logger zerolog.Logger, base http.RoundTripper, customize ...func(event *zerolog.Event) *zerolog.Event) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return loggingRoundTripper{base: base, logger: logger, customize: customize}
}

func peek(r io.Reader, dst []byte) (n int, newReader io.Reader, err error) {
	var tmp bytes.Buffer
	n, err = io.TeeReader(r, &tmp).Read(dst)
	if errors.Is(err, io.EOF) {
		err = nil
	}
	return n, io.MultiReader(&tmp, r), err
}
