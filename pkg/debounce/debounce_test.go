package debounce

import (
	"testing"
	"time"
)

func TestDeboucer(t *testing.T) {
	t.Logf("creating debouncer")
	d := New(1 * time.Second)
	n := 0
	t.Logf("queuing first call")
	d.Func(func() { n++ })
	t.Logf("queuing second call")
	d.Func(func() { n++ })
	t.Logf("waiting")
	d.Wait()
	if n != 1 {
		t.Errorf("n value was unexpected: %v", n)
	}
}
