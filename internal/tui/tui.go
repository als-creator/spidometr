// Package tui — интерактивный терминальный интерфейс spidometr.
package tui

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/als-creator/spidometr/engine"
	"github.com/als-creator/spidometr/internal/i18n"
	"github.com/als-creator/spidometr/internal/netinfo"
	"github.com/als-creator/spidometr/internal/settings"
	"github.com/als-creator/spidometr/internal/sysinfo"
	"github.com/als-creator/spidometr/internal/theme"
	"github.com/als-creator/spidometr/sources"
)

// Options — начальные настройки из CLI-флагов.
type Options struct {
	Version string
	Source  sources.SpeedSource
	Threads int
	Seconds int
	Upload  bool
	Theme   string
	Layout  int
}

type phase int

const (
	phaseReady phase = iota
	phaseDownload
	phaseUpload
	phaseDone
	phaseError
	phaseStopped
)

// Runner управляет состоянием замера и отрисовкой.
type Runner struct {
	tr      *i18n.Translator
	app     *tview.Application
	pages   *tview.Pages
	version string
	th      theme.Theme
	themeID string
	layout  int
	sys     sysinfo.Info

	headerTitle       *tview.TextView
	headerStatus      *tview.TextView
	headerVersion     *tview.TextView
	info              *tview.TextView
	downloadCard      *tview.TextView
	uploadCard        *tview.TextView
	historyCard       *tview.TextView
	stats             *tview.TextView
	footerHelp        *tview.TextView
	main              tview.Primitive
	list              *tview.List
	themeList         *tview.List
	help              *tview.TextView
	connection        *tview.TextView
	connectionCompact *tview.TextView
	geoModal          *tview.TextView

	modalOpen   bool
	activeModal string

	baseCtx context.Context
	stopAll context.CancelFunc
	cancel  context.CancelFunc
	runMu   sync.Mutex
	wg      sync.WaitGroup

	mu      sync.Mutex
	source  sources.SpeedSource
	threads int
	seconds int
	upload  bool

	phase         phase
	phaseStart    time.Time
	phaseDuration time.Duration
	phaseElapsed  time.Duration

	liveBytes atomic.Int64
	lastTick  time.Time
	lastBytes int64

	downloadStats engine.Stats
	uploadStats   engine.Stats
	downloadTime  time.Duration
	uploadTime    time.Duration
	downloadSpeed float64
	uploadSpeed   float64
	downloadPeak  float64
	uploadPeak    float64
	downloadDone  bool
	uploadDone    bool
	err           error
	uploadErr     error

	downloadHistory []float64
	uploadHistory   []float64
	latency         time.Duration
	latencyErr      error

	geo        netinfo.Info
	geoErr     error
	geoLoading bool
	country    string
}

// Run запускает интерактивный интерфейс и блокируется до выхода.
func Run(tr *i18n.Translator, opts Options) error {
	app := tview.NewApplication()
	ctx, stopAll := context.WithCancel(context.Background())

	th, ok := theme.Get(opts.Theme)
	if !ok {
		th = theme.Default()
	}
	layout := opts.Layout
	if layout < 0 || layout >= LayoutCount {
		layout = 0
	}
	r := &Runner{
		tr:         tr,
		app:        app,
		version:    opts.Version,
		th:         th,
		themeID:    th.ID,
		layout:     layout,
		sys:        sysinfo.Collect(),
		source:     opts.Source,
		threads:    opts.Threads,
		seconds:    opts.Seconds,
		upload:     opts.Upload,
		baseCtx:    ctx,
		stopAll:    stopAll,
		geoLoading: true,
	}

	app.SetRoot(r.build(), true)
	app.EnableMouse(true)
	r.start()
	go r.loop()
	go r.watchSignals()
	go r.loadGeo()

	err := app.Run()
	stopAll()
	r.wg.Wait()
	return err
}

func (r *Runner) build() *tview.Pages {
	header := r.buildHeader()
	r.main = r.buildMain()
	footer := r.buildFooter()

	root := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(header, 1, 0, false).
		AddItem(r.main, 0, 1, false).
		AddItem(footer, 1, 0, false)
	root.SetBackgroundColor(tcell.GetColor(r.th.Background))

	r.pages = tview.NewPages().
		AddPage("main", root, true, true).
		AddPage("sources", r.buildSourceModal(), true, false).
		AddPage("themes", r.buildThemeModal(), true, false).
		AddPage("help", r.buildHelpModal(), true, false)

	r.app.SetInputCapture(r.handleKey)
	r.app.SetFocus(r.main)
	return r.pages
}

func (r *Runner) buildHeader() tview.Primitive {
	r.headerTitle = tview.NewTextView().
		SetDynamicColors(true).
		SetText(fmt.Sprintf("[%s::b]spidometr[-::-]", r.th.Foreground)).
		SetTextAlign(tview.AlignLeft)
	r.headerTitle.SetBackgroundColor(tcell.GetColor(r.th.HeaderBg))
	r.headerTitle.SetTextColor(tcell.GetColor(r.th.Foreground))

	r.headerStatus = tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignRight)
	r.headerStatus.SetBackgroundColor(tcell.GetColor(r.th.HeaderBg))
	r.headerStatus.SetTextColor(tcell.GetColor(r.th.Foreground))

	r.headerVersion = tview.NewTextView().
		SetDynamicColors(true).
		SetText(fmt.Sprintf("v%s ", r.version)).
		SetTextAlign(tview.AlignRight)
	r.headerVersion.SetBackgroundColor(tcell.GetColor(r.th.HeaderBg))
	r.headerVersion.SetTextColor(tcell.GetColor(r.th.Muted))

	header := tview.NewFlex().
		AddItem(r.headerTitle, 0, 1, false).
		AddItem(r.headerStatus, 16, 0, false).
		AddItem(r.headerVersion, 12, 0, false)
	header.SetBackgroundColor(tcell.GetColor(r.th.HeaderBg))
	return header
}

// LayoutCount — число вариантов расположения, переключаемых клавишей l.
const LayoutCount = 5

func (r *Runner) buildMain() tview.Primitive {
	r.buildCards()
	switch r.layout {
	case 1:
		// Исходное расположение плюс карточка соединения.
		return newCentered(r.buildDashboardWithConnection(), 88, 24, r.th.Background)
	case 2:
		// Сведения о соединении отдельной панелью сбоку.
		return r.buildSidebarLayout()
	case 3:
		// Исходный вид с гистограммой истории.
		return newCentered(r.buildDashboardGrid(), 88, 24, r.th.Background)
	case 4:
		// Крупные карточки скорости друг под другом.
		return newCentered(r.buildFocusGrid(), 88, 24, r.th.Background)
	default:
		// Дефолт: соединение, результат, источник, скачивание и загрузка.
		return newCentered(r.buildDefaultGrid(), 88, 24, r.th.Background)
	}
}

