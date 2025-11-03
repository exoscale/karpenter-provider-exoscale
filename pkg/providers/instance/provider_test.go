package instance

import (
	"errors"
	"testing"

	egov3 "github.com/exoscale/egoscale/v3"
)

func TestConvertSecurityGroups(t *testing.T) {
	p := &Provider{}
	ids := []string{"sg-1", "sg-2", "sg-3"}

	got := p.convertSecurityGroups(ids)

	if len(got) != len(ids) {
		t.Errorf("convertSecurityGroups() len = %d, want %d", len(got), len(ids))
	}

	for i, id := range ids {
		if string(got[i].ID) != id {
			t.Errorf("convertSecurityGroups()[%d] = %v, want %v", i, got[i].ID, id)
		}
	}
}

func TestConvertAntiAffinityGroups(t *testing.T) {
	p := &Provider{}
	ids := []string{"aag-1", "aag-2"}

	got := p.convertAntiAffinityGroups(ids)

	if len(got) != len(ids) {
		t.Errorf("convertAntiAffinityGroups() len = %d, want %d", len(got), len(ids))
	}

	for i, id := range ids {
		if string(got[i].ID) != id {
			t.Errorf("convertAntiAffinityGroups()[%d] = %v, want %v", i, got[i].ID, id)
		}
	}
}

func TestIsNotFoundError(t *testing.T) {
	p := &Provider{}

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "not found error",
			err:  egov3.ErrNotFound,
			want: true,
		},
		{
			name: "other error",
			err:  errors.New("some other error"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := p.isNotFoundError(tt.err)
			if got != tt.want {
				t.Errorf("isNotFoundError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFindCheapestInstanceType(t *testing.T) {
	tests := []struct {
		name          string
		instanceTypes []string
		prices        map[string]float64
		want          string
	}{
		{
			name:          "single instance type",
			instanceTypes: []string{"standard.medium"},
			prices:        map[string]float64{"standard.medium": 0.05},
			want:          "standard.medium",
		},
		{
			name:          "cheapest instance type",
			instanceTypes: []string{"standard.medium", "standard.large", "standard.small"},
			prices: map[string]float64{
				"standard.medium": 0.05,
				"standard.large":  0.10,
				"standard.small":  0.03,
			},
			want: "standard.small",
		},
		{
			name:          "no prices available",
			instanceTypes: []string{"standard.medium", "standard.large"},
			prices:        map[string]float64{},
			want:          "standard.medium",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findCheapestInstanceType(tt.instanceTypes, tt.prices)
			if got != tt.want {
				t.Errorf("findCheapestInstanceType() = %v, want %v", got, tt.want)
			}
		})
	}
}
