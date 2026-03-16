package heatmap

// Sector defines a sector with name and constituent symbols.
type Sector struct {
	Name    string
	Symbols []string
}

// DefaultSectors returns sector definitions derived from the default watchlist groupings.
var DefaultSectors = []Sector{
	{Name: "Tech", Symbols: []string{"AAPL", "MSFT", "GOOGL", "AMZN", "NVDA", "TSLA", "META", "AMD", "NFLX", "COIN"}},
	{Name: "Defense", Symbols: []string{"LMT", "RTX", "NOC", "GD", "LHX", "BA", "HII", "KTOS", "LDOS"}},
	{Name: "Energy", Symbols: []string{"XOM", "CVX", "OXY", "HAL", "DVN", "COP", "SLB", "EOG", "FANG", "PSX", "MPC", "VLO"}},
	{Name: "Shipping", Symbols: []string{"FRO", "STNG", "INSW", "ZIM"}},
	{Name: "Nuclear", Symbols: []string{"CCJ", "UEC", "DNN", "LEU", "NNE", "SMR"}},
	{Name: "Cyber", Symbols: []string{"PANW", "CRWD", "FTNT", "ZS", "NET", "S"}},
	{Name: "Gold", Symbols: []string{"GLD", "NEM", "GOLD", "AEM", "WPM", "RGLD"}},
	{Name: "AI/Compute", Symbols: []string{"AVGO", "ARM", "TSM", "MRVL", "SMCI", "VRT", "ANET", "DELL", "CRM", "PLTR", "SNOW"}},
	{Name: "Rates", Symbols: []string{"O", "AMT", "DHI", "LEN", "SCHD", "SQ", "SHOP", "SOFI", "ABBV", "KRE"}},
	{Name: "Infra", Symbols: []string{"CAT", "DE", "URI", "VMC", "MLM", "NUE", "STLD", "PWR", "EATON", "FAST", "XYL", "UNP", "CSX", "AAON"}},
	{Name: "Clean", Symbols: []string{"ENPH", "FSLR", "CEG", "VST", "NEE", "ALB", "RIVN"}},
	{Name: "Health", Symbols: []string{"LLY", "JNJ", "MRK", "PFE", "NVO", "AMGN", "ISRG", "MDT", "ABT", "SYK", "UNH", "HUM", "WELL", "VRTX", "REGN"}},
	{Name: "GLP-1", Symbols: []string{"HIMS", "TDOC", "DXCM", "PODD", "CRL", "WST", "PTON"}},
	{Name: "Robotics", Symbols: []string{"ROK", "TER", "CGNX", "GXO", "UBER", "ADI", "ON", "AVAV"}},
	{Name: "Commods", Symbols: []string{"FCX", "SCCO", "TECK", "SQM", "BHP", "RIO", "MOS", "NTR", "ADM"}},
	{Name: "CryptoEq", Symbols: []string{"MARA", "CLSK", "RIOT", "WULF", "MSTR", "HOOD"}},
	{Name: "Space", Symbols: []string{"RKLB", "ASTS", "GSAT", "IRDM"}},
	{Name: "Crypto", Symbols: []string{"BTC-USD", "ETH-USD", "SOL-USD", "XRP-USD", "ADA-USD", "DOGE-USD", "AVAX-USD", "LINK-USD", "DOT-USD"}},
}