func (r *Runner) buildSidebarLayout() tview.Primitive {
	// На узком экране панель сбоку не помещается — показываем карточку
	// соединения внутри основной сетки.
	narrow := newCentered(r.buildDashboardWithConnection(), 88, 24, r.th.Background)
	wide := tview.NewFlex().
		AddItem(newCentered(r.buildOriginalGrid(), 76, 24, r.th.Background), 0, 1, false).
		AddItem(r.buildConnectionPanel(), 34, 0, false)
	return newResponsiveFull(narrow, wide, 100)
}

func (r *Runner) buildOriginalGrid() tview.Primitive {
	grid := tview.NewGrid().
		SetRows(4, 1, -1, 1, 5).
		SetColumns(0, 1, 0)
	grid.SetBackgroundColor(tcell.GetColor(r.th.Background))
	grid.AddItem(r.info, 0, 0, 1, 3, 0, 0, false)
	grid.AddItem(r.downloadCard, 2, 0, 1, 1, 0, 0, false)
	grid.AddItem(r.uploadCard, 2, 2, 1, 1, 0, 0, false)
	grid.AddItem(r.stats, 4, 0, 1, 3, 0, 0, false)
	return grid
}

func (r *Runner) buildCards() {
	r.connection = nil
	r.connectionCompact = nil
	r.info = r.newCard(r.tr.T("tuiSourceTitle"), r.th.Highlight, tview.AlignLeft)
	r.downloadCard = r.newCard(r.tr.T("tuiDownloadTitle"), r.th.Download, tview.AlignCenter)
	r.uploadCard = r.newCard(r.tr.T("tuiUploadTitle"), r.th.Upload, tview.AlignCenter)
	r.historyCard = r.newCard(r.tr.T("tuiHistoryTitle"), r.th.Foreground, tview.AlignLeft)
	r.stats = r.newCard(r.tr.T("tuiStatsTitle"), r.th.Foreground, tview.AlignLeft)
}

func (r *Runner) buildConnectionPanel() *tview.TextView {
	r.connection = r.newCard(r.tr.T("tuiConnectionTitle"), r.th.Highlight, tview.AlignLeft)
	r.connection.SetWrap(true)
	return r.connection
}

func (r *Runner) buildDefaultGrid() tview.Primitive {
	r.connectionCompact = r.newCard(r.tr.T("tuiConnectionTitle"), r.th.Highlight, tview.AlignLeft)
	r.connectionCompact.SetWrap(false)

	grid := tview.NewGrid().
		SetRows(4, 1, 5, 1, 4, 1, -1).
		SetColumns(0, 1, 0)
	grid.SetBackgroundColor(tcell.GetColor(r.th.Background))
	grid.AddItem(r.connectionCompact, 0, 0, 1, 3, 0, 0, false)
	grid.AddItem(r.stats, 2, 0, 1, 3, 0, 0, false)
	grid.AddItem(r.info, 4, 0, 1, 3, 0, 0, false)
	grid.AddItem(r.downloadCard, 6, 0, 1, 1, 0, 0, false)
	grid.AddItem(r.uploadCard, 6, 2, 1, 1, 0, 0, false)
	return grid
}

func (r *Runner) buildDashboardGrid() tview.Primitive {
	fullGrid := tview.NewGrid().
		SetRows(4, 1, -1, 1, 4, 1, 5).
		SetColumns(0, 1, 0)
	fullGrid.SetBackgroundColor(tcell.GetColor(r.th.Background))
	fullGrid.AddItem(r.info, 0, 0, 1, 3, 0, 0, false)
	fullGrid.AddItem(r.downloadCard, 2, 0, 1, 1, 0, 0, false)
	fullGrid.AddItem(r.uploadCard, 2, 2, 1, 1, 0, 0, false)
	fullGrid.AddItem(r.historyCard, 4, 0, 1, 3, 0, 0, false)
	fullGrid.AddItem(r.stats, 6, 0, 1, 3, 0, 0, false)

	compactGrid := tview.NewGrid().
		SetRows(4, 1, -1, 1, 5).
		SetColumns(0, 1, 0)
	compactGrid.SetBackgroundColor(tcell.GetColor(r.th.Background))
	compactGrid.AddItem(r.info, 0, 0, 1, 3, 0, 0, false)
	compactGrid.AddItem(r.downloadCard, 2, 0, 1, 1, 0, 0, false)
	compactGrid.AddItem(r.uploadCard, 2, 2, 1, 1, 0, 0, false)
	compactGrid.AddItem(r.stats, 4, 0, 1, 3, 0, 0, false)

	return newResponsiveDashboard(fullGrid, compactGrid, 22)
}

func (r *Runner) buildDashboardWithConnection() tview.Primitive {
	r.connectionCompact = r.newCard(r.tr.T("tuiConnectionTitle"), r.th.Highlight, tview.AlignLeft)
	r.connectionCompact.SetWrap(false)

	grid := tview.NewGrid().
		SetRows(4, 1, -1, 1, 4, 1, 5).
		SetColumns(0, 1, 0)
	grid.SetBackgroundColor(tcell.GetColor(r.th.Background))
	grid.AddItem(r.info, 0, 0, 1, 3, 0, 0, false)
	grid.AddItem(r.downloadCard, 2, 0, 1, 1, 0, 0, false)
	grid.AddItem(r.uploadCard, 2, 2, 1, 1, 0, 0, false)
	grid.AddItem(r.connectionCompact, 4, 0, 1, 3, 0, 0, false)
	grid.AddItem(r.stats, 6, 0, 1, 3, 0, 0, false)
	return grid
}

func (r *Runner) buildFocusGrid() tview.Primitive {
	grid := tview.NewGrid().
		SetRows(4, 1, -1, 1, -1).
		SetColumns(0)
	grid.SetBackgroundColor(tcell.GetColor(r.th.Background))
	grid.AddItem(r.info, 0, 0, 1, 1, 0, 0, false)
	grid.AddItem(r.downloadCard, 2, 0, 1, 1, 0, 0, false)
	grid.AddItem(r.uploadCard, 4, 0, 1, 1, 0, 0, false)
	return grid
}

func (r *Runner) newCard(title, titleColor string, align int) *tview.TextView {
	card := tview.NewTextView().
		SetDynamicColors(true).
		SetWrap(false).
		SetTextAlign(align)
	card.SetBorder(true).
		SetTitle(" "+title+" ").
		SetTitleAlign(tview.AlignCenter).
		SetTitleColor(tcell.GetColor(titleColor)).
		SetBorderColor(tcell.GetColor(r.th.Borders)).
		SetBorderPadding(0, 0, 1, 1)
	card.SetBackgroundColor(tcell.GetColor(r.th.Background))
	card.SetTextColor(tcell.GetColor(r.th.Foreground))
	return card
}

func (r *Runner) buildFooter() tview.Primitive {
	r.footerHelp = tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter).
		SetWrap(false)
	r.footerHelp.SetBackgroundColor(tcell.GetColor(r.th.HelpBg))
	r.footerHelp.SetTextColor(tcell.GetColor(r.th.HelpFg))

	footer := tview.NewFlex().
		AddItem(r.footerHelp, 0, 1, false)
	footer.SetBackgroundColor(tcell.GetColor(r.th.Background))
	return footer
}

