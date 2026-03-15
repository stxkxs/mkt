package config

// Default values for the configuration.
var (
	DefaultWatchlist = []string{
		// Crypto
		"BTC-USD",
		"ETH-USD",
		"SOL-USD",
		"XRP-USD",
		"ADA-USD",
		"DOGE-USD",
		"AVAX-USD",
		"LINK-USD",
		"DOT-USD",
		"NEAR-USD",
		"SUI-USD",
		"ARB-USD",
		"OP-USD",
		"PEPE-USD",
		// Stocks — tech
		"AAPL",
		"MSFT",
		"GOOGL",
		"AMZN",
		"NVDA",
		"TSLA",
		"META",
		"AMD",
		"NFLX",
		"COIN",
		// Defense / aerospace
		"LMT",
		"RTX",
		"NOC",
		"GD",
		"LHX",
		"BA",
		"HII",
		"KTOS",
		"LDOS",
		// Energy / oil & gas
		"XOM",
		"CVX",
		"OXY",
		"HAL",
		"DVN",
		"COP",
		"SLB",
		"EOG",
		"FANG",
		"PSX",
		"MPC",
		"VLO",
		// Shipping / tankers
		"FRO",
		"STNG",
		"INSW",
		// Uranium / nuclear
		"CCJ",
		"UEC",
		"DNN",
		"LEU",
		"NNE",
		"SMR",
		// Cybersecurity
		"PANW",
		"CRWD",
		"FTNT",
		"ZS",
		"NET",
		"S",
		// Gold / safe haven
		"GLD",
		"NEM",
		"GOLD",
		"AEM",
		"WPM",
		"RGLD",
		// Rare earth / critical minerals
		"MP",
		"VALE",
		// Shipping & logistics
		"ZIM",
		// AI / compute
		"AVGO",
		"ARM",
		"TSM",
		"MRVL",
		"SMCI",
		"VRT",
		"ANET",
		"DELL",
		"CRM",
		"PLTR",
		"SNOW",
		// Rate-sensitive
		"O",
		"AMT",
		"DHI",
		"LEN",
		"SCHD",
		"SQ",
		"SHOP",
		"SOFI",
		"ABBV",
		"KRE",
		// Infrastructure / reshoring
		"CAT",
		"DE",
		"URI",
		"VMC",
		"MLM",
		"NUE",
		"STLD",
		"PWR",
		"INTC",
		"AMAT",
		"LRCX",
		"EATON",
		"FAST",
		"XYL",
		"UNP",
		"CSX",
		"AAON",
		// Energy transition
		"ENPH",
		"FSLR",
		"CEG",
		"VST",
		"NEE",
		"ALB",
		"RIVN",
		// Healthcare / aging / GLP-1
		"LLY",
		"JNJ",
		"MRK",
		"PFE",
		"NVO",
		"AMGN",
		"HIMS",
		"TDOC",
		"ISRG",
		"MDT",
		"ABT",
		"SYK",
		"UNH",
		"HUM",
		"WELL",
		"VRTX",
		"REGN",
		"DXCM",
		"PODD",
		"CRL",
		"WST",
		"PTON",
		// Robotics & automation
		"ROK",
		"TER",
		"CGNX",
		"GXO",
		"UBER",
		"ADI",
		"ON",
		"AVAV",
		// Commodities
		"FCX",
		"SCCO",
		"TECK",
		"SQM",
		"BHP",
		"RIO",
		"MOS",
		"NTR",
		"ADM",
		// Crypto equities
		"MARA",
		"CLSK",
		"RIOT",
		"WULF",
		"MSTR",
		"HOOD",
		// Space
		"RKLB",
		"ASTS",
		"GSAT",
		"IRDM",
	}

	DefaultPortfolios = []Portfolio{
		{
			Name: "Geopolitical Tension",
			Holdings: []Holding{
				// Defense & aerospace — elevated global spending, force modernization
				{Symbol: "LMT", Name: "Lockheed Martin", Quantity: 15, CostBasis: 450.00},
				{Symbol: "RTX", Name: "RTX (Raytheon)", Quantity: 25, CostBasis: 95.00},
				{Symbol: "NOC", Name: "Northrop Grumman", Quantity: 10, CostBasis: 470.00},
				{Symbol: "GD", Name: "General Dynamics", Quantity: 12, CostBasis: 275.00},
				{Symbol: "LHX", Name: "L3Harris Technologies", Quantity: 15, CostBasis: 210.00},
				{Symbol: "BA", Name: "Boeing", Quantity: 10, CostBasis: 195.00},
				{Symbol: "HII", Name: "Huntington Ingalls", Quantity: 8, CostBasis: 260.00},
				{Symbol: "KTOS", Name: "Kratos Defense", Quantity: 50, CostBasis: 22.00},
				{Symbol: "LDOS", Name: "Leidos Holdings", Quantity: 12, CostBasis: 115.00},

				// Energy — supply disruption risk, chokepoint exposure
				{Symbol: "XOM", Name: "Exxon Mobil", Quantity: 30, CostBasis: 105.00},
				{Symbol: "CVX", Name: "Chevron", Quantity: 20, CostBasis: 155.00},
				{Symbol: "COP", Name: "ConocoPhillips", Quantity: 18, CostBasis: 115.00},
				{Symbol: "OXY", Name: "Occidental Petroleum", Quantity: 40, CostBasis: 58.00},
				{Symbol: "DVN", Name: "Devon Energy", Quantity: 35, CostBasis: 45.00},
				{Symbol: "HAL", Name: "Halliburton", Quantity: 50, CostBasis: 35.00},
				{Symbol: "SLB", Name: "Schlumberger", Quantity: 40, CostBasis: 48.00},
				// Refiners — margin expansion when crude is volatile
				{Symbol: "PSX", Name: "Phillips 66", Quantity: 12, CostBasis: 135.00},
				{Symbol: "MPC", Name: "Marathon Petroleum", Quantity: 10, CostBasis: 155.00},
				{Symbol: "VLO", Name: "Valero Energy", Quantity: 15, CostBasis: 140.00},

				// Tankers & shipping — route disruption, longer voyages
				{Symbol: "FRO", Name: "Frontline", Quantity: 60, CostBasis: 28.00},
				{Symbol: "STNG", Name: "Scorpio Tankers", Quantity: 30, CostBasis: 55.00},
				{Symbol: "INSW", Name: "International Seaways", Quantity: 25, CostBasis: 48.00},
				{Symbol: "ZIM", Name: "ZIM Integrated Shipping", Quantity: 20, CostBasis: 22.00},

				// Uranium & nuclear — energy independence, baseload security
				{Symbol: "CCJ", Name: "Cameco", Quantity: 60, CostBasis: 42.00},
				{Symbol: "UEC", Name: "Uranium Energy Corp", Quantity: 100, CostBasis: 7.50},
				{Symbol: "LEU", Name: "Centrus Energy", Quantity: 15, CostBasis: 70.00},

				// Cybersecurity — state-sponsored threats, critical infra protection
				{Symbol: "PANW", Name: "Palo Alto Networks", Quantity: 10, CostBasis: 310.00},
				{Symbol: "CRWD", Name: "CrowdStrike", Quantity: 12, CostBasis: 280.00},
				{Symbol: "FTNT", Name: "Fortinet", Quantity: 20, CostBasis: 75.00},
				{Symbol: "NET", Name: "Cloudflare", Quantity: 25, CostBasis: 85.00},

				// Gold & safe haven — flight to quality
				{Symbol: "GLD", Name: "SPDR Gold Trust", Quantity: 25, CostBasis: 195.00},
				{Symbol: "NEM", Name: "Newmont", Quantity: 40, CostBasis: 42.00},
				{Symbol: "GOLD", Name: "Barrick Gold", Quantity: 50, CostBasis: 18.00},
				{Symbol: "WPM", Name: "Wheaton Precious Metals", Quantity: 25, CostBasis: 48.00},

				// Critical minerals — supply chain concentration risk
				{Symbol: "MP", Name: "MP Materials", Quantity: 35, CostBasis: 18.00},
				{Symbol: "VALE", Name: "Vale S.A.", Quantity: 50, CostBasis: 12.00},
			},
		},
		{
			Name: "AI / Compute Buildout",
			Holdings: []Holding{
				// Silicon — GPU, custom ASIC, networking chips
				{Symbol: "NVDA", Name: "NVIDIA", Quantity: 20, CostBasis: 475.00},
				{Symbol: "AMD", Name: "Advanced Micro Devices", Quantity: 30, CostBasis: 155.00},
				{Symbol: "AVGO", Name: "Broadcom", Quantity: 12, CostBasis: 160.00},
				{Symbol: "ARM", Name: "Arm Holdings", Quantity: 15, CostBasis: 130.00},
				{Symbol: "TSM", Name: "Taiwan Semiconductor", Quantity: 25, CostBasis: 140.00},
				{Symbol: "MRVL", Name: "Marvell Technology", Quantity: 40, CostBasis: 70.00},

				// Infrastructure — servers, networking, power/cooling
				{Symbol: "SMCI", Name: "Super Micro Computer", Quantity: 10, CostBasis: 50.00},
				{Symbol: "DELL", Name: "Dell Technologies", Quantity: 20, CostBasis: 120.00},
				{Symbol: "ANET", Name: "Arista Networks", Quantity: 10, CostBasis: 280.00},
				{Symbol: "VRT", Name: "Vertiv Holdings", Quantity: 25, CostBasis: 75.00},

				// Hyperscalers — capex drivers, model trainers
				{Symbol: "MSFT", Name: "Microsoft", Quantity: 10, CostBasis: 380.00},
				{Symbol: "GOOGL", Name: "Alphabet", Quantity: 15, CostBasis: 140.00},
				{Symbol: "AMZN", Name: "Amazon", Quantity: 12, CostBasis: 175.00},
				{Symbol: "META", Name: "Meta Platforms", Quantity: 10, CostBasis: 480.00},

				// AI application layer
				{Symbol: "CRM", Name: "Salesforce", Quantity: 12, CostBasis: 260.00},
				{Symbol: "PLTR", Name: "Palantir Technologies", Quantity: 50, CostBasis: 22.00},
				{Symbol: "SNOW", Name: "Snowflake", Quantity: 15, CostBasis: 165.00},

				// Power for the data centers
				{Symbol: "CEG", Name: "Constellation Energy", Quantity: 10, CostBasis: 200.00},
				{Symbol: "VST", Name: "Vistra", Quantity: 20, CostBasis: 85.00},
				{Symbol: "CCJ", Name: "Cameco", Quantity: 30, CostBasis: 42.00},
				{Symbol: "SMR", Name: "NuScale Power", Quantity: 40, CostBasis: 12.00},
			},
		},
		{
			Name: "Rate Cut Beneficiaries",
			Holdings: []Holding{
				// REITs — lower cost of capital, higher property valuations
				{Symbol: "O", Name: "Realty Income", Quantity: 30, CostBasis: 52.00},
				{Symbol: "AMT", Name: "American Tower", Quantity: 10, CostBasis: 195.00},

				// Homebuilders — mortgage rate sensitivity
				{Symbol: "DHI", Name: "D.R. Horton", Quantity: 15, CostBasis: 140.00},
				{Symbol: "LEN", Name: "Lennar", Quantity: 12, CostBasis: 150.00},

				// Growth tech — long-duration assets benefit from lower discount rates
				{Symbol: "TSLA", Name: "Tesla", Quantity: 8, CostBasis: 250.00},
				{Symbol: "COIN", Name: "Coinbase Global", Quantity: 15, CostBasis: 220.00},
				{Symbol: "SQ", Name: "Block (Square)", Quantity: 20, CostBasis: 65.00},
				{Symbol: "SHOP", Name: "Shopify", Quantity: 18, CostBasis: 70.00},
				{Symbol: "SOFI", Name: "SoFi Technologies", Quantity: 80, CostBasis: 8.00},

				// Dividend / income — yield becomes more attractive as rates fall
				{Symbol: "SCHD", Name: "Schwab US Div Equity ETF", Quantity: 40, CostBasis: 76.00},
				{Symbol: "ABBV", Name: "AbbVie", Quantity: 15, CostBasis: 160.00},

				// Regional banks — net interest margin recovery
				{Symbol: "KRE", Name: "SPDR Regional Banking ETF", Quantity: 30, CostBasis: 50.00},

				// Gold — inversely correlated with real rates
				{Symbol: "GLD", Name: "SPDR Gold Trust", Quantity: 20, CostBasis: 195.00},

				// Crypto — risk-on beneficiary of liquidity expansion
				{Symbol: "BTC-USD", Name: "Bitcoin", Quantity: 0.25, CostBasis: 60000.00},
				{Symbol: "ETH-USD", Name: "Ethereum", Quantity: 5, CostBasis: 3200.00},
			},
		},
		{
			Name: "Deglobalization / Reshoring",
			Holdings: []Holding{
				// Heavy equipment & construction — factory buildout
				{Symbol: "CAT", Name: "Caterpillar", Quantity: 10, CostBasis: 310.00},
				{Symbol: "DE", Name: "Deere & Company", Quantity: 8, CostBasis: 380.00},
				{Symbol: "URI", Name: "United Rentals", Quantity: 6, CostBasis: 600.00},
				{Symbol: "PWR", Name: "Quanta Services", Quantity: 12, CostBasis: 230.00},

				// Aggregates & materials — concrete, gravel, steel
				{Symbol: "VMC", Name: "Vulcan Materials", Quantity: 10, CostBasis: 230.00},
				{Symbol: "MLM", Name: "Martin Marietta", Quantity: 6, CostBasis: 500.00},
				{Symbol: "NUE", Name: "Nucor", Quantity: 20, CostBasis: 170.00},
				{Symbol: "STLD", Name: "Steel Dynamics", Quantity: 18, CostBasis: 120.00},

				// US semiconductor fab buildout
				{Symbol: "TSM", Name: "Taiwan Semiconductor", Quantity: 20, CostBasis: 140.00},
				{Symbol: "INTC", Name: "Intel", Quantity: 50, CostBasis: 30.00},
				{Symbol: "AMAT", Name: "Applied Materials", Quantity: 15, CostBasis: 180.00},
				{Symbol: "LRCX", Name: "Lam Research", Quantity: 8, CostBasis: 750.00},

				// Defense industrial base — domestic production mandates
				{Symbol: "LMT", Name: "Lockheed Martin", Quantity: 8, CostBasis: 450.00},
				{Symbol: "GD", Name: "General Dynamics", Quantity: 10, CostBasis: 275.00},

				// Critical minerals — onshoring supply chains
				{Symbol: "MP", Name: "MP Materials", Quantity: 50, CostBasis: 18.00},
				{Symbol: "ALB", Name: "Albemarle", Quantity: 15, CostBasis: 100.00},

				// Grid & electrical infrastructure
				{Symbol: "EATON", Name: "Eaton Corporation", Quantity: 10, CostBasis: 280.00},
			},
		},
		{
			Name: "Energy Transition",
			Holdings: []Holding{
				// Solar
				{Symbol: "ENPH", Name: "Enphase Energy", Quantity: 15, CostBasis: 120.00},
				{Symbol: "FSLR", Name: "First Solar", Quantity: 10, CostBasis: 200.00},

				// Nuclear renaissance — baseload clean power
				{Symbol: "CEG", Name: "Constellation Energy", Quantity: 12, CostBasis: 200.00},
				{Symbol: "VST", Name: "Vistra", Quantity: 20, CostBasis: 85.00},
				{Symbol: "CCJ", Name: "Cameco", Quantity: 40, CostBasis: 42.00},
				{Symbol: "UEC", Name: "Uranium Energy Corp", Quantity: 80, CostBasis: 7.50},
				{Symbol: "DNN", Name: "Denison Mines", Quantity: 150, CostBasis: 2.00},
				{Symbol: "LEU", Name: "Centrus Energy", Quantity: 10, CostBasis: 70.00},
				{Symbol: "NNE", Name: "Nano Nuclear Energy", Quantity: 20, CostBasis: 25.00},
				{Symbol: "SMR", Name: "NuScale Power", Quantity: 30, CostBasis: 12.00},

				// Grid modernization & utilities
				{Symbol: "NEE", Name: "NextEra Energy", Quantity: 20, CostBasis: 72.00},
				{Symbol: "PWR", Name: "Quanta Services", Quantity: 10, CostBasis: 230.00},

				// Battery & storage materials
				{Symbol: "ALB", Name: "Albemarle", Quantity: 15, CostBasis: 100.00},

				// EVs
				{Symbol: "TSLA", Name: "Tesla", Quantity: 5, CostBasis: 250.00},
				{Symbol: "RIVN", Name: "Rivian Automotive", Quantity: 40, CostBasis: 15.00},
			},
		},
		{
			Name: "Infrastructure Spending",
			Holdings: []Holding{
				// Heavy equipment & machinery
				{Symbol: "CAT", Name: "Caterpillar", Quantity: 10, CostBasis: 310.00},
				{Symbol: "DE", Name: "Deere & Company", Quantity: 8, CostBasis: 380.00},
				{Symbol: "URI", Name: "United Rentals", Quantity: 5, CostBasis: 600.00},

				// Aggregates, cement, asphalt — roads, bridges, tunnels
				{Symbol: "VMC", Name: "Vulcan Materials", Quantity: 12, CostBasis: 230.00},
				{Symbol: "MLM", Name: "Martin Marietta", Quantity: 6, CostBasis: 500.00},

				// Steel — structural, rebar, plate
				{Symbol: "NUE", Name: "Nucor", Quantity: 15, CostBasis: 170.00},
				{Symbol: "STLD", Name: "Steel Dynamics", Quantity: 15, CostBasis: 120.00},

				// Electrical & grid — power lines, substations, broadband
				{Symbol: "PWR", Name: "Quanta Services", Quantity: 12, CostBasis: 230.00},
				{Symbol: "EATON", Name: "Eaton Corporation", Quantity: 10, CostBasis: 280.00},

				// Engineering & construction
				{Symbol: "AAON", Name: "AAON", Quantity: 15, CostBasis: 85.00},
				{Symbol: "FAST", Name: "Fastenal", Quantity: 25, CostBasis: 60.00},

				// Water infrastructure
				{Symbol: "XYL", Name: "Xylem", Quantity: 15, CostBasis: 120.00},

				// Rail — freight capacity for materials
				{Symbol: "UNP", Name: "Union Pacific", Quantity: 10, CostBasis: 240.00},
				{Symbol: "CSX", Name: "CSX Corporation", Quantity: 30, CostBasis: 35.00},
			},
		},
		{
			Name: "Aging Population",
			Holdings: []Holding{
				// Big pharma — patent portfolios, aging-related drugs
				{Symbol: "LLY", Name: "Eli Lilly", Quantity: 5, CostBasis: 700.00},
				{Symbol: "ABBV", Name: "AbbVie", Quantity: 15, CostBasis: 160.00},
				{Symbol: "JNJ", Name: "Johnson & Johnson", Quantity: 15, CostBasis: 155.00},
				{Symbol: "MRK", Name: "Merck & Co.", Quantity: 15, CostBasis: 120.00},
				{Symbol: "PFE", Name: "Pfizer", Quantity: 40, CostBasis: 28.00},

				// Medical devices — implants, surgical robots, diagnostics
				{Symbol: "ISRG", Name: "Intuitive Surgical", Quantity: 5, CostBasis: 400.00},
				{Symbol: "MDT", Name: "Medtronic", Quantity: 20, CostBasis: 82.00},
				{Symbol: "ABT", Name: "Abbott Laboratories", Quantity: 15, CostBasis: 110.00},
				{Symbol: "SYK", Name: "Stryker", Quantity: 8, CostBasis: 340.00},

				// Health insurance — Medicare Advantage enrollment growth
				{Symbol: "UNH", Name: "UnitedHealth Group", Quantity: 5, CostBasis: 520.00},
				{Symbol: "HUM", Name: "Humana", Quantity: 8, CostBasis: 350.00},

				// Senior living & home health
				{Symbol: "WELL", Name: "Welltower", Quantity: 20, CostBasis: 95.00},

				// Biotech — Alzheimer's, oncology, gene therapy
				{Symbol: "VRTX", Name: "Vertex Pharmaceuticals", Quantity: 8, CostBasis: 410.00},
				{Symbol: "REGN", Name: "Regeneron", Quantity: 4, CostBasis: 900.00},
			},
		},
		{
			Name: "GLP-1 / Obesity Revolution",
			Holdings: []Holding{
				// Drug makers — GLP-1 agonists (semaglutide, tirzepatide)
				{Symbol: "LLY", Name: "Eli Lilly", Quantity: 5, CostBasis: 700.00},
				{Symbol: "NVO", Name: "Novo Nordisk", Quantity: 15, CostBasis: 130.00},
				{Symbol: "AMGN", Name: "Amgen", Quantity: 8, CostBasis: 290.00},

				// Telehealth & compounding — patient access plays
				{Symbol: "HIMS", Name: "Hims & Hers Health", Quantity: 60, CostBasis: 16.00},
				{Symbol: "TDOC", Name: "Teladoc Health", Quantity: 30, CostBasis: 20.00},

				// Diabetes / metabolic monitoring — companion devices
				{Symbol: "DXCM", Name: "DexCom", Quantity: 12, CostBasis: 95.00},
				{Symbol: "PODD", Name: "Insulet", Quantity: 8, CostBasis: 175.00},
				{Symbol: "ABT", Name: "Abbott Laboratories", Quantity: 15, CostBasis: 110.00},

				// CDMOs — contract drug manufacturing for GLP-1 scale-up
				{Symbol: "CRL", Name: "Charles River Labs", Quantity: 8, CostBasis: 210.00},
				{Symbol: "WST", Name: "West Pharmaceutical", Quantity: 6, CostBasis: 350.00},

				// Fitness & wellness — behavioral tailwind
				{Symbol: "PTON", Name: "Peloton Interactive", Quantity: 50, CostBasis: 5.00},
			},
		},
		{
			Name: "Robotics & Automation",
			Holdings: []Holding{
				// Surgical robotics
				{Symbol: "ISRG", Name: "Intuitive Surgical", Quantity: 5, CostBasis: 400.00},

				// Industrial robots & motion control
				{Symbol: "ROK", Name: "Rockwell Automation", Quantity: 8, CostBasis: 270.00},
				{Symbol: "TER", Name: "Teradyne", Quantity: 15, CostBasis: 100.00},
				{Symbol: "CGNX", Name: "Cognex", Quantity: 20, CostBasis: 42.00},

				// Warehouse & logistics automation
				{Symbol: "AMZN", Name: "Amazon", Quantity: 8, CostBasis: 175.00},
				{Symbol: "GXO", Name: "GXO Logistics", Quantity: 20, CostBasis: 50.00},

				// Autonomous vehicles & mobility
				{Symbol: "TSLA", Name: "Tesla", Quantity: 5, CostBasis: 250.00},
				{Symbol: "UBER", Name: "Uber Technologies", Quantity: 25, CostBasis: 65.00},

				// Semiconductor enablers — edge AI, sensors
				{Symbol: "NVDA", Name: "NVIDIA", Quantity: 5, CostBasis: 475.00},
				{Symbol: "ADI", Name: "Analog Devices", Quantity: 12, CostBasis: 210.00},
				{Symbol: "ON", Name: "onsemi", Quantity: 20, CostBasis: 70.00},

				// Defense drones & unmanned systems
				{Symbol: "KTOS", Name: "Kratos Defense", Quantity: 40, CostBasis: 22.00},
				{Symbol: "AVAV", Name: "AeroVironment", Quantity: 8, CostBasis: 175.00},
			},
		},
		{
			Name: "Commodities Supercycle",
			Holdings: []Holding{
				// Copper — electrification, EVs, grid, data centers
				{Symbol: "FCX", Name: "Freeport-McMoRan", Quantity: 40, CostBasis: 42.00},
				{Symbol: "SCCO", Name: "Southern Copper", Quantity: 12, CostBasis: 95.00},
				{Symbol: "TECK", Name: "Teck Resources", Quantity: 25, CostBasis: 42.00},

				// Lithium — EV batteries, grid storage
				{Symbol: "ALB", Name: "Albemarle", Quantity: 15, CostBasis: 100.00},
				{Symbol: "SQM", Name: "Sociedad Quimica Minera", Quantity: 15, CostBasis: 48.00},

				// Diversified mining
				{Symbol: "BHP", Name: "BHP Group", Quantity: 20, CostBasis: 60.00},
				{Symbol: "RIO", Name: "Rio Tinto", Quantity: 15, CostBasis: 65.00},
				{Symbol: "VALE", Name: "Vale S.A.", Quantity: 50, CostBasis: 12.00},

				// Uranium
				{Symbol: "CCJ", Name: "Cameco", Quantity: 40, CostBasis: 42.00},
				{Symbol: "UEC", Name: "Uranium Energy Corp", Quantity: 80, CostBasis: 7.50},

				// Gold & silver — monetary metals
				{Symbol: "NEM", Name: "Newmont", Quantity: 30, CostBasis: 42.00},
				{Symbol: "GOLD", Name: "Barrick Gold", Quantity: 40, CostBasis: 18.00},
				{Symbol: "WPM", Name: "Wheaton Precious Metals", Quantity: 20, CostBasis: 48.00},

				// Oil — structural underinvestment
				{Symbol: "XOM", Name: "Exxon Mobil", Quantity: 15, CostBasis: 105.00},
				{Symbol: "CVX", Name: "Chevron", Quantity: 10, CostBasis: 155.00},

				// Agriculture — food security, fertilizer
				{Symbol: "MOS", Name: "Mosaic Company", Quantity: 25, CostBasis: 30.00},
				{Symbol: "NTR", Name: "Nutrien", Quantity: 15, CostBasis: 52.00},
				{Symbol: "ADM", Name: "Archer-Daniels-Midland", Quantity: 20, CostBasis: 55.00},
			},
		},
		{
			Name: "Crypto Ecosystem",
			Holdings: []Holding{
				// Direct crypto exposure
				{Symbol: "BTC-USD", Name: "Bitcoin", Quantity: 0.5, CostBasis: 60000.00},
				{Symbol: "ETH-USD", Name: "Ethereum", Quantity: 10, CostBasis: 3200.00},
				{Symbol: "SOL-USD", Name: "Solana", Quantity: 50, CostBasis: 140.00},

				// Exchanges & infrastructure
				{Symbol: "COIN", Name: "Coinbase Global", Quantity: 15, CostBasis: 220.00},

				// Bitcoin miners — leveraged BTC exposure
				{Symbol: "MARA", Name: "Marathon Digital", Quantity: 40, CostBasis: 20.00},
				{Symbol: "CLSK", Name: "CleanSpark", Quantity: 30, CostBasis: 15.00},
				{Symbol: "RIOT", Name: "Riot Platforms", Quantity: 40, CostBasis: 12.00},
				{Symbol: "WULF", Name: "TeraWulf", Quantity: 60, CostBasis: 5.00},

				// Corporate BTC holders
				{Symbol: "MSTR", Name: "MicroStrategy", Quantity: 5, CostBasis: 1500.00},

				// Crypto-adjacent fintech
				{Symbol: "SQ", Name: "Block (Square)", Quantity: 15, CostBasis: 65.00},
				{Symbol: "HOOD", Name: "Robinhood Markets", Quantity: 40, CostBasis: 18.00},
				{Symbol: "SOFI", Name: "SoFi Technologies", Quantity: 60, CostBasis: 8.00},

				// DeFi-correlated tokens
				{Symbol: "LINK-USD", Name: "Chainlink", Quantity: 100, CostBasis: 15.00},
				{Symbol: "AVAX-USD", Name: "Avalanche", Quantity: 50, CostBasis: 35.00},
			},
		},
		{
			Name: "Space Economy",
			Holdings: []Holding{
				// Launch & rockets
				{Symbol: "RKLB", Name: "Rocket Lab USA", Quantity: 60, CostBasis: 18.00},
				{Symbol: "BA", Name: "Boeing", Quantity: 8, CostBasis: 195.00},
				{Symbol: "LMT", Name: "Lockheed Martin", Quantity: 5, CostBasis: 450.00},

				// Satellite communications
				{Symbol: "ASTS", Name: "AST SpaceMobile", Quantity: 40, CostBasis: 25.00},
				{Symbol: "GSAT", Name: "Globalstar", Quantity: 200, CostBasis: 2.00},
				{Symbol: "IRDM", Name: "Iridium Communications", Quantity: 20, CostBasis: 30.00},

				// Earth observation & data
				{Symbol: "PLTR", Name: "Palantir Technologies", Quantity: 30, CostBasis: 22.00},

				// Space defense & national security
				{Symbol: "NOC", Name: "Northrop Grumman", Quantity: 5, CostBasis: 470.00},
				{Symbol: "LHX", Name: "L3Harris Technologies", Quantity: 8, CostBasis: 210.00},
				{Symbol: "KTOS", Name: "Kratos Defense", Quantity: 30, CostBasis: 22.00},

				// GPS, navigation, space electronics
				{Symbol: "MRVL", Name: "Marvell Technology", Quantity: 20, CostBasis: 70.00},
			},
		},
	}

	DefaultPollInterval = "15s"
	DefaultSparklineLen = 60
	DefaultTheme        = "tokyonight"
)
