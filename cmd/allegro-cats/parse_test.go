package main

import (
	"strings"
	"testing"
)

const sample = `<nav class="meqh_en" data-role="breadcrumbs-list"><ol>
<li data-role="breadcrumb-item"><a class="mp7g" href="/" itemprop="item" data-analytics-clickable="" data-analytics-click-custom-navigation-category-id="954b95b6-43cf-4104-8354-dea4d9b10ddf"><span itemprop="name">Allegro</span></a></li>
<li data-role="breadcrumb-item"><a class="mp7g" href="/dzial/elektronika" itemprop="item" data-analytics-clickable="" data-analytics-click-custom-navigation-category-id="42540aec-367a-4e5e-b411-17c09b08e41f"><span itemprop="name">Elektronika</span></a>
<div data-role="breadcrumb-item-apron"><a class="mg9e_8" href="/kategoria/fotografia" data-analytics-click-custom-navigation-category-id="8845" data-analytics-clickable="" data-analytics-click-custom-placement="apron">Fotografia</a><a class="mg9e_8" href="/kategoria/komputery" data-analytics-click-custom-navigation-category-id="2" data-analytics-clickable="" data-analytics-click-custom-placement="apron">Komputery</a><a class="mg9e_8" href="/kategoria/komputery-stacjonarne-486" data-analytics-click-custom-navigation-category-id="486" data-analytics-clickable="" data-analytics-click-custom-placement="apron">Komputery stacjonarne</a></div></li>
<li data-role="breadcrumb-item"><a class="mp7g" href="/kategoria/komputery" itemprop="item" data-analytics-clickable="" data-analytics-click-custom-navigation-category-id="2"><span itemprop="name">Komputery</span></a></li>
<li data-role="breadcrumb-item"><a class="mp7g" href="/kategoria/komputery-stacjonarne-486" itemprop="item" data-analytics-clickable="" data-analytics-click-custom-navigation-category-id="486"><span itemprop="name">Komputery stacjonarne</span></a></li>
</ol></nav>`

func TestBreadcrumbs(t *testing.T) {
	path := breadcrumbs(sample)
	if len(path) < 3 {
		t.Fatalf("path entries = %d, want >= 3 (got %+v)", len(path), path)
	}
	last := path[len(path)-1]
	if last.id != "486" || last.name != "Komputery stacjonarne" {
		t.Fatalf("leaf = %+v, want id 486 name Komputery stacjonarne", last)
	}
	if got := strings.Join(names(path), " > "); got != "Elektronika > Komputery > Komputery stacjonarne" {
		t.Fatalf("names = %q", got)
	}
}

func TestAnchors(t *testing.T) {
	links := anchors(sample)
	byID := map[string]linkEntry{}
	for _, l := range links {
		byID[l.id] = l
	}
	if l, ok := byID["491"]; ok && l.name == "Laptopy" {
		// not in this sample; fine
	}
	if l := byID["8845"]; l.name != "Fotografia" || l.alias != "fotografia" {
		t.Fatalf("8845 = %+v", l)
	}
	if l := byID["2"]; l.name != "Komputery" {
		t.Fatalf("2 = %+v", l)
	}
}
