using System.ComponentModel;
using System.Net.Http.Json;
using System.Runtime.CompilerServices;
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

    public async Task<IReadOnlyList<ClipboardRecordView>> ListAsync(string keyword, string date, bool sensitiveOnly, bool favoriteOnly, int offset, CancellationToken cancellationToken, bool excludeFavorite = false)
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
        if (favoriteOnly)
        {
            query.Add("favorite=true");
        }
        if (excludeFavorite)
        {
            query.Add("exclude_favorite=true");
        }
        query.Add("offset=" + offset);
        var suffix = query.Count == 0 ? "" : "?" + string.Join("&", query);
        var result = await GetAsync<RecordsResponse>("api/records" + suffix, cancellationToken);
        return result.Records;
    }

    public Task SetFavoriteAsync(string id, bool isFavorite, CancellationToken cancellationToken)
    {
        return PostNoContentAsync($"api/records/{Uri.EscapeDataString(id)}/favorite", new { is_favorite = isFavorite }, cancellationToken);
    }

    public async Task<IReadOnlyList<PromptView>> ListPromptsAsync(string category, string keyword, CancellationToken cancellationToken)
    {
        var query = new List<string>();
        if (!string.IsNullOrWhiteSpace(category))
        {
            query.Add("category=" + Uri.EscapeDataString(category.Trim()));
        }
        if (!string.IsNullOrWhiteSpace(keyword))
        {
            query.Add("keyword=" + Uri.EscapeDataString(keyword.Trim()));
        }
        var suffix = query.Count == 0 ? "" : "?" + string.Join("&", query);
        var result = await GetAsync<PromptsResponse>("api/prompts" + suffix, cancellationToken);
        return result.Prompts;
    }

    public Task<PromptView> SavePromptAsync(PromptView prompt, CancellationToken cancellationToken)
    {
        return PostAsync<PromptView>("api/prompts", prompt, cancellationToken);
    }

    public async Task DeletePromptAsync(string id, CancellationToken cancellationToken)
    {
        using var response = await _http.DeleteAsync($"api/prompts/{Uri.EscapeDataString(id)}", cancellationToken);
        await EnsureSuccessAsync(response, cancellationToken);
    }

    public Task UsePromptAsync(string id, CancellationToken cancellationToken)
    {
        return PostNoContentAsync($"api/prompts/{Uri.EscapeDataString(id)}/use", new { }, cancellationToken);
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
        return PasteAsync(id, field, 0, cancellationToken);
    }

    public Task PasteAsync(string id, string field, ulong targetHwnd, CancellationToken cancellationToken)
    {
        return PostNoContentAsync($"api/records/{Uri.EscapeDataString(id)}/paste", new { field, target_hwnd = targetHwnd }, cancellationToken);
    }

    public async Task DeleteAsync(string id, CancellationToken cancellationToken)
    {
        using var response = await _http.DeleteAsync($"api/records/{Uri.EscapeDataString(id)}", cancellationToken);
        await EnsureSuccessAsync(response, cancellationToken);
    }

    public Task BatchDeleteAsync(IEnumerable<string> ids, bool skipSensitive, bool skipFavorite, CancellationToken cancellationToken)
    {
        return PostNoContentAsync("api/records/batch-delete", new { ids = ids.ToArray(), skip_sensitive = skipSensitive, skip_favorite = skipFavorite }, cancellationToken);
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
        var body = await response.Content.ReadAsStringAsync(cancellationToken);
        var message = body;
        var errorCode = "";
        try
        {
            using var document = JsonDocument.Parse(body);
            if (document.RootElement.TryGetProperty("error", out var error))
            {
                message = error.GetString() ?? message;
            }
            if (document.RootElement.TryGetProperty("code", out var code))
            {
                errorCode = code.GetString() ?? "";
            }
        }
        catch (JsonException)
        {
            // Preserve the raw response for non-JSON failures.
        }
        if (string.IsNullOrWhiteSpace(message))
        {
            message = response.ReasonPhrase ?? "Agent request failed.";
        }
        throw new AgentRequestException((int)response.StatusCode, errorCode, message);
    }
}

public sealed class AgentRequestException : InvalidOperationException
{
    public AgentRequestException(int statusCode, string errorCode, string message)
        : base(message)
    {
        StatusCode = statusCode;
        ErrorCode = errorCode;
    }

    public int StatusCode { get; }

    public string ErrorCode { get; }
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

public sealed class ClipboardRecordView : INotifyPropertyChanged
{
    public event PropertyChangedEventHandler? PropertyChanged;

    private void OnPropertyChanged([CallerMemberName] string? propertyName = null)
    {
        PropertyChanged?.Invoke(this, new PropertyChangedEventArgs(propertyName));
    }

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

    private bool _isFavorite;

    [JsonPropertyName("is_favorite")]
    public bool IsFavorite
    {
        get => _isFavorite;
        set
        {
            if (_isFavorite != value)
            {
                _isFavorite = value;
                OnPropertyChanged();
                OnPropertyChanged(nameof(IsFavoriteGlyph));
                OnPropertyChanged(nameof(IsFavoriteBrush));
                OnPropertyChanged(nameof(StarToolTip));
            }
        }
    }

    [JsonPropertyName("favorite_at")]
    public DateTime? FavoriteAt { get; set; }

    public string IsFavoriteGlyph => IsFavorite ? "\uE735" : "\uE734";

    public string IsFavoriteBrush => IsFavorite ? "#FFB900" : "#8A8A8A";

    public string StarToolTip => IsFavorite ? "取消收藏" : "加入收藏";

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

public sealed class PromptsResponse
{
    [JsonPropertyName("prompts")]
    public List<PromptView> Prompts { get; set; } = [];
}

public sealed class PromptView
{
    [JsonPropertyName("id")]
    public string Id { get; set; } = "";

    [JsonPropertyName("title")]
    public string Title { get; set; } = "";

    [JsonPropertyName("category")]
    public string Category { get; set; } = "通用";

    [JsonPropertyName("template_content")]
    public string TemplateContent { get; set; } = "";

    [JsonPropertyName("variables")]
    public List<string> Variables { get; set; } = [];

    [JsonPropertyName("shortcut")]
    public string Shortcut { get; set; } = "";

    [JsonPropertyName("usage_count")]
    public int UsageCount { get; set; }

    [JsonPropertyName("is_favorite")]
    public bool IsFavorite { get; set; }

    public string VariablesSummary => Variables.Count > 0 ? "插槽: " + string.Join(", ", Variables) : "无变量";

    public string UsageSummary => $"调用 {UsageCount} 次";
}


