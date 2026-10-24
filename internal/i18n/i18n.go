// Package i18n содержит минимальную локализацию CLI (ru/en).
package i18n

import (
	"fmt"
	"os"
	"strings"
)

// Lang — язык интерфейса.
type Lang int

const (
	RU Lang = iota
	EN
)

func (l Lang) String() string {
	if l == RU {
		return "ru"
	}
	return "en"
}

// Translator выдаёт строки на выбранном языке.
type Translator struct {
	lang Lang
}

// New создаёт переводчик для языка.
func New(lang Lang) *Translator {
	return &Translator{lang: lang}
}

// Lang возвращает текущий язык.
func (t *Translator) Lang() Lang {
	return t.lang
}

// T возвращает строку по ключу и подставляет аргументы fmt.Sprintf.
func (t *Translator) T(key string, args ...any) string {
	msg, ok := messages[key]
	if !ok {
		return key
	}
	format := msg.en
	if t.lang == RU {
		format = msg.ru
	}
	if len(args) == 0 {
		return format
	}
	return fmt.Sprintf(format, args...)
}

// Detect выбирает язык: явный флаг, затем локаль системы, затем английский.
func Detect(explicit string) Lang {
	if lang, ok := DetectLocale(explicit); ok {
		return lang
	}
	return EN
}

// DetectLocale возвращает язык, если он задан явно или распознан в локали.
// Второе значение false означает, что локаль не даёт однозначного ответа.
func DetectLocale(explicit string) (Lang, bool) {
	switch strings.ToLower(strings.TrimSpace(explicit)) {
	case "ru":
		return RU, true
	case "en":
		return EN, true
	}
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG", "LANGUAGE"} {
		if lang, ok := parseLocale(os.Getenv(key)); ok {
			return lang, true
		}
	}
	return 0, false
}

// DetectForCountry сначала смотрит локаль, а если она неопределённая —
// включает русский для стран с массовым русскоязычным населением.
func DetectForCountry(explicit, countryCode string) Lang {
	if lang, ok := DetectLocale(explicit); ok {
		return lang
	}
	if RussianSpeakingCountry(countryCode) {
		return RU
	}
	return EN
}

// RussianSpeakingCountry сообщает, входит ли страна в число стран, где
// русский язык широко используется как язык общения.
func RussianSpeakingCountry(code string) bool {
	switch strings.ToUpper(strings.TrimSpace(code)) {
	case "RU", "BY", "KZ", "KG", "TJ", "UZ", "AM", "AZ", "MD", "UA", "GE":
		return true
	default:
		return false
	}
}

func parseLocale(value string) (Lang, bool) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return 0, false
	}
	// LANGUAGE может содержать список: "ru:en".
	value, _, _ = strings.Cut(value, ":")
	// "ru_RU.UTF-8" -> "ru"
	value, _, _ = strings.Cut(value, ".")
	value, _, _ = strings.Cut(value, "_")
	switch value {
	case "ru":
		return RU, true
	case "en":
		return EN, true
	}
	return 0, false
}

type message struct {
	ru string
	en string
}

