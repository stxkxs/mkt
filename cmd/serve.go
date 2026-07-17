package cmd

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/wish/v2"
	"charm.land/wish/v2/activeterm"
	"charm.land/wish/v2/bubbletea"
	"charm.land/wish/v2/logging"
	"github.com/charmbracelet/ssh"
	"github.com/spf13/cobra"
	"github.com/stxkxs/mkt/internal/config"
)

// defaultServeAddr binds loopback by default: even though every session is
// gated by the public-key allowlist, a safe default means an unqualified
// `mkt serve` is never reachable off-host until the operator opts in with
// serve.addr: 0.0.0.0:2222 (or --addr).
const defaultServeAddr = "127.0.0.1:2222"

// serveIdleTimeout disconnects sessions idle this long so abandoned
// dashboards don't hold a program + its broadcaster slot forever.
const serveIdleTimeout = 30 * time.Minute

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Serve the dashboard over SSH to authorized keys (Wish)",
	Long: `serve runs mkt as an SSH app (Charm Wish): each connection gets its own
live, per-session dashboard driven by one shared data plane.

	ssh -p 2222 your.host

Access is gated by a public-key allowlist — list keys under serve.authorized_keys
(or point serve.authorized_keys_file at an authorized_keys file). serve refuses to
start with an empty allowlist rather than expose your holdings openly. The SSH host
key is generated on first run if it doesn't exist.

Live order-book streaming (crypto detail view) falls back to REST snapshots in
serve mode; all other tabs stream live.`,
	RunE: runServe,
}

func init() {
	rootCmd.AddCommand(serveCmd)
	serveCmd.Flags().String("addr", "", "SSH listen address (overrides serve.addr; default "+defaultServeAddr+")")
	serveCmd.Flags().String("host-key", "", "path to the SSH host key, auto-generated if missing (overrides serve.host_key)")
}

func runServe(cmd *cobra.Command, args []string) error {
	b, cleanup, err := setupBackend()
	if err != nil {
		return err
	}
	defer cleanup()

	sc := b.cfg.Serve
	addrFlag, _ := cmd.Flags().GetString("addr")
	hostKeyFlag, _ := cmd.Flags().GetString("host-key")

	addr := firstNonEmpty(addrFlag, sc.Addr, defaultServeAddr)
	hostKey := firstNonEmpty(hostKeyFlag, expandPath(sc.HostKey), filepath.Join(config.ConfigDir(), "ssh_host_key"))

	allow, err := loadAuthorizedKeys(sc)
	if err != nil {
		return fmt.Errorf("mkt serve: %w", err)
	}
	if len(allow) == 0 {
		return fmt.Errorf("mkt serve: no authorized keys configured — add a serve.authorized_keys list " +
			"(or serve.authorized_keys_file) to your config; refusing to serve an open SSH dashboard of your holdings")
	}

	// The read-only HTTP surface can run alongside SSH when --listen is set.
	apiShutdown, err := b.startAPIIfRequested(cmd)
	if err != nil {
		return err
	}
	defer apiShutdown()

	// One shared data plane feeds every session's program via the broadcaster.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b.startDataPlane(ctx)

	// Per-session: fresh model, its own program wired to the SSH PTY, and a
	// broadcaster registration torn down when the session's context ends.
	handler := func(sess ssh.Session) *tea.Program {
		app := b.buildApp()
		p := tea.NewProgram(app, bubbletea.MakeOptions(sess)...)
		b.bc.Add(p)
		go func() {
			<-sess.Context().Done()
			b.bc.Remove(p)
		}()
		return p
	}

	srv, err := wish.NewServer(
		wish.WithAddress(addr),
		wish.WithHostKeyPath(hostKey),
		wish.WithIdleTimeout(serveIdleTimeout),
		wish.WithPublicKeyAuth(func(_ ssh.Context, key ssh.PublicKey) bool {
			for _, k := range allow {
				if ssh.KeysEqual(key, k) {
					return true
				}
			}
			return false
		}),
		wish.WithMiddleware(
			bubbletea.MiddlewareWithProgramHandler(handler),
			activeterm.Middleware(), // reject sessions without a PTY
			logging.Middleware(),
		),
	)
	if err != nil {
		return fmt.Errorf("mkt serve: %w", err)
	}

	fmt.Fprintf(os.Stderr, "mkt serve: SSH dashboard on %s — %d authorized key(s), host key %s\n", addr, len(allow), hostKey)

	// Graceful shutdown on SIGINT/SIGTERM.
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
		<-sig
		shutCtx, shutCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutCancel()
		_ = srv.Shutdown(shutCtx)
	}()

	if err := srv.ListenAndServe(); err != nil && err != ssh.ErrServerClosed {
		return fmt.Errorf("mkt serve: %w", err)
	}
	return nil
}

// loadAuthorizedKeys builds the public-key allowlist from the inline
// serve.authorized_keys entries plus an optional authorized_keys file.
func loadAuthorizedKeys(sc config.ServeConfig) ([]ssh.PublicKey, error) {
	var keys []ssh.PublicKey
	for i, line := range sc.AuthorizedKeys {
		line = strings.TrimSpace(line)
		if line == "" || line[0] == '#' {
			continue
		}
		k, _, _, _, err := ssh.ParseAuthorizedKey([]byte(line))
		if err != nil {
			return nil, fmt.Errorf("authorized_keys[%d]: %w", i, err)
		}
		keys = append(keys, k)
	}
	if path := expandPath(sc.AuthorizedKeysFile); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("authorized_keys_file %s: %w", path, err)
		}
		fileKeys, err := parseAuthorizedKeysFile(data)
		if err != nil {
			return nil, fmt.Errorf("authorized_keys_file %s: %w", path, err)
		}
		keys = append(keys, fileKeys...)
	}
	return keys, nil
}

// parseAuthorizedKeysFile parses every key line in an authorized_keys blob.
func parseAuthorizedKeysFile(data []byte) ([]ssh.PublicKey, error) {
	var keys []ssh.PublicKey
	for len(bytes.TrimSpace(data)) > 0 {
		k, _, _, rest, err := ssh.ParseAuthorizedKey(data)
		if err != nil {
			return nil, err
		}
		keys = append(keys, k)
		data = rest
	}
	return keys, nil
}

// firstNonEmpty returns the first non-empty string, or "".
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// expandPath expands a leading ~ to the user's home directory. Empty in,
// empty out.
func expandPath(p string) string {
	if p == "" {
		return ""
	}
	if p == "~" || (len(p) >= 2 && p[:2] == "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			if p == "~" {
				return home
			}
			return filepath.Join(home, p[2:])
		}
	}
	return p
}
