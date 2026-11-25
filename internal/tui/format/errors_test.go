package format

import (
	"errors"
	"fmt"
	"net/url"
	"testing"
)

func TestErrorReasonDropsTheURL(t *testing.T) {
	err := fmt.Errorf("fetch: %w", &url.Error{Op: "Get", URL: "https://query1.finance.yahoo.com/v7/finance/quote?symbols=AAPL", Err: errors.New("context deadline exceeded")})
	if got := ErrorReason(err); got != "context deadline exceeded" {
		t.Errorf("ErrorReason = %q", got)
	}
}

func TestErrorReasonKeepsOneLine(t *testing.T) {
	if got := ErrorReason(errors.New("first\nsecond")); got != "first" {
		t.Errorf("ErrorReason = %q", got)
	}
}
