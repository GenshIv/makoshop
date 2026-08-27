package db

import (
	"testing"
)

func TestTransaction_BasicOperations(t *testing.T) {
	// Create a mock store (for testing only)
	store := &Store{}

	// Create transaction
	txn := NewTransaction(store)

	// Test Begin
	if err := txn.Begin(); err != nil {
		t.Fatalf("Begin() failed: %v", err)
	}

	// Test IsActive
	if !txn.IsActive() {
		t.Error("IsActive() should return true after Begin()")
	}

	// Test DocPut
	if err := txn.DocPut("test_key", []byte("test_value")); err != nil {
		t.Fatalf("DocPut() failed: %v", err)
	}

	// Test TurboWrite
	if err := txn.TurboWrite("test_turbo_key", []byte("test_turbo_value")); err != nil {
		t.Fatalf("TurboWrite() failed: %v", err)
	}

	// Test TurboPutBatchIndexString
	count, err := txn.TurboPutBatchIndexString("test_token", []string{"doc1", "doc2"})
	if err != nil {
		t.Fatalf("TurboPutBatchIndexString() failed: %v", err)
	}
	if count != 2 {
		t.Errorf("TurboPutBatchIndexString() should return 2, got %d", count)
	}

	// Test TurboPutSortIndexString
	if err := txn.TurboPutSortIndexString("test_sort_token", []string{"doc1", "doc2"}); err != nil {
		t.Fatalf("TurboPutSortIndexString() failed: %v", err)
	}

	// Test Len (3 operations: DocPut, TurboWrite, TurboPutBatchIndexString)
	if txn.Len() != 3 {
		t.Errorf("Len() should return 3, got %d", txn.Len())
	}

	// Test Abort
	if err := txn.Abort(); err != nil {
		t.Fatalf("Abort() failed: %v", err)
	}

	// Test IsFinished
	if !txn.IsFinished() {
		t.Error("IsFinished() should return true after Abort()")
	}

	// Test that operations fail after Abort
	if err := txn.DocPut("test_key2", []byte("test_value2")); err == nil {
		t.Error("DocPut() should fail after Abort()")
	}
}

func TestTransaction_MultipleOperations(t *testing.T) {
	// Create a mock store (for testing only)
	store := &Store{}

	// Create transaction
	txn := NewTransaction(store)

	// Test Begin
	if err := txn.Begin(); err != nil {
		t.Fatalf("Begin() failed: %v", err)
	}

	// Test multiple operations on the same key
	if err := txn.DocPut("key1", []byte("value1")); err != nil {
		t.Fatalf("DocPut() failed: %v", err)
	}
	if err := txn.DocPut("key1", []byte("value2")); err != nil {
		t.Fatalf("DocPut() failed: %v", err)
	}

	// Should only have 1 entry for key1
	if txn.Len() != 1 {
		t.Errorf("Len() should return 1, got %d", txn.Len())
	}

	// Test Abort
	if err := txn.Abort(); err != nil {
		t.Fatalf("Abort() failed: %v", err)
	}
}
