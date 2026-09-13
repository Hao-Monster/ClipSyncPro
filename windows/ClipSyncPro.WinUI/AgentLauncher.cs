using System.Diagnostics;

namespace ClipSyncPro_WinUI;

public static class AgentLauncher
{
    private static readonly Uri StatusUri = new("http://127.0.0.1:37618/api/status");
    private static readonly SemaphoreSlim StartGate = new(1, 1);
    private static Process? _ownedProcess;

    public static string? LastError { get; private set; }

    public static async Task EnsureRunningAsync(CancellationToken cancellationToken)
    {
        await StartGate.WaitAsync(cancellationToken);
        try
        {
            if (await IsAgentReadyAsync(cancellationToken))
            {
                LastError = null;
                return;
            }

            var agentPath = FindAgentPath();
            if (agentPath is null)
            {
                LastError = "找不到本地 agent 文件。";
                return;
            }

            if (!IsProcessRunning(_ownedProcess))
            {
                _ownedProcess = Process.Start(new ProcessStartInfo
                {
                    FileName = agentPath,
                    WorkingDirectory = Path.GetDirectoryName(agentPath) ?? AppContext.BaseDirectory,
                    UseShellExecute = false,
                    CreateNoWindow = true,
                    WindowStyle = ProcessWindowStyle.Hidden,
                });
            }

            var deadline = DateTimeOffset.UtcNow.AddSeconds(8);
            while (DateTimeOffset.UtcNow < deadline && !cancellationToken.IsCancellationRequested)
            {
                if (await IsAgentReadyAsync(cancellationToken))
                {
                    LastError = null;
                    return;
                }
                await Task.Delay(350, cancellationToken);
            }
            LastError = "本地 agent 启动超时，系统将自动重试。";
        }
        catch (OperationCanceledException) when (cancellationToken.IsCancellationRequested)
        {
            throw;
        }
        catch (Exception ex)
        {
            LastError = $"本地 agent 启动失败：{ex.Message}";
        }
        finally
        {
            StartGate.Release();
        }
    }

    public static async Task WatchAsync(CancellationToken cancellationToken)
    {
        var retryDelay = TimeSpan.FromSeconds(2);
        while (!cancellationToken.IsCancellationRequested)
        {
            try
            {
                await EnsureRunningAsync(cancellationToken);
                retryDelay = TimeSpan.FromSeconds(2);
                await Task.Delay(TimeSpan.FromSeconds(3), cancellationToken);
            }
            catch (OperationCanceledException) when (cancellationToken.IsCancellationRequested)
            {
                return;
            }
            catch
            {
                await Task.Delay(retryDelay, cancellationToken);
                retryDelay = retryDelay < TimeSpan.FromSeconds(30)
                    ? retryDelay + retryDelay
                    : TimeSpan.FromSeconds(30);
            }
        }
    }

    private static async Task<bool> IsAgentReadyAsync(CancellationToken cancellationToken)
    {
        try
        {
            using var client = new HttpClient { Timeout = TimeSpan.FromSeconds(1.5) };
            using var response = await client.GetAsync(StatusUri, cancellationToken);
            return response.IsSuccessStatusCode;
        }
        catch
        {
            return false;
        }
    }

    private static string? FindAgentPath()
    {
        var names = new[]
        {
            Path.Combine(AppContext.BaseDirectory, "ClipSyncPro-Agent.exe"),
            Path.GetFullPath(Path.Combine(AppContext.BaseDirectory, "..", "..", "..", "..", "..", "..", "dist", "ClipSyncPro-Agent.exe")),
            Path.GetFullPath(Path.Combine(Environment.CurrentDirectory, "dist", "ClipSyncPro-Agent.exe")),
        };
        return names.FirstOrDefault(File.Exists);
    }

    private static bool IsProcessRunning(Process? process)
    {
        try
        {
            return process is not null && !process.HasExited;
        }
        catch
        {
            return false;
        }
    }
}
