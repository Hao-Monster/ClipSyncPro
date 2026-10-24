using Microsoft.Win32;

namespace ClipSyncPro_WinUI;

public static class StartupRegistration
{
    private const string RunKeyPath = @"Software\Microsoft\Windows\CurrentVersion\Run";
    private const string ValueName = "ClipSyncPro";

    public static string? LastError { get; private set; }

    public static void EnsureEnabled()
    {
        try
        {
            var executable = Environment.ProcessPath;
            if (string.IsNullOrWhiteSpace(executable))
            {
                throw new InvalidOperationException("无法获取程序路径。");
            }

            using var key = Registry.CurrentUser.CreateSubKey(RunKeyPath, writable: true)
                ?? throw new InvalidOperationException("无法打开当前用户的启动项注册表。");
            key.SetValue(ValueName, $"\"{executable}\"", RegistryValueKind.String);
            LastError = null;
        }
        catch (Exception ex)
        {
            LastError = ex.Message;
        }
    }
}
