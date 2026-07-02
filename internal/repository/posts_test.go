package repository

import (
	"reflect"
	"testing"
)

func TestNormalizePublicationFilters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		input  []PublicationFilter
		expect []PublicationFilter
	}{
		{
			name:   "keeps valid platforms in order",
			input:  []PublicationFilter{{Platform: "facebook", State: PublicationFilterPublished}, {Platform: "tiktok", State: PublicationFilterMissing}},
			expect: []PublicationFilter{{Platform: "facebook", State: PublicationFilterPublished}, {Platform: "tiktok", State: PublicationFilterMissing}},
		},
		{
			name: "trims keeps non-empty and deduplicates",
			input: []PublicationFilter{
				{Platform: " facebook ", State: PublicationFilterPublished},
				{Platform: "", State: PublicationFilterPublished},
				{Platform: "facebook", State: PublicationFilterPublished},
				{Platform: " instagram ", State: PublicationFilterMissing},
				{Platform: "instagram", State: PublicationFilterState("nope")},
			},
			expect: []PublicationFilter{
				{Platform: "facebook", State: PublicationFilterPublished},
				{Platform: "instagram", State: PublicationFilterMissing},
			},
		},
		{
			name:   "returns nil when nothing non-empty remains",
			input:  []PublicationFilter{{Platform: "", State: PublicationFilterPublished}, {Platform: "   ", State: PublicationFilterMissing}},
			expect: nil,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := normalizePublicationFilters(tt.input)
			if !reflect.DeepEqual(got, tt.expect) {
				t.Fatalf("normalizePublicationFilters(%v) = %v, want %v", tt.input, got, tt.expect)
			}
		})
	}
}
