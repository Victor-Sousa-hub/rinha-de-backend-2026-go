package model

import (
	"fmt"
	"regexp"
	"time"
)

var mccPattern = regexp.MustCompile(`^\d{4}$`)

type FraudScoreRequest struct {
	ID              string           `json:"id"`
	Transaction     Transaction      `json:"transaction"`
	Customer        Customer         `json:"customer"`
	Merchant        Merchant         `json:"merchant"`
	Terminal        Terminal         `json:"terminal"`
	LastTransaction *LastTransaction `json:"last_transaction"`
}

type Transaction struct {
	Amount       float64   `json:"amount"`
	Installments int       `json:"installments"`
	RequestedAt  time.Time `json:"requested_at"`
}

type Customer struct {
	AvgAmount      float64  `json:"avg_amount"`
	TxCount24h     int      `json:"tx_count_24h"`
	KnownMerchants []string `json:"known_merchants"`
}

type Merchant struct {
	ID        string  `json:"id"`
	MCC       string  `json:"mcc"`
	AvgAmount float64 `json:"avg_amount"`
}

type Terminal struct {
	IsOnline    bool    `json:"is_online"`
	CardPresent bool    `json:"card_present"`
	KmFromHome  float64 `json:"km_from_home"`
}

type LastTransaction struct {
	Timestamp     time.Time `json:"timestamp"`
	KmFromCurrent float64   `json:"km_from_current"`
}

type FraudScoreResponse struct {
	Approved   bool    `json:"approved"`
	FraudScore float64 `json:"fraud_score"`
}

type ValidationErrors []string

func (ve ValidationErrors) Error() string {
	return fmt.Sprintf("validation failed: %v", []string(ve))
}

func (r *FraudScoreRequest) Validate() error {
	var errs ValidationErrors

	if r.ID == "" {
		errs = append(errs, "id: obrigatório")
	}

	if r.Transaction.Amount <= 0 {
		errs = append(errs, "transaction.amount: deve ser maior que zero")
	}
	if r.Transaction.Installments < 1 {
		errs = append(errs, "transaction.installments: deve ser pelo menos 1")
	}
	if r.Transaction.RequestedAt.IsZero() {
		errs = append(errs, "transaction.requested_at: obrigatório")
	}

	if r.Customer.AvgAmount < 0 {
		errs = append(errs, "customer.avg_amount: não pode ser negativo")
	}
	if r.Customer.TxCount24h < 0 {
		errs = append(errs, "customer.tx_count_24h: não pode ser negativo")
	}

	if r.Merchant.ID == "" {
		errs = append(errs, "merchant.id: obrigatório")
	}
	if !mccPattern.MatchString(r.Merchant.MCC) {
		errs = append(errs, "merchant.mcc: deve ter exatamente 4 dígitos numéricos")
	}
	if r.Merchant.AvgAmount < 0 {
		errs = append(errs, "merchant.avg_amount: não pode ser negativo")
	}

	if r.Terminal.KmFromHome < 0 {
		errs = append(errs, "terminal.km_from_home: não pode ser negativo")
	}

	if r.LastTransaction != nil {
		if r.LastTransaction.Timestamp.IsZero() {
			errs = append(errs, "last_transaction.timestamp: obrigatório quando presente")
		}
		if r.LastTransaction.KmFromCurrent < 0 {
			errs = append(errs, "last_transaction.km_from_current: não pode ser negativo")
		}
	}

	if len(errs) > 0 {
		return errs
	}
	return nil
}
