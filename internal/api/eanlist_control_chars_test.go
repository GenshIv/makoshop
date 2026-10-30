package api

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/GenshIv/makoshop/internal/db"
	"github.com/GenshIv/makoshop/internal/model"
	silentjson "github.com/GenshIv/silentjson/v2"
)

// Regression test for blank product pages on SPA navigation: product
// attribute values imported from vendor feeds contain raw control
// characters (e.g. newlines), and the silentjson AVX2 fast path used to
// emit them unescaped. The pure-JSON EAN page endpoint then returned
// invalid JSON, axios JSON.parse failed, and the frontend rendered
// nothing. The full-page HTML shell worked because writeSafeJSON
// escaped control characters while embedding __INITIAL_DATA__ — hence
// "click shows nothing, refresh works".
func TestEANListRespControlCharsValidJSON(t *testing.T) {
	docking := "Various docking solutions supported via USB-C.\nFor more compatible docking solutions, please visit <a href=\"https://psref.lenovo.com/ac\">link</a>\t\x01"

	data := db.EANListRespData{
		EANPage: &model.EANPage{
			EAN:         "123",
			Slug:        "test-slug",
			Title:       "Title",
			Description: "desc\nwith\nnewlines and \ttabs",
		},
		Products: []model.Product{
			{ID: 1, Name: "P", Attributes: []model.KeyValue{
				{Key: "Docking", Value: docking},
			}},
		},
		Total: 1,
	}

	reg := silentjson.BuildRegistry(reflect.TypeOf(db.EANListRespData{}))
	buf := silentjson.Marshal(&data, reg, nil)

	var back db.EANListRespData
	if err := json.Unmarshal(buf, &back); err != nil {
		t.Fatalf("marshaled EANListRespData is not valid JSON: %v\npayload: %s", err, buf)
	}
	if got := back.Products[0].Attributes[0].Value; got != docking {
		t.Fatalf("attribute value did not round-trip:\n got: %q\nwant: %q", got, docking)
	}
	if back.EANPage.Description != data.EANPage.Description {
		t.Fatalf("description did not round-trip:\n got: %q\nwant: %q",
			back.EANPage.Description, data.EANPage.Description)
	}
}
