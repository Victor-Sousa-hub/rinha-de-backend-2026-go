package scoring_test

import (
	"math"
	"testing"
	"time"

	"github.com/Victor-Sousa-hub/rinha-de-backend-2026-go/internal/model"
	"github.com/Victor-Sousa-hub/rinha-de-backend-2026-go/internal/scoring"
)

func TestVectorize_ExamplePayload(t *testing.T) {
	req := &model.FraudScoreRequest{
		ID: "tx-1329056812",
		Transaction: model.Transaction{
			Amount:       41.12,
			Installments: 2,
			RequestedAt:  time.Date(2026, 3, 11, 18, 45, 53, 0, time.UTC),
		},
		Customer: model.Customer{
			AvgAmount:      82.24,
			TxCount24h:     3,
			KnownMerchants: []string{"MERC-003", "MERC-016"},
		},
		Merchant: model.Merchant{
			ID:        "MERC-016",
			MCC:       "5411",
			AvgAmount: 60.25,
		},
		Terminal: model.Terminal{
			IsOnline:    false,
			CardPresent: true,
			KmFromHome:  29.2331036248,
		},
		LastTransaction: &model.LastTransaction{KmFromCurrent: -1}, // null no original
	}

	want := [14]float64{
		0.0041, 0.1667, 0.05, 0.7826, 0.3333,
		-1, -1,
		0.0292, 0.15, 0, 1, 0, 0.15, 0.006,
	}

	got := scoring.Vectorize(req)

	for i, w := range want {
		if math.Abs(got[i]-w) > 0.001 {
			t.Errorf("dim[%d]: got %.6f, want %.4f", i, got[i], w)
		}
	}
}

func TestVectorize_NullLastTx_Sentinel(t *testing.T) {
	req := &model.FraudScoreRequest{
		Transaction: model.Transaction{Amount: 100, Installments: 1, RequestedAt: time.Now()},
		Customer:    model.Customer{AvgAmount: 100, TxCount24h: 1, KnownMerchants: []string{}},
		Merchant:    model.Merchant{ID: "MERC-001", MCC: "5411", AvgAmount: 100},
		Terminal:    model.Terminal{},
		LastTransaction: &model.LastTransaction{KmFromCurrent: -1},
	}

	v := scoring.Vectorize(req)
	if v[5] != -1 {
		t.Errorf("dim[5] minutes_since_last_tx: esperava -1, got %f", v[5])
	}
	if v[6] != -1 {
		t.Errorf("dim[6] km_from_last_tx: esperava -1, got %f", v[6])
	}
}

func TestVectorize_UnknownMerchant(t *testing.T) {
	req := &model.FraudScoreRequest{
		Transaction:     model.Transaction{Amount: 100, Installments: 1, RequestedAt: time.Now()},
		Customer:        model.Customer{AvgAmount: 100, TxCount24h: 1, KnownMerchants: []string{"MERC-001"}},
		Merchant:        model.Merchant{ID: "MERC-999", MCC: "5411", AvgAmount: 100},
		Terminal:        model.Terminal{},
		LastTransaction: &model.LastTransaction{KmFromCurrent: -1},
	}

	v := scoring.Vectorize(req)
	if v[11] != 1.0 {
		t.Errorf("dim[11] unknown_merchant: esperava 1.0 (desconhecido), got %f", v[11])
	}
}

func TestVectorize_Clamp(t *testing.T) {
	req := &model.FraudScoreRequest{
		Transaction:     model.Transaction{Amount: 999_999, Installments: 999, RequestedAt: time.Now()},
		Customer:        model.Customer{AvgAmount: 1, TxCount24h: 999, KnownMerchants: []string{}},
		Merchant:        model.Merchant{ID: "MERC-001", MCC: "5411", AvgAmount: 999_999},
		Terminal:        model.Terminal{KmFromHome: 999_999},
		LastTransaction: &model.LastTransaction{KmFromCurrent: -1},
	}

	v := scoring.Vectorize(req)
	for i, val := range v {
		if val == -1 {
			continue // sentinela
		}
		if val < 0 || val > 1 {
			t.Errorf("dim[%d] fora do intervalo [0,1]: %f", i, val)
		}
	}
}
