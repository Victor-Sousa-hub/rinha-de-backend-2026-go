package scoring

import (
	_ "embed"
	"encoding/json"
	"math"

	"github.com/Victor-Sousa-hub/rinha-de-backend-2026-go/internal/model"
)

const (
	maxAmount            = 10_000.0
	maxInstallments      = 12.0
	amountVsAvgRatio     = 10.0
	maxMinutes           = 1_440.0 // 24h em minutos
	maxKm                = 1_000.0
	maxTxCount24h        = 20.0
	maxMerchantAvgAmount = 10_000.0
	defaultMCCRisk       = 0.5
)

//go:embed mcc_risk.json
var mccRiskJSON []byte

var mccRisk map[string]float64

func init() {
	json.Unmarshal(mccRiskJSON, &mccRisk)
}

func clamp(v float64) float64 {
	return math.Min(1.0, math.Max(0.0, v))
}

func boolVal(b bool) float64 {
	if b {
		return 1.0
	}
	return 0.0
}

// Vectorize converte uma requisição nas 14 dimensões normalizadas para o modelo KNN.
// Dimensões 5 e 6 retornam -1 quando last_transaction é nulo (sentinela).
func Vectorize(req *model.FraudScoreRequest) [14]float64 {
	t := req.Transaction.RequestedAt.UTC()

	// dim 3 — hora do dia (0–23)
	hour := float64(t.Hour()) / 23.0

	// dim 4 — dia da semana: seg=0, dom=6
	dow := float64((int(t.Weekday())+6)%7) / 6.0

	// dims 5 e 6 — dependem de last_transaction
	minutesSinceLast := -1.0
	kmFromLast := -1.0
	if lt := req.LastTransaction; lt != nil && lt.KmFromCurrent >= 0 {
		diff := req.Transaction.RequestedAt.Sub(lt.Timestamp).Minutes()
		minutesSinceLast = clamp(diff / maxMinutes)
		kmFromLast = clamp(lt.KmFromCurrent / maxKm)
	}

	// dim 11 — merchant desconhecido
	unknownMerchant := 1.0
	for _, m := range req.Customer.KnownMerchants {
		if m == req.Merchant.ID {
			unknownMerchant = 0.0
			break
		}
	}

	// dim 12 — risco do MCC
	mccScore, ok := mccRisk[req.Merchant.MCC]
	if !ok {
		mccScore = defaultMCCRisk
	}

	// dim 2 — amount vs avg do cliente (protege contra avg_amount zero)
	amountVsAvg := 0.0
	if req.Customer.AvgAmount > 0 {
		amountVsAvg = clamp((req.Transaction.Amount / req.Customer.AvgAmount) / amountVsAvgRatio)
	}

	return [14]float64{
		clamp(req.Transaction.Amount / maxAmount),                       // 0  amount
		clamp(float64(req.Transaction.Installments) / maxInstallments),  // 1  installments
		amountVsAvg,                                                     // 2  amount_vs_avg
		hour,                                                            // 3  hour_of_day
		dow,                                                             // 4  day_of_week
		minutesSinceLast,                                                // 5  minutes_since_last_tx
		kmFromLast,                                                      // 6  km_from_last_tx
		clamp(req.Terminal.KmFromHome / maxKm),                          // 7  km_from_home
		clamp(float64(req.Customer.TxCount24h) / maxTxCount24h),         // 8  tx_count_24h
		boolVal(req.Terminal.IsOnline),                                  // 9  is_online
		boolVal(req.Terminal.CardPresent),                               // 10 card_present
		unknownMerchant,                                                 // 11 unknown_merchant
		mccScore,                                                        // 12 mcc_risk
		clamp(req.Merchant.AvgAmount / maxMerchantAvgAmount),            // 13 merchant_avg_amount
	}
}
