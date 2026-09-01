package api

import (
	"testing"
)

func TestImportProgressBatchRun(t *testing.T) {
	p := NewImportProgress()

	// Idle snapshot before any run.
	snap := p.Snapshot()
	if snap.Status != "idle" || snap.Running {
		t.Fatalf("expected idle, got status=%q running=%v", snap.Status, snap.Running)
	}

	// Start a batch of 3 companies.
	p.Begin(3)
	if snap := p.Snapshot(); snap.TotalCompanies != 3 || snap.Status != "running" || !snap.Running {
		t.Fatalf("after Begin: total=%d status=%q running=%v", snap.TotalCompanies, snap.Status, snap.Running)
	}

	// Company 1: download -> parse -> products -> cleanup.
	p.SetCompany(1, "Acme", "nokaut")
	p.SetStep(StepParse)
	p.AddParsed(100)
	p.SetStep(StepProducts)
	p.AddCreated(60)
	p.AddUpdated(40)
	p.SetStep(StepCleanup)
	p.SetDeleted(5)
	p.CompanyDone(1, CompanyStateCompleted, "")

	snap = p.Snapshot()
	if snap.CurrentIndex != 1 || snap.CurrentCompany != "Acme" {
		t.Fatalf("after company1: current_index=%d current_company=%q", snap.CurrentIndex, snap.CurrentCompany)
	}
	// Aggregates are summed across companies.
	if snap.OffersParsed != 100 || snap.ProductsCreated != 60 || snap.ProductsUpdated != 40 || snap.ProductsDeleted != 5 {
		t.Fatalf("aggregates after company1: parsed=%d created=%d updated=%d deleted=%d",
			snap.OffersParsed, snap.ProductsCreated, snap.ProductsUpdated, snap.ProductsDeleted)
	}
	if len(snap.Companies) != 3 {
		t.Fatalf("expected 3 company entries, got %d", len(snap.Companies))
	}
	if snap.Companies[0].Status != CompanyStateCompleted {
		t.Fatalf("company1 status=%q", snap.Companies[0].Status)
	}
	if snap.Companies[1].Status != CompanyStatePending {
		t.Fatalf("company2 status=%q (want pending)", snap.Companies[1].Status)
	}

	// Company 2 fails.
	p.SetCompany(2, "Beta", "json")
	p.AddParsed(10)
	p.CompanyDone(2, CompanyStateFailed, "download_error")

	snap = p.Snapshot()
	if snap.OffersParsed != 110 {
		t.Fatalf("aggregates after company2: parsed=%d (want 110)", snap.OffersParsed)
	}
	if snap.Companies[1].Status != CompanyStateFailed || snap.Companies[1].Error != "download_error" {
		t.Fatalf("company2 status=%q err=%q", snap.Companies[1].Status, snap.Companies[1].Error)
	}

	// Company 3 skipped.
	p.SetCompany(3, "Gamma", "csv")
	p.CompanyDone(3, CompanyStateSkipped, "")

	// Finish the run.
	p.Finish()
	snap = p.Snapshot()
	if snap.Running || snap.Status != "completed" {
		t.Fatalf("after Finish: running=%v status=%q", snap.Running, snap.Status)
	}
	if snap.FinishedAt == 0 {
		t.Fatal("expected FinishedAt to be set")
	}

	// Updates after Finish are no-ops.
	p.AddParsed(999)
	p.SetStep(StepCommit)
	snap = p.Snapshot()
	if snap.OffersParsed != 110 {
		t.Fatalf("post-finish AddParsed should be ignored, parsed=%d", snap.OffersParsed)
	}
}

func TestImportProgressNilSafe(t *testing.T) {
	var p *ImportProgress
	// All methods must be safe on a nil tracker.
	p.Begin(1)
	p.SetCompany(1, "X", "nokaut")
	p.SetStep(StepParse)
	p.AddParsed(1)
	p.AddCreated(1)
	p.AddUpdated(1)
	p.AddSkipped(1)
	p.SetDeleted(1)
	p.CompanyDone(1, CompanyStateCompleted, "")
	p.Fail("boom")
	p.Finish()
	snap := p.Snapshot()
	if snap.Status != "idle" {
		t.Fatalf("nil tracker should report idle, got %q", snap.Status)
	}
}
