package utils

// ToStringSet converts a slice of strings to a map[string]bool for set operations
func ToStringSet(items []string) map[string]bool {
	result := make(map[string]bool)
	for _, item := range items {
		result[item] = true
	}
	return result
}

// ToStringSetFiltered converts a slice of strings to a map[string]bool, excluding empty strings
func ToStringSetFiltered(items []string) map[string]bool {
	result := make(map[string]bool)
	for _, item := range items {
		if item != "" {
			result[item] = true
		}
	}
	return result
}

// CompareSets compares two string sets for equality
func CompareSets(expected, actual map[string]bool) bool {
	if len(expected) != len(actual) {
		return false
	}
	for item := range expected {
		if !actual[item] {
			return false
		}
	}
	return true
}
