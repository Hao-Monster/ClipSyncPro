using Microsoft.UI.Composition;
using Microsoft.UI.Composition.SystemBackdrops;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Media;
using Windows.UI;

namespace ClipSyncPro_WinUI;

public sealed class TerminalAcrylicBackdrop : SystemBackdrop
{
    private DesktopAcrylicController? _controller;
    private SystemBackdropConfiguration? _configuration;

    protected override void OnTargetConnected(ICompositionSupportsSystemBackdrop connectedTarget, XamlRoot xamlRoot)
    {
        if (!DesktopAcrylicController.IsSupported())
        {
            return;
        }

        _configuration = new SystemBackdropConfiguration
        {
            IsInputActive = true,
            Theme = SystemBackdropTheme.Dark,
        };

        _controller = new DesktopAcrylicController
        {
            TintColor = Color.FromArgb(255, 0, 0, 0),
            TintOpacity = 0.58f,
            LuminosityOpacity = 0.08f,
            FallbackColor = Color.FromArgb(255, 12, 12, 12),
        };
        _controller.AddSystemBackdropTarget(connectedTarget);
        _controller.SetSystemBackdropConfiguration(_configuration);
    }

    protected override void OnTargetDisconnected(ICompositionSupportsSystemBackdrop disconnectedTarget)
    {
        _controller?.RemoveSystemBackdropTarget(disconnectedTarget);
        _controller?.Dispose();
        _controller = null;
        _configuration = null;
    }

    protected override void OnDefaultSystemBackdropConfigurationChanged(ICompositionSupportsSystemBackdrop target, XamlRoot xamlRoot)
    {
        if (_configuration is not null)
        {
            _configuration.IsInputActive = true;
            _configuration.Theme = SystemBackdropTheme.Dark;
        }
    }
}
