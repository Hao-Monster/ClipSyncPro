using System.Net.Http.Json;
using System.Text;
using System.Text.Json;
using System.Text.Json.Serialization;

namespace ClipSyncPro_WinUI;

public sealed class AgentClient
{
    private readonly HttpClient _http;
    private readonly JsonSerializerOptions _json = new(JsonSerializerDefaults.Web);

    public AgentClient()
    {
        var baseUrl = Environment.GetEnvironmentVariable("CLIPSYNC_UI_URL");
        if (string.IsNullOrWhiteSpace(baseUrl))
        {
            baseUrl = "http://127.0.0.1:37618";
        }
        _http = new HttpClient { BaseAddress = new Uri(baseUrl.TrimEnd('/') + "/") };
    }

    public async Task<AgentStatus> StatusAsync(CancellationToken cancellationToken)
    {
        return await GetAsync<AgentStatus>("api/status", cancellationToken);
    }

    public async Task<IReadOnlyList<ClipboardRecordView>> ListAsync(string keyword, string date, bool sensitiveOnly, CancellationToken cancellationToken)
    {
        var query = new List<string>();
        if (!string.IsNullOrWhiteSpace(keyword))
        {
            query.Add("keyword=" + Uri.EscapeDataString(keyword.Trim()));
        }
        if (!string.IsNullOrWhiteSpace(date))
        {
            query.Add("date=" + Uri.EscapeDataString(date.Trim()));
        }
        if (sensitiveOnly)
        {
            query.Add("sensitive=true");
        }
        var suffix = query.Count == 0 ? "" : "?" + string.Join("&", query);
        var result = await GetAsync<RecordsResponse>("api/records" + suffix, cancellationToken);
        return result.Records;
    }

    public Task<RevealedSensitive> RevealAsync(string id, CancellationToken cancellationToken)
    {
        return PostAsync<RevealedSensitive>($"api/records/{Uri.EscapeDataString(id)}/reveal", new { }, cancellationToken);
    }

    public Task MarkPasswordAsync(string id, string account, string remark, CancellationToken cancellationToken)
    {
        return PostNoContentAsync($"api/records/{Uri.EscapeDataString(id)}/mark-password", new { account, remark }, cancellationToken);
    }

    public Task MarkAccountAsync(string id, string password, string remark, CancellationToken cancellationToken)
    {
        return PostNoContentAsync($"api/records/{Uri.EscapeDataString(id)}/mark-account", new { password, remark }, cancellationToken);
    }

    public Task UpdateRemarkAsync(string id, string remark, CancellationToken cancellationToken)
    {
        return PostNoContentAsync($"api/records/{Uri.EscapeDataString(id)}/remark", new { remark }, cancellationToken);
    }

    public Task CopyAsync(string id, string field, CancellationToken cancellationToken)
    {
        return PostNoContentAsync($"api/records/{Uri.EscapeDataString(id)}/copy", new { field }, cancellationToken);
    }

    public Task PasteAsync(string id, string field, CancellationToken cancellationToken)
    {
        return PostNoContentAsync($"api/records/{Uri.EscapeDataString(id)}/paste", new { field }, cancellationToken);
    }

    public async Task DeleteAsync(string id, CancellationToken cancellationToken)
    {
        using var response = await _http.DeleteAsync($"api/records/{Uri.EscapeDataString(id)}", cancellationToken);
        await EnsureSuccessAsync(response, cancellationToken);
    }

    public Task BatchDeleteAsync(IEnumerable<string> ids, bool skipSensitive, CancellationToken cancellationToken)
    {
        return PostNoContentAsync("api/records/batch-delete", new { ids = ids.ToArray(), skip_sensitive = skipSensitive }, cancellationToken);
    }

    public Task CaptureCurrentAsync(CancellationToken cancellationToken)
    {
        return PostNoContentAsync("api/clipboard/capture-current", new { }, cancellationToken);
    }

    public async Task<Stream> OpenEventsAsync(CancellationToken cancellationToken)
    {
        return await _http.GetStreamAsync("api/events", cancellationToken);
    }

