package aggregator

import "unsafe"

// serializeIDs преобразует []int32 в []byte без копирования (zero-copy)
func serializeIDs(ids []int32) []byte {
	if len(ids) == 0 {
		return nil
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(&ids[0])), len(ids)*4)
}

// deserializeIDs преобразует []byte обратно в []int32 без копирования (zero-copy)
func deserializeIDs(b []byte) []int32 {
	if len(b) == 0 {
		return nil
	}
	return unsafe.Slice((*int32)(unsafe.Pointer(&b[0])), len(b)/4)
}

// serializeInt64s преобразует []int64 в []byte без копирования (zero-copy)
func serializeInt64s(ids []int64) []byte {
	if len(ids) == 0 {
		return nil
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(&ids[0])), len(ids)*8)
}

// deserializeInt64s преобразует []byte обратно в []int64 без копирования (zero-copy)
func deserializeInt64s(b []byte) []int64 {
	if len(b) == 0 {
		return nil
	}
	return unsafe.Slice((*int64)(unsafe.Pointer(&b[0])), len(b)/8)
}
