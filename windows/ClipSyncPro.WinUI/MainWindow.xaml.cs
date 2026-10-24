using Microsoft.UI.Xaml;
using Microsoft.UI;
using Microsoft.UI.Windowing;
using System.Runtime.InteropServices;
using WinRT.Interop;

// To learn more about WinUI, the WinUI project structure,
// and more about our project templates, see: http://aka.ms/winui-project-info.

namespace ClipSyncPro_WinUI;

/// <summary>
/// The application window. This hosts a Frame that displays pages. Add your
/// UI and logic to MainPage.xaml / MainPage.xaml.cs instead of here so you
/// can use Page features such as navigation events and the Loaded lifecycle.
/// </summary>
public sealed partial class MainWindow : Window
{
    private const int HotkeyID = 37618;
    private const int ModAlt = 0x0001;
    private const int ModNoRepeat = 0x4000;
    private const int VkV = 0x56;
    private const int WmHotkey = 0x0312;
    private const int GwlpWndProc = -4;
    private const int SwMinimize = 6;
    private const int SwRestore = 9;
    private const int DwmwaSystemBackdropType = 38;
    private const int DwmSbtTransientWindow = 3;
    private const int MonitorDefaultToNearest = 2;
    private const int WindowEdgeMargin = 24;

    private readonly IntPtr _hwnd;
    private readonly WndProcDelegate _wndProc;
    private readonly IntPtr _oldWndProc;
    private IntPtr _pasteTargetHwnd;
    private bool _minimizedByHotkey;

    public MainWindow()
    {
        InitializeComponent();

        SystemBackdrop = new TerminalAcrylicBackdrop();
        ExtendsContentIntoTitleBar = true;
        SetTitleBar(AppTitleBar);

        AppWindow.SetIcon("Assets/AppIcon.ico");

        AppWindow.Resize(new Windows.Graphics.SizeInt32(800, 800));

        _hwnd = WindowNative.GetWindowHandle(this);
        _pasteTargetHwnd = GetForegroundWindow();
        ConfigureAcrylicWindow();
        PositionTopRight();
        _wndProc = WndProc;
        _oldWndProc = SetWindowLongPtr(_hwnd, GwlpWndProc, Marshal.GetFunctionPointerForDelegate(_wndProc));
        _ = RegisterHotKey(_hwnd, HotkeyID, ModAlt | ModNoRepeat, VkV);
        Closed += MainWindow_Closed;

        // Navigate the root frame to the main page on startup.
        RootFrame.Navigate(typeof(MainPage));
    }

    private void PositionTopRight()
    {
        var monitor = MonitorFromWindow(_hwnd, MonitorDefaultToNearest);
        if (monitor == IntPtr.Zero)
        {
            return;
        }

        var monitorInfo = new MonitorInfo { CbSize = Marshal.SizeOf<MonitorInfo>() };
        if (!GetMonitorInfo(monitor, ref monitorInfo))
        {
            return;
        }

        var size = AppWindow.Size;
        var x = monitorInfo.WorkArea.Right - size.Width - WindowEdgeMargin;
        var y = monitorInfo.WorkArea.Top + WindowEdgeMargin;
        AppWindow.Move(new Windows.Graphics.PointInt32(x, y));
    }

    private void ConfigureAcrylicWindow()
    {
        if (AppWindowTitleBar.IsCustomizationSupported())
        {
            var titleBar = AppWindow.TitleBar;
            titleBar.BackgroundColor = Colors.Transparent;
            titleBar.ForegroundColor = Windows.UI.Color.FromArgb(255, 245, 247, 250);
            titleBar.InactiveBackgroundColor = Colors.Transparent;
            titleBar.InactiveForegroundColor = Windows.UI.Color.FromArgb(180, 245, 247, 250);
            titleBar.ButtonBackgroundColor = Colors.Transparent;
            titleBar.ButtonForegroundColor = Windows.UI.Color.FromArgb(255, 245, 247, 250);
            titleBar.ButtonInactiveBackgroundColor = Colors.Transparent;
            titleBar.ButtonInactiveForegroundColor = Windows.UI.Color.FromArgb(180, 245, 247, 250);
            titleBar.ButtonHoverBackgroundColor = Windows.UI.Color.FromArgb(30, 255, 255, 255);
            titleBar.ButtonHoverForegroundColor = Windows.UI.Color.FromArgb(255, 245, 247, 250);
            titleBar.ButtonPressedBackgroundColor = Windows.UI.Color.FromArgb(48, 255, 255, 255);
            titleBar.ButtonPressedForegroundColor = Windows.UI.Color.FromArgb(255, 245, 247, 250);
        }

        var backdropType = DwmSbtTransientWindow;
        _ = DwmSetWindowAttribute(_hwnd, DwmwaSystemBackdropType, ref backdropType, sizeof(int));
    }