func (r *Runner) newModalList() *tview.List {
	list := tview.NewList().
		SetWrapAround(true).
		SetHighlightFullLine(true)
	bg := tcell.GetColor(r.th.ModalBg)
	list.SetBackgroundColor(bg)
	list.SetMainTextStyle(tcell.StyleDefault.
		Background(bg).
		Foreground(tcell.GetColor(r.th.Foreground)))
	list.SetSecondaryTextStyle(tcell.StyleDefault.
		Background(bg).
		Foreground(tcell.GetColor(r.th.Muted)))
	list.SetSelectedStyle(tcell.StyleDefault.
		Background(tcell.GetColor(r.th.Highlight)).
		Foreground(tcell.GetColor(r.th.Background)))
	return list
}

func (r *Runner) modalHint() *tview.TextView {
	hint := tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter).
		SetText(fmt.Sprintf("[%s]%s[-]", r.th.Muted, r.tr.T("tuiModalHint")))
	hint.SetBackgroundColor(tcell.GetColor(r.th.ModalBg))
	return hint
}

func (r *Runner) closeHint() *tview.TextView {
	hint := tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter).
		SetText(fmt.Sprintf("[%s]%s[-]", r.th.Muted, r.tr.T("tuiHelpHint")))
	hint.SetBackgroundColor(tcell.GetColor(r.th.ModalBg))
	return hint
}

func (r *Runner) buildSourceModal() tview.Primitive {
	r.list = r.newModalList()
	for _, src := range sources.All() {
		secondary := r.tr.T("tuiSourceNoUpload")
		if src.UploadURL() != "" {
			secondary = r.tr.T("tuiSourceUpload", src.UploadName())
		}
		r.list.AddItem(tview.Escape(src.Name()), tview.Escape(secondary), 0, nil)
	}
	content := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.list, 0, 2, true).
		AddItem(r.modalHint(), 1, 0, false)
	content.SetBackgroundColor(tcell.GetColor(r.th.ModalBg))
	return centeredModal(newModalBox(content, r.tr.T("tuiModalTitle"), r.th), 68, 20, r.th.Background)
}

func (r *Runner) buildThemeModal() tview.Primitive {
	r.themeList = r.newModalList()
	themes := theme.All()
	for _, th := range themes {
		r.themeList.AddItem(th.Name, th.ID, 0, nil)
	}
	for i, th := range themes {
		if th.ID == r.themeID {
			r.themeList.SetCurrentItem(i)
			break
		}
	}
	content := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.themeList, 0, 1, true).
		AddItem(r.modalHint(), 1, 0, false)
	content.SetBackgroundColor(tcell.GetColor(r.th.ModalBg))
	return centeredModal(newModalBox(content, r.tr.T("tuiThemeTitle"), r.th), 52, 17, r.th.Background)
}

func (r *Runner) buildHelpModal() tview.Primitive {
	lines := []string{
		r.tr.T("tuiHelpSource"),
		r.tr.T("tuiHelpThreads"),
		r.tr.T("tuiHelpTime"),
		r.tr.T("tuiHelpUpload"),
		r.tr.T("tuiHelpRestart"),
		r.tr.T("tuiHelpTheme"),
		r.tr.T("tuiHelpLayout"),
		r.tr.T("tuiHelpInfo"),
		r.tr.T("tuiHelpHelp"),
		r.tr.T("tuiHelpQuit"),
	}
	r.help = tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignLeft).
		SetText(strings.Join(lines, "\n") + "\n\n" +
			fmt.Sprintf("[%s]%s[-]", r.th.Muted, r.tr.T("tuiHelpHint")))
	r.help.SetBorder(true).
		SetTitle(" "+r.tr.T("tuiHelpTitle")+" ").
		SetTitleColor(tcell.GetColor(r.th.Highlight)).
		SetTitleAlign(tview.AlignCenter).
		SetBorderColor(tcell.GetColor(r.th.Highlight)).
		SetBorderPadding(1, 1, 2, 2)
	r.help.SetBackgroundColor(tcell.GetColor(r.th.ModalBg))
	r.help.SetTextColor(tcell.GetColor(r.th.Foreground))
	return centeredModal(r.help, 70, 21, r.th.Background)
}

// modalBox рисует сплошной фон и рамку, а содержимое — с отступом.
type modalBox struct {
	*tview.Box
	content tview.Primitive
	bg      string
}

func newModalBox(content tview.Primitive, title string, th theme.Theme) *modalBox {
	m := &modalBox{Box: tview.NewBox(), content: content, bg: th.ModalBg}
	m.SetBorder(true)
	m.SetTitle(" " + title + " ")
	m.SetTitleColor(tcell.GetColor(th.Highlight))
	m.SetTitleAlign(tview.AlignCenter)
	m.SetBorderColor(tcell.GetColor(th.Highlight))
	m.SetBackgroundColor(tcell.GetColor(th.ModalBg))
	return m
}

func (m *modalBox) Draw(screen tcell.Screen) {
	// Flex/Grid не заливают фон (dontClear), поэтому модалку заливаем сами,
	// иначе сквозь отступы просвечивает дашборд.
	mx, my, mw, mh := m.GetRect()
	style := tcell.StyleDefault.Background(tcell.GetColor(m.bg))
	for yy := my; yy < my+mh; yy++ {
		for xx := mx; xx < mx+mw; xx++ {
			screen.SetContent(xx, yy, ' ', nil, style)
		}
	}
	m.Box.DrawForSubclass(screen, m)
	x, y, w, h := m.GetInnerRect()
	if w > 2 && h > 2 {
		x++
		y++
		w -= 2
		h -= 2
	}
	m.content.SetRect(x, y, w, h)
	m.content.Draw(screen)
}

// centeredModal ставит модалку по центру экрана.
func centeredModal(p tview.Primitive, width, height int, bg string) tview.Primitive {
	row := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(p, width, 0, true).
		AddItem(nil, 0, 1, false)
	outer := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(nil, 0, 1, false).
		AddItem(row, height, 0, true).
		AddItem(nil, 0, 1, false)
	outer.SetBackgroundColor(tcell.GetColor(bg))
	return outer
}

// responsiveDashboard выбирает полную или компактную сетку по высоте:
// на высоком терминале добавляется карточка с графиком скорости.
type responsiveDashboard struct {
	*tview.Box
	full          tview.Primitive
	compact       tview.Primitive
	minFullHeight int
}

func newResponsiveDashboard(full, compact tview.Primitive, minFullHeight int) *responsiveDashboard {
	return &responsiveDashboard{
		Box:           tview.NewBox(),
		full:          full,
		compact:       compact,
		minFullHeight: minFullHeight,
	}
}

func (d *responsiveDashboard) Draw(screen tcell.Screen) {
	x, y, w, h := d.GetRect()
	child := d.compact
	if h >= d.minFullHeight {
		child = d.full
	}
	child.SetRect(x, y, w, h)
	child.Draw(screen)
}

