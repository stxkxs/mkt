package format

import (
	"errors"
	"net/url"
	"strings"
)

// ErrorReason shortens an upstream error to the part a reader can act on,
// on one line: a *url.Error carries the method and full URL, which are
// noise beside the cause and push the message past the frame.
func ErrorReason(err error) string {
	var ue *url.Error
	if errors.As(err, &ue) && ue.Err != nil {
		err = ue.Err
	}
	msg, _, _ := strings.Cut(err.Error(), "\n")
	return msg
}
