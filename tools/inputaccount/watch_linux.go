//go:build linux

package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// watcher records every file opened in a set of directories, by anyone,
// from watch to stop: inotify IN_OPEN, which the kernel queues at the open
// itself, so the queue is complete once the leg has exited.
type watcher struct {
	fd       int
	dirs     map[int32]string
	mu       sync.Mutex
	opened   map[string]bool
	overflow bool
	err      error
	done     chan struct{}
	quit     chan struct{}
}

// watch starts recording opens in dirs (slash paths relative to the
// working directory; the recorded paths are dir/name in the same form).
func watch(dirs []string) (*watcher, error) {
	fd, err := syscall.InotifyInit1(syscall.IN_CLOEXEC | syscall.IN_NONBLOCK)
	if err != nil {
		return nil, fmt.Errorf("inotify: %w", err)
	}
	w := &watcher{fd: fd, dirs: map[int32]string{}, opened: map[string]bool{}, done: make(chan struct{}), quit: make(chan struct{})}
	for _, d := range dirs {
		wd, err := syscall.InotifyAddWatch(fd, filepath.FromSlash(d), syscall.IN_OPEN)
		if err != nil {
			_ = syscall.Close(fd)
			return nil, fmt.Errorf("inotify watch %s: %w", d, err)
		}
		w.dirs[int32(wd)] = d
	}
	go w.loop()
	return w, nil
}

func (w *watcher) loop() {
	defer close(w.done)
	buf := make([]byte, 64*1024)
	for {
		n, err := syscall.Read(w.fd, buf)
		if errors.Is(err, syscall.EAGAIN) {
			select {
			case <-w.quit:
				return // the queue is drained and the leg has exited
			default:
				time.Sleep(2 * time.Millisecond)
				continue
			}
		}
		if err != nil {
			w.mu.Lock()
			w.err = fmt.Errorf("inotify read: %w", err)
			w.mu.Unlock()
			return
		}
		w.parse(buf[:n])
	}
}

func (w *watcher) parse(b []byte) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for len(b) >= syscall.SizeofInotifyEvent {
		ev := (*syscall.InotifyEvent)(unsafe.Pointer(&b[0]))
		end := syscall.SizeofInotifyEvent + int(ev.Len)
		if end > len(b) {
			w.err = errors.New("inotify: a truncated event")
			return
		}
		if ev.Mask&syscall.IN_Q_OVERFLOW != 0 {
			w.overflow = true
		}
		name := strings.TrimRight(string(b[syscall.SizeofInotifyEvent:end]), "\x00")
		if d, ok := w.dirs[ev.Wd]; ok && name != "" {
			w.opened[d+"/"+name] = true
		}
		b = b[end:]
	}
}

// stop drains the queue and returns every file opened.
func (w *watcher) stop() (map[string]bool, error) {
	close(w.quit)
	<-w.done
	_ = syscall.Close(w.fd)
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.err != nil {
		return nil, w.err
	}
	if w.overflow {
		return nil, errors.New("inotify: the event queue overflowed, so some opens were lost: the accounting cannot say what was read")
	}
	return w.opened, nil
}
