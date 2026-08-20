//go:build windows

package nativeui

import (
	"context"
	"crypto/rand"
	"fmt"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"clipsyncpro/internal/app"
	"clipsyncpro/internal/domain"
	"clipsyncpro/internal/storage"
	"clipsyncpro/internal/win32"
)

const (
	wsOverlapped          = 0x00000000
	wsCaption             = 0x00C00000
	wsSysMenu             = 0x00080000
	wsThickFrame          = 0x00040000
	wsMinimizeBox         = 0x00020000
	wsMaximizeBox         = 0x00010000
	wsVisible             = 0x10000000
	wsChild               = 0x40000000
	wsClipChildren        = 0x02000000
	wsVScroll             = 0x00200000
	wsBorder              = 0x00800000
	wsTabStop             = 0x00010000
	wsGroup               = 0x00020000
	esAutoHScroll         = 0x0080
	esMultiline           = 0x0004
	bsPushButton          = 0x00000000
	bsFlat                = 0x00008000
	ssLeft                = 0x00000000
	ssNotify              = 0x00000100
	csDblClks             = 0x0008
	cwUseDefault          = 0x80000000
	swShow                = 5
	swHide                = 0
	swRestore             = 9
	swMinimize            = 6
	wmCreate              = 0x0001
	wmDestroy             = 0x0002
	wmSize                = 0x0005
	wmPaint               = 0x000F
	wmCommand             = 0x0111
	wmClose               = 0x0010
	wmEraseBkgnd          = 0x0014
	wmSetFont             = 0x0030
	wmKeyDown             = 0x0100
	wmLButtonDown         = 0x0201
	wmLButtonDblClk       = 0x0203
	wmMouseWheel          = 0x020A
	wmHotkey              = 0x0312
	wmAppRefresh          = 0x8001
	dtLeft                = 0x0000
	dtCenter              = 0x0001
	dtRight               = 0x0002
	dtVCenter             = 0x0004
	dtSingleLine          = 0x0020
	dtNoPrefix            = 0x0800
	dtEndEllipsis         = 0x8000
	transparent           = 1
	fwNormal              = 400
	fwSemibold            = 600
	enChange              = 0x0300
	stnClicked            = 0
	stnDblClk             = 1
	modAlt                = 0x0001
	modNoRepeat           = 0x4000
	vkReturn              = 0x0D
	vkV                   = 0x56
	idSearch              = 1001
	idAll                 = 1002
	idSensitive           = 1003
	idBatch               = 1004
	idDate                = 1005
	idDelete              = 1006
	idPassword            = 1007
	idAccount             = 1008
	idView                = 1009
	idCopy                = 1010
	idPaste               = 1011
	idListArea            = 1012
	idStatus              = 1013
	idHotkey              = 37618
	idPromptEdit          = 2001
	idPromptOK            = 2002
	idPromptCancel        = 2003
	idPromptGenerate      = 2004
	idRevealCopyAccount   = 3001
	idRevealCopyPassword  = 3002
	idRevealOK            = 3003
	idRevealStatus        = 3004
	idRevealAccountLabel  = 3005
	idRevealAccountValue  = 3006
	idRevealPasswordLabel = 3007
	idRevealPasswordValue = 3008
	idRevealRemarkLabel   = 3009
	idRevealRemarkValue   = 3010
	idRevealModifyRemark  = 3011
	idConfirmOK           = 4001
	idConfirmCancel       = 4002
	idConfirmMessage      = 4003
	rowIDBase             = 10000
)

var (
	user32                  = syscall.NewLazyDLL("user32.dll")
	gdi32                   = syscall.NewLazyDLL("gdi32.dll")
	kernel32                = syscall.NewLazyDLL("kernel32.dll")
	procRegisterClassEx     = user32.NewProc("RegisterClassExW")
	procCreateWindowEx      = user32.NewProc("CreateWindowExW")
	procDefWindowProc       = user32.NewProc("DefWindowProcW")
	procBeginPaint          = user32.NewProc("BeginPaint")
	procEndPaint            = user32.NewProc("EndPaint")
	procFillRect            = user32.NewProc("FillRect")
	procDrawText            = user32.NewProc("DrawTextW")
	procShowWindow          = user32.NewProc("ShowWindow")
	procUpdateWindow        = user32.NewProc("UpdateWindow")
	procGetMessage          = user32.NewProc("GetMessageW")
	procTranslateMessage    = user32.NewProc("TranslateMessage")
	procDispatchMessage     = user32.NewProc("DispatchMessageW")
	procPostQuitMessage     = user32.NewProc("PostQuitMessage")
	procPostMessage         = user32.NewProc("PostMessageW")
	procDestroyWindow       = user32.NewProc("DestroyWindow")
	procMoveWindow          = user32.NewProc("MoveWindow")
	procSendMessage         = user32.NewProc("SendMessageW")
	procSetWindowText       = user32.NewProc("SetWindowTextW")
	procGetWindowText       = user32.NewProc("GetWindowTextW")
	procGetWindowTextLen    = user32.NewProc("GetWindowTextLengthW")
	procGetWindowRect       = user32.NewProc("GetWindowRect")
	procSetFocus            = user32.NewProc("SetFocus")
	procMessageBox          = user32.NewProc("MessageBoxW")
	procInvalidateRect      = user32.NewProc("InvalidateRect")
	procRegisterHotKey      = user32.NewProc("RegisterHotKey")
	procUnregisterHotKey    = user32.NewProc("UnregisterHotKey")
	procEnableWindow        = user32.NewProc("EnableWindow")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	procGetModuleHandle     = kernel32.NewProc("GetModuleHandleW")
	procGetForegroundWindow = user32.NewProc("GetForegroundWindow")
	procCreateSolidBrush    = gdi32.NewProc("CreateSolidBrush")
	procDeleteObject        = gdi32.NewProc("DeleteObject")
	procSetBkMode           = gdi32.NewProc("SetBkMode")
	procSetTextColor        = gdi32.NewProc("SetTextColor")
	procSelectObject        = gdi32.NewProc("SelectObject")
	procCreateFont          = gdi32.NewProc("CreateFontW")
)

type App struct {
	service     *app.Service
	hwnd        uintptr
	search      uintptr
	listArea    uintptr
	status      uintptr
	buttons     []uintptr
	rowControls []rowControl
	font        uintptr
	fontBold    uintptr
	fontSmall   uintptr
	records     []domain.ClipboardRecord
	revealed    map[string]revealEntry
	keyword     string
	date        string
	sensitive   bool
	hidden      bool
	selected    int
	rowOffset   int
	statusText  string
	listLeft    int32
	listTop     int32
	listWidth   int32
	listHeight  int32
	rowHeight   int32
	width       int32
	height      int32
}

type revealEntry struct {
	value   domain.RevealedSensitive
	expires time.Time
}

type rowControl struct {
	index int
	hwnds []uintptr
}

