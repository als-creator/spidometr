package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/schollz/progressbar/v3"
	"golang.org/x/term"

	"github.com/als-creator/spidometr/engine"
	"github.com/als-creator/spidometr/internal/i18n"
	"github.com/als-creator/spidometr/internal/settings"
	"github.com/als-creator/spidometr/internal/theme"
	"github.com/als-creator/spidometr/internal/tui"
	"github.com/als-creator/spidometr/sources"
)

// version подставляется при релизной сборке через -ldflags "-X main.version=...".
var version = "1.0.0"

type options struct {
	source  sources.SpeedSource
	threads int
	seconds int
	upload  bool
}

func main() {
	tr := i18n.New(i18n.Detect("auto"))
	flag.Usage = func() { usage(tr) }
	if err := run(tr); err != nil {
		fmt.Fprintf(os.Stderr, "❌ %v\n", err)
		os.Exit(1)
	}
}

func run(tr *i18n.Translator) error {
	sourceID := flag.String("source", "", tr.T("flagSource"))
	threads := flag.Int("threads", 4, tr.T("flagThreads"))
	seconds := flag.Int("time", 7, tr.T("flagTime"))
	themeID := flag.String("theme", "", tr.T("flagTheme"))
	layoutID := flag.Int("layout", -1, tr.T("flagLayout"))
	list := flag.Bool("list", false, tr.T("flagList"))
	noUpload := flag.Bool("no-upload", false, tr.T("flagNoUpload"))
	plain := flag.Bool("plain", false, tr.T("flagPlain"))
	showVersion := flag.Bool("version", false, tr.T("flagVersion"))
	flag.Parse()

	// Значение -theme могло быть указано после вычисления языка для usage.
	st, _ := settings.Load()

	themeName := *themeID
	if themeName == "" {
		themeName = st.Theme
	}
	if themeName == "" {
		themeName = theme.Default().ID
	}
	if _, ok := theme.Get(themeName); !ok {
		return fmt.Errorf(tr.T("errUnknownTheme"), themeName, strings.Join(themeIDs(), ", "))
	}
	layout := *layoutID
	if layout < 0 {
		layout = st.Layout
	}
	if layout < 0 || layout >= tui.LayoutCount {
		layout = 0
	}

	if *showVersion {
		fmt.Println(tr.T("versionLine", version))
		return nil
	}
	if *list {
		printSources(tr)
		return nil
	}
	if *threads < 1 || *threads > 256 {
		return fmt.Errorf(tr.T("errThreads"), *threads)
	}
	if *seconds < 1 || *seconds > 3600 {
		return fmt.Errorf(tr.T("errTime"), *seconds)
	}

	src, err := selectSource(tr, *sourceID)
	if err != nil {
		return err
	}

	opts := options{
		source:  src,
		threads: *threads,
		seconds: *seconds,
		upload:  !*noUpload,
	}

	// Интерактивный TUI только в настоящем терминале; скрипты и Docker
	// продолжают получать обычный построчный вывод.
	if !*plain && interactive() {
		return tui.Run(tr, tui.Options{
			Version: version,
			Source:  src,
			Threads: *threads,
			Seconds: *seconds,
			Upload:  !*noUpload,
			Theme:   themeName,
			Layout:  layout,
		})
	}
	return runCLI(tr, opts)
}

