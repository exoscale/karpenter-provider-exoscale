package instance

import (
	"slices"
	"testing"
)

func TestComputeExpectedSecurityGroups(t *testing.T) {
	nodeClassSGs := []string{"sg-a", "sg-b"}
	sksDefault := "sg-sks-default"

	tests := []struct {
		name            string
		nodeClassSGs    []string
		sksDefaultFound bool
		sksDefaultSGID  string
		want            []string
	}{
		{
			name:         "SKS default not found: returns nodeClass SGs as-is",
			nodeClassSGs: nodeClassSGs,
			want:         nodeClassSGs,
		},
		{
			name:            "SKS default found with empty ID: returns nodeClass SGs as-is",
			nodeClassSGs:    nodeClassSGs,
			sksDefaultFound: true,
			want:            nodeClassSGs,
		},
		{
			name:            "SKS default found and not in nodeClass: appended",
			nodeClassSGs:    nodeClassSGs,
			sksDefaultFound: true,
			sksDefaultSGID:  sksDefault,
			want:            []string{"sg-a", "sg-b", sksDefault},
		},
		{
			name:            "SKS default already in nodeClass: no duplicate",
			nodeClassSGs:    []string{"sg-a", sksDefault},
			sksDefaultFound: true,
			sksDefaultSGID:  sksDefault,
			want:            []string{"sg-a", sksDefault},
		},
		{
			name:            "empty nodeClass SGs with SKS default: only SKS default",
			sksDefaultFound: true,
			sksDefaultSGID:  sksDefault,
			want:            []string{sksDefault},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := slices.Clone(tt.nodeClassSGs)
			got := computeExpectedSecurityGroups(tt.nodeClassSGs, tt.sksDefaultFound, tt.sksDefaultSGID)
			if !slices.Equal(got, tt.want) {
				t.Fatalf("computeExpectedSecurityGroups() = %v, want %v", got, tt.want)
			}
			if !slices.Equal(tt.nodeClassSGs, original) {
				t.Errorf("input slice was mutated: %v -> %v", original, tt.nodeClassSGs)
			}
		})
	}
}
