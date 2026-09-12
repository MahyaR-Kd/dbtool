// Package telegram delivers dump archives to a Telegram chat via a bot,
// splitting them into chunks that respect Telegram's per-file size limit.
package telegram

// Range describes one chunk's byte extent within a larger file: bytes
// [Offset, Offset+Length) belong to this chunk.
type Range struct {
	Offset int64
	Length int64
}

// chunkRanges splits a file of the given size into consecutive Ranges of at
// most chunkSize bytes each. It returns a single zero-length Range for a
// zero-size input, and returns nil when chunkSize is not positive.
func chunkRanges(size, chunkSize int64) []Range {
	if chunkSize <= 0 {
		return nil
	}
	if size <= 0 {
		return []Range{{Offset: 0, Length: 0}}
	}

	var ranges []Range
	for offset := int64(0); offset < size; offset += chunkSize {
		length := chunkSize
		if remaining := size - offset; remaining < length {
			length = remaining
		}
		ranges = append(ranges, Range{Offset: offset, Length: length})
	}
	return ranges
}
