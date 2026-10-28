//go:build !linux

package main

import "syscall"

func becomeSubreaper() bool {
	return false
}

func childAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

func zombieChildren() []int {
	return nil
}

func reap(int) {}
