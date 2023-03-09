package xio

import (
	"bytes"
	"io"
	"strings"

	"golang.org/x/xerrors"
)

// PrefixSuffixWriter wraps an io.Writer such that the output is limited
// to a certain number of bytes. Flush must be called in order to write
// the suffix to the underlying writer.
type PrefixSuffixWriter struct {
	W io.Writer
	// N is the max size of the prefix or suffix. The total number of bytes
	// retained is N*2.
	N int
	// prefix is the number of bytes written to the prefix.
	prefix int
	// suffix is a circular slice of the last bytes to be written.
	suffix    []byte
	suffixOff int
	skipped   bool
}

// Flush flushes the suffix to the underlying writer.
func (p *PrefixSuffixWriter) Flush() error {
	_, err := io.Copy(p.W, bytes.NewReader(p.suffix[p.suffixOff:]))
	if err != nil {
		return err
	}

	_, err = io.Copy(p.W, bytes.NewReader(p.suffix[:p.suffixOff]))
	return err
}

func (p *PrefixSuffixWriter) Write(b []byte) (int, error) {
	lenb := len(b)

	n, err := p.writePrefix(b)
	if err != nil {
		return n, err
	}
	if n == lenb {
		return lenb, nil
	}

	b = b[n:]

	b = p.fillSuffix(b)
	if len(b) > 0 {
		err = p.writeSkipMessageOnce()
		if err != nil {
			return 0, err
		}
	}

	p.overwriteSuffix(b)

	return lenb, nil
}

func (p *PrefixSuffixWriter) fillSuffix(b []byte) []byte {
	if len(p.suffix) == p.N {
		return b
	}

	if remain := p.N - len(p.suffix); remain > 0 {
		add := minInt(len(b), remain)
		p.suffix = append(p.suffix, b[:add]...)
		b = b[add:]
	}

	return b
}

func (p *PrefixSuffixWriter) overwriteSuffix(b []byte) {
	for len(b) > 0 {
		n := copy(p.suffix[p.suffixOff:], b)
		b = b[n:]
		p.suffixOff += n
		if p.suffixOff == p.N {
			p.suffixOff = 0
		}
	}
}

const skipMessage = "\nTruncating output...\n\n"

func (p *PrefixSuffixWriter) writeSkipMessageOnce() error {
	if p.skipped {
		return nil
	}

	p.skipped = true

	_, err := io.Copy(p.W, strings.NewReader(skipMessage))
	return err
}

func (p *PrefixSuffixWriter) writePrefix(b []byte) (int, error) {
	limit := p.N - p.prefix
	if limit <= 0 {
		return 0, nil
	}

	if limit > len(b) {
		limit = len(b)
	}

	n, err := p.W.Write(b[:limit])
	p.prefix += n
	return n, err
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// LimitWriter will only write bytes to the underlying writer until the limit is reached.
type LimitWriter struct {
	Limit int64
	N     int64
	W     io.Writer
}

func NewLimitWriter(w io.Writer, n int64) *LimitWriter {
	// If anyone tries this, just make a 0 writer.
	if n < 0 {
		n = 0
	}
	return &LimitWriter{
		Limit: n,
		N:     0,
		W:     w,
	}
}

var ErrLimitReached = xerrors.Errorf("writer limit reached")

func (l *LimitWriter) Write(p []byte) (int, error) {
	if l.N >= l.Limit {
		return 0, ErrLimitReached
	}

	if int64(len(p)) > l.Limit-l.N {
		p = p[:l.Limit-l.N]
	}

	n, err := l.W.Write(p)
	l.N += int64(n)
	return n, err
}
