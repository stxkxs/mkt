package cmd

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"strings"

	"charm.land/ssh"

	"github.com/stxkxs/mkt/internal/config"
)

// authorizedKeyHandler is the SSH public-key callback for mkt serve: a key
// is admitted when it equals one on the allowlist, and refused otherwise.
// An empty allowlist refuses every key; runServe also refuses to start with
// one.
func authorizedKeyHandler(allow []ssh.PublicKey) func(ssh.Context, ssh.PublicKey) bool {
	return func(_ ssh.Context, key ssh.PublicKey) bool {
		for _, k := range allow {
			if ssh.KeysEqual(key, k) {
				return true
			}
		}
		return false
	}
}

// loadAuthorizedKeys builds the public-key allowlist from the inline
// serve.authorized_keys entries plus an optional authorized_keys file.
//
// mkt serve matches a key and nothing else: it enforces none of the
// OpenSSH key options (from=, command=, restrict, no-pty, expiry-time=, …).
// Admitting an optioned key would grant it more than OpenSSH does, so such
// a key is never admitted. An inline entry carrying options is a config
// error, since it was written for mkt. A line in a shared authorized_keys
// file is skipped with a warning, so pointing serve at ~/.ssh/authorized_keys
// admits its unrestricted keys and none of its restricted ones.
func loadAuthorizedKeys(sc config.ServeConfig) ([]ssh.PublicKey, error) {
	var keys []ssh.PublicKey
	for i, line := range sc.AuthorizedKeys {
		line = strings.TrimSpace(line)
		if line == "" || line[0] == '#' {
			continue
		}
		k, _, options, _, err := ssh.ParseAuthorizedKey([]byte(line))
		if err != nil {
			return nil, fmt.Errorf("authorized_keys[%d]: %w", i, err)
		}
		if len(options) > 0 {
			return nil, fmt.Errorf("authorized_keys[%d]: key options (%s) are not enforced by mkt serve; "+
				"remove them, or leave the key out of the allowlist", i, strings.Join(options, ","))
		}
		keys = append(keys, k)
	}
	if path := expandPath(sc.AuthorizedKeysFile); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("authorized_keys_file %s: %w", path, err)
		}
		fileKeys, skipped, err := parseAuthorizedKeysFile(data)
		if err != nil {
			return nil, fmt.Errorf("authorized_keys_file %s: %w", path, err)
		}
		if skipped > 0 {
			log.Printf("mkt serve: %d key(s) in %s carry OpenSSH options mkt does not enforce and are not admitted", skipped, path)
		}
		keys = append(keys, fileKeys...)
	}
	return keys, nil
}

// parseAuthorizedKeysFile parses every key line in an authorized_keys blob.
// It returns the keys that carry no options, and how many lines it left out
// because they did.
func parseAuthorizedKeysFile(data []byte) (keys []ssh.PublicKey, skipped int, err error) {
	for len(bytes.TrimSpace(data)) > 0 {
		k, _, options, rest, perr := ssh.ParseAuthorizedKey(data)
		if perr != nil {
			return nil, 0, perr
		}
		if len(options) > 0 {
			skipped++
		} else {
			keys = append(keys, k)
		}
		data = rest
	}
	return keys, skipped, nil
}
