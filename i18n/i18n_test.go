package i18n

import (
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestSupportedLanguagesAreKoreanAndEnglishOnly(t *testing.T) {
	got := SupportedLanguages()
	want := []string{LangKo, LangEn}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SupportedLanguages() = %v, want %v", got, want)
	}

	if got := ParseAcceptLanguage("zh-CN,zh;q=0.9,en;q=0.8"); got != DefaultLang {
		t.Fatalf("ParseAcceptLanguage(zh-CN) = %q, want %q", got, DefaultLang)
	}
}

func TestKoreanLocaleCoversEnglishKeys(t *testing.T) {
	enMessages := loadLocaleForTest(t, "locales/en.yaml")
	koMessages := loadLocaleForTest(t, "locales/ko.yaml")

	for key := range enMessages {
		if _, ok := koMessages[key]; !ok {
			t.Fatalf("ko.yaml is missing translation key %q", key)
		}
	}
}

func TestInitLoadsKoreanAndEnglishLocales(t *testing.T) {
	if err := Init(); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	if got := Translate(LangKo, MsgInvalidParams); got == MsgInvalidParams {
		t.Fatalf("Translate(%q, %q) returned untranslated key", LangKo, MsgInvalidParams)
	}
	if got := Translate(LangEn, MsgInvalidParams); got != "Invalid parameters" {
		t.Fatalf("Translate(%q, %q) = %q, want %q", LangEn, MsgInvalidParams, got, "Invalid parameters")
	}
}

func loadLocaleForTest(t *testing.T, path string) map[string]string {
	t.Helper()

	data, err := localeFS.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	var messages map[string]string
	if err := yaml.Unmarshal(data, &messages); err != nil {
		t.Fatalf("unmarshal %s: %v", path, err)
	}
	return messages
}
