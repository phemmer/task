package task

import (
	"sync"
	"sync/atomic"
)

type atomicValue[T any] atomic.Value

func (av *atomicValue[T]) Store(v T) {
	(*atomic.Value)(av).Store(v)
}
func (av *atomicValue[T]) Load() T {
	var v T
	vi := (*atomic.Value)(av).Load()
	if vi != nil {
		v = vi.(T)
	}
	return v
}
func (av *atomicValue[T]) CompareAndSwap(old, new T) bool {
	return (*atomic.Value)(av).CompareAndSwap(old, new)
}

// syncMap is a generic wrapper around sync.Map to provided typed keys & values.
type syncMap[K comparable, V any] sync.Map

func (sm *syncMap[K, V]) Load(k K) (V, bool) {
	var v V
	vAny, ok := (*sync.Map)(sm).Load(k)
	if ok {
		v = vAny.(V)
	}
	return v, ok
}

// LoadDefault retrieves the value indexed by the given key. If the value does not exist, the result of the provided
// function is stored and returned.
// Note that in a race condition it may be possible for the function to execute and a different value to be returned,
// but all callers will receive the same value.
func (sm *syncMap[K, V]) LoadDefault(k K, f func() V) V {
	v, ok := sm.Load(k)
	if ok {
		return v
	}
	vNew := f()
	vAny, _ := (*sync.Map)(sm).LoadOrStore(k, vNew)
	return vAny.(V)
}

func (sm *syncMap[K, V]) Store(k K, v V) {
	(*sync.Map)(sm).Store(k, v)
}

// Range iterates over the map providing the key & value to the given function. Iteration stops if the function returns
// false.
func (sm *syncMap[K, V]) Range(f func(k K, v V) bool) {
	(*sync.Map)(sm).Range(func(kAny any, vAny any) bool {
		return f(kAny.(K), vAny.(V))
	})
}

func (sm *syncMap[K, V]) Delete(k K) (V, bool) {
	vAny, ok := (*sync.Map)(sm).LoadAndDelete(k)
	var v V
	if ok {
		v = vAny.(V)
	}
	return v, ok
}

func (sm *syncMap[K, V]) IsEmpty() bool {
	empty := true
	sm.Range(func(_ K, _ V) bool {
		empty = false
		return false
	})
	return empty
}
