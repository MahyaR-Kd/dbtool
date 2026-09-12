package telegram

import "testing"

func TestChunkRanges(t *testing.T) {
	tests := []struct {
		name      string
		size      int64
		chunkSize int64
		want      []Range
	}{
		{
			name:      "exact multiple",
			size:      300,
			chunkSize: 100,
			want: []Range{
				{Offset: 0, Length: 100},
				{Offset: 100, Length: 100},
				{Offset: 200, Length: 100},
			},
		},
		{
			name:      "remainder in final chunk",
			size:      250,
			chunkSize: 100,
			want: []Range{
				{Offset: 0, Length: 100},
				{Offset: 100, Length: 100},
				{Offset: 200, Length: 50},
			},
		},
		{
			name:      "size smaller than chunk",
			size:      10,
			chunkSize: 100,
			want: []Range{
				{Offset: 0, Length: 10},
			},
		},
		{
			name:      "size equal to chunk",
			size:      100,
			chunkSize: 100,
			want: []Range{
				{Offset: 0, Length: 100},
			},
		},
		{
			name:      "zero size",
			size:      0,
			chunkSize: 100,
			want:      []Range{{Offset: 0, Length: 0}},
		},
		{
			name:      "zero chunk size",
			size:      100,
			chunkSize: 0,
			want:      nil,
		},
		{
			name:      "negative chunk size",
			size:      100,
			chunkSize: -1,
			want:      nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := chunkRanges(tc.size, tc.chunkSize)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d ranges, want %d: %+v", len(got), len(tc.want), got)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("range[%d] = %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}