// responsiveFull показывает правую панель только там, где хватает ширины.
type responsiveFull struct {
	*tview.Box
	narrow tview.Primitive
	wide   tview.Primitive
	minW   int
}

func newResponsiveFull(narrow, wide tview.Primitive, minW int) *responsiveFull {
	return &responsiveFull{Box: tview.NewBox(), narrow: narrow, wide: wide, minW: minW}
}

func (d *responsiveFull) Draw(screen tcell.Screen) {
	x, y, w, h := d.GetRect()
	child := d.narrow
	if w >= d.minW {
		child = d.wide
	}
	child.SetRect(x, y, w, h)
	child.Draw(screen)
}

// centered держит дашборд по центру и ограничивает его максимальный размер,
// чтобы на широком терминале интерфейс не растягивался на весь экран.
type centered struct {
	*tview.Box
	child     tview.Primitive
	maxWidth  int
	maxHeight int
}

func newCentered(child tview.Primitive, maxWidth, maxHeight int, bg string) *centered {
	c := &centered{Box: tview.NewBox(), child: child, maxWidth: maxWidth, maxHeight: maxHeight}
	if bg != "" {
		c.SetBackgroundColor(tcell.GetColor(bg))
	}
	return c
}

func (c *centered) Draw(screen tcell.Screen) {
	x, y, width, height := c.GetRect()
	w, h := width, height
	if w > c.maxWidth {
		w = c.maxWidth
	}
	if h > c.maxHeight {
		h = c.maxHeight
	}
	cx := x + (width-w)/2
	cy := y + (height-h)/2
	c.child.SetRect(cx, cy, w, h)
	c.child.Draw(screen)
}

func (r *Runner) handleKey(event *tcell.EventKey) *tcell.EventKey {
	if r.modalOpen {
		if event.Key() == tcell.KeyEscape || hotkey(event) == 'q' || hotkey(event) == 'h' {
			r.closeModal()
			return nil
		}
		if r.handleModalKey(event) {
			return nil
		}
		return event
	}

	switch hotkey(event) {
	case 's':
		r.openSourceModal()
		return nil
	case 'p':
		r.cycleThreads()
		return nil
	case 'd':
		r.cycleSeconds()
		return nil
	case 'u':
		r.toggleUpload()
		return nil
	case 'r':
		r.start()
		return nil
	case 't':
		r.openThemeModal()
		return nil
	case 'l':
		r.cycleLayout()
		return nil
	case 'i':
		r.openGeoModal()
		return nil
	case 'h':
		r.openHelpModal()
		return nil
	case 'q':
		r.app.Stop()
		return nil
	}

	if event.Key() == tcell.KeyEscape || event.Key() == tcell.KeyCtrlC {
		r.app.Stop()
		return nil
	}
	return event
}

// handleModalKey обслуживает списки выбора самостоятельно: так навигация
// работает одинаково в любой раскладке и не зависит от фокуса tview.
func (r *Runner) handleModalKey(event *tcell.EventKey) bool {
	list := r.activeList()
	if list == nil {
		return false
	}
	switch event.Key() {
	case tcell.KeyUp:
		moveList(list, -1)
		return true
	case tcell.KeyDown:
		moveList(list, 1)
		return true
	case tcell.KeyEnter:
		r.applyModalChoice()
		return true
	}
	switch hotkey(event) {
	case 'k':
		moveList(list, -1)
		return true
	case 'j':
		moveList(list, 1)
		return true
	}
	return false
}

func (r *Runner) activeList() *tview.List {
	switch r.activeModal {
	case "sources":
		return r.list
	case "themes":
		return r.themeList
	default:
		return nil
	}
}

func moveList(list *tview.List, delta int) {
	n := list.GetItemCount()
	if n == 0 {
		return
	}
	index := (list.GetCurrentItem() + delta + n) % n
	list.SetCurrentItem(index)
}

func (r *Runner) applyModalChoice() {
	switch r.activeModal {
	case "sources":
		all := sources.All()
		index := r.list.GetCurrentItem()
		if index < 0 || index >= len(all) {
			return
		}
		r.setSource(all[index])
		r.closeModal()
		r.start()
	case "themes":
		all := theme.All()
		index := r.themeList.GetCurrentItem()
		if index < 0 || index >= len(all) {
			return
		}
		r.setTheme(all[index].ID)
	}
}

func (r *Runner) cycleThreads() {
	r.mu.Lock()
	r.threads = nextValue(r.threads, []int{1, 2, 4, 8, 16})
	r.mu.Unlock()
	r.start()
}

func (r *Runner) cycleSeconds() {
	r.mu.Lock()
	r.seconds = nextValue(r.seconds, []int{5, 7, 10, 15, 30, 60})
	r.mu.Unlock()
	r.start()
}

func nextValue(current int, values []int) int {
	for i, v := range values {
		if v == current {
			return values[(i+1)%len(values)]
		}
	}
	return values[0]
}

func (r *Runner) toggleUpload() {
	r.mu.Lock()
	r.upload = !r.upload
	r.mu.Unlock()
	r.start()
}

func (r *Runner) setSource(src sources.SpeedSource) {
	r.mu.Lock()
	r.source = src
	r.mu.Unlock()
}

func (r *Runner) openSourceModal() {
	all := sources.All()
	r.mu.Lock()
	current := r.source
	r.mu.Unlock()
	for i, src := range all {
		if src.ID() == current.ID() {
			r.list.SetCurrentItem(i)
			break
		}
	}
	r.modalOpen = true
	r.activeModal = "sources"
	r.pages.ShowPage("sources")
	r.app.SetFocus(r.list)
}

func (r *Runner) openThemeModal() {
	r.modalOpen = true
	r.activeModal = "themes"
	r.pages.ShowPage("themes")
	r.app.SetFocus(r.themeList)
}

func (r *Runner) openHelpModal() {
	r.modalOpen = true
	r.activeModal = "help"
	r.pages.ShowPage("help")
	r.app.SetFocus(r.help)
}

func (r *Runner) openGeoModal() {
	if r.pages.HasPage("geo") {
		r.pages.RemovePage("geo")
	}
	text := tview.NewTextView().
		SetDynamicColors(true).
		SetWrap(true).
		SetText(r.connectionText())
	text.SetBackgroundColor(tcell.GetColor(r.th.ModalBg))
	text.SetTextColor(tcell.GetColor(r.th.Foreground))
	r.geoModal = text

	content := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(text, 0, 1, true).
		AddItem(r.closeHint(), 1, 0, false)
	content.SetBackgroundColor(tcell.GetColor(r.th.ModalBg))
	page := centeredModal(newModalBox(content, r.tr.T("tuiConnectionTitle"), r.th), 58, 20, r.th.Background)
	r.pages.AddPage("geo", page, true, true)
	r.modalOpen = true
	r.activeModal = "geo"
	r.app.SetFocus(text)
}

