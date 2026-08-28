package pricesrc

import (
	"testing"
)

func TestHDWRAttrParser(t *testing.T) {
	parser := NewHDWRAttrParser()

	// Test 1: With colons (example 1)
	desc1 := `Automatyczny i ekonomiczny - przedstawiamy czytnik HD42A. 
Specyfikacja urządzenia Gwarancja: 2 lata Źródło światła: 650nm Laser Materiał wykonania: ABS + PC Metoda skanowania: manualnie (na przycisk)/ automatycznie (po zbliżeniu kodu) Potwierdzenie skanowania: sygnał dźwiękowy i świetlny Interfejs: USB Szybkość skanowania: 200 skanów na sekundę Długość przewodu: 200 cm Szerokość odczytu: 100 mm Dokładność odczytu: 0.10-0.825 mm Stopień ochrony: IP54 Wymiary urządzenia: 13,5 x 9 x 6,5 cm Wymiary z podstawką: 15,3 x 17,5 x 26 cm Wymiary opakowania: 20 x 14 x 8 cm Waga urządzenia: 360 g Waga z opakowaniem: 470 g Odczytywane kody 1D: EAN-13, EAN-8, UPC-A, UPC-E, Code 39, Code 93, Code 128, Interleaved 2 z 5, ITF-14, Industrial 2 z 5, Matrix 2 z 5, Codabar, Code 11, GS1 DataBar, GS1 DataBar Limited, GS1 DataBar Expanded Temperatura pracy: 0°C do 45°C Temperatura przechowywania: -40°C do 60°C Wilgotność pracy: 5 do 95% Wilgotność przechowywania: 5 do 95% Najczęściej zadawane pytania`

	attrs1 := parser.Parse(desc1)

	t.Logf("Test 1: Found %d attributes", len(attrs1))
	for k, v := range attrs1 {
		t.Logf("  %s: %v", k, v)
	}

	// Check some key attributes
	if len(attrs1["Gwarancja"]) != 1 || attrs1["Gwarancja"][0] != "2 lata" {
		t.Errorf("Expected Gwarancja=['2 lata'], got %v", attrs1["Gwarancja"])
	}
	if len(attrs1["Interfejs"]) != 1 || attrs1["Interfejs"][0] != "USB" {
		t.Errorf("Expected Interfejs=['USB'], got %v", attrs1["Interfejs"])
	}

	// Test 2: Without colons (example 2) - should find NO attributes
	desc2 := `Do zabudowy w dowolnym miejscu - poznaj moduł skanujący HD-SM302. 
Specyfikacja urządzenia Gwarancja 30 dni Źródło światła 650nm Laser Metoda skanowania automatycznie Potwierdzenie skanowania sygnał dźwiękowy Interfejs USB Waga urządzenia 33 g Waga z opakowaniem 98 g Wymiary urządzenia 40 x 33 x 15 mm Wymiary opakowania 140 x 110 x 45 mm Odczytywane kody 1D EAN8, EAN13, EAN128, UPC-A, UPC-E, CODE128, CODE39, CODE93, CODE11, GS1-DATAE, INDUS25, IATA25, MATRIX25, CHINESE25, CODABAR, MSI, pozostałe jednowymiarowe Temperatura pracy 0 do 45°C Temperatura przechowywania -20 do 50°C Wilgotność pracy 5 do 95% Wilgotność przechowywania 5 do 95% Pokaż pełną specyfikację`

	attrs2 := parser.Parse(desc2)

	t.Logf("Test 2: Found %d attributes (expected 0)", len(attrs2))
	if len(attrs2) != 0 {
		t.Errorf("Expected 0 attributes, got %d", len(attrs2))
	}

	// Test 3: No spec marker - should find NO attributes
	desc3 := `Opis produktu bez sekcji specyfikacji`

	attrs3 := parser.Parse(desc3)

	t.Logf("Test 3: Found %d attributes (expected 0)", len(attrs3))
	if len(attrs3) != 0 {
		t.Errorf("Expected 0 attributes, got %d", len(attrs3))
	}

	// Test 4: Multiple values separated by commas
	desc4 := `Test product description. 
Specyfikacja urządzenia Color: Red, Blue, Green Size: M, L, XL Najczęściej zadawane pytania`

	attrs4 := parser.Parse(desc4)

	t.Logf("Test 4: Found %d attributes", len(attrs4))
	for k, v := range attrs4 {
		t.Logf("  %s: %v", k, v)
	}

	if len(attrs4["Color"]) != 3 {
		t.Errorf("Expected Color to have 3 values, got %d", len(attrs4["Color"]))
	}
	if len(attrs4["Size"]) != 3 {
		t.Errorf("Expected Size to have 3 values, got %d", len(attrs4["Size"]))
	}
}
