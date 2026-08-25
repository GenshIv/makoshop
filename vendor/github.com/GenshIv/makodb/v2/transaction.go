package makodb

import (
	"errors"
	"fmt"
	"syscall"
)

// TrackedOffset хранит информацию об отслеживаемом документе
type TrackedOffset struct {
	token  string
	docID  key128
	offset uint64
}

// Transaction представляет транзакцию с выделенным mmap
type Transaction struct {
	id          uint64
	db          *ShardedDB
	mmap        []byte
	mmapSize    uint64
	indexes     map[string]*TransactionIndex
	isCommitted bool
	isAborted   bool

	// Optimistic locking: track offsets from main index
	trackedOffsets []TrackedOffset
}

// TransactionIndex хранит изменения для одного индекса в транзакции
type TransactionIndex struct {
	token  string
	offset uint64             // Базовое смещение в mmap
	size   uint64             // Выделенный размер
	docs   map[key128]docInfo // docID -> информация о документе
}

// docInfo хранит информацию о документе в транзакции
type docInfo struct {
	offset uint64 // Смещение в mmap
	length uint32 // Длина значения
}

// BeginTransaction начинает новую транзакцию
func (s *ShardedDB) BeginTransaction() (*Transaction, error) {
	if s.isClosed {
		return nil, errors.New("makodb: database is closed")
	}

	if s.activeTxn != nil {
		return nil, errors.New("makodb: transaction already active")
	}

	// Выделить mmap для транзакции (50MB по умолчанию)
	txnSize := uint64(50 * 1024 * 1024)
	mmap, err := syscall.Mmap(-1, 0, int(txnSize),
		syscall.PROT_READ|syscall.PROT_WRITE,
		syscall.MAP_PRIVATE|syscall.MAP_ANONYMOUS)
	if err != nil {
		return nil, fmt.Errorf("makodb: failed to allocate transaction memory: %w", err)
	}

	txn := &Transaction{
		id:             s.nextTxnID,
		db:             s,
		mmap:           mmap,
		mmapSize:       txnSize,
		indexes:        make(map[string]*TransactionIndex),
		trackedOffsets: make([]TrackedOffset, 0),
	}

	s.nextTxnID++
	s.activeTxn = txn

	return txn, nil
}

// AllocateIndexSpace выделяет место для индекса в транзакции
func (t *Transaction) AllocateIndexSpace(token string, size uint64) error {
	if t.isCommitted || t.isAborted {
		return errors.New("makodb: transaction already finished")
	}

	if size == 0 {
		size = 5 * 1024 * 1024 // 5MB по умолчанию
	}

	if _, exists := t.indexes[token]; exists {
		return errors.New("makodb: index already allocated in transaction")
	}

	offset, ok := t.findFreeSpace(size)
	if !ok {
		return errors.New("makodb: not enough space in transaction")
	}

	t.indexes[token] = &TransactionIndex{
		token:  token,
		offset: offset,
		size:   size,
		docs:   make(map[key128]docInfo),
	}

	return nil
}

func (t *Transaction) findFreeSpace(size uint64) (uint64, bool) {
	lastOffset := uint64(0)
	for _, idx := range t.indexes {
		if idx.offset+idx.size > lastOffset {
			lastOffset = idx.offset + idx.size
		}
	}

	// Check if we have enough space
	if lastOffset+size > t.mmapSize {
		return 0, false
	}

	// Return the offset where we can start the new allocation
	return lastOffset, true
}

// SetTokenDoc устанавливает документ для токена (добавляет или замещает)
// docID может быть any (string, uint64, key128, []byte)
func (t *Transaction) SetTokenDoc(token string, docID any, value []byte) error {
	if t.isCommitted || t.isAborted {
		return errors.New("makodb: transaction already finished")
	}

	// Convert docID to key128
	docID128 := toKey128(docID)

	idx, exists := t.indexes[token]
	if !exists {
		if err := t.AllocateIndexSpace(token, 0); err != nil {
			return err
		}
		idx = t.indexes[token]
	}

	// Проверить, есть ли уже этот документ
	info, exists := idx.docs[docID128]
	if !exists {
		// Найти свободное место в пределах индекса
		var used uint64
		for _, docInfo := range idx.docs {
			if docInfo.offset-idx.offset+uint64(docInfo.length) > used {
				used = docInfo.offset - idx.offset + uint64(docInfo.length)
			}
		}

		if used+uint64(len(value)) > idx.size {
			// Расширить индекс
			if err := t.expandIndex(token, uint64(len(value))); err != nil {
				return err
			}
			idx = t.indexes[token]
			used = 0
			for _, docInfo := range idx.docs {
				if docInfo.offset-idx.offset+uint64(docInfo.length) > used {
					used = docInfo.offset - idx.offset + uint64(docInfo.length)
				}
			}
		}

		info = docInfo{
			offset: idx.offset + used,
			length: uint32(len(value)),
		}
		idx.docs[docID128] = info
	}

	// Записать значение (замещает предыдущее)
	copy(t.mmap[info.offset:info.offset+uint64(len(value))], value)

	return nil
}