var messages = map[string]message{
	"usageDesc": {
		ru: "spidometr — замер скорости интернет-соединения",
		en: "spidometr — internet connection speed test",
	},
	"usageLine": {
		ru: "Использование:\n  spidometr [флаги]",
		en: "Usage:\n  spidometr [flags]",
	},
	"usageFlags": {
		ru: "Флаги:",
		en: "Flags:",
	},
	"sourcesHeader": {
		ru: "Источники:",
		en: "Sources:",
	},
	"flagSource": {
		ru: "идентификатор источника; без флага — интерактивный выбор в терминале",
		en: "source ID; when omitted, choose interactively in a terminal",
	},
	"flagThreads": {
		ru: "количество параллельных потоков",
		en: "number of parallel streams",
	},
	"flagTime": {
		ru: "длительность каждой фазы (скачивание/загрузка) в секундах",
		en: "duration of each phase (download/upload) in seconds",
	},
	"flagList": {
		ru: "показать доступные источники и выйти",
		en: "list available sources and exit",
	},
	"flagNoUpload": {
		ru: "не измерять скорость отдачи",
		en: "skip upload measurement",
	},
	"flagPlain": {
		ru: "не использовать интерактивный интерфейс",
		en: "disable the interactive UI",
	},
	"flagVersion": {
		ru: "показать версию и выйти",
		en: "show version and exit",
	},
	"sourceLine": {
		ru: "Источник: %s",
		en: "Source: %s",
	},
	"streamsLine": {
		ru: "Потоков: %d | Фаза: %d с",
		en: "Streams: %d | Phase: %d s",
	},
	"uploadAPILine": {
		ru: "Upload API: %s",
		en: "Upload API: %s",
	},
	"phaseDownload": {
		ru: "📥 Скачивание",
		en: "📥 Download",
	},
	"phaseUpload": {
		ru: "📤 Загрузка",
		en: "📤 Upload",
	},
	"resultsTitle": {
		ru: "🏁 Результаты:",
		en: "🏁 Results:",
	},
	"downloadSpeed": {
		ru: "Download: %.2f Мбит/с",
		en: "Download: %.2f Mbit/s",
	},
	"uploadSpeed": {
		ru: "Upload:   %.2f Мбит/с",
		en: "Upload:   %.2f Mbit/s",
	},
	"downloaded": {
		ru: "Скачано: %.1f МБ",
		en: "Downloaded: %.1f MB",
	},
	"uploaded": {
		ru: "Отправлено: %.1f МБ",
		en: "Uploaded: %.1f MB",
	},
	"warnPartial": {
		ru: "⚠️  Ошибок запросов: %d (первая: %v)",
		en: "⚠️  Request errors: %d (first: %v)",
	},
	"warnNoUpload": {
		ru: "⚠️  Для источника не задан upload API, скорость отдачи не измеряется",
		en: "⚠️  No upload API for this source, skipping upload measurement",
	},
	"warnUploadFailed": {
		ru: "⚠️  Upload API не принял данные: %v",
		en: "⚠️  Upload API accepted no data: %v",
	},
	"errUnknownSource": {
		ru: "неизвестный источник %q, доступны: %s",
		en: "unknown source %q, available: %s",
	},
	"errThreads": {
		ru: "флаг -threads должен быть от 1 до 256, получено %d",
		en: "flag -threads must be between 1 and 256, got %d",
	},
	"errTime": {
		ru: "флаг -time должен быть от 1 до 3600 секунд, получено %d",
		en: "flag -time must be between 1 and 3600 seconds, got %d",
	},
	"errNoData": {
		ru: "источник не отдал данные",
		en: "source returned no data",
	},
	"errAllFailed": {
		ru: "ни один запрос не удался: %w",
		en: "all requests failed: %w",
	},
	"menuTitle": {
		ru: "Выберите API-источник:",
		en: "Choose an API source:",
	},
	"menuPrompt": {
		ru: "Номер [1]: ",
		en: "Number [1]: ",
	},
	"menuInvalid": {
		ru: "Нет такого пункта, попробуйте ещё раз.",
		en: "Invalid choice, try again.",
	},
	"listTitle": {
		ru: "Доступные источники:",
		en: "Available sources:",
	},
	"listNoUpload": {
		ru: "upload: нет",
		en: "upload: none",
	},
	"listUpload": {
		ru: "upload: %s",
		en: "upload: %s",
	},
	"versionLine": {
		ru: "spidometr %s",
		en: "spidometr %s",
	},

	// Интерактивный интерфейс.
	"tuiStatusReady": {
		ru: "● ГОТОВ",
		en: "● READY",
	},
	"tuiStatusDownload": {
		ru: "● СКАЧИВАНИЕ",
		en: "● DOWNLOAD",
	},
	"tuiStatusUpload": {
		ru: "● ЗАГРУЗКА",
		en: "● UPLOAD",
	},
	"tuiStatusDone": {
		ru: "✓ ГОТОВО",
		en: "✓ DONE",
	},
	"tuiStatusError": {
		ru: "✗ ОШИБКА",
		en: "✗ ERROR",
	},
	"tuiStatusStopped": {
		ru: "■ ОСТАНОВЛЕНО",
		en: "■ STOPPED",
	},
	"tuiLabelUpload": {
		ru: "Отдача",
		en: "Upload",
	},
	"tuiSourceTitle": {
		ru: "ИСТОЧНИК",
		en: "SOURCE",
	},
	"tuiStatsTitle": {
		ru: "ПАРАМЕТРЫ",
		en: "STATS",
	},
	"tuiResultTitle": {
		ru: "РЕЗУЛЬТАТ",
		en: "RESULT",
	},
	"tuiTestDone": {
		ru: "ТЕСТ ЗАВЕРШЁН",
		en: "TEST COMPLETE",
	},
	"tuiResultDetails": {
		ru: "потоки %d · скачано %s · отдано %s · %.1f с",
		en: "threads %d · received %s · sent %s · %.1f s",
	},
	"tuiHistoryTitle": {
		ru: "ГРАФИК",
		en: "HISTORY",
	},
	"tuiTransfer": {
		ru: "передано",
		en: "transfer",
	},
	"tuiPeak": {
		ru: "пик",
		en: "peak",
	},
	"tuiDownloadTitle": {
		ru: "СКАЧИВАНИЕ",
		en: "DOWNLOAD",
	},
	"tuiUploadTitle": {
		ru: "ЗАГРУЗКА",
		en: "UPLOAD",
	},
	"tuiPhaseLabel": {
		ru: "фаза",
		en: "phase",
	},
	"tuiPhaseDone": {
		ru: "готово",
		en: "done",
	},
	"tuiPhaseError": {
		ru: "ошибка",
		en: "error",
	},
	"tuiPhaseStopped": {
		ru: "остановлено",
		en: "stopped",
	},
	"tuiPending": {
		ru: "ожидает",
		en: "waiting",
	},
	"tuiAvg": {
		ru: "сред. %s · %s",
		en: "avg %s · %s",
	},
	"tuiPhase": {
		ru: "%s · %.1f / %d с",
		en: "%s · %.1f / %d s",
	},
	"tuiThreads": {
		ru: "потоки",
		en: "threads",
	},
	"tuiUploadOn": {
		ru: "вкл",
		en: "on",
	},
	"tuiUploadOff": {
		ru: "выкл",
		en: "off",
	},
	"tuiNotMeasured": {
		ru: "не измеряется",
		en: "not measured",
	},
	"tuiMbit": {
		ru: "Мбит/с",
		en: "Mbit/s",
	},
	"tuiMB": {
		ru: "МБ",
		en: "MB",
	},
	"tuiNoData": {
		ru: "нет данных",
		en: "no data",
	},
	"tuiStarting": {
		ru: "запуск…",
		en: "starting…",
	},
	"tuiError": {
		ru: "Ошибка: %v",
		en: "Error: %v",
	},
	"tuiModalTitle": {
		ru: "Выбор API",
		en: "Choose API",
	},
	"tuiModalHint": {
		ru: "↑/↓ — выбор · Enter — применить · Esc — отмена",
		en: "↑/↓ select · Enter apply · Esc cancel",
	},
	"tuiHelpTitle": {
		ru: "Горячие клавиши",
		en: "Keyboard shortcuts",
	},
	"tuiHelpSource": {
		ru: "s — API-источник",
		en: "s — API source",
	},
	"tuiHelpThreads": {
		ru: "p — потоки: 1 / 2 / 4 / 8 / 16",
		en: "p — threads: 1 / 2 / 4 / 8 / 16",
	},
	"tuiHelpTime": {
		ru: "d — время фазы: 5 / 7 / 10 / 15 / 30 / 60 с",
		en: "d — phase time: 5 / 7 / 10 / 15 / 30 / 60 s",
	},
	"tuiHelpUpload": {
		ru: "u — замер отдачи вкл/выкл",
		en: "u — toggle upload measurement",
	},
	"tuiHelpRestart": {
		ru: "r — заново (перезапустить замер)",
		en: "r — restart test",
	},
	"tuiHelpHelp": {
		ru: "h — помощь",
		en: "h — help",
	},
	"tuiHelpQuit": {
		ru: "q / Esc — выход",
		en: "q / Esc — quit",
	},
	"tuiFootSource": {
		ru: "API",
		en: "API",
	},
	"tuiFootThreads": {
		ru: "потоки",
		en: "threads",
	},
	"tuiFootTime": {
		ru: "время",
		en: "time",
	},
	"tuiFootUpload": {
		ru: "вкл",
		en: "upload",
	},
	"tuiFootRestart": {
		ru: "заново",
		en: "restart",
	},
	"tuiFootTheme": {
		ru: "тема",
		en: "theme",
	},
	"tuiFootLayout": {
		ru: "макет",
		en: "layout",
	},
	"tuiFootInfo": {
		ru: "инфо",
		en: "info",
	},
	"tuiFootHelp": {
		ru: "помощь",
		en: "help",
	},
	"tuiFootQuit": {
		ru: "выход",
		en: "quit",
	},
	"tuiHelpHint": {
		ru: "Esc / h — закрыть",
		en: "Esc / h — close",
	},
	"tuiSourceUpload": {
		ru: "отдача: %s",
		en: "upload: %s",
	},
	"tuiSourceNoUpload": {
		ru: "отдача: нет",
		en: "upload: none",
	},
	"hkUpload": {
		ru: "отдача",
		en: "up",
	},
	"latencyLine": {
		ru: "Задержка (HTTP): %s",
		en: "Latency (HTTP): %s",
	},
	"latencyTitle": {
		ru: "Задержка",
		en: "Latency",
	},
	"warnLatency": {
		ru: "⚠️  Не удалось измерить задержку: %v",
		en: "⚠️  Could not measure latency: %v",
	},
	"flagTheme": {
		ru: "тема оформления: classic, dracula, ayu, tokyonight, orange, nord",
		en: "UI theme: classic, dracula, ayu, tokyonight, orange, nord",
	},
	"flagLayout": {
		ru: "вариант расположения: 0 — соединение, результат, источник, скорость, 1 — исходный с информацией, 2 — панель сбоку, 3 — с историей, 4 — крупные карточки",
		en: "layout: 0 — connection, result, source, speed, 1 — original with info, 2 — sidebar, 3 — with history, 4 — large cards",
	},
	"errUnknownTheme": {
		ru: "неизвестная тема %q, доступны: %s",
		en: "unknown theme %q, available: %s",
	},
	"tuiThemeTitle": {
		ru: "Тема оформления",
		en: "Theme",
	},
	"tuiConnectionTitle": {
		ru: "СОЕДИНЕНИЕ",
		en: "CONNECTION",
	},
	"geoLoading": {
		ru: "определяется…",
		en: "detecting…",
	},
	"geoUnavailable": {
		ru: "недоступно",
		en: "unavailable",
	},
	"geoIP": {
		ru: "IP",
		en: "IP",
	},
	"geoProvider": {
		ru: "Провайдер",
		en: "Provider",
	},
	"geoASN": {
		ru: "ASN",
		en: "ASN",
	},
	"geoCity": {
		ru: "Город",
		en: "City",
	},
	"geoRegion": {
		ru: "Регион",
		en: "Region",
	},
	"geoCountry": {
		ru: "Страна",
		en: "Country",
	},
	"geoSource": {
		ru: "Источник",
		en: "Source",
	},
	"geoHost": {
		ru: "Компьютер",
		en: "Computer",
	},
	"geoOS": {
		ru: "ОС",
		en: "OS",
	},
	"geoTerminal": {
		ru: "Терминал",
		en: "Terminal",
	},
	"geoProxy": {
		ru: "Прокси",
		en: "Proxy",
	},
	"geoApp": {
		ru: "Клиент",
		en: "Client",
	},
	"geoLatency": {
		ru: "Задержка",
		en: "Latency",
	},
	"proxyNone": {
		ru: "не используется",
		en: "not used",
	},
	"tuiLatency": {
		ru: "задержка",
		en: "latency",
	},
	"tuiMs": {
		ru: "мс",
		en: "ms",
	},
	"tuiHelpTheme": {
		ru: "t — тема оформления",
		en: "t — theme",
	},
	"tuiHelpLayout": {
		ru: "l — макет (расположение элементов)",
		en: "l — layout",
	},
	"tuiHelpInfo": {
		ru: "i — инфо о соединении",
		en: "i — info",
	},
}
