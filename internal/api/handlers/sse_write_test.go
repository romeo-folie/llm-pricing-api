package handlers

import (
	"bufio"
	"bytes"
	"errors"
	"net"
	"testing"
	"time"
)

// deadlineRecorder is a net.Conn that records write deadlines. The embedded
// net.Conn is nil: only the overridden methods are ever called.
type deadlineRecorder struct {
	net.Conn
	deadline time.Time
	calls    int
}

func (d *deadlineRecorder) SetWriteDeadline(t time.Time) error {
	d.deadline = t
	d.calls++
	return nil
}

// erroringWriter fails every write, standing in for a socket that has gone away.
type erroringWriter struct{}

func (erroringWriter) Write([]byte) (int, error) { return 0, errors.New("connection reset") }

// TestWriteWithDeadlineSetsDeadline is the regression test for the SSE leak:
// without a socket deadline on every write, a peer that vanishes without an
// RST blocks the stream loop forever, leaking its goroutines, Redis
// subscription and per-key connection slot.
func TestWriteWithDeadlineSetsDeadline(t *testing.T) {
	conn := &deadlineRecorder{}
	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)

	if err := writeWithDeadline(conn, w, "hello"); err != nil {
		t.Fatalf("writeWithDeadline: %v", err)
	}

	if conn.calls == 0 {
		t.Fatal("no write deadline was set; a half-open peer could block the stream forever")
	}
	if !conn.deadline.After(time.Now()) {
		t.Errorf("deadline = %v, want a time in the future", conn.deadline)
	}
	if got := buf.String(); got != "hello" {
		t.Errorf("payload = %q, want %q", got, "hello")
	}
}

// TestWriteWithDeadlineWithoutConn covers the nil connection: the payload must
// still be written rather than panicking.
func TestWriteWithDeadlineWithoutConn(t *testing.T) {
	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)

	if err := writeWithDeadline(nil, w, ": heartbeat\n\n"); err != nil {
		t.Fatalf("writeWithDeadline(nil, ...): %v", err)
	}
	if got := buf.String(); got != ": heartbeat\n\n" {
		t.Errorf("payload = %q, want %q", got, ": heartbeat\n\n")
	}
}

// TestWriteWithDeadlinePropagatesWriteError proves the caller can detect a dead
// peer and leave the loop, which is the stream's only exit path.
func TestWriteWithDeadlinePropagatesWriteError(t *testing.T) {
	w := bufio.NewWriter(erroringWriter{})

	if err := writeWithDeadline(nil, w, "data"); err == nil {
		t.Fatal("writeWithDeadline = nil, want the underlying write error")
	}
}
