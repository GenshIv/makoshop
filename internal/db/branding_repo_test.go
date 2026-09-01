package db

import (
	"path/filepath"
	"testing"

	"github.com/GenshIv/makoshop/internal/model"
	"github.com/GenshIv/makoshop/pkg/config"
)

func newTestBrandingStore(t *testing.T) *Store {
	t.Helper()
	tmpDir := t.TempDir()
	cfg := config.DatabaseConfig{
		Path:               filepath.Join(tmpDir, "test_db"),
		NumShards:          4,
		MaxTotalSize:       100 * 1024 * 1024,
		NumBucketsPerShard: 100_000,
	}
	store, err := NewStore(cfg)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestValidateBrandSet(t *testing.T) {
	cases := []struct {
		name    string
		set     model.BrandSet
		wantErr bool
	}{
		{
			name: "valid empty set",
			set:  model.BrandSet{Name: "New Year 2025"},
		},
		{
			name: "valid with element",
			set: model.BrandSet{
				Name: "Sale",
				Elements: []model.BrandElement{{
					Slot:     model.BrandSlotHeaderFullwidth,
					ImageURL: "/uploads/branding/a.png",
				}},
			},
		},
		{
			name:    "empty name",
			set:     model.BrandSet{Name: "  "},
			wantErr: true,
		},
		{
			name: "invalid slot",
			set: model.BrandSet{
				Name:     "X",
				Elements: []model.BrandElement{{Slot: "nope", ImageURL: "/x.png"}},
			},
			wantErr: true,
		},
		{
			name: "missing image",
			set: model.BrandSet{
				Name:     "X",
				Elements: []model.BrandElement{{Slot: model.BrandSlotHomeBanner}},
			},
			wantErr: true,
		},
		{
			name: "duplicate slot",
			set: model.BrandSet{
				Name: "X",
				Elements: []model.BrandElement{
					{Slot: model.BrandSlotHomeBanner, ImageURL: "/a.png"},
					{Slot: model.BrandSlotHomeBanner, ImageURL: "/b.png"},
				},
			},
			wantErr: true,
		},
		{
			name: "too many patterns",
			set: model.BrandSet{
				Name: "X",
				Elements: []model.BrandElement{{
					Slot:         model.BrandSlotHomeBanner,
					ImageURL:     "/a.png",
					PagePatterns: make([]string, 11),
				}},
			},
			wantErr: true,
		},
		{
			name: "empty pattern",
			set: model.BrandSet{
				Name: "X",
				Elements: []model.BrandElement{{
					Slot:         model.BrandSlotHomeBanner,
					ImageURL:     "/a.png",
					PagePatterns: []string{"^/$", "  "},
				}},
			},
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := model.ValidateBrandSet(&tc.set)
			if (err != nil) != tc.wantErr {
				t.Errorf("ValidateBrandSet() error = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}

func TestBrandingRepo_SetsCRUD(t *testing.T) {
	store := newTestBrandingStore(t)
	repo := NewBrandingRepo(store)

	// Create
	s := &model.BrandSet{
		Name:     "Summer Promo",
		Priority: 10,
		Elements: []model.BrandElement{{
			Slot:         model.BrandSlotHeaderFullwidth,
			ImageURL:     "/uploads/branding/summer.png",
			PagePatterns: []string{"^/shop/telefony"},
		}},
	}
	if err := repo.CreateSet(s); err != nil {
		t.Fatalf("CreateSet: %v", err)
	}
	if s.ID == 0 {
		t.Fatal("CreateSet did not assign ID")
	}

	// Get
	got, err := repo.GetSet(s.ID)
	if err != nil {
		t.Fatalf("GetSet: %v", err)
	}
	if got.Name != "Summer Promo" || len(got.Elements) != 1 {
		t.Errorf("GetSet mismatch: %+v", got)
	}

	// List
	sets, err := repo.ListSets()
	if err != nil {
		t.Fatalf("ListSets: %v", err)
	}
	if len(sets) != 1 {
		t.Fatalf("ListSets = %d, want 1", len(sets))
	}

	// Update (toggle enabled)
	if err := repo.UpdateSet(s.ID, func(x *model.BrandSet) { x.Enabled = true }); err != nil {
		t.Fatalf("UpdateSet: %v", err)
	}
	got, _ = repo.GetSet(s.ID)
	if !got.Enabled {
		t.Error("UpdateSet did not enable the set")
	}

	// Update with invalid data must fail and keep the old state
	if err := repo.UpdateSet(s.ID, func(x *model.BrandSet) { x.Name = "" }); err == nil {
		t.Fatal("UpdateSet with empty name should fail")
	}
	got, _ = repo.GetSet(s.ID)
	if got.Name != "Summer Promo" {
		t.Error("failed update must not change the set")
	}

	// Delete
	if err := repo.DeleteSet(s.ID); err != nil {
		t.Fatalf("DeleteSet: %v", err)
	}
	if _, err := repo.GetSet(s.ID); err == nil {
		t.Error("GetSet after delete should fail")
	}
	sets, _ = repo.ListSets()
	if len(sets) != 0 {
		t.Errorf("ListSets after delete = %d, want 0", len(sets))
	}
}

func TestBrandingRepo_CatThemes(t *testing.T) {
	store := newTestBrandingStore(t)
	repo := NewBrandingRepo(store)

	// Upsert (create)
	th := &model.BrandCategoryTheme{
		CategoryID: 7,
		Slot:       model.BrandSlotCategoryBanner,
		ImageURL:   "/uploads/branding/cat7.png",
	}
	if err := repo.UpsertCatTheme(th); err != nil {
		t.Fatalf("UpsertCatTheme: %v", err)
	}
	if th.ID == 0 {
		t.Fatal("UpsertCatTheme did not assign ID")
	}
	createdID := th.ID

	// Upsert again (update) — same (category, slot) must keep the ID
	th.ImageURL = "/uploads/branding/cat7-v2.png"
	if err := repo.UpsertCatTheme(th); err != nil {
		t.Fatalf("UpsertCatTheme (update): %v", err)
	}
	if th.ID != createdID {
		t.Errorf("update changed ID: %d != %d", th.ID, createdID)
	}

	// Get
	got, err := repo.GetCatTheme(7, model.BrandSlotCategoryBanner)
	if err != nil {
		t.Fatalf("GetCatTheme: %v", err)
	}
	if got.ImageURL != "/uploads/branding/cat7-v2.png" {
		t.Errorf("GetCatTheme = %q, want updated url", got.ImageURL)
	}

	// List
	list, err := repo.ListCatThemes()
	if err != nil {
		t.Fatalf("ListCatThemes: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("ListCatThemes = %d, want 1", len(list))
	}

	// Delete
	if err := repo.DeleteCatTheme(7, model.BrandSlotCategoryBanner); err != nil {
		t.Fatalf("DeleteCatTheme: %v", err)
	}
	list, _ = repo.ListCatThemes()
	if len(list) != 0 {
		t.Errorf("ListCatThemes after delete = %d, want 0", len(list))
	}
}

func TestBrandingRepo_Version(t *testing.T) {
	store := newTestBrandingStore(t)
	repo := NewBrandingRepo(store)

	if v := repo.GetVersion(); v != 0 {
		t.Errorf("initial version = %d, want 0", v)
	}
	for i := int64(1); i <= 3; i++ {
		if err := repo.BumpVersion(); err != nil {
			t.Fatalf("BumpVersion #%d: %v", i, err)
		}
		if v := repo.GetVersion(); v != i {
			t.Errorf("version after bump #%d = %d, want %d", i, v, i)
		}
	}
}
