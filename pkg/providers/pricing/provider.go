package pricing

import (
	"context"
)

type Currency string

const (
	CHF Currency = "chf"
	EUR Currency = "eur"
	USD Currency = "usd"
)

type Provider interface {
	GetPrice(ctx context.Context, instanceType string, currency Currency) (float64, error)
	GetAllPrices(ctx context.Context, currency Currency) (map[string]float64, error)
}

type ProviderOptions struct {
	DefaultCurrency Currency
}
