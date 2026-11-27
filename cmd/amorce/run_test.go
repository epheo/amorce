package main

import (
	"errors"
	"testing"
	"time"
)

// A terminal that is not there is logged once and retried ever slower; a
// new error is logged, and a good run starts over.
func TestRetrier(t *testing.T) {
	var r retrier
	gone := errors.New("open /dev/tty1: no such file or directory")
	var logged int
	var wait time.Duration
	for range 10 {
		log, w := r.next(gone)
		if log {
			logged++
		}
		wait = w
	}
	if logged != 1 || wait != 30*time.Second {
		t.Fatalf("logged %d times, waits %v", logged, wait)
	}
	if log, w := r.next(errors.New("another")); !log || w != time.Second {
		t.Fatalf("a new error: %v %v", log, w)
	}
	if log, w := r.next(nil); log || w != time.Second {
		t.Fatalf("a good run: %v %v", log, w)
	}
	if log, _ := r.next(gone); !log {
		t.Fatal("the error after a good run was not logged")
	}
}
