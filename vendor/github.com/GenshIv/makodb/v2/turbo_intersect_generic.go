package makodb

import "unsafe"

func turboIntersectUint64(a, b []uint64, res []uint64) uint64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	var count uint64
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		if a[i] == b[j] {
			res[count] = a[i]
			count++
			i++
			j++
		} else if a[i] < b[j] {
			i++
		} else {
			j++
		}
	}
	return count
}

func turboUnionUint64(a, b []uint64, res []uint64) uint64 {
	if len(a) == 0 && len(b) == 0 {
		return 0
	}
	if len(a) == 0 {
		copy(res, b)
		return uint64(len(b))
	}
	if len(b) == 0 {
		copy(res, a)
		return uint64(len(a))
	}

	var count uint64
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		if a[i] < b[j] {
			res[count] = a[i]
			count++
			i++
		} else if a[i] > b[j] {
			res[count] = b[j]
			count++
			j++
		} else {
			res[count] = a[i]
			count++
			i++
			j++
		}
	}
	for i < len(a) {
		res[count] = a[i]
		count++
		i++
	}
	for j < len(b) {
		res[count] = b[j]
		count++
		j++
	}
	return count
}

func _extract_docids_uint64(src, count, dst unsafe.Pointer) {
	c := uintptr(count)
	for i := uintptr(0); i < c; i++ {
		// src is a pointer to [Value, DocID] pairs (uint64, uint64)
		// DocID is at offset 8 within the pair
		docIDPtr := (*uint64)(unsafe.Pointer(uintptr(src) + i*16 + 8))
		dstPtr := (*uint64)(unsafe.Pointer(uintptr(dst) + i*8))
		*dstPtr = *docIDPtr
	}
}

func _extract_docids_rev_uint64(src, count, dst unsafe.Pointer) {
	c := uintptr(count)
	for i := uintptr(0); i < c; i++ {
		// (count - 1 - i) * 16 + 8
		idx := c - 1 - i
		docIDPtr := (*uint64)(unsafe.Pointer(uintptr(src) + idx*16 + 8))
		dstPtr := (*uint64)(unsafe.Pointer(uintptr(dst) + i*8))
		*dstPtr = *docIDPtr
	}
}

// key128 versions for numeric sort index (value + key128 docID)
// Entry size: 24 bytes = 8 (value) + 16 (key128 docID)

func _extract_docids_key128(src, count, dst unsafe.Pointer) {
	c := uintptr(count)
	for i := uintptr(0); i < c; i++ {
		// Each entry: 8 bytes value + 16 bytes key128 docID
		docIDPtr := (*key128)(unsafe.Pointer(uintptr(src) + i*24 + 8))
		dstPtr := (*key128)(unsafe.Pointer(uintptr(dst) + i*16))
		*dstPtr = *docIDPtr
	}
}

func _extract_docids_rev_key128(src, count, dst unsafe.Pointer) {
	c := uintptr(count)
	for i := uintptr(0); i < c; i++ {
		// (count - 1 - i) * 24 + 8
		idx := c - 1 - i
		docIDPtr := (*key128)(unsafe.Pointer(uintptr(src) + idx*24 + 8))
		dstPtr := (*key128)(unsafe.Pointer(uintptr(dst) + i*16))
		*dstPtr = *docIDPtr
	}
}

func turboDiffUint64(a, b []uint64, res []uint64) uint64 {
	if len(a) == 0 {
		return 0
	}
	if len(b) == 0 {
		copy(res, a)
		return uint64(len(a))
	}
	var count uint64
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		if a[i] < b[j] {
			res[count] = a[i]
			count++
			i++
		} else if a[i] > b[j] {
			j++
		} else {
			i++
			j++
		}
	}
	for i < len(a) {
		res[count] = a[i]
		count++
		i++
	}
	return count
}

func turboIntersectCountUint64(a, b []uint64) uint64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	var count uint64
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		if a[i] == b[j] {
			count++
			i++
			j++
		} else if a[i] < b[j] {
			i++
		} else {
			j++
		}
	}
	return count
}

// key128 versions for 128-bit keys

func turboIntersectKey128(a, b []key128, res []key128) uint64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	var count uint64
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		cmp := bytesCompareKey128(a[i], b[j])
		if cmp == 0 {
			res[count] = a[i]
			count++
			i++
			j++
		} else if cmp < 0 {
			i++
		} else {
			j++
		}
	}
	return count
}

func turboUnionKey128(a, b []key128, res []key128) uint64 {
	if len(a) == 0 && len(b) == 0 {
		return 0
	}
	if len(a) == 0 {
		copy(res, b)
		return uint64(len(b))
	}
	if len(b) == 0 {
		copy(res, a)
		return uint64(len(a))
	}

	var count uint64
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		cmp := bytesCompareKey128(a[i], b[j])
		if cmp < 0 {
			res[count] = a[i]
			count++
			i++
		} else if cmp > 0 {
			res[count] = b[j]
			count++
			j++
		} else {
			res[count] = a[i]
			count++
			i++
			j++
		}
	}
	for i < len(a) {
		res[count] = a[i]
		count++
		i++
	}
	for j < len(b) {
		res[count] = b[j]
		count++
		j++
	}
	return count
}

func turboDiffKey128(a, b []key128, res []key128) uint64 {
	if len(a) == 0 {
		return 0
	}
	if len(b) == 0 {
		copy(res, a)
		return uint64(len(a))
	}
	var count uint64
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		cmp := bytesCompareKey128(a[i], b[j])
		if cmp < 0 {
			res[count] = a[i]
			count++
			i++
		} else if cmp > 0 {
			j++
		} else {
			i++
			j++
		}
	}
	for i < len(a) {
		res[count] = a[i]
		count++
		i++
	}
	return count
}

func turboIntersectCountKey128(a, b []key128) uint64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	var count uint64
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		cmp := bytesCompareKey128(a[i], b[j])
		if cmp == 0 {
			count++
			i++
			j++
		} else if cmp < 0 {
			i++
		} else {
			j++
		}
	}
	return count
}
