package app

// terminal_smoke_test.go: verifies the mygo terminal plugin renders headless
// against a pipe Conn (the real panel swaps in a backend-PTY bridge; this only
// proves the view paints and Feed shows bytes).

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

type smokeConn struct {
	data chan []byte
}

func (c *smokeConn) Read(p []byte) (int, error) {
	b := <-c.data
	n := copy(p, b)
	return n, nil
}
func (c *smokeConn) Write(p []byte) (int, error) { return len(p), nil }
func (c *smokeConn) Close() error                { return nil }

func TestTerminalPluginSmokeHeadless(t *testing.T) {
	conn := &smokeConn{data: make(chan []byte, 8)}
	term, err := terminal.New(terminal.Options{Conn: conn})
	if err != nil {
		t.Skipf("terminal plugin unavailable: %v", err)
	}
	var once sync.Once
	done := make(chan struct{})
	tt := ui.NewTester(func(c *ui.Context) {
		terminal.View(c, term).Fill()
		once.Do(func() { close(done) })
	}, 480, 240)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("view never built")
	}
	conn.data <- []byte("hello smoke\r\n")
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(term.Text(), "hello smoke") {
			return
		}
		time.Sleep(20 * time.Millisecond)
		tt.Frame()
	}
	t.Fatalf("feed did not show: %q", term.Text())
}
