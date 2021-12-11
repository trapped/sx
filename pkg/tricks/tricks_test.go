package tricks

import (
	"reflect"
	"testing"
)

func TestAllocLessConversions(t *testing.T) {
	s := "test string"
	b := []byte("test string")
	t.Run("convert string -> []byte", func(t *testing.T) {
		if !reflect.DeepEqual(StringToBytes(s), b) {
			t.FailNow()
		}
	})
	t.Run("convert []byte -> string", func(t *testing.T) {
		if BytesToString(b) != s {
			t.FailNow()
		}
	})
}