func (r *Runner) closeModal() {
	r.modalOpen = false
	r.activeModal = ""
	r.pages.HidePage("sources")
	r.pages.HidePage("themes")
	r.pages.HidePage("help")
	r.pages.HidePage("geo")
	r.geoModal = nil
	r.app.SetFocus(r.main)
}

func (r *Runner) cycleLayout() {
	r.layout = (r.layout + 1) % LayoutCount
	r.saveSettings()
	r.rebuild()
}

func (r *Runner) setTheme(id string) {
	th, ok := theme.Get(id)
	if !ok {
		return
	}
	r.th = th
	r.themeID = th.ID
	r.saveSettings()
	r.rebuild()
}

func (r *Runner) saveSettings() {
	_ = settings.Save(settings.Settings{
		Theme:  r.themeID,
		Layout: r.layout,
	})
}

func (r *Runner) rebuild() {
	r.modalOpen = false
	r.activeModal = ""
	r.geoModal = nil
	root := r.build()
	r.app.SetRoot(root, true)
	r.app.SetFocus(r.main)
	r.render()
}

func (r *Runner) loadGeo() {
	ctx, cancel := context.WithTimeout(r.baseCtx, 15*time.Second)
	defer cancel()
	info, err := netinfo.Lookup(ctx)

	// QueueUpdateDraw из отдельной горутины безопасен: event loop уже запущен.
	go r.app.QueueUpdateDraw(func() {
		r.mu.Lock()
		r.geo = info
		r.geoErr = err
		r.geoLoading = false
		if info.CountryCode != "" {
			r.country = info.CountryCode
		}
		r.mu.Unlock()

		// Страна помогает, когда локаль не даёт однозначного ответа
		// (C/POSIX/пустая). Явный выбор языка не нужен.
		lang := i18n.DetectForCountry("auto", info.CountryCode)
		if lang != r.tr.Lang() {
			r.tr = i18n.New(lang)
			r.rebuild()
			return
		}
		r.render()
	})
}

func (r *Runner) connectionText() string {
	r.mu.Lock()
	info := r.geo
	err := r.geoErr
	loading := r.geoLoading
	latency := r.latency
	latencyErr := r.latencyErr
	r.mu.Unlock()

	if loading {
		return fmt.Sprintf("[%s]%s[-]", r.th.Muted, r.tr.T("geoLoading"))
	}
	if err != nil {
		return fmt.Sprintf("[%s]%s[-]", r.th.Muted, r.tr.T("geoUnavailable"))
	}

	line := func(label, value string) string {
		if value == "" {
			value = "—"
		}
		return fmt.Sprintf("[%s]%-10s[-] %s\n", r.th.Muted, label, tview.Escape(value))
	}
	country := info.Country
	if info.CountryCode != "" {
		if country == "" {
			country = info.CountryCode
		} else {
			country = country + " (" + info.CountryCode + ")"
		}
	}
	provider := info.Provider
	if provider == "" {
		provider = info.Org
	}
	latencyText := "—"
	if latencyErr == nil && latency > 0 {
		latencyText = fmt.Sprintf("%.0f %s", latency.Seconds()*1000, r.tr.T("tuiMs"))
	}
	proxy := r.sys.Proxy
	if proxy == "" {
		proxy = r.tr.T("proxyNone")
	}

	var b strings.Builder
	b.WriteString(line(r.tr.T("geoIP"), info.IP))
	b.WriteString(line(r.tr.T("geoProvider"), provider))
	b.WriteString(line(r.tr.T("geoASN"), info.ASN))
	b.WriteString(line(r.tr.T("geoCity"), info.City))
	b.WriteString(line(r.tr.T("geoRegion"), info.Region))
	b.WriteString(line(r.tr.T("geoCountry"), country))
	b.WriteString(line(r.tr.T("geoLatency"), latencyText))
	b.WriteString(line(r.tr.T("geoHost"), r.sys.Hostname))
	b.WriteString(line(r.tr.T("geoOS"), r.sys.OS))
	b.WriteString(line(r.tr.T("geoTerminal"), r.sys.Terminal))
	b.WriteString(line(r.tr.T("geoProxy"), proxy))
	b.WriteString(line(r.tr.T("geoApp"), "spidometr "+r.version))
	b.WriteString(fmt.Sprintf("[%s]%s[-] [%s]%s[-]",
		r.th.Muted, r.tr.T("geoSource"), r.th.Highlight, tview.Escape(info.Source)))
	return b.String()
}

func (r *Runner) connectionTextCompact() string {
	r.mu.Lock()
	info := r.geo
	err := r.geoErr
	loading := r.geoLoading
	r.mu.Unlock()

	if loading {
		return fmt.Sprintf("[%s]%s[-]", r.th.Muted, r.tr.T("geoLoading"))
	}
	if err != nil {
		return fmt.Sprintf("[%s]%s[-]", r.th.Muted, r.tr.T("geoUnavailable"))
	}

	provider := info.Provider
	if provider == "" {
		provider = info.Org
	}
	location := info.City
	if info.CountryCode != "" {
		if location == "" {
			location = info.CountryCode
		} else {
			location += ", " + info.CountryCode
		}
	}
	second := provider
	if location != "" {
		if second != "" {
			second += " · "
		}
		second += location
	}
	return fmt.Sprintf("[%s]%s[-] %s\n[%s]%s[-]",
		r.th.Muted, r.tr.T("geoIP"), tview.Escape(info.IP),
		r.th.Muted, tview.Escape(second))
}

func (r *Runner) start() {
	r.mu.Lock()
	if r.cancel != nil {
		r.cancel()
	}
	ctx, cancel := context.WithCancel(r.baseCtx)
	r.cancel = cancel
	r.phase = phaseReady
	r.err = nil
	r.uploadErr = nil
	r.downloadDone = false
	r.uploadDone = false
	r.downloadStats = engine.Stats{}
	r.uploadStats = engine.Stats{}
	r.downloadTime = 0
	r.uploadTime = 0
	r.downloadSpeed = 0
	r.uploadSpeed = 0
	r.downloadPeak = 0
	r.uploadPeak = 0
	r.phaseElapsed = 0
	r.phaseStart = time.Now()
	r.liveBytes.Store(0)
	r.lastTick = time.Time{}
	r.lastBytes = 0
	r.downloadHistory = nil
	r.uploadHistory = nil
	r.latency = 0
	r.latencyErr = nil
	src, threads, seconds, upload := r.source, r.threads, r.seconds, r.upload
	r.mu.Unlock()

	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		r.runMu.Lock()
		defer r.runMu.Unlock()
		if ctx.Err() != nil {
			return
		}
		r.execute(ctx, src, threads, seconds, upload)
	}()
	r.refresh()
}

