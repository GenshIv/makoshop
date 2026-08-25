package makodb

import (
	"reflect"
	"sync"

	"github.com/GenshIv/silentjson/v2"
)

var (
	registryCache = make(map[reflect.Type]*silentjson.Registry)
	cacheMutex    sync.RWMutex
)

// getRegistry returns a cached silentjson.Registry for the given type, compiling it if necessary.
func getRegistry(typ reflect.Type) *silentjson.Registry {
	cacheMutex.RLock()
	reg, ok := registryCache[typ]
	cacheMutex.RUnlock()
	if ok {
		return reg
	}

	cacheMutex.Lock()
	defer cacheMutex.Unlock()
	// Recheck
	reg, ok = registryCache[typ]
	if !ok {
		reg = silentjson.BuildRegistry(typ)
		registryCache[typ] = reg
	}
	return reg
}

// Query parses a document stored at the given key into the target structure.
// target must be a pointer to a struct.
// Allocates a byte slice for the value. For zero-allocation, use QueryZeroAlloc.
func (db *DB) Query(key string, target interface{}) error {
	k := hashKey(key)
	val, err := db.getKey128(k)
	if err != nil {
		return err
	}

	valVal := reflect.ValueOf(target)
	if valVal.Kind() != reflect.Ptr || valVal.Elem().Kind() != reflect.Struct {
		panic("makodb: target must be a pointer to a struct")
	}

	typ := valVal.Elem().Type()
	reg := getRegistry(typ)

	ptr := valVal.UnsafePointer()
	return silentjson.ParseObject(val, reg, ptr)
}

// QueryZeroAlloc parses a document stored at the given key into the target structure
// without allocating memory for the value.
// Uses a direct view into the memory-mapped file.
//
// WARNING: The parsed data may become invalid if the underlying key is updated,
// deleted, or the database is resized. silentjson reads the data immediately,
// so this is safe as long as target is populated before any write operation.
func (db *DB) QueryZeroAlloc(key string, target interface{}) error {
	k := hashKey(key)
	val, err := db.getKey128ZeroAlloc(k)
	if err != nil {
		return err
	}

	valVal := reflect.ValueOf(target)
	if valVal.Kind() != reflect.Ptr || valVal.Elem().Kind() != reflect.Struct {
		panic("makodb: target must be a pointer to a struct")
	}

	typ := valVal.Elem().Type()
	reg := getRegistry(typ)

	ptr := valVal.UnsafePointer()
	return silentjson.ParseObject(val, reg, ptr)
}