func runCLI(tr *i18n.Translator, opts options) error {
	fmt.Printf("🚀 %s\n", tr.T("versionLine", version))
	fmt.Println(tr.T("sourceLine", opts.source.Name()))
	fmt.Println(tr.T("streamsLine", opts.threads, opts.seconds))
	if opts.source.UploadURL() != "" {
		fmt.Println(tr.T("uploadAPILine", opts.source.UploadName()))
	}
	fmt.Println()

	baseCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	probeCtx, cancelProbe := context.WithTimeout(baseCtx, 5*time.Second)
	latency, latencyErr := engine.ProbeLatency(probeCtx, opts.source.DownloadURL())
	cancelProbe()
	if latencyErr != nil {
		fmt.Fprintln(os.Stderr, tr.T("warnLatency", latencyErr))
	} else {
		fmt.Println(tr.T("latencyLine", formatLatency(tr, latency)))
	}
	fmt.Println()

	// --- Скачивание ---
	downloadCtx, cancelDownload := context.WithTimeout(baseCtx, duration(opts.seconds))
	bar := newProgressBar(tr.T("phaseDownload"))
	start := time.Now()
	download := engine.RunDownload(downloadCtx, opts.source.DownloadURL(), opts.threads, func(n int) {
		_ = bar.Add(n)
	})
	downloadTime := time.Since(start)
	cancelDownload()
	fmt.Println()

	if download.Bytes == 0 {
		if download.FirstErr != nil {
			return fmt.Errorf(tr.T("errAllFailed"), download.FirstErr)
		}
		return errors.New(tr.T("errNoData"))
	}

	// --- Загрузка ---
	var (
		upload      engine.Stats
		uploadTime  time.Duration
		uploadShown bool
		uploadURL   = opts.source.UploadURL()
	)
	switch {
	case !opts.upload || uploadURL == "":
		if opts.upload {
			fmt.Fprintln(os.Stderr, tr.T("warnNoUpload"))
		}
	case baseCtx.Err() != nil:
		// Замер прерван пользователем — не начинаем вторую фазу.
	default:
		uploadCtx, cancelUpload := context.WithTimeout(baseCtx, duration(opts.seconds))
		bar = newProgressBar(tr.T("phaseUpload"))
		start = time.Now()
		upload = engine.RunUpload(uploadCtx, uploadURL, opts.threads, func(n int) {
			_ = bar.Add(n)
		})
		uploadTime = time.Since(start)
		cancelUpload()
		fmt.Println()
		uploadShown = true
	}

	fmt.Println(tr.T("resultsTitle"))
	if latencyErr == nil {
		fmt.Printf("  %s: %s\n", tr.T("latencyTitle"), formatLatency(tr, latency))
	}
	fmt.Printf("  %s\n", tr.T("downloadSpeed", speedMbps(download.Bytes, downloadTime)))
	if uploadShown {
		fmt.Printf("  %s\n", tr.T("uploadSpeed", speedMbps(upload.Bytes, uploadTime)))
	}
	fmt.Printf("  %s\n", tr.T("downloaded", megabytes(download.Bytes)))
	if uploadShown {
		fmt.Printf("  %s\n", tr.T("uploaded", megabytes(upload.Bytes)))
	}

	if download.Errors > 0 {
		fmt.Fprintf(os.Stderr, tr.T("warnPartial"), download.Errors, download.FirstErr)
		fmt.Fprintln(os.Stderr)
	}
	if uploadShown {
		if upload.Bytes == 0 {
			fmt.Fprintf(os.Stderr, tr.T("warnUploadFailed"), upload.FirstErr)
			fmt.Fprintln(os.Stderr)
		} else if upload.Errors > 0 {
			fmt.Fprintf(os.Stderr, tr.T("warnPartial"), upload.Errors, upload.FirstErr)
			fmt.Fprintln(os.Stderr)
		}
	}
	return nil
}

func newProgressBar(description string) *progressbar.ProgressBar {
	return progressbar.NewOptions(-1,
		progressbar.OptionSetDescription(description),
		progressbar.OptionSetWriter(os.Stderr),
		progressbar.OptionShowBytes(true),
		progressbar.OptionSetWidth(15),
		progressbar.OptionThrottle(80*time.Millisecond),
		progressbar.OptionSpinnerType(14),
		progressbar.OptionFullWidth(),
		progressbar.OptionSetPredictTime(false),
	)
}

func selectSource(tr *i18n.Translator, id string) (sources.SpeedSource, error) {
	if id == "" {
		return sources.All()[0], nil
	}
	src, ok := sources.Get(id)
	if !ok {
		return nil, fmt.Errorf(tr.T("errUnknownSource"), id, strings.Join(sourceIDs(), ", "))
	}
	return src, nil
}

func printSources(tr *i18n.Translator) {
	fmt.Println(tr.T("listTitle"))
	printEntries(os.Stdout, tr)
}

func uploadLabel(tr *i18n.Translator, src sources.SpeedSource) string {
	if src.UploadURL() == "" {
		return tr.T("listNoUpload")
	}
	return tr.T("listUpload", src.UploadName())
}

func sourceIDs() []string {
	all := sources.All()
	ids := make([]string, 0, len(all))
	for _, src := range all {
		ids = append(ids, src.ID())
	}
	return ids
}

func usage(tr *i18n.Translator) {
	out := flag.CommandLine.Output()
	fmt.Fprintln(out, tr.T("usageDesc"))
	fmt.Fprintln(out)
	fmt.Fprintln(out, tr.T("usageLine"))
	fmt.Fprintln(out)
	fmt.Fprintln(out, tr.T("usageFlags"))
	flag.PrintDefaults()
	fmt.Fprintln(out)
	fmt.Fprintln(out, tr.T("sourcesHeader"))
	printEntries(out, tr)
}

func printEntries(out io.Writer, tr *i18n.Translator) {
	for _, src := range sources.All() {
		fmt.Fprintf(out, "  %-12s %s (%s)\n", src.ID(), src.Name(), uploadLabel(tr, src))
	}
}

func interactive() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}

func duration(seconds int) time.Duration {
	return time.Duration(seconds) * time.Second
}

func speedMbps(bytes int64, elapsed time.Duration) float64 {
	if elapsed <= 0 {
		return 0
	}
	return float64(bytes) * 8 / 1_000_000 / elapsed.Seconds()
}

func megabytes(bytes int64) float64 {
	return float64(bytes) / (1024 * 1024)
}

func formatLatency(tr *i18n.Translator, d time.Duration) string {
	return fmt.Sprintf("%.0f %s", d.Seconds()*1000, tr.T("tuiMs"))
}

func themeIDs() []string {
	themes := theme.All()
	ids := make([]string, 0, len(themes))
	for _, th := range themes {
		ids = append(ids, th.ID)
	}
	return ids
}
