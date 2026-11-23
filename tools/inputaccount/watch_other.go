//go:build !linux

package main

import "errors"

type watcher struct{}

// watch is Linux only: the accounting reads the kernel record of opens
// (inotify), and the fast lane runs on Linux.
func watch([]string) (*watcher, error) {
	return nil, errors.New("inputaccount runs on Linux only (inotify)")
}

func (*watcher) stop() (map[string]bool, error) { return nil, errors.New("unreachable") }
