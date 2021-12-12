package debounce

import (
	"sync"
	"time"
)

// time.AfterFunc doesn't provide C
// so this is a DIY notifying timer
type timer struct {
	mu     sync.Mutex
	t      *time.Timer
	closed bool
	done   chan bool
}

func (t *timer) stop() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.closed {
		close(t.done)
		t.t.Stop()
		t.closed = true
	}
}

type debouncer struct {
	sync.RWMutex
	t *timer
	d time.Duration
}

func New(d time.Duration) *debouncer {
	return &debouncer{d: d}
}

func (d *debouncer) Func(fn func()) {
	d.Lock()
	defer d.Unlock()
	if d.t != nil {
		d.t.stop()
	}
	d.t = &timer{
		done: make(chan bool),
		t: time.AfterFunc(d.d, func() {
			fn()
			d.Lock()
			defer d.Unlock()
			d.t.stop()
		}),
	}
}

func (d *debouncer) Wait() {
	d.RLock()
	if d.t != nil {
		c := d.t.done
		d.RUnlock()
		<-c
	} else {
		d.RUnlock()
	}
}
