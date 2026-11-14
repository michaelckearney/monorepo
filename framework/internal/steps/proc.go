package steps

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// outMu serialises writes to the terminal, so lines from Vite, the app, and
// desktop itself never interleave mid-line.
var outMu sync.Mutex

// PrefixWriter puts a label on every line a child process writes. Without one
// a Vite warning and a Go panic look the same, and an agent reading the output
// cannot tell which half of the app to fix.
type PrefixWriter struct {
	prefix string
	out    io.Writer
	buf    []byte
	drop   [][]byte
}

// NewPrefixWriter labels every line with prefix, and drops any line that
// contains one of drop.
func NewPrefixWriter(prefix string, out io.Writer, drop ...string) *PrefixWriter {
	w := &PrefixWriter{prefix: prefix, out: out}
	for _, d := range drop {
		w.drop = append(w.drop, []byte(d))
	}
	return w
}

func (w *PrefixWriter) dropped(line []byte) bool {
	for _, d := range w.drop {
		if bytes.Contains(line, d) {
			return true
		}
	}
	return false
}

func (w *PrefixWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		if line := w.buf[:i+1]; !w.dropped(line) {
			outMu.Lock()
			_, _ = w.out.Write(append([]byte(w.prefix), line...))
			outMu.Unlock()
		}
		w.buf = w.buf[i+1:]
	}
	return len(p), nil
}

// Proc is a long-running child: the Vite dev server, the app, or the app's
// test server.
type Proc struct {
	cmd  *exec.Cmd
	Done chan struct{}
	Err  error
}

// Start runs the command in its own process group. Vite starts esbuild
// workers of its own, and killing only the node process left them holding the
// port, so the next run failed with "port already in use".
func Start(cmd *exec.Cmd, label string, drop ...string) (*Proc, error) {
	cmd.Stdout = NewPrefixWriter(label, os.Stdout, drop...)
	cmd.Stderr = NewPrefixWriter(label, os.Stderr, drop...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	p := &Proc{cmd: cmd, Done: make(chan struct{})}
	go func() {
		p.Err = cmd.Wait()
		close(p.Done)
	}()
	return p, nil
}

// Gone is a stand-in for a process that has already exited and been
// reported, so a select on its Done channel never fires again.
func Gone() *Proc {
	return &Proc{Done: make(chan struct{})}
}

func (p *Proc) Exited() bool {
	select {
	case <-p.Done:
		return true
	default:
		return false
	}
}

// Stop asks the whole process group to exit, and kills it if it has not
// within three seconds.
func (p *Proc) Stop() {
	if p == nil || p.cmd == nil || p.Exited() {
		return
	}
	pgid := p.cmd.Process.Pid
	_ = syscall.Kill(-pgid, syscall.SIGTERM)
	select {
	case <-p.Done:
	case <-time.After(3 * time.Second):
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		<-p.Done
	}
}