var active *App

func Run(ctx context.Context, service *app.Service, refresh <-chan struct{}) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	active = &App{
		service:  service,
		revealed: make(map[string]revealEntry),
		selected: -1,
	}
	if err := registerMainClass(); err != nil {
		return err
	}
	hwnd, err := createMainWindow()
	if err != nil {
		return err
	}
	active.hwnd = hwnd
	active.createFonts()
	active.createControls()
	active.layout(960, 640)
	active.reload()
	if ok, _, err := procRegisterHotKey.Call(hwnd, idHotkey, modAlt|modNoRepeat, vkV); ok == 0 {
		active.showError("Alt+V 注册失败，可能已被其他程序占用: " + err.Error())
	}
	defer procUnregisterHotKey.Call(hwnd, idHotkey)

	procShowWindow.Call(hwnd, swShow)
	procUpdateWindow.Call(hwnd)
	if refresh != nil {
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case <-refresh:
					procPostMessage.Call(hwnd, wmAppRefresh, 0, 0)
				}
			}
		}()
	}
	go func() {
		<-ctx.Done()
		procDestroyWindow.Call(hwnd)
	}()

	var m msg
	for {
		ret, _, err := procGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(ret) == -1 {
			return err
		}
		if ret == 0 {
			return nil
		}
		if m.Message == wmKeyDown && m.WParam == vkReturn && active != nil {
			active.copySelected()
			continue
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessage.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func registerMainClass() error {
	className := utf16Ptr("ClipSyncProNativeWindow")
	wnd := wndClassEx{
		Size:      uint32(unsafe.Sizeof(wndClassEx{})),
		Style:     csDblClks,
		WndProc:   syscall.NewCallback(mainWndProc),
		Instance:  moduleHandle(),
		ClassName: className,
	}
	ret, _, err := procRegisterClassEx.Call(uintptr(unsafe.Pointer(&wnd)))
	if ret == 0 {
		if errno, ok := err.(syscall.Errno); ok && errno == 1410 {
			return nil
		}
		return err
	}
	return nil
}

func createMainWindow() (uintptr, error) {
	hwnd, _, err := procCreateWindowEx.Call(
		0,
		uintptr(unsafe.Pointer(utf16Ptr("ClipSyncProNativeWindow"))),
		uintptr(unsafe.Pointer(utf16Ptr("ClipSync Pro"))),
		wsOverlapped|wsCaption|wsSysMenu|wsThickFrame|wsMinimizeBox|wsMaximizeBox|wsVisible|wsClipChildren,
		cwUseDefault, cwUseDefault, 980, 680,
		0, 0, moduleHandle(), 0,
	)
	if hwnd == 0 {
		return 0, err
	}
	return hwnd, nil
}

func (a *App) createFonts() {
	a.font = createUIFont(15, fwNormal)
	a.fontBold = createUIFont(15, fwSemibold)
	a.fontSmall = createUIFont(13, fwNormal)
}

func (a *App) disposeFonts() {
	for _, font := range []uintptr{a.font, a.fontBold, a.fontSmall} {
		if font != 0 {
			procDeleteObject.Call(font)
		}
	}
}

func (a *App) createControls() {
	a.search = createControl("EDIT", "", wsChild|wsVisible|wsBorder|wsTabStop|esAutoHScroll, idSearch, a.hwnd)
	setControlFont(a.search, a.font)
	labels := []struct {
		id   int
		text string
	}{
		{idAll, "全部"},
		{idSensitive, "账户密码"},
		{idBatch, "批量删除"},
		{idDate, "按时间筛选"},
		{idDelete, "删除"},
		{idPassword, "密码"},
		{idAccount, "账户"},
		{idView, "查看"},
		{idCopy, "复制"},
		{idPaste, "输入"},
	}
	for _, item := range labels {
		button := createControl("BUTTON", item.text, wsChild|wsVisible|wsTabStop|bsPushButton|bsFlat, item.id, a.hwnd)
		setControlFont(button, a.font)
		a.buttons = append(a.buttons, button)
	}
	a.statusText = "未选择"
	procSetFocus.Call(a.search)
}

func (a *App) layout(width, height int32) {
	if width < 760 {
		width = 760
	}
	if height < 460 {
		height = 460
	}
	a.width = width
	a.height = height
	a.rowHeight = 34
	top := int32(14)
	left := int32(16)
	procMoveWindow.Call(a.search, uintptr(left), uintptr(top), uintptr(width-32), 32, 1)
	top += 46
	x := left
	gap := int32(8)
	buttonWidth := int32(88)
	if count := int32(len(a.buttons)); count > 0 {
		available := width - 32
		total := buttonWidth*count + gap*(count-1)
		if total > available {
			buttonWidth = (available - gap*(count-1)) / count
			if buttonWidth < 64 {
				buttonWidth = 64
			}
		}
	}
	for _, hwnd := range a.buttons {
		procMoveWindow.Call(hwnd, uintptr(x), uintptr(top), uintptr(buttonWidth), 32, 1)
		x += buttonWidth + gap
	}
	top += 44
	a.listLeft = left
	a.listTop = top
	a.listWidth = width - 32
	a.listHeight = height - top - 48
	if a.listHeight < 120 {
		a.listHeight = 120
	}
	if a.listArea != 0 {
		procMoveWindow.Call(a.listArea, uintptr(left), uintptr(top), uintptr(a.listWidth), uintptr(a.listHeight), 1)
	}
	if a.status != 0 {
		procMoveWindow.Call(a.status, uintptr(left), uintptr(height-30), uintptr(width-32), 22, 1)
	}
	a.clampRowOffset()
	a.drawRows()
}

func (a *App) reload() {
	filter := storage.Filter{
		Keyword:       a.keyword,
		SensitiveOnly: a.sensitive,
		Limit:         1000,
	}
	if a.date != "" {
		start, end, err := domain.ParseFilterDate(a.date)
		if err != nil {
			a.showError(err.Error())
			a.date = ""
		} else {
			filter.DateStart = &start
			filter.DateEnd = &end
		}
	}
	records, err := a.service.List(context.Background(), filter)
	if err != nil {
		a.showError("加载记录失败: " + err.Error())
		return
	}
	a.records = records
	if a.selected >= len(a.records) {
		a.selected = -1
	}
	if a.rowOffset >= len(a.records) {
		a.rowOffset = len(a.records) - 1
	}
	if a.rowOffset < 0 {
		a.rowOffset = 0
	}
	a.clampRowOffset()
	a.drawRows()
	a.updateStatus()
}

func (a *App) rowSummary(rec domain.ClipboardRecord) string {
	summary := domain.RedactedSummary(rec)
	if rec.IsSensitive {
		if revealed, ok := a.revealedValue(rec.ID); ok {
			summary = fmt.Sprintf("账户: %s    密码: %s    备注: %s", revealed.Account, revealed.Password, revealed.Remark)
		}
	}
	summary = strings.ReplaceAll(summary, "\r\n", " ")
	summary = strings.ReplaceAll(summary, "\n", " ")
	if len([]rune(summary)) > 90 {
		runes := []rune(summary)
		summary = string(runes[:90]) + "..."
	}
	return summary
}

func (a *App) drawRows() {
	if a.hwnd != 0 {
		procInvalidateRect.Call(a.hwnd, 0, 1)
	}
}

func (a *App) clearRows() {
	for _, row := range a.rowControls {
		for _, hwnd := range row.hwnds {
			if hwnd != 0 {
				procDestroyWindow.Call(hwnd)
			}
		}
	}
	a.rowControls = nil
}

func (a *App) selectedRecord() (domain.ClipboardRecord, bool) {
	if a.selected < 0 || a.selected >= len(a.records) {
		return domain.ClipboardRecord{}, false
	}
	return a.records[a.selected], true
}

func (a *App) updateStatus() {
	selected := "未选择"
	if rec, ok := a.selectedRecord(); ok {
		selected = "已选择: " + shortRecordID(rec.ID)
	}
	filter := "全部"
	if a.sensitive {
		filter = "账户密码"
	}
	if a.date != "" {
		filter += " / " + a.date
	}
	if a.keyword != "" {
		filter += " / 搜索: " + a.keyword
	}
	a.statusText = fmt.Sprintf("%s    %s", selected, filter)
	if a.status != 0 {
		setWindowText(a.status, a.statusText)
	}
	a.drawRows()
}

func (a *App) onCommand(wparam, lparam uintptr) {
	id := loword(wparam)
	notify := hiword(wparam)
	if row, action, ok := parseRowCommandID(id); ok {
		a.onRowCommand(row, action, notify, lparam)
		return
	}
	switch id {
	case idSearch:
		if notify == enChange {
			a.keyword = getWindowText(a.search)
			a.reload()
		}
	case idAll:
		a.keyword = ""
		a.date = ""
		a.sensitive = false
		setWindowText(a.search, "")
		a.reload()
	case idSensitive:
		a.sensitive = !a.sensitive
		a.reload()
	case idBatch:
		a.batchDeleteNonSensitive()
	case idDate:
		if value, ok := promptText(a.hwnd, "按时间筛选", "yyyyMMdd", a.date); ok {
			a.date = strings.TrimSpace(value)
			a.reload()
		}
	case idDelete:
		a.deleteSelected(lparam)
	case idPassword:
		a.markPassword(lparam)
	case idAccount:
		a.markAccount(lparam)
	case idView:
		a.revealSelected(lparam)
	case idCopy:
		a.copySelected()
	case idPaste:
		a.pasteSelected()
	}
}

func (a *App) onRowCommand(row, action, notify int, source uintptr) {
	actual := a.rowOffset + row
	if actual < 0 || actual >= len(a.records) {
		return
	}
	a.selected = actual
	a.updateStatus()
	switch action {
	case 0:
		if notify == stnDblClk {
			a.pasteSelected()
		}
		return
	case 1:
		a.deleteRecord(a.records[actual], source)
	case 2:
		a.markPassword(source)
	case 3:
		a.markAccount(source)
	case 4:
		a.revealSelected(source)
	case 5:
		a.copySelected()
	case 6:
		a.pasteSelected()
	}
}

func (a *App) scrollRows(delta int) {
	if len(a.records) == 0 {
		return
	}
	oldOffset := a.rowOffset
	if delta < 0 {
		a.rowOffset += 3
	} else {
		a.rowOffset -= 3
	}
	maxOffset := len(a.records) - a.visibleRowCount()
	if maxOffset < 0 {
		maxOffset = 0
	}
	if a.rowOffset > maxOffset {
		a.rowOffset = maxOffset
	}
	if a.rowOffset < 0 {
		a.rowOffset = 0
	}
	if a.rowOffset != oldOffset {
		a.drawRows()
	}
}

func (a *App) clampRowOffset() {
	maxOffset := len(a.records) - a.visibleCapacity()
	if maxOffset < 0 {
		maxOffset = 0
	}
	if a.rowOffset > maxOffset {
		a.rowOffset = maxOffset
	}
	if a.rowOffset < 0 {
		a.rowOffset = 0
	}
}

func (a *App) deleteSelected(anchor uintptr) {
	rec, ok := a.selectedRecord()
	if !ok {
		return
	}
	a.deleteRecord(rec, anchor)
}

func (a *App) deleteRecord(rec domain.ClipboardRecord, anchor uintptr) {
	if rec.IsSensitive && !confirmSensitiveDelete(a.hwnd, anchor, rec) {
		return
	}
	a.deleteIDs([]string{rec.ID})
}

func (a *App) deleteIDs(ids []string) {
	if err := a.service.Delete(context.Background(), ids); err != nil {
		a.showError("删除失败: " + err.Error())
		return
	}
	a.reload()
}

func (a *App) batchDeleteNonSensitive() {
	ids := make([]string, 0, len(a.records))
	skipped := 0
	for _, rec := range a.records {
		if rec.IsSensitive {
			skipped++
			continue
		}
		ids = append(ids, rec.ID)
	}
	if len(ids) == 0 {
		if skipped > 0 {
			a.showError("当前结果全部是账户/密码记录，已跳过")
		}
		return
	}
	if err := a.service.Delete(context.Background(), ids); err != nil {
		a.showError("批量删除失败: " + err.Error())
		return
	}
	a.reload()
	if skipped > 0 {
		setWindowText(a.status, fmt.Sprintf("批量删除 %d 条，跳过账户/密码记录 %d 条", len(ids), skipped))
	}
}

func (a *App) markPassword(anchor uintptr) {
	rec, ok := a.selectedRecord()
	if !ok {
		return
	}
	account, ok := promptTextAt(a.hwnd, anchor, "密码", "账户", "")
	if !ok {
		return
	}
	remark, ok := promptTextAt(a.hwnd, anchor, "密码", "备注", "")
	if !ok {
		return
	}
	if _, err := a.service.MarkPassword(context.Background(), rec.ID, account, remark); err != nil {
		a.showError(err.Error())
		return
	}
	a.reload()
}

func (a *App) markAccount(anchor uintptr) {
	rec, ok := a.selectedRecord()
	if !ok {
		return
	}
	password, ok := promptPasswordAt(a.hwnd, anchor, "账户", "密码", "")
	if !ok {
		return
	}
	remark, ok := promptTextAt(a.hwnd, anchor, "账户", "备注", "")
	if !ok {
		return
	}
	if _, err := a.service.MarkAccount(context.Background(), rec.ID, password, remark); err != nil {
		a.showError(err.Error())
		return
	}
	a.reload()
}

func (a *App) revealSelected(anchor uintptr) {
	rec, ok := a.selectedRecord()
	if !ok {
		return
	}
	if rec.ContentType == domain.ContentImage {
		messageBox(a.hwnd, "这是一条图片记录。可使用“复制”或“输入”将图片放回目标应用。", "查看图片")
		return
	}
	revealed, err := a.service.Reveal(context.Background(), rec.ID)
	if err != nil {
		a.showError("查看失败: " + err.Error())
		return
	}
	a.revealed[rec.ID] = revealEntry{value: revealed, expires: time.Now().Add(30 * time.Minute)}
	if !rec.IsSensitive {
		messageBox(a.hwnd, revealed.Content, "查看")
		return
	}
	showRevealWindow(a.hwnd, anchor, revealed, func(remark string) error {
		if _, err := a.service.UpdateRemark(context.Background(), rec.ID, remark); err != nil {
			return err
		}
		revealed.Remark = remark
		a.revealed[rec.ID] = revealEntry{value: revealed, expires: time.Now().Add(30 * time.Minute)}
		return nil
	})
	a.reload()
}

func (a *App) copySelected() {
	rec, selected := a.selectedRecord()
	if selected && rec.ContentType == domain.ContentImage {
		mimeType, data, err := domain.DecodeClipboardImage(rec.ContentText)
		if err != nil {
			a.showError("图片数据无效")
			return
		}
		if err := win32.WriteClipboardImage(mimeType, data); err != nil {
			a.showError("复制失败: " + err.Error())
		}
		return
	}
	text, ok := a.textForSelected()
	if !ok {
		return
	}
	if err := win32.WriteClipboardText(text); err != nil {
		a.showError("复制失败: " + err.Error())
	}
}

func (a *App) pasteSelected() {
	rec, selected := a.selectedRecord()
	if selected && rec.ContentType == domain.ContentImage {
		mimeType, data, err := domain.DecodeClipboardImage(rec.ContentText)
		if err != nil {
			a.showError("图片数据无效")
			return
		}
		procShowWindow.Call(a.hwnd, swMinimize)
		time.Sleep(120 * time.Millisecond)
		if err := win32.PasteImage(mimeType, data); err != nil {
			a.showError("输入失败: " + err.Error())
		}
		return
	}
	text, ok := a.textForSelected()
	if !ok {
		return
	}
	procShowWindow.Call(a.hwnd, swMinimize)
	time.Sleep(120 * time.Millisecond)
	if err := win32.PasteText(text); err != nil {
		a.showError("输入失败: " + err.Error())
	}
}

func (a *App) textForSelected() (string, bool) {
	rec, ok := a.selectedRecord()
	if !ok {
		return "", false
	}
	if !rec.IsSensitive {
		return rec.ContentText, true
	}
	revealed, ok := a.revealedValue(rec.ID)
	if !ok {
		a.showError("请先点击查看，解密后再复制或输入")
		return "", false
	}
	defaultField := "password"
	if rec.SensitiveType == domain.SensitiveAccount {
		defaultField = "account"
	}
	field, ok := promptText(a.hwnd, "选择字段", "content/account/password/remark", defaultField)
	if !ok {
		return "", false
	}
	switch strings.TrimSpace(field) {
	case "content":
		return revealed.Content, true
	case "account":
		return revealed.Account, true
	case "password":
		return revealed.Password, true
	case "remark":
		return revealed.Remark, true
	default:
		a.showError("未知字段")
		return "", false
	}
}

func (a *App) revealedValue(id string) (domain.RevealedSensitive, bool) {
	entry, ok := a.revealed[id]
	if !ok || time.Now().After(entry.expires) {
		delete(a.revealed, id)
		return domain.RevealedSensitive{}, false
	}
	return entry.value, true
}

func (a *App) toggleVisible() {
	if a.hidden {
		procShowWindow.Call(a.hwnd, swRestore)
		procSetForegroundWindow.Call(a.hwnd)
		procSetFocus.Call(a.search)
		a.hidden = false
		return
	}
	procShowWindow.Call(a.hwnd, swHide)
	a.hidden = true
}

func (a *App) showError(text string) {
	messageBox(a.hwnd, text, "ClipSync Pro")
}

func (a *App) paint(hwnd uintptr) {
	var ps paintStruct
	hdc, _, _ := procBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	if hdc != 0 {
		a.paintSurface(hdc)
	}
	procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
}

func (a *App) paintSurface(hdc uintptr) {
	width := a.width
	height := a.height
	if width <= 0 {
		width = 980
	}
	if height <= 0 {
		height = 680
	}
	fillRect(hdc, rect{Left: 0, Top: 0, Right: width, Bottom: height}, rgb(248, 250, 252))
	a.paintList(hdc)
	a.paintStatus(hdc)
}

func (a *App) paintList(hdc uintptr) {
	if a.listWidth <= 0 || a.listHeight <= 0 {
		return
	}
	panel := rect{
		Left:   a.listLeft,
		Top:    a.listTop,
		Right:  a.listLeft + a.listWidth,
		Bottom: a.listTop + a.listHeight,
	}
	fillRect(hdc, panel, rgb(255, 255, 255))
	strokeRect(hdc, panel, rgb(226, 232, 240))
	if len(a.records) == 0 {
		drawText(hdc, "暂无剪贴记录", insetRect(panel, 12, 12), rgb(100, 116, 139), a.font, dtCenter|dtVCenter|dtSingleLine|dtNoPrefix)
		return
	}

	start := a.rowOffset
	if start > len(a.records) {
		start = len(a.records)
	}
	visibleRows := a.visibleRowCount()
	timeWidth := int32(124)
	innerLeft := panel.Left + 8
	innerRight := panel.Right - 8
	innerTop := panel.Top + 8
	for i := 0; i < visibleRows; i++ {
		actual := start + i
		if actual >= len(a.records) {
			break
		}
		rec := a.records[actual]
		row := rect{
			Left:   innerLeft,
			Top:    innerTop + int32(i)*a.rowHeight,
			Right:  innerRight,
			Bottom: innerTop + int32(i+1)*a.rowHeight - 2,
		}
		if a.selected == actual {
			fillRect(hdc, row, rgb(219, 234, 254))
			fillRect(hdc, rect{Left: row.Left, Top: row.Top, Right: row.Left + 4, Bottom: row.Bottom}, rgb(37, 99, 235))
		} else if i%2 == 1 {
			fillRect(hdc, row, rgb(250, 252, 255))
		}
		fillRect(hdc, rect{Left: row.Left + 12, Top: row.Bottom - 1, Right: row.Right - 12, Bottom: row.Bottom}, rgb(241, 245, 249))

		textColor := rgb(15, 23, 42)
		if rec.IsSensitive {
			textColor = rgb(126, 34, 206)
		}
		font := a.font
		if a.selected == actual {
			font = a.fontBold
		}
		contentRect := rect{Left: row.Left + 14, Top: row.Top, Right: row.Right - timeWidth - 12, Bottom: row.Bottom}
		timeRect := rect{Left: row.Right - timeWidth, Top: row.Top, Right: row.Right - 12, Bottom: row.Bottom}
		drawText(hdc, a.rowSummary(rec), contentRect, textColor, font, dtLeft|dtVCenter|dtSingleLine|dtEndEllipsis|dtNoPrefix)
		drawText(hdc, rec.ClientCreatedAt.Local().Format("200601021504"), timeRect, rgb(100, 116, 139), a.fontSmall, dtRight|dtVCenter|dtSingleLine|dtNoPrefix)
	}
}

func (a *App) paintStatus(hdc uintptr) {
	if a.height <= 0 {
		return
	}
	statusRect := rect{Left: 16, Top: a.height - 38, Right: a.width - 16, Bottom: a.height - 12}
	fillRect(hdc, statusRect, rgb(241, 245, 249))
	fillRect(hdc, rect{Left: statusRect.Left, Top: statusRect.Top, Right: statusRect.Right, Bottom: statusRect.Top + 1}, rgb(226, 232, 240))
	drawText(hdc, a.statusText, insetRect(statusRect, 10, 0), rgb(71, 85, 105), a.fontSmall, dtLeft|dtVCenter|dtSingleLine|dtEndEllipsis|dtNoPrefix)
}

func (a *App) visibleRowCount() int {
	count := a.visibleCapacity()
	remaining := len(a.records) - a.rowOffset
	if count > remaining {
		count = remaining
	}
	if count < 0 {
		return 0
	}
	return count
}

func (a *App) visibleCapacity() int {
	if a.rowHeight <= 0 || a.listHeight <= 16 {
		return 0
	}
	return int((a.listHeight - 16) / a.rowHeight)
}

func (a *App) onListMouse(x, y int32, doubleClick bool) bool {
	actual, ok := a.recordAtPoint(x, y)
	if !ok {
		return false
	}
	a.selected = actual
	a.updateStatus()
	if doubleClick {
		a.pasteSelected()
	}
	return true
}

func (a *App) recordAtPoint(x, y int32) (int, bool) {
	if x < a.listLeft || x >= a.listLeft+a.listWidth {
		return 0, false
	}
	innerTop := a.listTop + 8
	innerBottom := a.listTop + a.listHeight - 8
	if y < innerTop || y >= innerBottom || a.rowHeight <= 0 {
		return 0, false
	}
	row := int((y - innerTop) / a.rowHeight)
	if row < 0 || row >= a.visibleRowCount() {
		return 0, false
	}
	actual := a.rowOffset + row
	if actual < 0 || actual >= len(a.records) {
		return 0, false
	}
	return actual, true
}

func mainWndProc(hwnd uintptr, msg uint32, wparam, lparam uintptr) uintptr {
	switch msg {
	case wmSize:
		if active != nil && active.hwnd != 0 {
			active.layout(int32(loword(lparam)), int32(hiword(lparam)))
		}
	case wmPaint:
		if active != nil {
			active.paint(hwnd)
			return 0
		}
	case wmEraseBkgnd:
		return 1
	case wmCommand:
		if active != nil {
			active.onCommand(wparam, lparam)
		}
	case wmLButtonDown:
		if active != nil {
			x, y := pointFromLParam(lparam)
			if active.onListMouse(x, y, false) {
				return 0
			}
		}
	case wmLButtonDblClk:
		if active != nil {
			x, y := pointFromLParam(lparam)
			if active.onListMouse(x, y, true) {
				return 0
			}
		}
	case wmMouseWheel:
		if active != nil {
			active.scrollRows(hiwordSigned(wparam))
		}
	case wmHotkey:
		if active != nil {
			active.toggleVisible()
		}
	case wmAppRefresh:
		if active != nil {
			active.reload()
		}
	case wmClose:
		if active != nil {
			procShowWindow.Call(hwnd, swHide)
			active.hidden = true
			return 0
		}
	case wmDestroy:
		if active != nil {
			active.disposeFonts()
		}
		procPostQuitMessage.Call(0)
		return 0
	}
	ret, _, _ := procDefWindowProc.Call(hwnd, uintptr(msg), wparam, lparam)
	return ret
}

func createControl(class, text string, style uint32, id int, parent uintptr) uintptr {
	hwnd, _, _ := procCreateWindowEx.Call(
		0,
		uintptr(unsafe.Pointer(utf16Ptr(class))),
		uintptr(unsafe.Pointer(utf16Ptr(text))),
		uintptr(style),
		0, 0, 100, 30,
		parent,
		uintptr(id),
		moduleHandle(),
		0,
	)
	return hwnd
}

func showRevealWindow(parent, anchor uintptr, revealed domain.RevealedSensitive, saveRemark func(string) error) {
	state := &revealState{parent: parent, anchor: anchor, value: revealed, originalRemark: revealed.Remark, saveRemark: saveRemark}
	currentReveal = state
	_ = registerRevealClass()
	x, y := popupPosition(parent, anchor)
	hwnd, _, _ := procCreateWindowEx.Call(
		0,
		uintptr(unsafe.Pointer(utf16Ptr("ClipSyncProRevealWindow"))),
		uintptr(unsafe.Pointer(utf16Ptr("查看"))),
		wsOverlapped|wsCaption|wsSysMenu|wsVisible,
		uintptr(x), uintptr(y), 340, 250,
		parent, 0, moduleHandle(), 0,
	)
	if hwnd == 0 {
		return
	}
	state.hwnd = hwnd
	procEnableWindow.Call(parent, 0)
	defer procEnableWindow.Call(parent, 1)
	procShowWindow.Call(hwnd, swShow)
	var m msg
	for !state.done {
		ret, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if ret == 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessage.Call(uintptr(unsafe.Pointer(&m)))
	}
}

type revealState struct {
	parent         uintptr
	anchor         uintptr
	hwnd           uintptr
	value          domain.RevealedSensitive
	done           bool
	remarkEditing  bool
	originalRemark string
	saveRemark     func(string) error
}

var currentReveal *revealState

func registerRevealClass() error {
	wnd := wndClassEx{
		Size:       uint32(unsafe.Sizeof(wndClassEx{})),
		WndProc:    syscall.NewCallback(revealWndProc),
		Instance:   moduleHandle(),
		ClassName:  utf16Ptr("ClipSyncProRevealWindow"),
		Background: 6,
	}
	ret, _, err := procRegisterClassEx.Call(uintptr(unsafe.Pointer(&wnd)))
	if ret == 0 {
		if errno, ok := err.(syscall.Errno); ok && errno == 1410 {
			return nil
		}
		return err
	}
	return nil
}

func revealWndProc(hwnd uintptr, msg uint32, wparam, lparam uintptr) uintptr {
	switch msg {
	case wmCreate:
		if currentReveal != nil {
			createControl("STATIC", "账户:", wsChild|wsVisible|ssLeft, idRevealAccountLabel, hwnd)
			createControl("STATIC", currentReveal.value.Account, wsChild|wsVisible|ssLeft, idRevealAccountValue, hwnd)
			createControl("BUTTON", "复制", wsChild|wsVisible|wsTabStop|bsPushButton, idRevealCopyAccount, hwnd)
			createControl("STATIC", "密码:", wsChild|wsVisible|ssLeft, idRevealPasswordLabel, hwnd)
			createControl("STATIC", currentReveal.value.Password, wsChild|wsVisible|ssLeft, idRevealPasswordValue, hwnd)
			createControl("BUTTON", "复制", wsChild|wsVisible|wsTabStop|bsPushButton, idRevealCopyPassword, hwnd)
			createControl("STATIC", "备注:", wsChild|wsVisible|ssLeft, idRevealRemarkLabel, hwnd)
			remark := createControl("EDIT", currentReveal.value.Remark, wsChild|wsVisible|wsBorder|wsTabStop|esAutoHScroll|esMultiline, idRevealRemarkValue, hwnd)
			procEnableWindow.Call(remark, 0)
			createControl("BUTTON", "修改", wsChild|wsVisible|wsTabStop|bsPushButton, idRevealModifyRemark, hwnd)
			createControl("STATIC", "", wsChild|wsVisible|ssLeft, idRevealStatus, hwnd)
			createControl("BUTTON", "确定", wsChild|wsVisible|wsTabStop|bsPushButton, idRevealOK, hwnd)
		}
	case wmSize:
		layoutReveal(hwnd)
	case wmCommand:
		id := loword(wparam)
		if currentReveal != nil && id == idRevealCopyAccount {
			if err := win32.WriteClipboardText(currentReveal.value.Account); err != nil {
				messageBox(hwnd, "复制失败: "+err.Error(), "查看")
			} else {
				setWindowText(childByID(hwnd, idRevealStatus), "已复制账户")
			}
			return 0
		}
		if currentReveal != nil && id == idRevealCopyPassword {
			if err := win32.WriteClipboardText(currentReveal.value.Password); err != nil {
				messageBox(hwnd, "复制失败: "+err.Error(), "查看")
			} else {
				setWindowText(childByID(hwnd, idRevealStatus), "已复制密码")
			}
			return 0
		}
		if currentReveal != nil && id == idRevealModifyRemark {
			currentReveal.remarkEditing = true
			remark := childByID(hwnd, idRevealRemarkValue)
			procEnableWindow.Call(remark, 1)
			procSetFocus.Call(remark)
			setWindowText(childByID(hwnd, idRevealStatus), "备注可修改，确定或关闭时保存")
			return 0
		}
		if currentReveal != nil && id == idRevealOK {
			if saveRevealRemarkAndClose(hwnd) {
				return 0
			}
			return 0
		}
	case wmClose:
		if currentReveal != nil && !saveRevealRemarkAndClose(hwnd) {
			return 0
		}
		return 0
	}
	ret, _, _ := procDefWindowProc.Call(hwnd, uintptr(msg), wparam, lparam)
	return ret
}

func layoutReveal(hwnd uintptr) {
	procMoveWindow.Call(childByID(hwnd, idRevealAccountLabel), 16, 18, 290, 20, 1)
	procMoveWindow.Call(childByID(hwnd, idRevealAccountValue), 16, 38, 222, 24, 1)
	procMoveWindow.Call(childByID(hwnd, idRevealCopyAccount), 248, 36, 58, 24, 1)
	procMoveWindow.Call(childByID(hwnd, idRevealPasswordLabel), 16, 74, 290, 20, 1)
	procMoveWindow.Call(childByID(hwnd, idRevealPasswordValue), 16, 94, 222, 24, 1)
	procMoveWindow.Call(childByID(hwnd, idRevealCopyPassword), 248, 92, 58, 24, 1)
	procMoveWindow.Call(childByID(hwnd, idRevealRemarkLabel), 16, 130, 290, 20, 1)
	procMoveWindow.Call(childByID(hwnd, idRevealRemarkValue), 16, 150, 222, 34, 1)
	procMoveWindow.Call(childByID(hwnd, idRevealModifyRemark), 248, 150, 58, 24, 1)
	procMoveWindow.Call(childByID(hwnd, idRevealStatus), 16, 190, 160, 22, 1)
	procMoveWindow.Call(childByID(hwnd, idRevealOK), 226, 186, 80, 28, 1)
}

func saveRevealRemarkAndClose(hwnd uintptr) bool {
	if currentReveal == nil {
		procDestroyWindow.Call(hwnd)
		return true
	}
	remark := getWindowText(childByID(hwnd, idRevealRemarkValue))
	if currentReveal.remarkEditing || remark != currentReveal.originalRemark {
		if currentReveal.saveRemark != nil {
			if err := currentReveal.saveRemark(remark); err != nil {
				messageBox(hwnd, "保存备注失败: "+err.Error(), "查看")
				return false
			}
		}
	}
	currentReveal.done = true
	procDestroyWindow.Call(hwnd)
	return true
}

func confirmSensitiveDelete(parent, anchor uintptr, rec domain.ClipboardRecord) bool {
	kind := "账户/密码"
	switch rec.SensitiveType {
	case domain.SensitiveAccount:
		kind = "账户"
	case domain.SensitivePassword:
		kind = "密码"
	}
	return confirmAt(parent, anchor, "删除", fmt.Sprintf("该内容已设置为%s，确定删除吗？", kind))
}

func confirmAt(parent, anchor uintptr, title, message string) bool {
	state := &confirmState{parent: parent, title: title, message: message}
	currentConfirm = state
	defer func() {
		currentConfirm = nil
	}()
	_ = registerConfirmClass()
	x, y := popupPosition(parent, anchor)
	hwnd, _, _ := procCreateWindowEx.Call(
		0,
		uintptr(unsafe.Pointer(utf16Ptr("ClipSyncProConfirmWindow"))),
		uintptr(unsafe.Pointer(utf16Ptr(title))),
		wsOverlapped|wsCaption|wsSysMenu|wsVisible,
		uintptr(x), uintptr(y), 320, 150,
		parent, 0, moduleHandle(), 0,
	)
	if hwnd == 0 {
		return false
	}
	state.hwnd = hwnd
	procEnableWindow.Call(parent, 0)
	defer procEnableWindow.Call(parent, 1)
	procShowWindow.Call(hwnd, swShow)
	var m msg
	for !state.done {
		ret, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if ret == 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessage.Call(uintptr(unsafe.Pointer(&m)))
	}
	return state.ok
}

type confirmState struct {
	parent  uintptr
	hwnd    uintptr
	title   string
	message string
	ok      bool
	done    bool
}

var currentConfirm *confirmState

func registerConfirmClass() error {
	wnd := wndClassEx{
		Size:       uint32(unsafe.Sizeof(wndClassEx{})),
		WndProc:    syscall.NewCallback(confirmWndProc),
		Instance:   moduleHandle(),
		ClassName:  utf16Ptr("ClipSyncProConfirmWindow"),
		Background: 6,
	}
	ret, _, err := procRegisterClassEx.Call(uintptr(unsafe.Pointer(&wnd)))
	if ret == 0 {
		if errno, ok := err.(syscall.Errno); ok && errno == 1410 {
			return nil
		}
		return err
	}
	return nil
}

func confirmWndProc(hwnd uintptr, msg uint32, wparam, lparam uintptr) uintptr {
	switch msg {
	case wmCreate:
		if currentConfirm != nil {
			createControl("STATIC", currentConfirm.message, wsChild|wsVisible|ssLeft, idConfirmMessage, hwnd)
			createControl("BUTTON", "确定", wsChild|wsVisible|wsTabStop|bsPushButton, idConfirmOK, hwnd)
			createControl("BUTTON", "取消", wsChild|wsVisible|wsTabStop|bsPushButton, idConfirmCancel, hwnd)
		}
	case wmSize:
		layoutConfirm(hwnd)
	case wmCommand:
		id := loword(wparam)
		if currentConfirm != nil && id == idConfirmOK {
			currentConfirm.ok = true
			currentConfirm.done = true
			procDestroyWindow.Call(hwnd)
			return 0
		}
		if currentConfirm != nil && id == idConfirmCancel {
			currentConfirm.done = true
			procDestroyWindow.Call(hwnd)
			return 0
		}
	case wmClose:
		if currentConfirm != nil {
			currentConfirm.done = true
		}
		procDestroyWindow.Call(hwnd)
		return 0
	}
	ret, _, _ := procDefWindowProc.Call(hwnd, uintptr(msg), wparam, lparam)
	return ret
}

func layoutConfirm(hwnd uintptr) {
	procMoveWindow.Call(childByID(hwnd, idConfirmMessage), 16, 18, 280, 44, 1)
	procMoveWindow.Call(childByID(hwnd, idConfirmOK), 136, 74, 74, 30, 1)
	procMoveWindow.Call(childByID(hwnd, idConfirmCancel), 222, 74, 74, 30, 1)
}

func promptText(parent uintptr, title, label, defaultValue string) (string, bool) {
	return promptInput(parent, 0, title, label, defaultValue, false)
}

func promptTextAt(parent, anchor uintptr, title, label, defaultValue string) (string, bool) {
	return promptInput(parent, anchor, title, label, defaultValue, false)
}

func promptPassword(parent uintptr, title, label, defaultValue string) (string, bool) {
	return promptInput(parent, 0, title, label, defaultValue, true)
}

func promptPasswordAt(parent, anchor uintptr, title, label, defaultValue string) (string, bool) {
	return promptInput(parent, anchor, title, label, defaultValue, true)
}

func promptInput(parent, anchor uintptr, title, label, defaultValue string, allowGenerate bool) (string, bool) {
	p := &promptState{parent: parent, title: title, label: label, value: defaultValue, allowGenerate: allowGenerate}
	currentPrompt = p
	_ = registerPromptClass()
	x, y := popupPosition(parent, anchor)
	hwnd, _, _ := procCreateWindowEx.Call(
		0,
		uintptr(unsafe.Pointer(utf16Ptr("ClipSyncProPromptWindow"))),
		uintptr(unsafe.Pointer(utf16Ptr(title))),
		wsOverlapped|wsCaption|wsSysMenu|wsVisible,
		uintptr(x), uintptr(y), 420, 160,
		parent, 0, moduleHandle(), 0,
	)
	if hwnd == 0 {
		return "", false
	}
	p.hwnd = hwnd
	procEnableWindow.Call(parent, 0)
	defer procEnableWindow.Call(parent, 1)
	procShowWindow.Call(hwnd, swShow)
	procSetFocus.Call(p.edit)
	var m msg
	for !p.done {
		ret, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if ret == 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessage.Call(uintptr(unsafe.Pointer(&m)))
	}
	return p.value, p.ok
}

type promptState struct {
	parent        uintptr
	hwnd          uintptr
	edit          uintptr
	title         string
	label         string
	value         string
	ok            bool
	done          bool
	allowGenerate bool
}

var currentPrompt *promptState

func registerPromptClass() error {
	wnd := wndClassEx{
		Size:       uint32(unsafe.Sizeof(wndClassEx{})),
		WndProc:    syscall.NewCallback(promptWndProc),
		Instance:   moduleHandle(),
		ClassName:  utf16Ptr("ClipSyncProPromptWindow"),
		Background: 6,
	}
	ret, _, err := procRegisterClassEx.Call(uintptr(unsafe.Pointer(&wnd)))
	if ret == 0 {
		if errno, ok := err.(syscall.Errno); ok && errno == 1410 {
			return nil
		}
		return err
	}
	return nil
}

func promptWndProc(hwnd uintptr, msg uint32, wparam, lparam uintptr) uintptr {
	switch msg {
	case wmCreate:
		if currentPrompt != nil {
			createControl("STATIC", currentPrompt.label, wsChild|wsVisible|ssLeft, 3001, hwnd)
			if currentPrompt.allowGenerate {
				createControl("BUTTON", "生成密码", wsChild|wsVisible|wsTabStop|bsPushButton, idPromptGenerate, hwnd)
			}
			currentPrompt.edit = createControl("EDIT", currentPrompt.value, wsChild|wsVisible|wsBorder|wsTabStop|esAutoHScroll, idPromptEdit, hwnd)
			createControl("BUTTON", "确定", wsChild|wsVisible|wsTabStop|bsPushButton, idPromptOK, hwnd)
			createControl("BUTTON", "取消", wsChild|wsVisible|wsTabStop|bsPushButton, idPromptCancel, hwnd)
		}
	case wmSize:
		procMoveWindow.Call(childByID(hwnd, 3001), 14, 16, 380, 22, 1)
		procMoveWindow.Call(childByID(hwnd, idPromptGenerate), 70, 12, 88, 28, 1)
		procMoveWindow.Call(childByID(hwnd, idPromptEdit), 14, 42, 380, 28, 1)
		procMoveWindow.Call(childByID(hwnd, idPromptOK), 226, 84, 78, 30, 1)
		procMoveWindow.Call(childByID(hwnd, idPromptCancel), 316, 84, 78, 30, 1)
	case wmCommand:
		id := loword(wparam)
		if currentPrompt != nil && id == idPromptGenerate {
			currentPrompt.value = generateComplexPassword(20)
			setWindowText(currentPrompt.edit, currentPrompt.value)
			procSetFocus.Call(currentPrompt.edit)
			return 0
		}
		if currentPrompt != nil && id == idPromptOK {
			currentPrompt.value = getWindowText(currentPrompt.edit)
			currentPrompt.ok = true
			currentPrompt.done = true
			procDestroyWindow.Call(hwnd)
			return 0
		}
		if currentPrompt != nil && id == idPromptCancel {
			currentPrompt.done = true
			procDestroyWindow.Call(hwnd)
			return 0
		}
	case wmClose:
		if currentPrompt != nil {
			currentPrompt.done = true
		}
		procDestroyWindow.Call(hwnd)
		return 0
	}
	ret, _, _ := procDefWindowProc.Call(hwnd, uintptr(msg), wparam, lparam)
	return ret
}

var procGetDlgItem = user32.NewProc("GetDlgItem")

func childByID(parent uintptr, id int) uintptr {
	hwnd, _, _ := procGetDlgItem.Call(parent, uintptr(id))
	return hwnd
}

func popupPosition(parent, anchor uintptr) (int32, int32) {
	if anchor != 0 {
		if r, ok := getWindowRect(anchor); ok {
			return r.Left, r.Bottom + 4
		}
	}
	if parent != 0 {
		if r, ok := getWindowRect(parent); ok {
			return r.Left + 120, r.Top + 120
		}
	}
	return 240, 180
}

func getWindowRect(hwnd uintptr) (rect, bool) {
	var r rect
	ok, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	return r, ok != 0
}

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}

type point struct {
	X int32
	Y int32
}

type rect struct {
	Left   int32
	Top    int32
	Right  int32
	Bottom int32
}

type msg struct {
	HWnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
}

type paintStruct struct {
	Hdc         uintptr
	Erase       int32
	RcPaint     rect
	Restore     int32
	IncUpdate   int32
	RgbReserved [32]byte
}

func utf16Ptr(s string) *uint16 {
	ptr, _ := syscall.UTF16PtrFromString(s)
	return ptr
}

func createUIFont(size int32, weight int) uintptr {
	font, _, _ := procCreateFont.Call(
		uintptr(int64(-size)),
		0,
		0,
		0,
		uintptr(weight),
		0,
		0,
		0,
		1,
		0,
		0,
		5,
		0,
		uintptr(unsafe.Pointer(utf16Ptr("Microsoft YaHei UI"))),
	)
	return font
}

func setControlFont(hwnd, font uintptr) {
	if hwnd != 0 && font != 0 {
		procSendMessage.Call(hwnd, wmSetFont, font, 1)
	}
}

func rgb(r, g, b byte) uintptr {
	return uintptr(r) | uintptr(g)<<8 | uintptr(b)<<16
}

func fillRect(hdc uintptr, r rect, color uintptr) {
	brush, _, _ := procCreateSolidBrush.Call(color)
	if brush == 0 {
		return
	}
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&r)), brush)
	procDeleteObject.Call(brush)
}

