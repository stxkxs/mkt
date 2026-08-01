package config

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// sampleLimit caps how many items a single removal line names before it
// summarizes the rest. Long enough to recognize what is going, short enough
// that a prompt stays readable when a 163-symbol watchlist is on the line.
const sampleLimit = 8

// removals describes, in the user's own vocabulary, everything that is in
// old (the config currently on disk) but would not be in next (the config we
// are about to write). An empty result means the write is purely additive
// and needs no confirmation.
//
// oldKeys / nextKeys are the top-level settings maps behind each config;
// they let the diff notice whole sections that would vanish even when the
// Config struct does not model them.
func removals(old, next *Config, oldKeys, nextKeys map[string]any) []string {
	var out []string
	out = append(out, portfolioRemovals(old, next)...)
	out = append(out, alertRemovals(old, next)...)
	out = append(out, watchlistRemovals(old, next)...)
	out = append(out, noteRemovals(old, next)...)
	out = append(out, listRemoval("edgar ticker", "edgar tickers", old.EDGARTickers, next.EDGARTickers)...)
	out = append(out, serveRemovals(old, next)...)
	out = append(out, feedRemovals(old, next)...)
	out = append(out, settingRemovals(old, next)...)
	out = append(out, sectionRemovals(oldKeys, nextKeys)...)
	return out
}

// portfolioRemovals reports whole portfolios that disappear, plus holdings
// and transactions dropped from portfolios that survive.
func portfolioRemovals(old, next *Config) []string {
	kept := make(map[string]*Portfolio, len(next.Portfolios))
	for i := range next.Portfolios {
		kept[next.Portfolios[i].Name] = &next.Portfolios[i]
	}

	var out []string
	for i := range old.Portfolios {
		p := &old.Portfolios[i]
		survivor, ok := kept[p.Name]
		if !ok {
			out = append(out, fmt.Sprintf("portfolio %q (%s)", p.Name, describeHoldings(p)))
			continue
		}
		have := make(map[string]bool, len(survivor.Holdings))
		for _, h := range survivor.Holdings {
			have[h.Symbol] = true
		}
		for _, h := range p.Holdings {
			if !have[h.Symbol] {
				out = append(out, fmt.Sprintf("holding %s from portfolio %q", describeHolding(h), p.Name))
			}
		}
		if dropped := len(p.Transactions) - len(survivor.Transactions); dropped > 0 {
			out = append(out, fmt.Sprintf("%d %s from portfolio %q", dropped, plural(dropped, "transaction"), p.Name))
		}
	}
	return out
}

// describeHoldings summarizes a portfolio's contents for a removal line:
// how many holdings, and a sample so the user recognizes which one it is.
func describeHoldings(p *Portfolio) string {
	if len(p.Holdings) == 0 {
		if len(p.Transactions) > 0 {
			return fmt.Sprintf("no holdings, %d %s", len(p.Transactions), plural(len(p.Transactions), "transaction"))
		}
		return "no holdings"
	}
	s := fmt.Sprintf("%d %s, %s", len(p.Holdings), plural(len(p.Holdings), "holding"), describeHolding(p.Holdings[0]))
	if n := len(p.Holdings) - 1; n > 0 {
		s += fmt.Sprintf(" + %d more", n)
	}
	return s
}

// describeHolding renders a position as the user would say it: "500 VTI".
func describeHolding(h Holding) string {
	return fmt.Sprintf("%s %s", num(h.Quantity), h.Symbol)
}

// alertRemovals reports alert rules that would be dropped. Rules are matched
// as a multiset — two identical rules are two rules, and losing one of them
// is still a loss.
func alertRemovals(old, next *Config) []string {
	remaining := make(map[string]int, len(next.Alerts))
	for _, r := range next.Alerts {
		remaining[ruleKey(r)]++
	}
	var out []string
	for _, r := range old.Alerts {
		k := ruleKey(r)
		if remaining[k] > 0 {
			remaining[k]--
			continue
		}
		out = append(out, "alert rule ("+describeRule(r)+")")
	}
	return out
}

// ruleKey is a rule's identity for diffing: everything that makes two rules
// behave differently, and nothing that doesn't.
func ruleKey(r AlertRule) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s|%s|%s|%d|%v", r.Symbol, r.Condition, num(r.Value), r.Period, r.Enabled)
	fmt.Fprintf(&b, "|%s", r.Match)
	for _, c := range r.Conditions {
		fmt.Fprintf(&b, "|%s:%s:%d", c.Condition, num(c.Value), c.Period)
	}
	for _, w := range r.Webhooks {
		fmt.Fprintf(&b, "|w:%s", w)
	}
	return b.String()
}

