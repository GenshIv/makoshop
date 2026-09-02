package db

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/GenshIv/makoshop/internal/model"
)

// ImportStatus represents the status of an import job.
type ImportStatus string

const (
	ImportStatusProcessing ImportStatus = "processing"
	ImportStatusCompleted  ImportStatus = "completed"
	ImportStatusFailed     ImportStatus = "failed"
)

// ImportJob represents a product import job.
type ImportJob struct {
	ID            int64        `json:"id"`
	Status        ImportStatus `json:"status"`
	TotalLines    int          `json:"total_lines"`
	ImportedCount int          `json:"imported_count"`
	SkippedCount  int          `json:"skipped_count"`
	Errors        []string     `json:"errors,omitempty"`
	CreatedAt     int64        `json:"created_at"`
	CompletedAt   *int64       `json:"completed_at,omitempty"`
}

type ProductImportRepo struct {
	store       *Store
	productRepo *ProductRepo
}

func NewProductImportRepo(store *Store, productRepo *ProductRepo) *ProductImportRepo {
	return &ProductImportRepo{store: store, productRepo: productRepo}
}

// CreateImportJob creates a new import job and processes the file.
func (r *ProductImportRepo) CreateImportJob(reader io.Reader, companyID int64) (*ImportJob, error) {
	id, err := r.store.NextID("import")
	if err != nil {
		return nil, fmt.Errorf("next_id import: %w", err)
	}

	job := &ImportJob{
		ID:        id,
		Status:    ImportStatusProcessing,
		CreatedAt: time.Now().Unix(),
		Errors:    []string{},
	}

	// Read all data
	data, err := io.ReadAll(reader)
	if err != nil {
		job.Status = ImportStatusFailed
		job.Errors = append(job.Errors, fmt.Sprintf("failed to read file: %v", err))
		r.saveJob(job)
		return job, nil
	}

	// Try JSON first, then CSV-like
	var products []model.Product
	if err := json.Unmarshal(data, &products); err != nil {
		job.Status = ImportStatusFailed
		job.Errors = append(job.Errors, fmt.Sprintf("invalid JSON: %v", err))
		r.saveJob(job)
		return job, nil
	}

	job.TotalLines = len(products)

	for i, p := range products {
		if p.Name == "" || p.EAN == "" {
			job.SkippedCount++
			job.Errors = append(job.Errors, fmt.Sprintf("line %d: skipped (missing name or sku)", i+1))
			continue
		}

		if p.CompanyID == 0 {
			p.CompanyID = companyID
		}

		if p.Currency == "" {
			p.Currency = "RUB"
		}

		if p.Status == "" {
			p.Status = model.ProductStatusDraft
		}

		if err := r.productRepo.Create(&p); err != nil {
			job.SkippedCount++
			job.Errors = append(job.Errors, fmt.Sprintf("line %d: failed to create product %s: %v", i+1, p.EAN, err))
			continue
		}

		job.ImportedCount++
	}

	now := time.Now().Unix()
	job.Status = ImportStatusCompleted
	job.CompletedAt = &now

	r.saveJob(job)
	return job, nil
}

// Get returns an import job by ID.
func (r *ProductImportRepo) Get(id int64) (*ImportJob, error) {
	key := fmt.Sprintf("import:%d", id)
	data, err := r.store.DocGet(key)
	if err != nil {
		return nil, fmt.Errorf("get import job %d: %w", id, err)
	}

	var job ImportJob
	if err := json.Unmarshal(data, &job); err != nil {
		return nil, fmt.Errorf("unmarshal import job %d: %w", id, err)
	}

	return &job, nil
}

func (r *ProductImportRepo) saveJob(job *ImportJob) error {
	key := fmt.Sprintf("import:%d", job.ID)
	data, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("marshal import job: %w", err)
	}
	return r.store.DocPut(key, data)
}
