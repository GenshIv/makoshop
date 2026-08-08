package attrs

import (
	"testing"
)

func TestParseTable(t *testing.T) {
	html := `<table>
<tr><td>Конструкция</td><td>универсальная</td></tr>
<tr><td>Тип</td><td>одноместная</td></tr>
<tr><td>Колеса</td><td>надувные</td></tr>
<tr><td>Крепление</td><td>ISOFIX</td></tr>
</table>`

	attrs := ParseTable(html)

	if len(attrs) != 4 {
		t.Errorf("expected 4 attrs, got %d: %+v", len(attrs), attrs)
	}

	checkAttr(t, attrs, "konstruktsiya", []string{"универсальная"})
	checkAttr(t, attrs, "tip", []string{"одноместная"})
	checkAttr(t, attrs, "kolesa", []string{"надувные"})
	checkAttr(t, attrs, "kreplenie", []string{"ISOFIX"})
}

func TestParseTableWithNBSP(t *testing.T) {
	// Real NBSP character
	html := `<table>
<tr><td>Конструкция</td><td>универсальная / с доп. блоком  /</td></tr>
</table>`

	attrs := ParseTable(html)
	checkAttr(t, attrs, "konstruktsiya", []string{"универсальная"})
}

func TestParseTableWithMultipleValues(t *testing.T) {
	html := `<table>
<tr><td>Цвет</td><td>черный, белый, красный</td></tr>
</table>`

	attrs := ParseTable(html)
	checkAttr(t, attrs, "tsvet", []string{"черный", "белый", "красный"})
}

func TestParseTableWithCombinedKey(t *testing.T) {
	// Example: value contains multiple params: "наклона спинки / 2 положения /, высоты подголовника / 8 положений /"
	html := `<table>
<tr><td>Регулировки</td><td>наклона спинки / 2 положения /, высоты подголовника / 8 положений /</td></tr>
</table>`

	attrs := ParseTable(html)
	t.Logf("attrs: %+v", attrs)

	// Should split into two attributes: "наклона спинки" -> "2 положения", "высоты подголовника" -> "8 положений"
	if vals, ok := attrs["naklona-spinki"]; !ok || len(vals) == 0 {
		t.Errorf("expected 'naklona-spinki' attr, got: %+v", attrs)
	} else {
		checkAttr(t, attrs, "naklona-spinki", []string{"2 положения"})
	}
	if vals, ok := attrs["vysoty-podgolovnika"]; !ok || len(vals) == 0 {
		t.Errorf("expected 'vysoty-podgolovnika' attr, got: %+v", attrs)
	} else {
		checkAttr(t, attrs, "vysoty-podgolovnika", []string{"8 положений"})
	}
}

func TestParseTableWithSlashNotes(t *testing.T) {
	html := `<table>
<tr><td>Защитный бампер</td><td>да / съемный /</td></tr>
<tr><td>Поворотные колеса</td><td>да</td></tr>
</table>`

	attrs := ParseTable(html)
	checkAttr(t, attrs, "zashchitnyy-bamper", []string{"да"})
	checkAttr(t, attrs, "povorotnye-kolesa", []string{"да"})
}

func TestSlugify(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Конструкция", "konstruktsiya"},
		{"Вес (в сборе)", "ves-v-sbore"},
		{"Диаметр задних колес", "diametr-zadnikh-koles"},
		{"ISOFIX", "isofix"},
		{"3 – 7 лет", "3-7-let"},
		{"Защитный бампер", "zashchitnyy-bamper"},
		{"Поворотные колеса", "povorotnye-kolesa"},
	}

	for _, tt := range tests {
		result := slugify(tt.input)
		if result != tt.expected {
			t.Errorf("slugify(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestNormalizeValues(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"универсальная", []string{"универсальная"}},
		{"черный, белый, красный", []string{"черный", "белый", "красный"}},
		{"да / 4 положения /", []string{"да"}},
		{"1 – 12 месяцев", []string{"1 - 12 месяцев"}},
	}

	for _, tt := range tests {
		result := normalizeValues(tt.input)
		if len(result) != len(tt.expected) {
			t.Errorf("normalizeValues(%q) = %v, want %v", tt.input, result, tt.expected)
			continue
		}
		for i, v := range result {
			if v != tt.expected[i] {
				t.Errorf("normalizeValues(%q)[%d] = %q, want %q", tt.input, i, v, tt.expected[i])
			}
		}
	}
}

func TestToFlatMap(t *testing.T) {
	attrs := ParsedAttrs{
		"color": []string{"red", "blue"},
		"size":  []string{"L"},
	}

	flat := attrs.ToFlatMap()
	if flat["color"] != "red" {
		t.Errorf("expected 'red', got %q", flat["color"])
	}
	if flat["size"] != "L" {
		t.Errorf("expected 'L', got %q", flat["size"])
	}
}

func TestParseTableWithCompositeAttribute(t *testing.T) {
	html := `<table>
<tr><td>Комплектация</td><td>прогулочный блок, люлька, накидка для ног, дождевик, москитная сетка, сумка для вещей, отделение для покупок, подстаканник</td></tr>
<tr><td>Конструкция</td><td>универсальная</td></tr>
</table>`

	attrs := ParseTable(html)
	t.Logf("attrs: %+v", attrs)

	// Should NOT have a single "komplektatsiya" with all items as one value
	if _, ok := attrs["komplektatsiya"]; ok {
		t.Errorf("expected no 'komplektatsiya' attr, but got: %+v", attrs["komplektatsiya"])
	}

	// Should have separate boolean attributes
	expectedCodes := []string{
		"komplektatsiya_progulochnyy-blok",
		"komplektatsiya_lyulka",
		"komplektatsiya_nakidka-dlya-nog",
		"komplektatsiya_dozhdevik",
		"komplektatsiya_moskitnaya-setka",
		"komplektatsiya_sumka-dlya-veshchey",
		"komplektatsiya_otdelenie-dlya-pokupok",
		"komplektatsiya_podstakannik",
	}

	for _, code := range expectedCodes {
		if vals, ok := attrs[code]; !ok {
			t.Errorf("missing composite attribute %q, got: %+v", code, attrs)
		} else if len(vals) != 1 || vals[0] != "да" {
			t.Errorf("attr %q: expected [\"да\"], got %v", code, vals)
		}
	}

	// Regular attribute should still work
	checkAttr(t, attrs, "konstruktsiya", []string{"универсальная"})
}

func TestIsCompositeAttribute(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"Комплектация", true},
		{"В комплекте", true},
		{"Аксессуары в комплекте", true},
		{"Конструкция", false},
		{"Тип", false},
		{"Колеса", false},
	}

	for _, tt := range tests {
		result := isCompositeAttribute(tt.input)
		if result != tt.expected {
			t.Errorf("isCompositeAttribute(%q) = %v, want %v", tt.input, result, tt.expected)
		}
	}
}

func checkAttr(t *testing.T, attrs ParsedAttrs, code string, expected []string) {
	t.Helper()
	values, ok := attrs[code]
	if !ok {
		t.Errorf("missing attribute code %q, got: %+v", code, attrs)
		return
	}
	if len(values) != len(expected) {
		t.Errorf("attr %q: expected %v, got %v", code, expected, values)
		return
	}
	for i, v := range values {
		if v != expected[i] {
			t.Errorf("attr %q[%d]: expected %q, got %q", code, i, expected[i], v)
		}
	}
}
