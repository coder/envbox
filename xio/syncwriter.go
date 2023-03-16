package xio

import (
	"io"
	"sync"
)

// SyncWriter synchronizes concurrent writes to an underlying writer.
type SyncWriter struct {
	mu sync.Mutex
	W  io.Writer
}

func (sw *SyncWriter) Write(b []byte) (int, error) {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	return sw.W.Write(b)
}
