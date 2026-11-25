package api

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/stxkxs/mkt/internal/observe"
)

// WithToken sets a bearer token required on every request. When set,
// clients must send it in the `Authorization: Bearer <token>` header.
// The token is not accepted as a query parameter, because query strings
// leak into proxy and access logs. Empty token (default) leaves
// the server unauthenticated, which the caller only permits for loopback
// binds (see checkListenSafety).
func (s *Server) WithToken(token string) *Server {
	s.token = token
	s.tokenHash = sha256.Sum256([]byte(token))
	return s
}

// auth wraps a handler with token authentication when a token has been
// configured. With no token configured it's a no-op.
func (s *Server) auth(h http.HandlerFunc) http.HandlerFunc {
	return s.authCounting(h, nil)
}

// authCounting is auth that also increments rejected on every 401, so a
// route whose refusals are a metric counts a bad token alongside its other
// rejections. A nil counter counts nothing.
func (s *Server) authCounting(h http.HandlerFunc, rejected *observe.Counter) http.HandlerFunc {
	if s.token == "" {
		return h
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.authorized(r) {
			if rejected != nil {
				rejected.Inc()
			}
			w.Header().Set("WWW-Authenticate", `Bearer realm="mkt"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		h(w, r)
	}
}

// authorized reports whether r presents the configured bearer token.
//
// The presented credential and the configured one are compared as SHA-256
// digests: subtle.ConstantTimeCompare is constant time only across equal
// lengths and returns early otherwise, so comparing the raw strings would
// let a caller recover the token's length from response timing. Hashing
// first makes both operands a fixed 32 bytes, so the comparison leaks
// neither length nor content.
//
// The `Bearer ` scheme is required (RFC 6750), matched case-insensitively
// per RFC 7235. Accepting a bare header value would also accept a Basic
// credential that happened to collide with the token.
func (s *Server) authorized(r *http.Request) bool {
	const scheme = "bearer "
	h := r.Header.Get("Authorization")
	if len(h) < len(scheme) || !strings.EqualFold(h[:len(scheme)], scheme) {
		return false
	}
	got := sha256.Sum256([]byte(strings.TrimSpace(h[len(scheme):])))
	return subtle.ConstantTimeCompare(got[:], s.tokenHash[:]) == 1
}
