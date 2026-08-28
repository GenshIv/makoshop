package pricesrc

import (
	"fmt"
	"os"
	"testing"
)

func TestZabudowaAttrParser(t *testing.T) {
	// Test with a sample description from the real file
	description := `
 
 SMAŻ I CIESZ SIĘ SMAKIEM 
 Płyta grzejna SenseFry®: wystarczy wybrać rodzaj potrawy i oczekiwany efekt gotowania. Urządzenie będzie samoczynnie utrzymać odpowiednią temperaturę powierzchni naczynia. Bez konieczności zgadywania i regulacji ustawień. Dzięki temu można skupić się wyłącznie na idealnym smaku potrawy. 
 
 
 
 WYBIERZ RODZAJ POTRAWY I OCZEKIWANY EFEKT GOTOWANIA. SPODZIEWAJ SIĘ ZAWSZE WSPANIAŁEGO SMAKU 
 
 Wyświetlacz dotykowy płyty indukcyjnej SenseFry® umożliwia wybór przyrządzanej potrawy i oczekiwanego efektu gotowania, aby urządzenie utrzymywało odpowiednią temperaturę naczynia. Eliminuje to konieczność zgadywania i regulacji ustawień podczas gotowania. Dzięki temu potrawy zawsze będą doskonale przyrządzone. 
 
 JEDEN WYŚWIETLACZ DOTYKOWY. PEŁNA KONTROLA NAD PROCESEM GOTOWANIA. 
 Kolorowy wyświetlacz dotykowy zapewnia pełną kontrolę nad płytą grzejną. Pozwala monitorować w czasie rzeczywistym działanie pól grzejnych i odpowiednio regulować ich ustawienia. To intuicyjny sposób na finezyjne gotowanie za dotknięciem palca. 
 
 TECHNOLOGIA FLEXIBRIDGE DLA ZAPEWNIENIA MAKSYMALNEJ ELASTYCZNOŚCI GOTOWANIA 
 
 Specjalne 4-segmentowe pole grzejne FlexiBridge umożliwia łączenie segmentów w 3 różnych konfiguracjach, w zależności od wielkości naczynia. 
 
 ZDALNA KONTROLA NAD ZAPACHAMI W KUCHNI 
 Funkcja Hob2Hood zapewnia bezprzewodowe połącznie pomiędzy płytą a okapem. Po prostu rozpocznij gotowanie, a okap automatycznie dostosuje swoje ustawienia na podstawie opcji wybranych na płycie. Skoncentruj się tylko na gotowaniu. 
 
 KAŻDE POLE GRZEJNE Z ODDZIELNYM ZEGAREM 
 Po włączeniu pola grzejnego można szybko i wygodnie ustawić jego zegar za pośrednictwem wyświetlacza dotykowego. Bez potrzeby korzystania z menu lub innych pól grzejnych. Możliwość indywidualnego odmierzania czasu dla każdej potrawy daje gwarancję pełnej kontroli nad gotowaniem. 
 
 
 
 PŁYTY INDUKCYJNE 
 Płyta indukcyjna pomoże Ci maksymalnie wykorzystać Twoje umiejętności kulinarne  . Ogrzewa ona naczynia szybciej niż jakakolwiek inna płyta grzejna, pozwalając natychmiast przystąpić do gotowania. 
 PODSTAWOWE DANE TECHNICZNE 
 
 Wymiary (S x G) [mm]:  780x520 
 Wymiary wycięcia (W x S x G) [mm]: 44x750x490 
 Przewód [m]:  1,5 
 Maksymalna moc gazu [W]:  0 
 Całkowity pobór mocy [W]:  7350 
 Napięcie [V]:  220-240/400V2N 
 Moc/średnica - pole prawe przednie: 1800/2800W/180mm 
 
 POBIERZ DOKUMENTY 
 
 Karta katalogowa produktuPDF 
 Instrukcja obsługiPDF 
 Karta informacyjna produktuPDF 
 
 DANE TECHNICZNE 
 
 Typ płyty: indukcyjna 
 Bezramkowa ze szlifem 
 Kolorowy wyświetlacz Maxisight™ 
 Położenie elementów sterujących: z przodu po prawej stronie 
 Podświetlane sterowanie 
 Strefy indukcyjne z opcją Booster 
 Wykrywanie obecności garnka 
 Strefa lewa przednia: indukcyjna, 2300/3200W/220mm 
 Strefa lewa tylna: indukcyjna, 2300/3200W/220mm 
 Strefa przednia, środkowa: brak, 
 Strefa tylna środkowa: indukcyjna,2300/3200W/210mm 
 Strefa prawa przednia: indukcyjna, 1800/2800W/180mm 
 Strefa prawa tylna: brak, 
 Blokada ustawień 
 Zabezpieczenie przed dziećmi 
 Zabezpieczenia płyty: automatyczny wyłącznik 
 Sygnał dźwiękowy z opcją wyłączenia 
 Funkcja odliczania czasu 
 Funkcja Öko Timer™:  Funkcja Öko Timer™ wyłącza grzanie przed końcem gotowania, aby jak najefektywniej wykorzystać pozostałe ciepło do przerwania procesu gotowania. 
 Kontrola OptiHeat:  3-stopniowa funkcja kontroli ciepła pozostałego 
 Łatwa instalacja 
 
 SPECYFIKACJA 
 
 Maksymalna moc gazu [W]:  0 
 Moc/średnica - pole prawe przednie:  1800/2800W/180mm 
 Moc/średnica - pole lewe przednie:  2300/3200W/220mm 
 Moc/średnica - pole lewe tylne :  2300/3200W/220mm 
 Moc/średnica - pole środkowe tylne:  2300/3200W/210mm 
 
 INSTALACJA 
 
 Wymiary (S x G) [mm]:  780x520 
 Wymiary wycięcia (W x S x G) [mm]:  44x750x490 
 Waga netto [kg]:  12,94 
 
 ZUŻYCIE ENERGII 
 
 Całkowity pobór mocy [W]:  7350 
 
 FUNKCJE 
 
 Etykieta PNC:  949 597 550 
 Przewód [m]:  1,5 
 Napięcie [V]:  220-240/400V2N 
 Wskaźnik ciepła pozostałego:  4 
 Akcesoria opcjonalne:  nie 
 Kod produktu (PNC):  949 597 550 
 Kod EAN:  7332543677498 
 Informacje dla profesjonalistów dotyczące nieniszczącego demontażu, zgodnie z Rozporządzeniem Komisji (UE) nr 66/2014 
`

	parser := NewZabudowaAttrParser()
	attrs := parser.Parse(description)

	fmt.Println("=== Extracted attributes ===")
	for key, values := range attrs {
		for _, value := range values {
			fmt.Printf("%s: %s\n", key, value)
		}
	}
	fmt.Printf("\nTotal: %d attributes\n", len(attrs))
}

func TestZabudowaAttrParserRealFile(t *testing.T) {
	// Test with the real file
	filePath := "../../prices/zabudowa-agd.pl/bfda9b08-9bf0-4c60-b6da-4eec94d45bd1.xml"

	f, err := os.Open(filePath)
	if err != nil {
		t.Skip("File not found, skipping test")
		return
	}
	defer f.Close()

	parser := NewNokautParser()
	zabudowaParser := NewZabudowaAttrParser()

	count := 0
	_, err = parser.Parse(f, func(offer Offer) error {
		count++
		if count <= 3 {
			fmt.Printf("\n=== Offer %d: %s ===\n", count, offer.Name)
			attrs := zabudowaParser.Parse(offer.Description)
			for key, values := range attrs {
				for _, value := range values {
					fmt.Printf("%s: %s\n", key, value)
				}
			}
			fmt.Printf("Total: %d attributes\n", len(attrs))
		}
		return nil
	})

	if err != nil {
		t.Fatalf("Failed to parse: %v", err)
	}

	fmt.Printf("\nTotal offers: %d\n", count)
}
