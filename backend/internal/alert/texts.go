package alert

import (
	"fmt"
	"time"
)

// Languages the notifications can be written in.
const (
	LanguageEnglish = "en"
	LanguageTurkish = "tr"
)

// phrases are the texts of one language. The format verbs are positional
// so that every language takes the same arguments in the same order.
type phrases struct {
	// one monitor: name, target (with its leading space or empty), ...
	down        string // name, target, threshold, cause
	up          string // name, target, duration
	reminder    string // name, target, duration, cause
	certExpires string // name, when, date, target
	certRenewed string // name, date, target
	certExpired string
	certToday   string
	certDays    string // days
	test        string

	// a group: where ("shop: " or empty), count, list
	groupDown, groupUp, groupReminder, groupCertExpires, groupCertRenewed string
	groupMore                                                             string // how many more

	subjectDown, subjectUp, subjectReminder, subjectCertExpires, subjectCertRenewed, subjectTest             string // name
	subjectGroupDown, subjectGroupUp, subjectGroupReminder, subjectGroupCertExpires, subjectGroupCertRenewed string // where, count

	factMonitor, factTarget, factNamespace, factStart, factEnd, factCause string

	// the unit words of a duration: day, hour, minute, second
	unitDay, unitHour, unitMin, unitSec string

	// openInKubeat labels the link back; monitorsWord names what a group
	// counts ("3 monitors" / "3 monitor").
	openInKubeat, monitorsWord string
}

var languages = map[string]phrases{
	LanguageEnglish: {
		down:        "🔴 %s is down%s. %d checks in a row failed. Last error: %s",
		up:          "🟢 %s is back up%s. Outage: %s",
		reminder:    "🔴 %s is still down%s. Outage so far: %s. Last error: %s",
		certExpires: "🟡 The certificate of %s %s (%s)%s",
		certRenewed: "🟢 The certificate of %s was renewed; it now expires on %s%s",
		certExpired: "has already expired",
		certToday:   "expires within 24 hours",
		certDays:    "expires in %d days",
		test:        "Kubeat test notification: this channel works.",

		groupDown:        "🔴 %s%d monitors are down: %s",
		groupUp:          "🟢 %s%d monitors are back up: %s",
		groupReminder:    "🔴 %s%d monitors are still down: %s",
		groupCertExpires: "🟡 %s%d certificates are expiring: %s",
		groupCertRenewed: "🟢 %s%d certificates were renewed: %s",
		groupMore:        "and %d more",

		subjectDown: "[Kubeat] %s is down", subjectUp: "[Kubeat] %s is back up", subjectReminder: "[Kubeat] %s is still down",
		subjectCertExpires: "[Kubeat] The certificate of %s is expiring", subjectCertRenewed: "[Kubeat] The certificate of %s was renewed",
		subjectTest:      "[Kubeat] Test notification",
		subjectGroupDown: "[Kubeat] %s%d monitors are down", subjectGroupUp: "[Kubeat] %s%d monitors are back up",
		subjectGroupReminder: "[Kubeat] %s%d monitors are still down", subjectGroupCertExpires: "[Kubeat] %s%d certificates are expiring",
		subjectGroupCertRenewed: "[Kubeat] %s%d certificates were renewed",

		factMonitor: "Monitor", factTarget: "Target", factNamespace: "Namespace", factStart: "Started", factEnd: "Ended", factCause: "Cause",

		unitDay: "d", unitHour: "h", unitMin: "m", unitSec: "s",

		openInKubeat: "Open in Kubeat", monitorsWord: "monitors",
	},
	LanguageTurkish: {
		down:        "🔴 %s erişilemiyor%s. Art arda %d kontrol başarısız. Son hata: %s",
		up:          "🟢 %s tekrar çalışıyor%s. Kesinti süresi: %s",
		reminder:    "🔴 %s hâlâ erişilemiyor%s. Kesinti süresi: %s. Son hata: %s",
		certExpires: "🟡 %s sertifikasının süresi bitiyor: %s (%s)%s",
		certRenewed: "🟢 %s sertifikası yenilendi; yeni bitiş tarihi %s%s",
		certExpired: "süresi doldu",
		certToday:   "24 saat içinde",
		certDays:    "%d gün sonra",
		test:        "Kubeat test bildirimi: bu kanal çalışıyor.",

		groupDown:        "🔴 %s%d monitor erişilemiyor: %s",
		groupUp:          "🟢 %s%d monitor tekrar çalışıyor: %s",
		groupReminder:    "🔴 %s%d monitor hâlâ erişilemiyor: %s",
		groupCertExpires: "🟡 %s%d sertifikanın süresi bitiyor: %s",
		groupCertRenewed: "🟢 %s%d sertifika yenilendi: %s",
		groupMore:        "ve %d monitor daha",

		subjectDown: "[Kubeat] %s erişilemiyor", subjectUp: "[Kubeat] %s tekrar çalışıyor", subjectReminder: "[Kubeat] %s hâlâ erişilemiyor",
		subjectCertExpires: "[Kubeat] %s sertifikasının süresi bitiyor", subjectCertRenewed: "[Kubeat] %s sertifikası yenilendi",
		subjectTest:      "[Kubeat] Test bildirimi",
		subjectGroupDown: "[Kubeat] %s%d monitor erişilemiyor", subjectGroupUp: "[Kubeat] %s%d monitor tekrar çalışıyor",
		subjectGroupReminder: "[Kubeat] %s%d monitor hâlâ erişilemiyor", subjectGroupCertExpires: "[Kubeat] %s%d sertifikanın süresi bitiyor",
		subjectGroupCertRenewed: "[Kubeat] %s%d sertifika yenilendi",

		factMonitor: "Monitor", factTarget: "Hedef", factNamespace: "Namespace", factStart: "Başlangıç", factEnd: "Bitiş", factCause: "Neden",

		unitDay: "gün", unitHour: "sa", unitMin: "dk", unitSec: "sn",

		openInKubeat: "Kubeat'te aç", monitorsWord: "monitor",
	},
}

// humanDuration writes a duration with the language's unit words, at most
// two units: "1 sa 15 dk" / "1h 15m", "45 sn" / "45s".
func humanDuration(d time.Duration, w phrases) string {
	if d < 0 {
		d = 0
	}
	total := int(d.Round(time.Second).Seconds())
	day, hour, minute := 86400, 3600, 60
	sep, unit := " ", func(n int, u string) string {
		if w.unitSec == "s" { // English writes 5m, Turkish 5 dk
			return fmt.Sprintf("%d%s", n, u)
		}
		return fmt.Sprintf("%d %s", n, u)
	}
	switch {
	case total >= day:
		if rest := (total % day) / hour; rest > 0 {
			return unit(total/day, w.unitDay) + sep + unit(rest, w.unitHour)
		}
		return unit(total/day, w.unitDay)
	case total >= hour:
		if rest := (total % hour) / minute; rest > 0 {
			return unit(total/hour, w.unitHour) + sep + unit(rest, w.unitMin)
		}
		return unit(total/hour, w.unitHour)
	case total >= minute:
		if rest := total % minute; rest > 0 {
			return unit(total/minute, w.unitMin) + sep + unit(rest, w.unitSec)
		}
		return unit(total/minute, w.unitMin)
	default:
		return unit(total, w.unitSec)
	}
}

// ValidLanguage reports whether notifications can be written in it.
func ValidLanguage(language string) bool {
	_, ok := languages[language]
	return ok
}

// wording returns the phrases of the notification's language; English
// when it names none.
func wording(n Notification) phrases {
	if p, ok := languages[n.Language]; ok {
		return p
	}
	return languages[LanguageEnglish]
}
