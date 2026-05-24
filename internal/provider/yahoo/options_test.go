package yahoo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

const sampleOptionsResp = `{
  "optionChain": {
    "result": [{
      "expirationDates": [1700000000],
      "options": [{
        "expirationDate": 1700000000,
        "calls": [
          {"contractSymbol":"AAPL240101C00100000","strike":100,"lastPrice":5.2,"bid":5.1,"ask":5.3,"volume":1000,"openInterest":5000,"impliedVolatility":0.35},
          {"contractSymbol":"AAPL240101C00110000","strike":110,"lastPrice":1.1,"bid":1.0,"ask":1.2,"volume":500,"openInterest":3000,"impliedVolatility":0.28}
        ],
        "puts": [
          {"contractSymbol":"AAPL240101P00100000","strike":100,"lastPrice":2.0,"bid":1.95,"ask":2.05,"volume":800,"openInterest":4000,"impliedVolatility":0.40},
          {"contractSymbol":"AAPL240101P00090000","strike":90,"lastPrice":0.5,"bid":0.45,"ask":0.55,"volume":200,"openInterest":1500,"impliedVolatility":0.45}
        ]
      }]
    }]
  }
}`

func TestFetchOptionsHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(sampleOptionsResp))
	}))
	defer srv.Close()

	prev := OptionsBaseURL
	OptionsBaseURL = srv.URL
	defer func() { OptionsBaseURL = prev }()

	p := New(15 * 1e9) // 15s
	chain, err := p.FetchOptionsChain(context.Background(), "AAPL")
	if err != nil {
		t.Fatalf("FetchOptionsChain: %v", err)
	}
	if chain.Symbol != "AAPL" {
		t.Errorf("symbol = %q", chain.Symbol)
	}
	if len(chain.Calls) != 2 || len(chain.Puts) != 2 {
		t.Fatalf("want 2/2, got %d/%d", len(chain.Calls), len(chain.Puts))
	}
	if chain.Calls[0].Strike != 100 || chain.Calls[0].Bid != 5.1 {
		t.Errorf("call[0]: %+v", chain.Calls[0])
	}
	if chain.Puts[1].Strike != 90 || chain.Puts[1].IV != 0.45 {
		t.Errorf("put[1]: %+v", chain.Puts[1])
	}
}

func TestFetchOptionsEmptyResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"optionChain":{"result":[]}}`))
	}))
	defer srv.Close()
	prev := OptionsBaseURL
	OptionsBaseURL = srv.URL
	defer func() { OptionsBaseURL = prev }()

	p := New(15 * 1e9)
	chain, err := p.FetchOptionsChain(context.Background(), "AAPL")
	if err != nil {
		t.Fatalf("FetchOptionsChain: %v", err)
	}
	if len(chain.Calls) != 0 || len(chain.Puts) != 0 {
		t.Errorf("want empty, got %d/%d", len(chain.Calls), len(chain.Puts))
	}
}

func TestFetchOptions500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()
	prev := OptionsBaseURL
	OptionsBaseURL = srv.URL
	defer func() { OptionsBaseURL = prev }()

	p := New(15 * 1e9)
	_, err := p.FetchOptionsChain(context.Background(), "AAPL")
	if err == nil {
		t.Fatal("expected error on 500")
	}
}