func (t *Transaction) expandIndex(token string, minSize uint64) error {
	idx := t.indexes[token]
	newSize := idx.size * 2
	if newSize < minSize {
		newSize = minSize * 2
	}

	newOffset, ok := t.findFreeSpace(newSize)
	if !ok {
		return errors.New("makodb: not enough space to expand index")
	}

	// Скопировать данные на новое место
	for docID, info := range idx.docs {
		newOff := newOffset + (info.offset - idx.offset)
		idx.docs[docID] = docInfo{
			offset: newOff,
			length: info.length,
		}
	}

	idx.offset = newOffset
	idx.size = newSize

	return nil
}

// RemoveTokenDoc удаляет документ из токена
// docID может быть any (string, uint64, key128, []byte)
func (t *Transaction) RemoveTokenDoc(token string, docID any) error {
	if t.isCommitted || t.isAborted {
		return errors.New("makodb: transaction already finished")
	}

	// Convert docID to key128
	docID128 := toKey128(docID)

	idx, exists := t.indexes[token]
	if !exists {
		return nil // Ничего не удалять
	}

	delete(idx.docs, docID128)

	return nil
}

// Commit применяет изменения к основному индексу
func (t *Transaction) Commit() error {
	if t.isCommitted || t.isAborted {
		return errors.New("makodb: transaction already finished")
	}

	// Check if any tracked offsets have changed
	if err := t.checkOffsets(); err != nil {
		// Offset changed, abort transaction
		_ = t.Abort()
		return fmt.Errorf("makodb: transaction conflict: %w", err)
	}

	for token, idx := range t.indexes {
		// Применить все документы из транзакции
		for docID, info := range idx.docs {
			value := t.mmap[info.offset : info.offset+uint64(info.length)]
			if err := t.db.setTokenDocInMainIndex(token, docID, value); err != nil {
				return err
			}
		}
	}

	syscall.Munmap(t.mmap)

	t.isCommitted = true
	t.db.activeTxn = nil

	return nil
}

// trackOffset запоминает offset документа из основного индекса
func (t *Transaction) trackOffset(token string, docID key128, offset uint64) {
	// Check if already tracked to avoid duplicates
	for _, tracked := range t.trackedOffsets {
		if tracked.token == token && tracked.docID == docID {
			return
		}
	}

	t.trackedOffsets = append(t.trackedOffsets, TrackedOffset{
		token:  token,
		docID:  docID,
		offset: offset,
	})
}

// checkOffsets проверяет, изменились ли offset-ы отслеживаемых документов
func (t *Transaction) checkOffsets() error {
	for _, tracked := range t.trackedOffsets {
		newOffset := t.db.getMainIndexOffset(tracked.token, tracked.docID)
		if newOffset != tracked.offset {
			return fmt.Errorf("offset changed for token=%s docID=%v", tracked.token, tracked.docID)
		}
	}
	return nil
}

// Abort отменяет транзакцию
func (t *Transaction) Abort() error {
	if t.isCommitted || t.isAborted {
		return errors.New("makodb: transaction already finished")
	}

	syscall.Munmap(t.mmap)

	t.isAborted = true
	t.db.activeTxn = nil

	return nil
}

// GetTokenDoc получает документ с учётом транзакции
// docID может быть any (string, uint64, key128, []byte)
func (s *ShardedDB) GetTokenDoc(token string, docID any) ([]byte, error) {
	// Convert docID to key128
	docID128 := toKey128(docID)

	// Сначала проверить транзакцию
	if s.activeTxn != nil {
		if idx, exists := s.activeTxn.indexes[token]; exists {
			if info, exists := idx.docs[docID128]; exists {
				return s.activeTxn.mmap[info.offset : info.offset+uint64(info.length)], nil
			}
		}
	}

	// Затем основной индекс
	value, err := s.getTokenDocFromMainIndex(token, docID128)
	if err != nil {
		return nil, err
	}

	// Если есть активная транзакция, запомнить offset для проверки при коммите
	if s.activeTxn != nil {
		mainOffset := s.getMainIndexOffset(token, docID128)
		s.activeTxn.trackOffset(token, docID128, mainOffset)
	}

	return value, nil
}

// setTokenDocInMainIndex устанавливает документ в основном индексе
func (s *ShardedDB) setTokenDocInMainIndex(token string, docID key128, value []byte) error {
	// Реализация зависит от структуры основного индекса
	// Это заглушка - нужно адаптировать под реальную структуру
	return nil
}

// getTokenDocFromMainIndex получает документ из основного индекса
func (s *ShardedDB) getTokenDocFromMainIndex(token string, docID key128) ([]byte, error) {
	// Реализация зависит от структуры основного индекса
	// Это заглушка - нужно адаптировать под реальную структуру
	return nil, errors.New("makodb: not implemented")
}

// getMainIndexOffset получает offset документа из основного индекса
func (s *ShardedDB) getMainIndexOffset(token string, docID key128) uint64 {
	// Реализация зависит от структуры основного индекса
	// Это заглушка - нужно адаптировать под реальную структуру
	return 0
}
