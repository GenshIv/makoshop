package i18n

import "strings"

import "github.com/GenshIv/makoshop/internal/model"

// ResolveCategoryName returns the localized category name.
// Uses name_ru/name_ua/name_pl/name_en directly.
// lang: "ru", "ua" (alias: "uk"), "pl", "en". Fallback: en -> ru -> ua -> pl.
func ResolveCategoryName(cat *model.Category, lang string) string {
	if cat == nil {
		return ""
	}

	l := strings.ToLower(lang)

	// Try requested language first
	switch l {
	case "ru":
		if cat.NameRu != "" {
			return cat.NameRu
		}
	case "ua", "uk":
		if cat.NameUa != "" {
			return cat.NameUa
		}
	case "pl":
		if cat.NamePl != "" {
			return cat.NamePl
		}
	case "en":
		if cat.NameEn != "" {
			return cat.NameEn
		}
	}

	// Fallback chain: en -> ru -> ua -> pl (matches frontend priority)
	if cat.NameEn != "" {
		return cat.NameEn
	}
	if cat.NameRu != "" {
		return cat.NameRu
	}
	if cat.NameUa != "" {
		return cat.NameUa
	}
	if cat.NamePl != "" {
		return cat.NamePl
	}

	return ""
}

// ResolveCategoryNameCurrent returns the category name resolved via i18n for the current language.
func ResolveCategoryNameCurrent(cat *model.Category) string {
	return ResolveCategoryName(cat, Current())
}

// ResolveAttrName returns the localized attribute definition name.
// lang: "ru", "ua" (alias: "uk"), "pl", "en". Fallback: en -> ru -> ua -> pl.
func ResolveAttrName(attr *model.AttributeDefinition, lang string) string {
	if attr == nil {
		return ""
	}

	l := strings.ToLower(lang)

	switch l {
	case "ru":
		if attr.NameRu != "" {
			return attr.NameRu
		}
	case "ua", "uk":
		if attr.NameUa != "" {
			return attr.NameUa
		}
	case "pl":
		if attr.NamePl != "" {
			return attr.NamePl
		}
	case "en":
		if attr.NameEn != "" {
			return attr.NameEn
		}
	}

	// Fallback chain: en -> ru -> ua -> pl
	if attr.NameEn != "" {
		return attr.NameEn
	}
	if attr.NameRu != "" {
		return attr.NameRu
	}
	if attr.NameUa != "" {
		return attr.NameUa
	}
	if attr.NamePl != "" {
		return attr.NamePl
	}

	return ""
}

// ResolveAttrNameCurrent returns the attribute name resolved via i18n for the current language.
func ResolveAttrNameCurrent(attr *model.AttributeDefinition) string {
	return ResolveAttrName(attr, Current())
}