func strokeRect(hdc uintptr, r rect, color uintptr) {
	fillRect(hdc, rect{Left: r.Left, Top: r.Top, Right: r.Right, Bottom: r.Top + 1}, color)
	fillRect(hdc, rect{Left: r.Left, Top: r.Bottom - 1, Right: r.Right, Bottom: r.Bottom}, color)
	fillRect(hdc, rect{Left: r.Left, Top: r.Top, Right: r.Left + 1, Bottom: r.Bottom}, color)
	fillRect(hdc, rect{Left: r.Right - 1, Top: r.Top, Right: r.Right, Bottom: r.Bottom}, color)
}

func insetRect(r rect, x, y int32) rect {
	return rect{Left: r.Left + x, Top: r.Top + y, Right: r.Right - x, Bottom: r.Bottom - y}
}

func drawText(hdc uintptr, text string, r rect, color, font uintptr, flags uint32) {
	var oldFont uintptr
	if font != 0 {
		oldFont, _, _ = procSelectObject.Call(hdc, font)
	}
	procSetBkMode.Call(hdc, transparent)
	procSetTextColor.Call(hdc, color)
	procDrawText.Call(
		hdc,
		uintptr(unsafe.Pointer(utf16Ptr(text))),
		^uintptr(0),
		uintptr(unsafe.Pointer(&r)),
		uintptr(flags),
	)
	if oldFont != 0 {
		procSelectObject.Call(hdc, oldFont)
	}
}

