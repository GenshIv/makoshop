package slug

import "testing"

func TestSlug(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"cyrillic", "Привет Мир", "privet-mir"},
		{"mixed", "Samsung Galaxy S7 Черный", "samsung-galaxy-s7-chernyy"},
		{"underscores", "hello_world_test", "hello-world-test"},
		{"multiple-hyphens", "a--b---c", "a-b-c"},
		{"leading-trailing", "  --abc--  ", "abc"},
		{"soft-signs", "объём", "obem"},
		{"empty", "", ""},
		{"only-specials", "!!! ???", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Slug(tt.in); got != tt.want {
				t.Errorf("Slug(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestSlugFromNameEn(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"basic", "Hello World", "hello-world"},
		{"already-slug", "hello-world", "hello-world"},
		{"underscore-dropped", "hello_world", "helloworld"},
		{"cyrillic-dropped", "Привет World", "world"},
		{"multiple-spaces", "a  b   c", "a-b-c"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SlugFromNameEn(tt.in); got != tt.want {
				t.Errorf("SlugFromNameEn(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestSlugKeepCase(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"case-preserved", "Hello World", "Hello-World"},
		{"digits", "ABC-123", "ABC-123"},
		{"multiple-hyphens", "a--b", "a-b"},
		{"leading-trailing", "  -abc-  ", "abc"},
		{"cyrillic-dropped", "Привет World", "World"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SlugKeepCase(tt.in); got != tt.want {
				t.Errorf("SlugKeepCase(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
