package usecase

import (
	"net/http"
	"net/http/httptest"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func newHandlerTransport(handler http.Handler) http.RoundTripper {
	return roundTripFunc(func(r *http.Request) (*http.Response, error) {
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, r)
		return rr.Result(), nil
	})
}
