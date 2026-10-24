package i18n

import "testing"

func TestDetect(t *testing.T) {
	keys := []string{"LC_ALL", "LC_MESSAGES", "LANG", "LANGUAGE"}

	tests := []struct {
		name     string
		explicit string
		env      map[string]string
		want     Lang
	}{
		{name: "explicit ru", explicit: "ru", want: RU},
		{name: "explicit en", explicit: "en", want: EN},
		{name: "russian locale", env: map[string]string{"LANG": "ru_RU.UTF-8"}, want: RU},
		{name: "english locale", env: map[string]string{"LANG": "en_US.UTF-8"}, want: EN},
		{name: "lc_all wins", env: map[string]string{"LC_ALL": "en_US.UTF-8", "LANG": "ru_RU.UTF-8"}, want: EN},
		{name: "language list", env: map[string]string{"LANGUAGE": "ru:en"}, want: RU},
		{name: "c locale", env: map[string]string{"LANG": "C"}, want: EN},
		{name: "unknown falls back", env: map[string]string{"LANG": "de_DE.UTF-8"}, want: EN},
		{name: "empty falls back", want: EN},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for _, key := range keys {
				t.Setenv(key, "")
			}
			for key, value := range tc.env {
				t.Setenv(key, value)
			}
			if got := Detect(tc.explicit); got != tc.want {
				t.Fatalf("Detect(%q) = %v, want %v", tc.explicit, got, tc.want)
			}
		})
	}
}

func TestTranslator(t *testing.T) {
	if got := New(RU).T("usageFlags"); got != "Флаги:" {
		t.Fatalf("ru usageFlags = %q", got)
	}
	if got := New(EN).T("usageFlags"); got != "Flags:" {
		t.Fatalf("en usageFlags = %q", got)
	}
	if got := New(EN).T("errThreads", 0); got != "flag -threads must be between 1 and 256, got 0" {
		t.Fatalf("formatted message = %q", got)
	}
	if got := New(EN).T("missing-key"); got != "missing-key" {
		t.Fatalf("missing key = %q", got)
	}
}

func TestDetectForCountry(t *testing.T) {
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG", "LANGUAGE"} {
		t.Setenv(key, "")
	}
	if got := DetectForCountry("auto", "RU"); got != RU {
		t.Fatalf("RU country = %v, want RU", got)
	}
	if got := DetectForCountry("auto", "DE"); got != EN {
		t.Fatalf("DE country = %v, want EN", got)
	}
	if got := DetectForCountry("en", "RU"); got != EN {
		t.Fatalf("explicit en = %v, want EN", got)
	}
	t.Setenv("LANG", "C")
	if got := DetectForCountry("auto", "RU"); got != RU {
		t.Fatalf("C locale + RU = %v, want RU", got)
	}
	t.Setenv("LANG", "de_DE.UTF-8")
	if got := DetectForCountry("auto", "KZ"); got != RU {
		t.Fatalf("unknown locale + KZ = %v, want RU", got)
	}
	t.Setenv("LANG", "en_US.UTF-8")
	if got := DetectForCountry("auto", "KZ"); got != EN {
		t.Fatalf("english locale = %v, want EN", got)
	}
}

func TestDetectLocaleUnknown(t *testing.T) {
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG", "LANGUAGE"} {
		t.Setenv(key, "")
	}
	if _, ok := DetectLocale("auto"); ok {
		t.Fatal("empty locale unexpectedly recognized")
	}
	if _, ok := DetectLocale("de"); ok {
		t.Fatal("German locale unexpectedly recognized")
	}
}
