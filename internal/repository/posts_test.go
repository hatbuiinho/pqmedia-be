package repository

import (
	"reflect"
	"testing"
)

func TestNormalizePublicationPlatforms(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		input  []string
		expect []string
	}{
		{
			name:   "keeps valid platforms in order",
			input:  []string{"facebook", "tiktok"},
			expect: []string{"facebook", "tiktok"},
		},
		{
			name:   "trims keeps non-empty and deduplicates",
			input:  []string{" facebook ", "", "invalid", "facebook", " instagram "},
			expect: []string{"facebook", "invalid", "instagram"},
		},
		{
			name:   "returns nil when nothing non-empty remains",
			input:  []string{"", "   "},
			expect: nil,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := normalizePublicationPlatforms(tt.input)
			if !reflect.DeepEqual(got, tt.expect) {
				t.Fatalf("normalizePublicationPlatforms(%v) = %v, want %v", tt.input, got, tt.expect)
			}
		})
	}
}
