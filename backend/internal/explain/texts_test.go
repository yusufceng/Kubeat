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
	// And those the checker gives itself: the timeouts it tells apart, and
	// the scope's refusals, by whose record holds the monitor.
	for _, code := range []string{ConnectTimeout, ResponseTimeout, TooManyRedirects, BlockedByScope, BlockedByScopeObject} {
		known(code)
	}
}

// The scope's refusal is worded by whose record holds the monitor, the same
// for either reason: each text says both causes, a connection outside the
// scope and a name that did not resolve, a fault of DNS more often than an
// attempt, and a remedy that does not widen the scope first. Only the
// user's, which a monitor without a record gets too, offers an
// administrator, and only if DNS works; the annotation monitor's keeps the
// check on the application's own host. Neither names an address. The
// results written before this wording show it too: the text goes by the
// code, not by when the result was written.
func TestScopeRefusalTexts(t *testing.T) {
	for _, tc := range []struct {
		code, language string
		says, not      []string
	}{
		{BlockedByScope, English,
			[]string{"target scope refused a connection of this check", "outside the namespaces of whoever set up the monitor", "or DNS failed to resolve its name", "if DNS works, an administrator can set it up"},
			nil},
		{BlockedByScope, Turkish,
			[]string{"Hedef kapsamı kontrolün bir bağlantısını reddetti", "monitor'ü kuranın namespace'lerinin dışına", "ya da adı o an çözülemedi", "DNS arızası değilse monitor'ü bir yönetici kurabilir"},
			nil},
		{BlockedByScopeObject, English,
			[]string{"target scope refused a connection of this check", "outside the annotated object's namespace", "or DNS failed to resolve its name", "the app's own host", "health path needing no sign-in"},
			[]string{"administrator"}},
		{BlockedByScopeObject, Turkish,
			[]string{"Hedef kapsamı kontrolün bir bağlantısını reddetti", "nesnenin namespace'i dışına", "ya da adı DNS'te çözülemedi", "uygulamanın kendi host'unda", "oturum istemeyen bir sağlık yolunda"},
			[]string{"yönetici"}},
	} {
		text := Text(store.Explanation{Code: tc.code}, tc.language)
		for _, want := range tc.says {
			if !strings.Contains(text, want) {
				t.Errorf("%s in %s does not say %q: %q", tc.code, tc.language, want, text)
			}
		}
		for _, unwanted := range tc.not {
			if strings.Contains(text, unwanted) {
				t.Errorf("%s in %s says %q: %q", tc.code, tc.language, unwanted, text)
			}
		}
		if address := regexp.MustCompile(`[0-9]|[a-z]\.[a-z]`).FindString(text); address != "" {
			t.Errorf("%s in %s names something like an address (%q): %q", tc.code, tc.language, address, text)
		}
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
