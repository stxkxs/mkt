package coinbase

import "testing"

func TestApplyL2Snapshot(t *testing.T) {
	bids := map[float64]float64{}
	asks := map[float64]float64{}
	events := []l2Event{{
		Type: "snapshot",
		Updates: []l2Update{
			{Side: "bid", PriceLevel: "100.00", NewQuantity: "1.0"},
			{Side: "bid", PriceLevel: "99.50", NewQuantity: "2.0"},
			{Side: "offer", PriceLevel: "100.50", NewQuantity: "1.5"},
		},
	}}
	if !applyL2(bids, asks, events) {
		t.Fatal("expected snapshot to register as changed")
	}
	if len(bids) != 2 || len(asks) != 1 {
		t.Fatalf("counts: bids=%d asks=%d", len(bids), len(asks))
	}
	if bids[100] != 1.0 || bids[99.5] != 2.0 || asks[100.5] != 1.5 {
		t.Errorf("contents: %+v %+v", bids, asks)
	}
}

func TestApplyL2UpdateUpsertAndDelete(t *testing.T) {
	bids := map[float64]float64{100.0: 1.0, 99.5: 2.0}
	asks := map[float64]float64{100.5: 1.5}
	events := []l2Event{{
		Type: "update",
		Updates: []l2Update{
			{Side: "bid", PriceLevel: "100.00", NewQuantity: "5.0"}, // upsert
			{Side: "bid", PriceLevel: "99.50", NewQuantity: "0"},    // delete
			{Side: "bid", PriceLevel: "99.00", NewQuantity: "3.0"},  // new
			{Side: "offer", PriceLevel: "100.50", NewQuantity: "0"}, // delete
		},
	}}
	if !applyL2(bids, asks, events) {
		t.Fatal("expected changes")
	}
	if bids[100] != 5.0 {
		t.Errorf("upsert: bids[100]=%v want 5", bids[100])
	}
	if _, exists := bids[99.5]; exists {
		t.Errorf("delete failed: bids[99.5] still present")
	}
	if bids[99] != 3.0 {
		t.Errorf("new level missing: bids[99]=%v want 3", bids[99])
	}
	if len(asks) != 0 {
		t.Errorf("ask delete failed: %+v", asks)
	}
}

func TestApplyL2SnapshotResetsState(t *testing.T) {
	bids := map[float64]float64{50.0: 99.0}
	asks := map[float64]float64{51.0: 99.0}
	events := []l2Event{{
		Type: "snapshot",
		Updates: []l2Update{
			{Side: "bid", PriceLevel: "100.00", NewQuantity: "1.0"},
		},
	}}
	_ = applyL2(bids, asks, events)
	if _, exists := bids[50]; exists {
		t.Errorf("snapshot didn't reset old bid")
	}
	if _, exists := asks[51]; exists {
		t.Errorf("snapshot didn't reset old ask")
	}
	if bids[100] != 1.0 {
		t.Errorf("new snapshot data missing")
	}
}

func TestApplyL2InvalidValuesSkipped(t *testing.T) {
	bids := map[float64]float64{}
	asks := map[float64]float64{}
	events := []l2Event{{
		Type: "update",
		Updates: []l2Update{
			{Side: "bid", PriceLevel: "abc", NewQuantity: "1.0"},    // bad price
			{Side: "bid", PriceLevel: "100.00", NewQuantity: "xyz"}, // bad qty
			{Side: "bid", PriceLevel: "100.00", NewQuantity: "1.0"}, // good
		},
	}}
	_ = applyL2(bids, asks, events)
	if len(bids) != 1 || bids[100] != 1.0 {
		t.Errorf("good update should remain: %+v", bids)
	}
}

func TestBuildBookSorted(t *testing.T) {
	bids := map[float64]float64{100: 1, 99: 2, 101: 3}
	asks := map[float64]float64{102: 1, 103: 2, 101.5: 0.5}
	book := buildBook("BTC-USD", 42, bids, asks)
	if book.Sequence != 42 || book.ProductID != "BTC-USD" {
		t.Errorf("header wrong: %+v", book)
	}
	// Bids descending
	if book.Bids[0].Price != 101 || book.Bids[2].Price != 99 {
		t.Errorf("bids not sorted desc: %+v", book.Bids)
	}
	// Asks ascending
	if book.Asks[0].Price != 101.5 || book.Asks[2].Price != 103 {
		t.Errorf("asks not sorted asc: %+v", book.Asks)
	}
}
