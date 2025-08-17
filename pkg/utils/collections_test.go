package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestToStringSet(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected map[string]bool
	}{
		{
			name:     "EmptyList",
			input:    []string{},
			expected: map[string]bool{},
		},
		{
			name:     "SingleItem",
			input:    []string{"item-1"},
			expected: map[string]bool{"item-1": true},
		},
		{
			name:     "MultipleItems",
			input:    []string{"item-1", "item-2", "item-3"},
			expected: map[string]bool{"item-1": true, "item-2": true, "item-3": true},
		},
		{
			name:     "DuplicateItems",
			input:    []string{"item-1", "item-2", "item-1"},
			expected: map[string]bool{"item-1": true, "item-2": true},
		},
		{
			name:     "WithEmptyStrings",
			input:    []string{"item-1", "", "item-2"},
			expected: map[string]bool{"item-1": true, "": true, "item-2": true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ToStringSet(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestToStringSetFiltered(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected map[string]bool
	}{
		{
			name:     "EmptyList",
			input:    []string{},
			expected: map[string]bool{},
		},
		{
			name:     "SingleItem",
			input:    []string{"item-1"},
			expected: map[string]bool{"item-1": true},
		},
		{
			name:     "MultipleItems",
			input:    []string{"item-1", "item-2", "item-3"},
			expected: map[string]bool{"item-1": true, "item-2": true, "item-3": true},
		},
		{
			name:     "WithEmptyStrings",
			input:    []string{"item-1", "", "item-2", ""},
			expected: map[string]bool{"item-1": true, "item-2": true},
		},
		{
			name:     "OnlyEmptyStrings",
			input:    []string{"", "", ""},
			expected: map[string]bool{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ToStringSetFiltered(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCompareSets(t *testing.T) {
	tests := []struct {
		name     string
		expected map[string]bool
		actual   map[string]bool
		equal    bool
	}{
		{
			name:     "BothEmpty",
			expected: map[string]bool{},
			actual:   map[string]bool{},
			equal:    true,
		},
		{
			name:     "IdenticalSets",
			expected: map[string]bool{"a": true, "b": true},
			actual:   map[string]bool{"a": true, "b": true},
			equal:    true,
		},
		{
			name:     "DifferentOrder",
			expected: map[string]bool{"a": true, "b": true, "c": true},
			actual:   map[string]bool{"c": true, "a": true, "b": true},
			equal:    true,
		},
		{
			name:     "ExpectedEmpty",
			expected: map[string]bool{},
			actual:   map[string]bool{"a": true},
			equal:    false,
		},
		{
			name:     "ActualEmpty",
			expected: map[string]bool{"a": true},
			actual:   map[string]bool{},
			equal:    false,
		},
		{
			name:     "DifferentItems",
			expected: map[string]bool{"a": true, "b": true},
			actual:   map[string]bool{"a": true, "c": true},
			equal:    false,
		},
		{
			name:     "ExtraItemInActual",
			expected: map[string]bool{"a": true},
			actual:   map[string]bool{"a": true, "b": true},
			equal:    false,
		},
		{
			name:     "MissingItemInActual",
			expected: map[string]bool{"a": true, "b": true},
			actual:   map[string]bool{"a": true},
			equal:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CompareSets(tt.expected, tt.actual)
			assert.Equal(t, tt.equal, result)
		})
	}
}