// describeRule renders a rule the way the alerts tab states it, so the user
// recognizes the rule they are about to lose.
func describeRule(r AlertRule) string {
	if len(r.Conditions) > 0 {
		match := r.Match
		if match == "" {
			match = "all"
		}
		return fmt.Sprintf("%s %s of %d %s", r.Symbol, match, len(r.Conditions), plural(len(r.Conditions), "condition"))
	}
	switch {
	case r.Value != 0:
		return fmt.Sprintf("%s %s %s", r.Symbol, r.Condition, num(r.Value))
	case r.Period != 0:
		return fmt.Sprintf("%s %s %d", r.Symbol, r.Condition, r.Period)
	default:
		return fmt.Sprintf("%s %s", r.Symbol, r.Condition)
	}
}

// watchlistRemovals reports symbols dropped from the flat watchlist, whole
// named groups that vanish, and symbols dropped from groups that survive.
func watchlistRemovals(old, next *Config) []string {
	out := listRemoval("watchlist symbol", "watchlist symbols", old.Watchlist, next.Watchlist)

	kept := make(map[string][]string, len(next.Watchlists))
	for _, w := range next.Watchlists {
		kept[w.Name] = w.Symbols
	}
	for _, w := range old.Watchlists {
		survivor, ok := kept[w.Name]
		if !ok {
			out = append(out, fmt.Sprintf("watchlist group %q (%d %s)", w.Name, len(w.Symbols), plural(len(w.Symbols), "symbol")))
			continue
		}
		out = append(out, listRemoval(
			fmt.Sprintf("symbol from watchlist group %q", w.Name),
			fmt.Sprintf("symbols from watchlist group %q", w.Name),
			w.Symbols, survivor)...)
	}
	return out
}

// noteRemovals reports per-symbol notes that would be lost. Notes are
// freeform prose the user typed; there is no getting them back.
func noteRemovals(old, next *Config) []string {
	if len(old.Notes) == 0 {
		return nil
	}
	symbols := make([]string, 0, len(old.Notes))
	for s := range old.Notes {
		symbols = append(symbols, s)
	}
	sort.Strings(symbols)

	var out []string
	for _, s := range symbols {
		if next.Notes[s] == old.Notes[s] {
			continue
		}
		if next.Notes[s] == "" {
			out = append(out, fmt.Sprintf("note for %s (%d %s)", s, len(old.Notes[s]), plural(len(old.Notes[s]), "char")))
			continue
		}
		out = append(out, fmt.Sprintf("the previous note for %s (%d %s, replaced)", s, len(old.Notes[s]), plural(len(old.Notes[s]), "char")))
	}
	return out
}

// serveRemovals reports SSH serve settings that would be cleared. Losing the
// authorized-key allowlist locks the operator out of their own dashboard.
func serveRemovals(old, next *Config) []string {
	var out []string
	if old.Serve.Addr != "" && next.Serve.Addr == "" {
		out = append(out, fmt.Sprintf("serve address %s", old.Serve.Addr))
	}
	if old.Serve.HostKey != "" && next.Serve.HostKey == "" {
		out = append(out, fmt.Sprintf("serve host key path %s", old.Serve.HostKey))
	}
	if old.Serve.AuthorizedKeysFile != "" && next.Serve.AuthorizedKeysFile == "" {
		out = append(out, fmt.Sprintf("serve authorized_keys file %s", old.Serve.AuthorizedKeysFile))
	}
	if dropped := len(old.Serve.AuthorizedKeys) - len(next.Serve.AuthorizedKeys); dropped > 0 {
		out = append(out, fmt.Sprintf("%d serve %s", dropped, plural(dropped, "authorized key")))
	}
	return out
}

// feedRemovals reports custom RSS sources that would be dropped. An empty
// news_feeds list means "use the built-ins", so losing a custom list quietly
// changes what the news tab shows.
func feedRemovals(old, next *Config) []string {
	kept := make(map[string]bool, len(next.NewsFeeds))
	for _, f := range next.NewsFeeds {
		kept[f.Name+"|"+f.URL] = true
	}
	var out []string
	for _, f := range old.NewsFeeds {
		if !kept[f.Name+"|"+f.URL] {
			out = append(out, fmt.Sprintf("news feed %q (%s)", f.Name, f.URL))
		}
	}
	return out
}

