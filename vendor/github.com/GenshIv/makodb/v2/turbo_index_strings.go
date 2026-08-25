package makodb

// TurboSortPageWithDocsParamsString holds parameters for TurboSortIndexPageWithDocsString.
// Same as TurboSortPageWithDocsParams but accepts string docIDs instead of key128.
type TurboSortPageWithDocsParamsString struct {
	// Name is the sort index name (e.g., "price_asc", "rating_desc").
	Name string
	// Candidates is an optional list of string docIDs to intersect with the sort index.
	Candidates []string
	// Page is the 0-based page number.
	Page int
	// PageSize is the number of documents per page.
	PageSize int
	// Desc reverses the sort order (true = descending).
	Desc bool
	// DocPrefix is ignored. The key128 docIDs should already contain any prefix
	// inside the hash (e.g., hash("turbo_doc:" + "scupage:" + "123")).
	DocPrefix string
}

// TurboPutIndexString adds a string document ID to a turbo index.
// The docID string is internally converted to key128 using hashDocID.
func (s *ShardedDB) TurboPutIndexString(token string, docID string) (bool, error) {
	return s.TurboPutIndex(token, docID)
}

// TurboDeleteIndexString removes a string document ID from a turbo index.
// The docID string is internally converted to key128 using hashDocID.
func (s *ShardedDB) TurboDeleteIndexString(token string, docID string) (bool, error) {
	return s.TurboDeleteIndex(token, docID)
}

// TurboContainsIndexString checks if a string document ID exists in a turbo index.
// The docID string is internally converted to key128 using hashDocID.
func (s *ShardedDB) TurboContainsIndexString(token string, docID string) (bool, error) {
	return s.TurboContainsIndex(token, docID)
}

// TurboPutBatchIndexString adds multiple string document IDs to a turbo index.
// The docIDs are internally converted to key128 using hashDocID.
func (s *ShardedDB) TurboPutBatchIndexString(token string, docIDs []string) (int, error) {
	// Convert []string to []any
	anyDocIDs := make([]any, len(docIDs))
	for i, docID := range docIDs {
		anyDocIDs[i] = docID
	}
	return s.TurboPutBatchIndex(token, anyDocIDs)
}

// TurboDeleteBatchIndexString removes multiple string document IDs from a turbo index.
// The docIDs are internally converted to key128 using hashDocID and sorted.
func (s *ShardedDB) TurboDeleteBatchIndexString(token string, docIDs []string) (int, error) {
	// Convert []string to []any
	anyDocIDs := make([]any, len(docIDs))
	for i, docID := range docIDs {
		anyDocIDs[i] = docID
	}
	return s.TurboDeleteBatchIndex(token, anyDocIDs)
}

// TurboGetIndexTokensFilteredString retrieves tokens from a turbo index with filtering.
// The include and exclude docIDs are internally converted to key128 using hashDocID and sorted.
func (s *ShardedDB) TurboGetIndexTokensFilteredString(token string, include []string, exclude []string, reverse bool, limit, offset int) ([]any, error) {
	// Convert []string to []any
	anyInclude := make([]any, len(include))
	for i, id := range include {
		anyInclude[i] = id
	}
	anyExclude := make([]any, len(exclude))
	for i, id := range exclude {
		anyExclude[i] = id
	}
	return s.TurboGetIndexTokensFiltered(token, anyInclude, anyExclude, reverse, limit, offset)
}

// TurboContainsAllString checks if all string docIDs are present in the given index.
// The docIDs are internally converted to key128 using hashDocID and sorted.
func (s *ShardedDB) TurboContainsAllString(token string, docIDs []string) (bool, error) {
	// Convert []string to []any
	anyDocIDs := make([]any, len(docIDs))
	for i, docID := range docIDs {
		anyDocIDs[i] = docID
	}
	return s.TurboContainsAll(token, anyDocIDs)
}