func setWindowText(hwnd uintptr, text string) {
	procSetWindowText.Call(hwnd, uintptr(unsafe.Pointer(utf16Ptr(text))))
}

func getWindowText(hwnd uintptr) string {
	n, _, _ := procGetWindowTextLen.Call(hwnd)
	buf := make([]uint16, n+1)
	procGetWindowText.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), n+1)
	return syscall.UTF16ToString(buf)
}

func messageBox(parent uintptr, text, title string) {
	procMessageBox.Call(parent, uintptr(unsafe.Pointer(utf16Ptr(text))), uintptr(unsafe.Pointer(utf16Ptr(title))), 0)
}

func moduleHandle() uintptr {
	h, _, _ := procGetModuleHandle.Call(0)
	return h
}

func loword(v uintptr) int {
	return int(uint16(v & 0xffff))
}

func hiword(v uintptr) int {
	return int(uint16((v >> 16) & 0xffff))
}

func hiwordSigned(v uintptr) int {
	return int(int16(uint16((v >> 16) & 0xffff)))
}

func pointFromLParam(v uintptr) (int32, int32) {
	return int32(int16(uint16(v & 0xffff))), int32(int16(uint16((v >> 16) & 0xffff)))
}

func shortRecordID(id string) string {
	runes := []rune(id)
	if len(runes) <= 18 {
		return id
	}
	return string(runes[:18]) + "..."
}