// settingRemovals reports secrets and explicit toggles that would be cleared
// back to their defaults.
func settingRemovals(old, next *Config) []string {
	var out []string
	for _, s := range []struct {
		key       string
		old, next string
	}{
		{"webhook_url", old.WebhookURL, next.WebhookURL},
		{"ntfy_topic", old.NtfyTopic, next.NtfyTopic},
		{"ntfy_server", old.NtfyServer, next.NtfyServer},
		{"pushover_user", old.PushoverUser, next.PushoverUser},
		{"pushover_token", old.PushoverToken, next.PushoverToken},
	} {
		if s.old != "" && s.next == "" {
			out = append(out, "cleared "+s.key)
		}
	}
	for _, t := range []struct {
		key       string
		old, next *bool
	}{
		{"desktop_notify", old.DesktopNotify, next.DesktopNotify},
		{"notifications", old.Notifications, next.Notifications},
		{"providers.binance", old.Providers.Binance, next.Providers.Binance},
		{"providers.defillama", old.Providers.DeFiLlama, next.Providers.DeFiLlama},
		{"providers.news", old.Providers.News, next.Providers.News},
		{"providers.macro", old.Providers.Macro, next.Providers.Macro},
	} {
		if t.old != nil && t.next == nil {
			out = append(out, fmt.Sprintf("cleared %s (was %v)", t.key, *t.old))
		}
	}
	return out
}

// sectionRemovals reports top-level YAML keys that mkt does not model at
// all. A hand-edited config can carry sections a future version reads, or a
// sibling tool wrote; rebuilding the file from the Config struct drops them,
// and the user deserves to hear about it before that happens.
func sectionRemovals(oldKeys, nextKeys map[string]any) []string {
	if len(oldKeys) == 0 {
		return nil
	}
	keys := make([]string, 0, len(oldKeys))
	for k := range oldKeys {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var out []string
	for _, k := range keys {
		if knownSections[k] {
			continue
		}
		if _, ok := nextKeys[k]; ok {
			continue
		}
		if isEmptyValue(oldKeys[k]) {
			continue
		}
		out = append(out, fmt.Sprintf("unrecognized section %q (mkt does not read it and cannot preserve it)", k))
	}
	return out
}

// knownSections is the set of top-level keys the Config struct models,
// derived from its yaml tags so it cannot drift as fields are added.
var knownSections = func() map[string]bool {
	t := reflect.TypeOf(Config{})
	m := make(map[string]bool, t.NumField())
	for i := range t.NumField() {
		tag, _, _ := strings.Cut(t.Field(i).Tag.Get("yaml"), ",")
		if tag != "" && tag != "-" {
			m[tag] = true
		}
	}
	return m
}()

// isEmptyValue reports whether a decoded YAML value carries nothing worth
// warning about — a null, an empty string, or an empty list/map.
func isEmptyValue(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.String, reflect.Slice, reflect.Map, reflect.Array:
		return rv.Len() == 0
	default:
		return false
	}
}

// listRemoval renders the set difference of two string lists as one line,
// naming up to sampleLimit of the missing entries. one / many are the label
// for the items in the singular and plural. Returns nil when nothing is lost.
func listRemoval(one, many string, old, next []string) []string {
	if len(old) == 0 {
		return nil
	}
	have := make(map[string]bool, len(next))
	for _, s := range next {
		have[s] = true
	}
	var gone []string
	for _, s := range old {
		if !have[s] {
			gone = append(gone, s)
		}
	}
	if len(gone) == 0 {
		return nil
	}
	sample := gone
	suffix := ""
	if len(sample) > sampleLimit {
		sample = sample[:sampleLimit]
		suffix = fmt.Sprintf(", +%d more", len(gone)-sampleLimit)
	}
	label := many
	if len(gone) == 1 {
		label = one
	}
	return []string{fmt.Sprintf("%d %s (%s%s)", len(gone), label, strings.Join(sample, ", "), suffix)}
}

// num formats a quantity or threshold the way the user wrote it: no trailing
// zeros, no scientific notation for the magnitudes money comes in.
func num(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// plural returns word, pluralized when n is not 1.
func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}
