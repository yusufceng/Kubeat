package explain

import (
	"maps"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/yusufceng/Kubeat/backend/internal/store"
)

// params are the values each code's wording uses; a code not listed uses
// none.
var params = map[string][]string{
	ProxyNoHealthyUpstream: {"proxy"},
	ProxyUpstreamConnect:   {"proxy"},
	ProxyUpstreamTLS:       {"proxy"},
	DNSServiceNotFound:     {"service", "namespace"},
	DNSNotFound:            {"host"},
	TLSHostnameMismatch:    {"host"},
}

var placeholder = regexp.MustCompile(`\{[a-z]+\}`)

// Every code is worded in both languages, with no other values than its
// own, in one short sentence.
func TestEveryCodeHasItsTexts(t *testing.T) {
	codes := Codes()
	if !slices.Equal(codes, slices.Sorted(maps.Keys(languages[Turkish]))) {
		t.Fatalf("the languages word different codes:\n%v\n%v", codes, slices.Sorted(maps.Keys(languages[Turkish])))
	}
	for _, code := range codes {
		e := store.Explanation{Code: code, Params: map[string]string{}}
		for _, name := range params[code] {
			e.Params[name] = "<" + name + ">"
		}
		english, turkish := Text(e, English), Text(e, Turkish)
		for language, text := range map[string]string{English: english, Turkish: turkish} {
			if left := placeholder.FindString(text); left != "" {
				t.Errorf("%s in %s: %s is not one of its values: %q", code, language, left, text)
			}
			for _, name := range params[code] {
				if !strings.Contains(text, "<"+name+">") {
					t.Errorf("%s in %s does not say its %s: %q", code, language, name, text)
				}
			}
			if len([]rune(text)) > 200 || !strings.HasSuffix(text, ".") {
				t.Errorf("%s in %s is not one short sentence: %q", code, language, text)
			}
		}
		if english == turkish {
			t.Errorf("%s is not translated: %q", code, english)
		}
	}

	// Every code the rules give is one of them.
	known := func(code string) {
		if !slices.Contains(codes, code) {
			t.Errorf("%s has no texts", code)
		}
	}
	for _, p := range pages {
		known(p.code)
	}
	for _, code := range statuses {
		known(code)
	}
	for _, f := range failures {
		known(f.code)
	}
	for _, code := range []string{ConnectTimeout, ResponseTimeout, TooManyRedirects} {
		known(code)
	}
}

func TestText(t *testing.T) {
	e := store.Explanation{Code: DNSServiceNotFound, Params: map[string]string{"service": "api", "namespace": "shop"}}
	if got := Text(e, English); got != "There is no Service api in the namespace shop: check both names in the address." {
		t.Errorf("English: %q", got)
	}
	if got := Text(e, Turkish); got != "shop namespace'inde api adında bir Service yok: adresteki iki adı da kontrol edin." {
		t.Errorf("Turkish: %q", got)
	}
	// Another language falls back to English; a code this version does
	// not know has no text.
	if Text(e, "de") != Text(e, English) {
		t.Error("an unknown language is not English")
	}
	if got := Text(store.Explanation{Code: "from_the_future"}, English); got != "" {
		t.Errorf("unknown code: %q", got)
	}
}
