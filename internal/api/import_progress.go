package api

import (
	"net/http"
	"sync"
	"time"

	"github.com/GenshIv/makoshop/internal/httpres"
)

// Import step identifiers. Each company's price import runs through a fixed
// sequence of steps; the admin UI shows the active step live while a batch
// "update all prices" import is running.
const (
	StepIdle        = "idle"
	StepDownload    = "download"
	StepParse       = "parse"
	StepAttrDefs    = "attr_defs"
	StepProducts    = "products"
	StepCleanup     = "cleanup"
	StepIndex       = "index"
	StepEANPages    = "ean_pages"
	StepRecalc      = "recalc"
	StepSortIndexes = "sort_indexes"
	StepTrees       = "category_trees"
	StepCommit      = "commit"
	StepPostCommit  = "post_commit"
)

// Company run states.
const (
	CompanyStatePending   = "pending"
	CompanyStateRunning   = "running"
	CompanyStateCompleted = "completed"
	CompanyStateFailed    = "failed"
	CompanyStateSkipped   = "skipped"
)

// CompanyProgress tracks the live progress of a single company inside a batch
// price import.
type CompanyProgress struct {
	Index           int    `json:"index"` // 1-based position in the batch
	Name            string `json:"name"`
	Format          string `json:"format"`
	Step            string `json:"step"`
	OffersParsed    int    `json:"offers_parsed"`
	ProductsCreated int    `json:"products_created"`
	ProductsUpdated int    `json:"products_updated"`
	ProductsSkipped int    `json:"products_skipped"`
	ProductsDeleted int    `json:"products_deleted"`
	Status          string `json:"status"`
	Error           string `json:"error,omitempty"`
}

// ImportProgress is a thread-safe, in-memory tracker for a batch price import.
// The import goroutine updates it as work proceeds; the /admin/import-progress
// endpoint reads a Snapshot so the admin UI can render live status (which
// company N/total, its name, the current step, and processed/updated/added/
// deleted counters).
//
// The pointer is created once in NewHandlers and never reassigned, so reading
// the pointer itself is safe; all field access goes through the mutex.
type ImportProgress struct {
	mu sync.Mutex

	Running        bool
	Status         string // idle|running|completed|failed
	StartedAt      time.Time
	FinishedAt     time.Time
	TotalCompanies int
	CurrentIndex   int // 1-based index of the company currently being processed (0 = none)
	Companies      []CompanyProgress
}

// NewImportProgress creates a tracker in the idle state.
func NewImportProgress() *ImportProgress {
	return &ImportProgress{Status: "idle"}
}

// ImportProgressSnapshot is the JSON-serializable view returned by the status
// endpoint. Aggregates are computed from the per-company counters so they are
// always consistent.
type ImportProgressSnapshot struct {
	Running         bool              `json:"running"`
	Status          string            `json:"status"`
	StartedAt       int64             `json:"started_at,omitempty"`
	FinishedAt      int64             `json:"finished_at,omitempty"`
	TotalCompanies  int               `json:"total_companies"`
	CurrentIndex    int               `json:"current_index"`
	CurrentCompany  string            `json:"current_company,omitempty"`
	CurrentFormat   string            `json:"current_format,omitempty"`
	Step            string            `json:"step,omitempty"`
	OffersParsed    int               `json:"offers_parsed"`
	ProductsCreated int               `json:"products_created"`
	ProductsUpdated int               `json:"products_updated"`
	ProductsSkipped int               `json:"products_skipped"`
	ProductsDeleted int               `json:"products_deleted"`
	Companies       []CompanyProgress `json:"companies"`
}

// Begin starts a new batch run covering totalCompanies companies, resetting
// all counters and per-company state.
func (p *ImportProgress) Begin(totalCompanies int) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Running = true
	p.Status = "running"
	p.StartedAt = time.Now()
	p.FinishedAt = time.Time{}
	p.TotalCompanies = totalCompanies
	p.CurrentIndex = 0
	companies := make([]CompanyProgress, totalCompanies)
	for i := range companies {
		companies[i] = CompanyProgress{Index: i + 1, Status: CompanyStatePending}
	}
	p.Companies = companies
}

// current returns the company entry for CurrentIndex. Caller must hold p.mu.
func (p *ImportProgress) current() *CompanyProgress {
	if p.CurrentIndex < 1 || p.CurrentIndex > len(p.Companies) {
		return nil
	}
	return &p.Companies[p.CurrentIndex-1]
}

