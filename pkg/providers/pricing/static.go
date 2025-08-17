package pricing

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
)

//go:embed prices.json
var pricesJSON []byte

type staticProvider struct {
	prices          map[Currency]map[string]float64
	defaultCurrency Currency
	mu              sync.RWMutex
}

type rawPricingData struct {
	CHF map[string]string `json:"chf"`
	EUR map[string]string `json:"eur"`
	USD map[string]string `json:"usd"`
}

func NewStaticProvider(opts *ProviderOptions) (Provider, error) {
	if opts == nil {
		opts = &ProviderOptions{
			DefaultCurrency: EUR,
		}
	}

	provider := &staticProvider{
		prices:          make(map[Currency]map[string]float64),
		defaultCurrency: opts.DefaultCurrency,
	}

	if err := provider.loadPrices(); err != nil {
		return nil, fmt.Errorf("failed to load prices: %w", err)
	}

	return provider, nil
}

func (p *staticProvider) loadPrices() error {
	var rawData rawPricingData
	if err := json.Unmarshal(pricesJSON, &rawData); err != nil {
		return fmt.Errorf("failed to unmarshal pricing data: %w", err)
	}

	if err := p.parseCurrency(CHF, rawData.CHF); err != nil {
		return fmt.Errorf("failed to parse CHF prices: %w", err)
	}

	if err := p.parseCurrency(EUR, rawData.EUR); err != nil {
		return fmt.Errorf("failed to parse EUR prices: %w", err)
	}

	if err := p.parseCurrency(USD, rawData.USD); err != nil {
		return fmt.Errorf("failed to parse USD prices: %w", err)
	}

	return nil
}

func (p *staticProvider) parseCurrency(currency Currency, rawPrices map[string]string) error {
	p.prices[currency] = make(map[string]float64)

	for rawKey, rawPrice := range rawPrices {
		price, err := strconv.ParseFloat(rawPrice, 64)
		if err != nil {
			return fmt.Errorf("failed to parse price for %s: %w", rawKey, err)
		}

		normalizedKey := normalizeInstanceType(rawKey)
		p.prices[currency][normalizedKey] = price
	}

	return nil
}

func normalizeInstanceType(rawKey string) string {
	key := strings.TrimPrefix(rawKey, "running_")
	key = strings.ToLower(key)
	key = strings.ReplaceAll(key, "_", "-")

	sizes := []string{"extra-large", "colossus", "jumbo", "titan", "micro", "tiny", "small", "medium", "large", "huge", "mega"}

	for _, size := range sizes {
		if key == size {
			return fmt.Sprintf("standard.%s", size)
		}

		suffix := "-" + size
		if strings.HasSuffix(key, suffix) {
			family := strings.TrimSuffix(key, suffix)
			family = strings.ReplaceAll(family, "-", "")
			return fmt.Sprintf("%s.%s", family, size)
		}
	}

	return key
}

func (p *staticProvider) GetPrice(_ context.Context, instanceType string, currency Currency) (float64, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if currency == "" {
		currency = p.defaultCurrency
	}

	currencyPrices, ok := p.prices[currency]
	if !ok {
		return 0, fmt.Errorf("currency %s not supported", currency)
	}

	if price, ok := currencyPrices[instanceType]; ok {
		return price, nil
	}

	normalizedType := strings.TrimPrefix(instanceType, "standard.")
	if price, ok := currencyPrices[normalizedType]; ok {
		return price, nil
	}

	return 0, fmt.Errorf("price not found for instance type %s", instanceType)
}

func (p *staticProvider) GetAllPrices(_ context.Context, currency Currency) (map[string]float64, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if currency == "" {
		currency = p.defaultCurrency
	}

	currencyPrices, ok := p.prices[currency]
	if !ok {
		return nil, fmt.Errorf("currency %s not supported", currency)
	}

	result := make(map[string]float64, len(currencyPrices))
	for k, v := range currencyPrices {
		result[k] = v
	}

	return result, nil
}