func rowCommandID(row, action int) int {
	return rowIDBase + row*10 + action
}

func parseRowCommandID(id int) (int, int, bool) {
	if id < rowIDBase {
		return 0, 0, false
	}
	offset := id - rowIDBase
	return offset / 10, offset % 10, true
}

func generateComplexPassword(length int) string {
	if length < 12 {
		length = 12
	}
	classes := []string{
		"ABCDEFGHJKLMNPQRSTUVWXYZ",
		"abcdefghijkmnopqrstuvwxyz",
		"23456789",
		"!@#$%^&*()-_=+[]{};:,.?",
	}
	all := strings.Join(classes, "")
	out := make([]byte, length)
	for i, class := range classes {
		out[i] = randomChar(class)
	}
	for i := len(classes); i < length; i++ {
		out[i] = randomChar(all)
	}
	for i := len(out) - 1; i > 0; i-- {
		j := int(randomByte()) % (i + 1)
		out[i], out[j] = out[j], out[i]
	}
	return string(out)
}

func randomChar(alphabet string) byte {
	return alphabet[int(randomByte())%len(alphabet)]
}

func randomByte() byte {
	var b [1]byte
	if _, err := rand.Read(b[:]); err != nil {
		return byte(time.Now().UnixNano())
	}
	return b[0]
}
