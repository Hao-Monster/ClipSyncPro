using System.Diagnostics;

namespace ClipSyncPro_WinUI;

public static class AgentLauncher
{
    private static readonly Uri StatusUri = new("http://127.0.0.1:37618/api/status");

    public static async Task EnsureRunningAsync(CancellationToken cancellationToken)
    {
        if (await IsAgentReadyAsync(cancellationToken))
        {
            return;
        }

        var agentPath = FindAgentPath();
        if (agentPath is null)
        {
            return;
        }

        Process.Start(new ProcessStartInfo
        {
            FileName = agentPath,
            WorkingDirectory = Path.GetDirectoryName(agentPath) ?? AppContext.BaseDirectory,
            UseShellExecute = false,
            CreateNoWindow = true,
            WindowStyle = ProcessWindowStyle.Hidden,
        });

        var deadline = DateTimeOffset.UtcNow.AddSeconds(8);
        while (DateTimeOffset.UtcNow < deadline && !cancellationToken.IsCancellationRequested)
        {
            if (await IsAgentReadyAsync(cancellationToken))
            {
                return;
            }
            await Task.Delay(350, cancellationToken);
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
}