// SetCompany marks the company at 1-based index idx as the active one and
// resets its step to the first (download) step.
func (p *ImportProgress) SetCompany(idx int, name, format string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.Running {
		return
	}
	p.CurrentIndex = idx
	c := p.current()
	if c == nil {
		return
	}
	c.Name = name
	c.Format = format
	c.Status = CompanyStateRunning
	c.Step = StepDownload
	c.Error = ""
}

// SetStep updates the current step of the active company.
func (p *ImportProgress) SetStep(step string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.Running {
		return
	}
	if c := p.current(); c != nil {
		c.Step = step
	}
}

// AddParsed adds n to the active company's parsed (processed) counter.
func (p *ImportProgress) AddParsed(n int) { p.addCounters(n, 0, 0, 0) }

// AddCreated adds n to the active company's created (added) counter.
func (p *ImportProgress) AddCreated(n int) { p.addCounters(0, n, 0, 0) }

// AddUpdated adds n to the active company's updated counter.
func (p *ImportProgress) AddUpdated(n int) { p.addCounters(0, 0, n, 0) }

// AddSkipped adds n to the active company's skipped counter.
func (p *ImportProgress) AddSkipped(n int) { p.addCounters(0, 0, 0, n) }

// addCounters increments the active company's counters.
func (p *ImportProgress) addCounters(parsed, created, updated, skipped int) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.Running {
		return
	}
	if c := p.current(); c != nil {
		c.OffersParsed += parsed
		c.ProductsCreated += created
		c.ProductsUpdated += updated
		c.ProductsSkipped += skipped
	}
}

// SetDeleted sets the active company's deleted counter. The deleted count is
// only known once the stale-product cleanup step runs, so it is assigned (not
// incremented).
func (p *ImportProgress) SetDeleted(n int) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.Running {
		return
	}
	if c := p.current(); c != nil {
		c.ProductsDeleted = n
	}
}

// CompanyDone finalizes the company at 1-based index idx with a terminal
// status and optional error message.
func (p *ImportProgress) CompanyDone(idx int, status, errMsg string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if idx < 1 || idx > len(p.Companies) {
		return
	}
	c := &p.Companies[idx-1]
	c.Status = status
	if errMsg != "" {
		c.Error = errMsg
	}
}

// Fail marks the whole run as failed (e.g. an early error before any company).
func (p *ImportProgress) Fail(errMsg string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.Running {
		return
	}
	p.Running = false
	p.Status = "failed"
	p.FinishedAt = time.Now()
	if errMsg != "" {
		if c := p.current(); c != nil && c.Status == CompanyStateRunning {
			c.Status = CompanyStateFailed
			c.Error = errMsg
		}
	}
}

// Finish marks the whole run as completed (unless it was already failed).
func (p *ImportProgress) Finish() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.Running {
		return
	}
	p.Running = false
	if p.Status != "failed" {
		p.Status = "completed"
	}
	p.FinishedAt = time.Now()
}

// Snapshot returns a consistent, JSON-serializable view of the current state,
// including aggregate counters summed across all companies.
func (p *ImportProgress) Snapshot() ImportProgressSnapshot {
	if p == nil {
		return ImportProgressSnapshot{Status: "idle"}
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	snap := ImportProgressSnapshot{
		Running:        p.Running,
		Status:         p.Status,
		TotalCompanies: p.TotalCompanies,
		CurrentIndex:   p.CurrentIndex,
		Companies:      make([]CompanyProgress, len(p.Companies)),
	}
	if !p.StartedAt.IsZero() {
		snap.StartedAt = p.StartedAt.Unix()
	}
	if !p.FinishedAt.IsZero() {
		snap.FinishedAt = p.FinishedAt.Unix()
	}

	for i := range p.Companies {
		snap.Companies[i] = p.Companies[i]
		snap.OffersParsed += p.Companies[i].OffersParsed
		snap.ProductsCreated += p.Companies[i].ProductsCreated
		snap.ProductsUpdated += p.Companies[i].ProductsUpdated
		snap.ProductsSkipped += p.Companies[i].ProductsSkipped
		snap.ProductsDeleted += p.Companies[i].ProductsDeleted
	}

	if cur := p.current(); cur != nil {
		snap.CurrentCompany = cur.Name
		snap.CurrentFormat = cur.Format
		snap.Step = cur.Step
	}
	return snap
}

// HandleAdminImportProgress returns the live progress of the current (or most
// recent) batch price import. The admin UI polls this endpoint while an
// "update all prices" run is in flight.
// GET /admin/import-progress
func (h *Handlers) HandleAdminImportProgress(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}
	httpres.WriteJSON(w, http.StatusOK, h.importProgress.Snapshot())
}