    private async Task<T> GetAsync<T>(string path, CancellationToken cancellationToken)
    {
        using var response = await _http.GetAsync(path, cancellationToken);
        await EnsureSuccessAsync(response, cancellationToken);
        var value = await response.Content.ReadFromJsonAsync<T>(_json, cancellationToken);
        return value ?? throw new InvalidOperationException("Agent returned empty JSON.");
    }

    private async Task<T> PostAsync<T>(string path, object body, CancellationToken cancellationToken)
    {
        using var response = await _http.PostAsJsonAsync(path, body, _json, cancellationToken);
        await EnsureSuccessAsync(response, cancellationToken);
        var value = await response.Content.ReadFromJsonAsync<T>(_json, cancellationToken);
        return value ?? throw new InvalidOperationException("Agent returned empty JSON.");
    }

    private async Task PostNoContentAsync(string path, object body, CancellationToken cancellationToken)
    {
        using var response = await _http.PostAsJsonAsync(path, body, _json, cancellationToken);
        await EnsureSuccessAsync(response, cancellationToken);
    }

    private static async Task EnsureSuccessAsync(HttpResponseMessage response, CancellationToken cancellationToken)
    {
        if (response.IsSuccessStatusCode)
        {
            return;
        }
        var message = await response.Content.ReadAsStringAsync(cancellationToken);
        if (string.IsNullOrWhiteSpace(message))
        {
            message = response.ReasonPhrase ?? "Agent request failed.";
        }
        throw new InvalidOperationException(message);
    }
}

public sealed class AgentStatus
{
    [JsonPropertyName("status")]
    public string Status { get; set; } = "";

    [JsonPropertyName("device_id")]
    public string DeviceId { get; set; } = "";

    [JsonPropertyName("server_url")]
    public string ServerUrl { get; set; } = "";

    [JsonPropertyName("ui_api_version")]
    public string ApiVersion { get; set; } = "";
}

public sealed class RecordsResponse
{
    [JsonPropertyName("records")]
    public List<ClipboardRecordView> Records { get; set; } = [];
}

public sealed class ClipboardRecordView
{
    [JsonPropertyName("id")]
    public string Id { get; set; } = "";

    [JsonPropertyName("summary")]
    public string Summary { get; set; } = "";

    [JsonPropertyName("content_text")]
    public string ContentText { get; set; } = "";

    [JsonPropertyName("content_type")]
    public string ContentType { get; set; } = "";

    [JsonPropertyName("is_sensitive")]
    public bool IsSensitive { get; set; }

    [JsonPropertyName("sensitive_type")]
    public string SensitiveType { get; set; } = "";

    [JsonPropertyName("source_platform")]
    public string SourcePlatform { get; set; } = "";

    [JsonPropertyName("display_time")]
    public string DisplayTime { get; set; } = "";

    [JsonPropertyName("sync_status")]
    public string SyncStatus { get; set; } = "";

    [JsonPropertyName("image_url")]
    public string ImageUrl { get; set; } = "";

    [JsonPropertyName("revealed")]
    public RevealedSensitive? Revealed { get; set; }

    public bool IsImage => ContentType == "image";

    public double ImagePreviewWidth => IsImage ? 56 : 0;

    public string KindLabel
    {
        get
        {
            if (!IsSensitive)
            {
                return ContentType switch
                {
                    "image" => "图片",
                    "url" => "链接",
                    "number" => "数字",
                    _ => "文本"
                };
            }
            return SensitiveType switch
            {
                "account" => "账户",
                "password" => "密码",
                _ => "敏感"
            };
        }
    }
}

public sealed class RevealedSensitive
{
    [JsonPropertyName("record_id")]
    public string RecordId { get; set; } = "";

    [JsonPropertyName("content")]
    public string Content { get; set; } = "";

    [JsonPropertyName("account")]
    public string Account { get; set; } = "";

    [JsonPropertyName("password")]
    public string Password { get; set; } = "";

    [JsonPropertyName("remark")]
    public string Remark { get; set; } = "";

    [JsonPropertyName("expires")]
    public string Expires { get; set; } = "";
}