func (r *Runner) execute(ctx context.Context, src sources.SpeedSource, threads, seconds int, upload bool) {
	lctx, lcancel := context.WithTimeout(ctx, 5*time.Second)
	latency, latencyErr := engine.ProbeLatency(lctx, src.DownloadURL())
	lcancel()
	r.mu.Lock()
	r.latency = latency
	r.latencyErr = latencyErr
	r.mu.Unlock()
	r.refresh()
	if ctx.Err() != nil {
		r.markStopped(0)
		return
	}

	r.setPhase(phaseDownload, seconds)
	dctx, cancel := context.WithTimeout(ctx, time.Duration(seconds)*time.Second)
	start := time.Now()
	download := engine.RunDownload(dctx, src.DownloadURL(), threads, func(n int) {
		r.liveBytes.Add(int64(n))
	})
	elapsed := time.Since(start)
	cancel()
	r.finishPhase(phaseDownload, download, elapsed)

	if download.Bytes == 0 {
		if ctx.Err() != nil {
			r.markStopped(elapsed)
			return
		}
		r.fail(download, elapsed)
		return
	}
	if ctx.Err() != nil {
		r.markStopped(elapsed)
		return
	}
	if !upload || src.UploadURL() == "" {
		r.finishAll()
		return
	}

	r.setPhase(phaseUpload, seconds)
	uctx, cancelUpload := context.WithTimeout(ctx, time.Duration(seconds)*time.Second)
	start = time.Now()
	up := engine.RunUpload(uctx, src.UploadURL(), threads, func(n int) {
		r.liveBytes.Add(int64(n))
	})
	elapsed = time.Since(start)
	cancelUpload()
	r.finishPhase(phaseUpload, up, elapsed)

	if ctx.Err() != nil {
		r.markStopped(elapsed)
		return
	}
	r.finishAll()
}

func (r *Runner) setPhase(p phase, seconds int) {
	r.mu.Lock()
	r.phase = p
	r.phaseStart = time.Now()
	r.phaseDuration = time.Duration(seconds) * time.Second
	r.phaseElapsed = 0
	r.liveBytes.Store(0)
	r.lastTick = time.Time{}
	r.lastBytes = 0
	if p == phaseDownload {
		r.downloadSpeed = 0
		r.downloadPeak = 0
		r.downloadHistory = nil
	} else {
		r.uploadSpeed = 0
		r.uploadPeak = 0
		r.uploadHistory = nil
	}
	r.mu.Unlock()
	r.refresh()
}

func (r *Runner) finishPhase(p phase, stats engine.Stats, elapsed time.Duration) {
	avg := speedMbps(stats.Bytes, elapsed)
	r.mu.Lock()
	r.phaseElapsed = elapsed
	if p == phaseDownload {
		r.downloadStats = stats
		r.downloadTime = elapsed
		r.downloadSpeed = avg
		r.downloadDone = stats.Bytes > 0
	} else {
		r.uploadStats = stats
		r.uploadTime = elapsed
		r.uploadSpeed = avg
		r.uploadDone = stats.Bytes > 0
		if stats.Bytes == 0 {
			r.uploadErr = stats.FirstErr
			if r.uploadErr == nil {
				r.uploadErr = errors.New(r.tr.T("errNoData"))
			}
		}
	}
	r.mu.Unlock()
}

func (r *Runner) fail(stats engine.Stats, elapsed time.Duration) {
	err := stats.FirstErr
	if err == nil {
		err = errors.New(r.tr.T("errNoData"))
	}
	r.mu.Lock()
	r.phase = phaseError
	r.phaseElapsed = elapsed
	r.err = err
	r.mu.Unlock()
	r.refresh()
}

func (r *Runner) markStopped(elapsed time.Duration) {
	r.mu.Lock()
	r.phase = phaseStopped
	r.phaseElapsed = elapsed
	r.mu.Unlock()
	r.refresh()
}

func (r *Runner) finishAll() {
	r.mu.Lock()
	r.phase = phaseDone
	r.mu.Unlock()
	r.refresh()
}

func (r *Runner) loop() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-r.baseCtx.Done():
			return
		case <-ticker.C:
			r.tick()
			r.refresh()
		}
	}
}

func (r *Runner) tick() {
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.phase != phaseDownload && r.phase != phaseUpload {
		return
	}

	total := r.liveBytes.Load()
	r.phaseElapsed = now.Sub(r.phaseStart)

	if !r.lastTick.IsZero() {
		dt := now.Sub(r.lastTick).Seconds()
		if dt > 0 {
			instant := float64(total-r.lastBytes) * 8 / 1_000_000 / dt
			if instant < 0 {
				instant = 0
			}
			if r.phase == phaseDownload {
				r.downloadSpeed = smooth(r.downloadSpeed, instant)
				if r.downloadSpeed > r.downloadPeak {
					r.downloadPeak = r.downloadSpeed
				}
				r.downloadHistory = appendHistory(r.downloadHistory, r.downloadSpeed)
			} else {
				r.uploadSpeed = smooth(r.uploadSpeed, instant)
				if r.uploadSpeed > r.uploadPeak {
					r.uploadPeak = r.uploadSpeed
				}
				r.uploadHistory = appendHistory(r.uploadHistory, r.uploadSpeed)
			}
		}
	}
	r.lastTick = now
	r.lastBytes = total
}

func smooth(current, sample float64) float64 {
	if current == 0 {
		return sample
	}
	return current*0.65 + sample*0.35
}

func (r *Runner) refresh() {
	// QueueUpdateDraw блокируется до обработки в event loop, поэтому
	// вызываем его из отдельной горутины и не тормозим замер.
	go r.app.QueueUpdateDraw(r.render)
}

type snapshot struct {
	source       sources.SpeedSource
	threads      int
	seconds      int
	upload       bool
	phase        phase
	elapsed      time.Duration
	download     engine.Stats
	uploadStats  engine.Stats
	downloadTime time.Duration
	uploadTime   time.Duration
	dlSpeed      float64
	ulSpeed      float64
	dlPeak       float64
	ulPeak       float64
	dlDone       bool
	ulDone       bool
	live         int64
	err          error
	uploadErr    error
	dlHistory    []float64
	ulHistory    []float64
	latency      time.Duration
	latencyErr   error
}

func (r *Runner) render() {
	r.mu.Lock()
	snap := snapshot{
		source:       r.source,
		threads:      r.threads,
		seconds:      r.seconds,
		upload:       r.upload,
		phase:        r.phase,
		elapsed:      r.phaseElapsed,
		download:     r.downloadStats,
		uploadStats:  r.uploadStats,
		downloadTime: r.downloadTime,
		uploadTime:   r.uploadTime,
		dlSpeed:      r.downloadSpeed,
		ulSpeed:      r.uploadSpeed,
		dlPeak:       r.downloadPeak,
		ulPeak:       r.uploadPeak,
		dlDone:       r.downloadDone,
		ulDone:       r.uploadDone,
		live:         r.liveBytes.Load(),
		err:          r.err,
		uploadErr:    r.uploadErr,
		dlHistory:    append([]float64(nil), r.downloadHistory...),
		ulHistory:    append([]float64(nil), r.uploadHistory...),
		latency:      r.latency,
		latencyErr:   r.latencyErr,
	}
	r.mu.Unlock()

	r.info.SetText(r.infoText(snap))
	r.downloadCard.SetText(r.downloadText(snap))
	r.uploadCard.SetText(r.uploadText(snap))
	r.historyCard.SetText(r.historyText(snap))
	r.stats.SetText(r.statsText(snap))
	r.applyStatsStyle(snap.phase)
	if r.connection != nil {
		r.connection.SetText(r.connectionText())
	}
	if r.connectionCompact != nil {
		r.connectionCompact.SetText(r.connectionTextCompact())
	}
	if r.geoModal != nil {
		r.geoModal.SetText(r.connectionText())
	}
	r.footerHelp.SetText(r.footerText())
	r.headerStatus.SetText(statusText(r.th, r.tr, snap.phase))
}