// TurboContainsAnyString checks if any string docIDs are present in the given index.
// The docIDs are internally converted to key128 using hashDocID and sorted.
func (s *ShardedDB) TurboContainsAnyString(token string, docIDs []string) (bool, error) {
	// Convert []string to []any
	anyDocIDs := make([]any, len(docIDs))
	for i, docID := range docIDs {
		anyDocIDs[i] = docID
	}
	return s.TurboContainsAny(token, anyDocIDs)
}

// TurboAtomicContainsString performs a lock-free check if a string docID exists in a turbo index.
// The docID is internally converted to key128 using hashDocID.
func (s *ShardedDB) TurboAtomicContainsString(token string, docID string) (bool, error) {
	return s.TurboAtomicContains(token, docID)
}

// TurboPutSortIndexString stores a sort index with string document IDs.
// The docIDs are internally converted to key128 using hashDocID.
// The caller must provide docIDs already in the correct sort order.
func (s *ShardedDB) TurboPutSortIndexString(name string, sortedDocIDs []string) error {
	// Convert []string to []any
	anyDocIDs := make([]any, len(sortedDocIDs))
	for i, docID := range sortedDocIDs {
		anyDocIDs[i] = docID
	}
	return s.TurboPutSortIndex(name, anyDocIDs)
}

// TurboSortIndexIntersectWithCandidatesString intersects a sort index with string candidate docIDs.
// The docIDs are internally converted to key128 using hashDocID.
func (s *ShardedDB) TurboSortIndexIntersectWithCandidatesString(name string, candidates []string) ([]any, error) {
	// Convert []string to []any
	anyCandidates := make([]any, len(candidates))
	for i, candidate := range candidates {
		anyCandidates[i] = candidate
	}
	return s.TurboSortIndexIntersectWithCandidatesFromDB(name, anyCandidates)
}

//func (s *ShardedDB) TurboBulkIntersectString(indexTokens []string) ([]key128, error) {
//	return s.turboBulkIntersect(indexTokens)
//}

// TurboSortIndexPageWithDocsString returns paginated results from a sort index with string candidates.
// The docIDs are internally converted to key128 using hashDocID.
func (s *ShardedDB) TurboSortIndexPageWithDocsString(params TurboSortPageWithDocsParamsString) (TurboSortPageWithDocsResult, error) {
	// Convert []string to []any
	var anyCandidates []any
	if params.Candidates != nil {
		anyCandidates = make([]any, len(params.Candidates))
		for i, c := range params.Candidates {
			anyCandidates[i] = c
		}
	}
	return s.TurboSortIndexPageWithDocsFromDB(TurboSortPageWithDocsParams{
		Name:       params.Name,
		Candidates: anyCandidates,
		Page:       params.Page,
		PageSize:   params.PageSize,
		Desc:       params.Desc,
		DocPrefix:  params.DocPrefix,
	})
}

// TurboIntersectSetsString performs AND intersection on multiple string docID sets.
// The docIDs are internally converted to key128 using hashDocID.
// Returns tokens that are present in ALL provided sets.
func TurboIntersectSetsString(sets [][]string) []any {
	if len(sets) == 0 {
		return nil
	}

	// Convert all sets to key128
	key128Sets := make([][]key128, len(sets))
	for i, s := range sets {
		key128Sets[i] = hashDocIDSlice(s)
	}

	return key128ToAnySlice(TurboIntersectSets(key128Sets))
}

// TurboUnionSetsString performs OR union on multiple string docID sets.
// The docIDs are internally converted to key128 using hashDocID.
// Returns all unique tokens from all provided sets.
func TurboUnionSetsString(sets [][]string) []any {
	if len(sets) == 0 {
		return nil
	}

	// Convert all sets to key128
	key128Sets := make([][]key128, len(sets))
	for i, s := range sets {
		key128Sets[i] = hashDocIDSlice(s)
	}

	return key128ToAnySlice(TurboUnionSets(key128Sets))
}
