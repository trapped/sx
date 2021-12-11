// package tricks provides Go tricks of many kinds.
package tricks

import (
	"reflect"
	"unsafe"
)

// StringToBytes converts a string to []byte with 0 memory allocations.
func StringToBytes(s string) (b []byte) {
	bh := (*reflect.SliceHeader)(unsafe.Pointer(&b))
	sh := (*reflect.StringHeader)(unsafe.Pointer(&s))
	bh.Data = sh.Data
	bh.Cap = sh.Len
	bh.Len = sh.Len
	return b
}

// BytesToString converts a []byte to string with 0 memory allocations.
func BytesToString(b []byte) string {
	return *(*string)(unsafe.Pointer(&b))
}
