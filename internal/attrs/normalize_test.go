package attrs

import (
	"testing"
)

func TestPolishToASCII(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"okładka", "okladka"},
		{"długopis", "dlugopis"},
		{"szczegóły produktu", "szczegoly produktu"},
		{"Pojemność", "Pojemnosc"},
		{"Ważność", "Waznosc"},
		{"Ładowność", "Ladownosc"},
		{"żelazko", "zelazko"},
		{"ISOFIX", "ISOFIX"},
	}
	for _, tt := range tests {
		if got := PolishToASCII(tt.input); got != tt.expected {
			t.Errorf("PolishToASCII(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestCodeFromKey(t *testing.T) {
	tests := []struct {
		input    string
		expected string
		ok       bool
	}{
		// Normal keys (real examples from the data)
		{"Moc", "moc", true},
		{"Kolor", "kolor", true},
		{"Kolor klawiszy", "kolor_klawiszy", true},
		{"Pojemność", "pojemnosc", true},
		{"okładka", "okladka", true},
		{"długopis", "dlugopis", true},
		{"szczegóły produktu", "szczegoly_produktu", true},
		{"Typ silnika", "typ_silnika", true},
		{"Materiał", "material", true},
		{"Waga (netto)", "waga_netto", true},
		{"Kolor - czarny", "kolor_czarny", true},

		// Values disguised as keys (must be rejected)
		{"0.06 m 3", "", false},
		{"10 cm", "", false},
		{"100 kg", "", false},
		{"5 W (łącznie 25 W)", "", false},
		{"jeepyFunkcje: warkot silnika", "", false},
		{"123", "", false},
		{"", "", false},

		// Sentences (must be rejected)
		{"10-kolorowy długopis firmy Astra dostępny w dwóch wzorach", "", false},
		{"z jej pomoc moesz przygotowa trzy rne warianty makaronu", "", false},
		{"w szkole spotykaj si rne typy przemocy wobec dzieci", "", false},
	}
	for _, tt := range tests {
		got, ok := CodeFromKey(tt.input)
		if ok != tt.ok {
			t.Errorf("CodeFromKey(%q) ok = %v, want %v (code=%q)", tt.input, ok, tt.ok, got)
			continue
		}
		if tt.ok && got != tt.expected {
			t.Errorf("CodeFromKey(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestValidateCode(t *testing.T) {
	valid := []string{"moc", "kolor_klawiszy", "typ_silnika", "abc", "a_b_c_d"}
	for _, c := range valid {
		if !ValidateCode(c) {
			t.Errorf("ValidateCode(%q) = false, want true", c)
		}
	}

	invalid := []string{
		"", "ab", // too short
		"10_cm", "006_m_3", // starts with digit
		"moc-", "_moc", "moc_", // dangling underscore
		"a_b_c_d_e",    // 5 words
		"moc silnika",  // space
		"moc!", "moc.", // punctuation
		"MO",          // uppercase
		"moc " + "x1", // fine actually - keep as valid below
	}
	// "moc x1" is valid, remove from invalid list
	invalid = invalid[:len(invalid)-1]
	for _, c := range invalid {
		if ValidateCode(c) {
			t.Errorf("ValidateCode(%q) = true, want false", c)
		}
	}
	if !ValidateCode("mocx1") {
		t.Error("ValidateCode(mocx1) should be true")
	}
}

func TestValidValue(t *testing.T) {
	valid := []string{
		"5 W",
		"czarny",
		"2 x 1",
		"ISOFIX",
		"15\"",
		"5 W (łącznie 25 W)",
		"iQdrive",
		"21L",
	}
	for _, v := range valid {
		if !ValidValue(v) {
			t.Errorf("ValidValue(%q) = false, want true", v)
		}
	}

	invalid := []string{
		"",
		"   ",
		"jeepyFunkcje: warkot silnika", // glued key:value
		"1 2 3 4 5 6 7",                // 7 words
		"________________",             // no alnum
		string(make([]rune, 41)),       // 41 runes
	}
	for _, v := range invalid {
		if ValidValue(v) {
			t.Errorf("ValidValue(%q) = true, want false", v)
		}
	}
}

func TestSplitValues(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"czarny", []string{"czarny"}},
		{"czarny, biały, czerwony", []string{"czarny", "biały", "czerwony"}},
		{"5 W", []string{"5 W"}},
		{"5 W, 10 W", []string{"5 W", "10 W"}},
		// Duplicates removed
		{"czarny, czarny, biały", []string{"czarny", "biały"}},
		// Glued key:value inside a value is dropped
		{"jeepyFunkcje: warkot silnika", nil},
		// Empty
		{"", nil},
	}
	for _, tt := range tests {
		got := SplitValues(tt.input)
		if len(got) != len(tt.expected) {
			t.Errorf("SplitValues(%q) = %v, want %v", tt.input, got, tt.expected)
			continue
		}
		for i := range got {
			if got[i] != tt.expected[i] {
				t.Errorf("SplitValues(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.expected[i])
			}
		}
	}
}

func TestCodeFromKeyIdempotent(t *testing.T) {
	// A canonical code must resolve to itself (idempotency for re-imports).
	codes := []string{"moc", "kolor_klawiszy", "typ_silnika", "pojemnosc"}
	for _, c := range codes {
		got, ok := CodeFromKey(c)
		if !ok || got != c {
			t.Errorf("CodeFromKey(%q) = %q, %v; want %q, true", c, got, ok, c)
		}
	}
}