func (r *Runner) infoText(snap snapshot) string {
	uploadAPI := "—"
	if snap.source.UploadURL() != "" {
		uploadAPI = tview.Escape(snap.source.UploadName())
	}
	return fmt.Sprintf("[%s]API[-]  [%s]%s[-]\n[%s]Upload API[-]  [%s]%s[-]",
		r.th.Muted, r.th.Highlight, tview.Escape(snap.source.Name()),
		r.th.Muted, r.th.Highlight, uploadAPI)
}

func (r *Runner) downloadText(snap snapshot) string {
	value, avg := downloadPanel(r.tr, snap)
	return speedCardText(r.th, r.tr, value, avg, snap.dlSpeed, snap.dlPeak, r.th.Download)
}

func (r *Runner) uploadText(snap snapshot) string {
	value, avg := uploadPanel(r.tr, snap)
	return speedCardText(r.th, r.tr, value, avg, snap.ulSpeed, snap.ulPeak, r.th.Upload)
}

func speedCardText(th theme.Theme, tr *i18n.Translator, value, avg string, speed, peak float64, color string) string {
	valueLine := fmt.Sprintf("[%s::b]%s[-::-] [%s]%s[-]",
		color, value, th.Muted, tr.T("tuiMbit"))
	if value == "—" {
		valueLine = fmt.Sprintf("[%s]—[-]", th.Muted)
	}
	peakLine := fmt.Sprintf("[%s]%s —[-]", th.Muted, tr.T("tuiPeak"))
	if peak > 0 {
		peakLine = fmt.Sprintf("[%s]%s %.2f %s[-]",
			th.Muted, tr.T("tuiPeak"), peak, tr.T("tuiMbit"))
	}
	return fmt.Sprintf("%s\n%s\n[%s]%s[-]\n%s",
		valueLine,
		gauge(speed, peak, color, th.Borders),
		th.Muted, tview.Escape(avg),
		peakLine)
}

func (r *Runner) statsText(snap snapshot) string {
	if snap.phase == phaseDone {
		return r.resultText(snap)
	}
	tr := r.tr

	phaseInfo := "—"
	if snap.phase != phaseReady {
		phaseInfo = tr.T("tuiPhase", phaseLabel(tr, snap.phase), snap.elapsed.Seconds(), snap.seconds)
	}

	uploadState := tr.T("tuiUploadOff")
	uploadColor := r.th.Muted
	if snap.upload && snap.source.UploadURL() != "" {
		uploadState = tr.T("tuiUploadOn")
		uploadColor = r.th.Highlight
	}

	transfer := "—"
	if snap.phase != phaseReady {
		downBytes := snap.download.Bytes
		upBytes := snap.uploadStats.Bytes
		if snap.phase == phaseDownload {
			downBytes = snap.live
		}
		if snap.phase == phaseUpload {
			upBytes = snap.live
		}
		if snap.upload && snap.source.UploadURL() != "" {
			transfer = fmt.Sprintf("↓ %s  ↑ %s", formatBytes(tr, downBytes), formatBytes(tr, upBytes))
		} else {
			transfer = fmt.Sprintf("↓ %s", formatBytes(tr, downBytes))
		}
	}

	latency := "—"
	if snap.latencyErr == nil && snap.latency > 0 {
		latency = fmt.Sprintf("%.0f %s", snap.latency.Seconds()*1000, tr.T("tuiMs"))
	}
	line1 := fmt.Sprintf("[%s]%-9s[-] %-24s  [%s]%-9s[-] %d  [%s]%s[-] %s",
		r.th.Muted, tr.T("tuiPhaseLabel"), phaseInfo,
		r.th.Muted, tr.T("tuiThreads"), snap.threads,
		r.th.Muted, tr.T("tuiLatency"), latency)
	line2 := fmt.Sprintf("[%s]%-9s[-] [%s]%-6s[-]   [%s]%-9s[-] %s",
		r.th.Muted, tr.T("hkUpload"), uploadColor, uploadState,
		r.th.Muted, tr.T("tuiTransfer"), transfer)

	line3 := ""
	if snap.err != nil {
		line3 = fmt.Sprintf("[%s]%s[-]", r.th.Red, tview.Escape(tr.T("tuiError", snap.err)))
	} else if snap.uploadErr != nil {
		line3 = fmt.Sprintf("[%s]%s[-]", r.th.Red, tview.Escape(tr.T("tuiError", snap.uploadErr)))
	}
	return line1 + "\n" + line2 + "\n" + line3
}

func (r *Runner) resultText(snap snapshot) string {
	tr := r.tr
	download := "—"
	if snap.download.Bytes > 0 {
		download = fmt.Sprintf("%.2f %s", snap.dlSpeed, tr.T("tuiMbit"))
	}
	upload := "—"
	if snap.upload && snap.source.UploadURL() != "" && snap.uploadErr == nil {
		upload = fmt.Sprintf("%.2f %s", snap.ulSpeed, tr.T("tuiMbit"))
	}
	latency := "—"
	if snap.latencyErr == nil && snap.latency > 0 {
		latency = fmt.Sprintf("%.0f %s", snap.latency.Seconds()*1000, tr.T("tuiMs"))
	}

	line1 := fmt.Sprintf("[%s::b]✓ %s[-::-]", r.th.Download, tr.T("tuiTestDone"))
	line2 := fmt.Sprintf("[%s]↓[-] [%s::b]%s[-::-]   [%s]↑[-] [%s::b]%s[-::-]   [%s]%s[-] %s",
		r.th.Muted, r.th.Download, download,
		r.th.Muted, r.th.Upload, upload,
		r.th.Muted, tr.T("tuiLatency"), latency)

	line3 := fmt.Sprintf("[%s]%s[-]", r.th.Muted,
		tr.T("tuiResultDetails", snap.threads, formatBytes(tr, snap.download.Bytes),
			formatBytes(tr, snap.uploadStats.Bytes),
			(snap.downloadTime+snap.uploadTime).Seconds()))
	if snap.uploadErr != nil {
		line3 = fmt.Sprintf("[%s]%s[-]", r.th.Red, tview.Escape(tr.T("tuiError", snap.uploadErr)))
	}
	return line1 + "\n" + line2 + "\n" + line3
}

func (r *Runner) applyStatsStyle(p phase) {
	title := r.tr.T("tuiStatsTitle")
	color := r.th.Foreground
	if p == phaseDone {
		title = r.tr.T("tuiResultTitle")
		color = r.th.Download
	}
	r.stats.SetTitle(" " + tview.Escape(title) + " ")
	r.stats.SetTitleColor(tcell.GetColor(color))
	r.stats.SetBorderColor(tcell.GetColor(color))
}

