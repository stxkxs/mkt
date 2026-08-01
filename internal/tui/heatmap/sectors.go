package heatmap

// Sector is one named tile group on the heatmap: a display name and the
// symbols whose quotes are averaged into that tile's change%.
type Sector struct {
	Name    string
	Symbols []string
}

// DefaultSectors is the fallback tile layout used when no sectors have
// been supplied. It exists only so a fresh install has something to
// draw: the heatmap can only colour symbols the hub is subscribed to, so
// the real source of sectors is the user's own watchlist groups (see
// SetSectors). Keep it aligned with config.DefaultWatchlists.
var DefaultSectors = []Sector{
	{Name: "Crypto Majors", Symbols: []string{"BTC-USD", "ETH-USD", "SOL-USD", "XRP-USD", "ADA-USD", "DOGE-USD", "AVAX-USD", "LINK-USD", "DOT-USD", "NEAR-USD", "SUI-USD", "ARB-USD", "OP-USD", "PEPE-USD"}},
	{Name: "Megacap Tech", Symbols: []string{"AAPL", "MSFT", "GOOGL", "AMZN", "META", "NVDA", "TSLA", "AMD", "NFLX", "AVGO"}},
	{Name: "AI & Data Center", Symbols: []string{"NVDA", "AMD", "AVGO", "ARM", "TSM", "MRVL", "SMCI", "VRT", "ANET", "DELL", "PLTR", "SNOW", "CRM", "CEG", "VST"}},
	{Name: "Semis & Reshoring", Symbols: []string{"INTC", "AMAT", "LRCX", "ADI", "ON", "TER", "CGNX", "ROK", "MP", "NUE", "STLD", "CAT", "DE", "URI"}},
	{Name: "Defense & Space", Symbols: []string{"LMT", "RTX", "NOC", "GD", "LHX", "BA", "HII", "KTOS", "LDOS", "AVAV", "RKLB", "ASTS", "GSAT", "IRDM"}},
	{Name: "Energy & Nuclear", Symbols: []string{"XOM", "CVX", "OXY", "HAL", "DVN", "COP", "SLB", "EOG", "FANG", "PSX", "MPC", "VLO", "CCJ", "UEC", "DNN", "LEU", "NNE", "SMR", "ENPH", "FSLR", "NEE"}},
	{Name: "Healthcare & GLP-1", Symbols: []string{"LLY", "NVO", "JNJ", "MRK", "PFE", "AMGN", "HIMS", "TDOC", "ISRG", "MDT", "ABT", "SYK", "UNH", "HUM", "WELL", "VRTX", "REGN", "DXCM", "PODD"}},
	{Name: "Commodities & Miners", Symbols: []string{"GLD", "NEM", "GOLD", "AEM", "WPM", "RGLD", "FCX", "SCCO", "TECK", "SQM", "ALB", "BHP", "RIO", "VALE", "MOS", "NTR", "ADM", "FRO", "STNG", "INSW", "ZIM"}},
	{Name: "Crypto Equities & Fintech", Symbols: []string{"COIN", "MSTR", "MARA", "CLSK", "RIOT", "WULF", "HOOD", "SQ", "SHOP", "SOFI", "KRE"}},
	{Name: "Cybersecurity", Symbols: []string{"PANW", "CRWD", "FTNT", "ZS", "NET", "S"}},
	{Name: "Rate-Sensitive & Income", Symbols: []string{"O", "AMT", "WELL", "DHI", "LEN", "SCHD", "ABBV", "KRE"}},
}

// NormalizeSectors drops sectors that carry no symbols and returns
// DefaultSectors when nothing usable is left, so the heatmap always has
// something to draw. Symbol order within a sector is preserved.
func NormalizeSectors(sectors []Sector) []Sector {
	out := make([]Sector, 0, len(sectors))
	for _, s := range sectors {
		if len(s.Symbols) == 0 {
			continue
		}
		name := s.Name
		if name == "" {
			name = "Unnamed"
		}
		out = append(out, Sector{Name: name, Symbols: s.Symbols})
	}
	if len(out) == 0 {
		return DefaultSectors
	}
	return out
}
