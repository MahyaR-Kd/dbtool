package db

import (
	"compress/gzip"
	"io"
	"os"
	"strings"

	"github.com/klauspost/compress/zstd"
)

// compressionFormat identifies which (if any) codec a mydumper output file
// is compressed with, detected from its filename. mydumper's --compress
// flag can produce either gzip (.gz) or zstd (.zst) output depending on
// version and configuration — dbtool's own patching and validation logic
// need to read and rewrite whichever one a given dump directory actually
// contains, not assume gzip.
type compressionFormat int

const (
	compressionNone compressionFormat = iota
	compressionGzip
	compressionZstd
)

func detectCompression(path string) compressionFormat {
	switch {
	case strings.HasSuffix(path, ".gz"):
		return compressionGzip
	case strings.HasSuffix(path, ".zst"):
		return compressionZstd
	default:
		return compressionNone
	}
}

// openCompressed opens path for reading, transparently decompressing based
// on its extension (.gz or .zst); any other extension is read as-is. The
// returned ReadCloser's Close releases both the decompressor and the
// underlying file.
func openCompressed(path string) (io.ReadCloser, error) {
	f, err := os.Open(path) // #nosec G304 -- path is built from a directory listing of a dump dir dbtool created itself
	if err != nil {
		return nil, err
	}

	switch detectCompression(path) {
	case compressionGzip:
		gz, err := gzip.NewReader(f)
		if err != nil {
			f.Close()
			return nil, err
		}
		return &decompressingFile{dec: gz, f: f}, nil
	case compressionZstd:
		zr, err := zstd.NewReader(f)
		if err != nil {
			f.Close()
			return nil, err
		}
		return &decompressingFile{dec: zstdDecoder{zr}, f: f}, nil
	default:
		return f, nil
	}
}

// decompressingFile pairs a decompressor with the underlying *os.File it
// reads from, so Close releases both.
type decompressingFile struct {
	dec io.ReadCloser
	f   *os.File
}

func (d *decompressingFile) Read(p []byte) (int, error) { return d.dec.Read(p) }

func (d *decompressingFile) Close() error {
	decErr := d.dec.Close()
	fErr := d.f.Close()
	if decErr != nil {
		return decErr
	}
	return fErr
}

// zstdDecoder adapts *zstd.Decoder — whose Close takes no error — to
// io.ReadCloser.
type zstdDecoder struct {
	*zstd.Decoder
}

func (z zstdDecoder) Close() error {
	z.Decoder.Close()
	return nil
}

// newCompressWriter returns an io.WriteCloser that compresses everything
// written to it into w, in format. Callers must Close it to flush any
// buffered output. Used for streaming a large file through a patch
// without holding it all in memory — see sanitizeSQLModePreamble.
func newCompressWriter(w io.Writer, format compressionFormat) (io.WriteCloser, error) {
	switch format {
	case compressionGzip:
		return gzip.NewWriter(w), nil
	case compressionZstd:
		return zstd.NewWriter(w)
	default:
		return nopWriteCloser{w}, nil
	}
}

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

// writeCompressed writes data to w, compressed to match format — the same
// format the original file was detected as, so rewriting a patched schema
// file preserves whatever compression its dump was written with.
// compressionNone writes data unchanged.
func writeCompressed(w io.Writer, format compressionFormat, data []byte) error {
	cw, err := newCompressWriter(w, format)
	if err != nil {
		return err
	}
	if _, err := cw.Write(data); err != nil {
		cw.Close()
		return err
	}
	return cw.Close()
}