    private void MainWindow_Closed(object sender, WindowEventArgs args)
    {
        UnregisterHotKey(_hwnd, HotkeyID);
        if (_oldWndProc != IntPtr.Zero)
        {
            SetWindowLongPtr(_hwnd, GwlpWndProc, _oldWndProc);
        }
    }

    private IntPtr WndProc(IntPtr hwnd, uint msg, UIntPtr wParam, IntPtr lParam)
    {
        if (msg == 0x0006 && (wParam.ToUInt32() & 0xFFFF) == 0)
        {
            CapturePasteTarget();
        }

        if (msg == 0x0010) { ShowWindow(_hwnd, SwMinimize); _minimizedByHotkey = true; return IntPtr.Zero; }

        if (msg == WmHotkey && wParam.ToUInt32() == HotkeyID)
        {
            CapturePasteTarget();
            ToggleVisible();
            return IntPtr.Zero;
        }
        return CallWindowProc(_oldWndProc, hwnd, msg, wParam, lParam);
    }

    public void FocusPasteTarget()
    {
        if (_pasteTargetHwnd == IntPtr.Zero || _pasteTargetHwnd == _hwnd)
        {
            return;
        }

        SetForegroundWindow(_pasteTargetHwnd);
    }

    public ulong PasteTargetHandle => unchecked((ulong)_pasteTargetHwnd.ToInt64());

    private void CapturePasteTarget()
    {
        var foreground = GetForegroundWindow();
        if (foreground != IntPtr.Zero && foreground != _hwnd)
        {
            _pasteTargetHwnd = foreground;
        }
    }

    private void ToggleVisible()
    {
        if (_minimizedByHotkey || IsIconic(_hwnd))
        {
            ShowWindow(_hwnd, SwRestore);
            SetForegroundWindow(_hwnd);
            Activate();
            _minimizedByHotkey = false;
            return;
        }
        ShowWindow(_hwnd, SwMinimize);
        _minimizedByHotkey = true;
    }

    private delegate IntPtr WndProcDelegate(IntPtr hwnd, uint msg, UIntPtr wParam, IntPtr lParam);

    [DllImport("user32.dll")]
    private static extern bool RegisterHotKey(IntPtr hWnd, int id, int fsModifiers, int vk);

    [DllImport("user32.dll")]
    private static extern bool UnregisterHotKey(IntPtr hWnd, int id);

    [DllImport("user32.dll", EntryPoint = "SetWindowLongPtrW")]
    private static extern IntPtr SetWindowLongPtr(IntPtr hWnd, int nIndex, IntPtr dwNewLong);

    [DllImport("user32.dll")]
    private static extern IntPtr CallWindowProc(IntPtr lpPrevWndFunc, IntPtr hWnd, uint msg, UIntPtr wParam, IntPtr lParam);

    [DllImport("user32.dll")]
    private static extern bool ShowWindow(IntPtr hWnd, int nCmdShow);

    [DllImport("user32.dll")]
    private static extern bool SetForegroundWindow(IntPtr hWnd);

    [DllImport("user32.dll")]
    private static extern IntPtr GetForegroundWindow();

    [DllImport("user32.dll")]
    private static extern bool IsIconic(IntPtr hWnd);

    [DllImport("user32.dll")]
    private static extern IntPtr MonitorFromWindow(IntPtr hWnd, int dwFlags);

    [DllImport("user32.dll", CharSet = CharSet.Unicode)]
    private static extern bool GetMonitorInfo(IntPtr hMonitor, ref MonitorInfo lpmi);

    [StructLayout(LayoutKind.Sequential, CharSet = CharSet.Unicode)]
    private struct MonitorInfo
    {
        public int CbSize;
        public RectI Monitor;
        public RectI WorkArea;
        public int Flags;
    }

    [StructLayout(LayoutKind.Sequential)]
    private struct RectI
    {
        public int Left;
        public int Top;
        public int Right;
        public int Bottom;
    }

    [DllImport("dwmapi.dll")]
    private static extern int DwmSetWindowAttribute(IntPtr hwnd, int dwAttribute, ref int pvAttribute, int cbAttribute);
}



