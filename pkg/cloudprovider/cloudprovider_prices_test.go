package cloudprovider

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParse_PricingList(t *testing.T) {
	err := parsePrices()
	assert.Nil(t, err)

	assert.NotEmpty(t, exoscalePricingList.Chf)
	assert.NotEmpty(t, exoscalePricingList.Eur)
	assert.NotEmpty(t, exoscalePricingList.Usd)

	assert.Equal(t, 0.0, exoscalePricingList.Chf["wtf"])
	assert.Equal(t, 0.18667, exoscalePricingList.Eur["extra-large"])
}

func TestPriceFromProfile(t *testing.T) {
	assert.Equal(t, 0.0, priceFromProfile("wtf"))
	assert.Equal(t, 0.18667, priceFromProfile("standard-extra-large"))
	assert.Equal(t, 1.12, priceFromProfile("memory-titan"))
}
