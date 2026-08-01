package alert

import (
	"errors"
	"fmt"
	"net/url"
)

// redactedPlaceholder stands in for a destination that cannot be parsed
// far enough to name safely.
const redactedPlaceholder = "<redacted url>"

// redactURL reduces a notification destination to scheme://host/… so it
// can appear in a log line or an error string without leaking the
// credential it carries.
//
// For every destination mkt supports the secret lives in the URL itself:
// a Slack or Discord webhook path is the credential, an ntfy topic is the
// credential, and a userinfo section is a password in plain sight. Only
// the scheme and host survive — enough to tell two destinations apart
// while debugging, not enough to post as the user.
func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return redactedPlaceholder
	}
	out := u.Host
	if u.Scheme != "" {
		out = u.Scheme + "://" + out
	}
	if (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		out += "/…"
	}
	return out
}

// redactErr rewrites the *url.Error the net/http client returns, which
// embeds the request URL verbatim — secret path and all — in its Error
// string. The cause is preserved with %w so errors.Is still works on
// context deadlines and transport errors.
func redactErr(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return fmt.Errorf("%s %s: %w", ue.Op, redactURL(ue.URL), ue.Err)
	}
	return err
}
