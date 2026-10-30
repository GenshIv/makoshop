package api

import (
	"testing"

	"github.com/GenshIv/makoshop/internal/model"
)

// TestParseJSONProductZeroPrice verifies the zero-price import rule at the
// parser level: an offer priced below the catalog minimum is returned together
// with skip=true (the caller must not import it as a product but keeps the
// data to create an EAN page without offers); a normally priced offer is
// returned with skip=false.
func TestParseJSONProductZeroPrice(t *testing.T) {
	mk := func(price string) JsonProductFileItem {
		return JsonProductFileItem{
			Name:        "Laptop Alpha",
			Description: "A solid laptop description",
			Offers: []jsonOfferFileItem{{
				PriceHistory: []jsonPriceEntry{{
					Price: jsonMoney{Value: price, Currency: "PLN"},
				}},
			}},
		}
	}

	attrDefCache := map[string]*model.AttrDef{}
	newAttrKeys := map[string]struct{}{}

	// Zero price: product data is still returned, but flagged to skip.
	p, skip, err := parseJSONProductForImport(mk("0"), 1, "acme", "Acme", "PLN", attrDefCache, newAttrKeys, nil, nil, nil)
	if err != nil {
		t.Fatalf("parse zero price: %v", err)
	}
	if !skip {
		t.Fatalf("zero price offer must be flagged skip")
	}
	if p == nil {
		t.Fatalf("zero price offer data must be returned for the no-offer EAN page")
	}
	if p.Name != "Laptop Alpha" || p.Description == "" {
		t.Fatalf("zero price offer lost its data: %+v", p)
	}

	// Sub-cent price (below EANPageMinCatalogPrice): same treatment.
	p2, skip2, err := parseJSONProductForImport(mk("0.005"), 1, "acme", "Acme", "PLN", attrDefCache, newAttrKeys, nil, nil, nil)
	if err != nil {
		t.Fatalf("parse sub-cent price: %v", err)
	}
	if !skip2 || p2 == nil {
		t.Fatalf("sub-cent price offer must be returned with skip=true, got %v/%v", p2, skip2)
	}

	// Normal price: imported.
	p3, skip3, err := parseJSONProductForImport(mk("10.00"), 1, "acme", "Acme", "PLN", attrDefCache, newAttrKeys, nil, nil, nil)
	if err != nil {
		t.Fatalf("parse normal price: %v", err)
	}
	if skip3 || p3 == nil || p3.Price != 10 {
		t.Fatalf("normal offer must be importable, got skip=%v price=%v", skip3, p3.Price)
	}
}
