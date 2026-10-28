package main

import (
	"fmt"
	"slices"
	"sync"
)

type capture struct {
	g       *guard
	mu      sync.Mutex
	buf     []byte
	dropped int
}

func (c *capture) Write(p []byte) (int, error) {
	c.g.progress()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.buf = append(c.buf, p...)
	if len(c.buf) > 2*captureLimit {
		cut := len(c.buf) - captureLimit
		c.dropped += cut
		c.buf = append(c.buf[:0], c.buf[cut:]...)
	}
	return len(p), nil
}

func (c *capture) Bytes() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.buf)
}

func (c *capture) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.dropped > 0 {
		return fmt.Sprintf("[the first %d bytes were dropped]\n", c.dropped) + string(c.buf)
	}
	return string(c.buf)
}