func (r *Runner) historyText(snap snapshot) string {
	return fmt.Sprintf("[%s]↓[-] %s\n[%s]↑[-] %s",
		r.th.Muted, sparkline(snap.dlHistory, r.th.Download, r.th.Muted),
		r.th.Muted, sparkline(snap.ulHistory, r.th.Upload, r.th.Muted))
}

func appendHistory(values []float64, value float64) []float64 {
	const limit = 120
	values = append(values, value)
	if len(values) > limit {
		values = values[len(values)-limit:]
	}
	return values
}

func sparkline(values []float64, color, muted string) string {
	const width = 60
	if len(values) == 0 {
		return fmt.Sprintf("[%s]—[-]", muted)
	}
	if len(values) > width {
		values = values[len(values)-width:]
	}
	peak := 0.0
	for _, v := range values {
		if v > peak {
			peak = v
		}
	}
	if peak <= 0 {
		return fmt.Sprintf("[%s]—[-]", muted)
	}
	chars := []rune("▁▂▃▄▅▆▇█")
	var b strings.Builder
	b.WriteString(fmt.Sprintf("[%s]", color))
	for _, v := range values {
		idx := int(v/peak*float64(len(chars)-1) + 0.5)
		if idx < 0 {
			idx = 0
		}
		if idx >= len(chars) {
			idx = len(chars) - 1
		}
		b.WriteRune(chars[idx])
	}
	b.WriteString("[-]")
	return strings.Repeat(" ", width-len(values)) + b.String()
}

func statusText(th theme.Theme, tr *i18n.Translator, p phase) string {
	label := tr.T("tuiStatusReady")
	color := th.Foreground
	switch p {
	case phaseDownload:
		label, color = tr.T("tuiStatusDownload"), th.Highlight
	case phaseUpload:
		label, color = tr.T("tuiStatusUpload"), th.Upload
	case phaseDone:
		label, color = tr.T("tuiStatusDone"), th.Foreground
	case phaseError:
		label, color = tr.T("tuiStatusError"), th.Red
	case phaseStopped:
		label, color = tr.T("tuiStatusStopped"), th.Muted
	}
	return fmt.Sprintf(" [%s::b]%s[-::-] ", color, label)
}

func downloadPanel(tr *i18n.Translator, snap snapshot) (string, string) {
	active := snap.phase == phaseDownload
	if !active && !snap.dlDone {
		if snap.err != nil {
			return "—", tr.T("tuiNoData")
		}
		return "—", tr.T("tuiPending")
	}

	bytes := snap.download.Bytes
	elapsed := snap.downloadTime
	if active {
		bytes = snap.live
		elapsed = snap.elapsed
	}
	average := speedMbps(bytes, elapsed)
	avgText := tr.T("tuiAvg", fmt.Sprintf("%.2f", average), formatBytes(tr, bytes))
	return fmt.Sprintf("%.2f", snap.dlSpeed), avgText
}

func uploadPanel(tr *i18n.Translator, snap snapshot) (string, string) {
	switch {
	case !snap.upload || snap.source.UploadURL() == "":
		return "—", tr.T("tuiNotMeasured")
	case snap.uploadErr != nil:
		return "—", tr.T("tuiError", snap.uploadErr)
	case snap.phase == phaseUpload:
		average := speedMbps(snap.live, snap.elapsed)
		avgText := tr.T("tuiAvg", fmt.Sprintf("%.2f", average), formatBytes(tr, snap.live))
		return fmt.Sprintf("%.2f", snap.ulSpeed), avgText
	case snap.ulDone:
		average := speedMbps(snap.uploadStats.Bytes, snap.uploadTime)
		avgText := tr.T("tuiAvg", fmt.Sprintf("%.2f", average), formatBytes(tr, snap.uploadStats.Bytes))
		return fmt.Sprintf("%.2f", snap.ulSpeed), avgText
	default:
		return "—", tr.T("tuiPending")
	}
}

func phaseLabel(tr *i18n.Translator, p phase) string {
	switch p {
	case phaseDownload:
		return strings.ToLower(tr.T("tuiDownloadTitle"))
	case phaseUpload:
		return strings.ToLower(tr.T("tuiUploadTitle"))
	case phaseDone:
		return tr.T("tuiPhaseDone")
	case phaseError:
		return tr.T("tuiPhaseError")
	case phaseStopped:
		return tr.T("tuiPhaseStopped")
	default:
		return "—"
	}
}

func gauge(speed, peak float64, color, borders string) string {
	const width = 24
	scale := math.Max(100, peak*1.15)
	ratio := speed / scale
	ratio = math.Max(0, math.Min(1, ratio))
	filled := int(math.Round(ratio * width))
	bar := strings.Repeat("█", filled)
	rest := strings.Repeat("░", width-filled)
	if rest == "" {
		return fmt.Sprintf("[%s]%s[-]", color, bar)
	}
	if bar == "" {
		return fmt.Sprintf("[%s]%s[-]", borders, rest)
	}
	return fmt.Sprintf("[%s]%s[-][%s]%s[-]", color, bar, borders, rest)
}

func formatBytes(tr *i18n.Translator, bytes int64) string {
	mb := float64(bytes) / (1024 * 1024)
	if mb >= 1024 {
		return fmt.Sprintf("%.2f GB", mb/1024)
	}
	return fmt.Sprintf("%.1f %s", mb, tr.T("tuiMB"))
}

func speedMbps(bytes int64, elapsed time.Duration) float64 {
	if elapsed <= 0 {
		return 0
	}
	return float64(bytes) * 8 / 1_000_000 / elapsed.Seconds()
}

func (r *Runner) footerText() string {
	key := func(k string) string {
		return fmt.Sprintf("[%s::b]%s[-::-]", r.th.Highlight, k)
	}
	// Подписи меню локализованы; порядок — как в первом варианте, но
	// параметры p/d/u стоят в конце перед info/help/quit.
	items := [][2]string{
		{"h", r.tr.T("tuiFootHelp")},
		{"s", r.tr.T("tuiFootSource")},
		{"r", r.tr.T("tuiFootRestart")},
		{"t", r.tr.T("tuiFootTheme")},
		{"l", r.tr.T("tuiFootLayout")},
		{"p", r.tr.T("tuiFootThreads")},
		{"d", r.tr.T("tuiFootTime")},
		{"u", r.tr.T("tuiFootUpload")},
		{"i", r.tr.T("tuiFootInfo")},
		{"q", r.tr.T("tuiFootQuit")},
	}
	parts := make([]string, 0, len(items))
	for _, item := range items {
		parts = append(parts, key(item[0])+" "+item[1])
	}
	return strings.Join(parts, " ")
}

func (r *Runner) watchSignals() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(ch)

	select {
	case <-ch:
		r.app.Stop()
	case <-r.baseCtx.Done():
	}
}
