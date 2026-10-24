using System.Collections.Concurrent;
using System.Collections.ObjectModel;
using System.Security.Cryptography;
using System.Text.Json;
using System.Text.RegularExpressions;
using Microsoft.UI.Input;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Input;
using Microsoft.UI.Xaml.Media.Imaging;
using Microsoft.UI.Xaml.Media;
using Windows.ApplicationModel.DataTransfer;
using Windows.System;
using Windows.UI.Core;
using DispatcherQueueTimer = Microsoft.UI.Dispatching.DispatcherQueueTimer;

namespace ClipSyncPro_WinUI;

public enum MainViewMode
{
    All,
    Favorites,
    Prompts,
    Sensitive
}

public sealed partial class MainPage : Page
{
    private readonly AgentClient _client = new();
    private readonly ObservableCollection<ClipboardRecordView> _records = [];
    private readonly ObservableCollection<PromptView> _prompts = [];
    private readonly SemaphoreSlim _loadGate = new(1, 1);
    private readonly SemaphoreSlim _actionGate = new(1, 1);
    private CancellationTokenSource? _lifetime;
    private DispatcherQueueTimer? _eventRefreshTimer;
    private ClipboardRecordView? _selected;
    private PromptView? _selectedPrompt;

    private MainViewMode _currentView = MainViewMode.All;
    private string _date = "";
    private bool _sensitiveOnly;
    private bool _favoriteOnly;
    private string _promptCategory = "";
    private int _offset;
    private bool _loadingMore;

    private readonly ConcurrentDictionary<string, DateTime> _suppressedEventRecordIds = new();

    private void SuppressRecordRefresh(string recordId)
    {
        _suppressedEventRecordIds[recordId] = DateTime.UtcNow.AddSeconds(3);
    }

    public MainPage()
    {
        InitializeComponent();
        RecordsList.ItemsSource = _records;
        PromptsList.ItemsSource = _prompts;
    }

    private async void Page_Loaded(object sender, RoutedEventArgs e)
    {
        _lifetime = new CancellationTokenSource();
        if (_eventRefreshTimer is null)
        {
            _eventRefreshTimer = DispatcherQueue.CreateTimer();
            _eventRefreshTimer.Interval = TimeSpan.FromMilliseconds(250);
            _eventRefreshTimer.IsRepeating = false;
            _eventRefreshTimer.Tick += EventRefreshTimer_Tick;
        }
        await AgentLauncher.EnsureRunningAsync(_lifetime.Token);
        _ = Task.Run(() => AgentLauncher.WatchAsync(_lifetime.Token));
        _ = Task.Run(() => WatchAgentEventsAsync(_lifetime.Token));
        await LoadCurrentViewDataAsync();
    }

    private void Page_Unloaded(object sender, RoutedEventArgs e)
    {
        _eventRefreshTimer?.Stop();
        _lifetime?.Cancel();
        _lifetime?.Dispose();
        _lifetime = null;
    }

    private async Task LoadCurrentViewDataAsync()
    {
        if (_currentView == MainViewMode.Prompts)
        {
            await LoadPromptsAsync();
        }
        else
        {
            await LoadRecordsAsync();
        }
    }

    private async Task LoadRecordsAsync()
    {
        if (!await _loadGate.WaitAsync(0))
        {
            return;
        }
        SetBusy(true);
        try
        {
            using var cts = RequestTimeout();
            var status = await _client.StatusAsync(cts.Token);
            _offset = 0;
            var records = await _client.ListAsync(SearchBox.Text, _date, _sensitiveOnly, _favoriteOnly, 0, cts.Token);

            _records.Clear();
            foreach (var record in records)
            {
                _records.Add(record);
            }
            StatusText.Text = BuildStatus(status);
            if (_records.Count > 0)
            {
                RecordsList.ScrollIntoView(_records[0]);
            }
        }
        catch (AgentRequestException ex) when (ex.StatusCode >= 500)
        {
            StatusText.Text = "Agent 已连接，但本地存储暂不可用，系统会自动重试。";
        }
        catch (HttpRequestException)
        {
            StatusText.Text = "本地 agent 未连接，系统会自动重试。";
        }
        catch (Exception ex)
        {
            StatusText.Text = CleanError(ex.Message);
        }
        finally
        {
            SetBusy(false);
            _loadGate.Release();
        }
    }

    private async Task LoadPromptsAsync()
    {
        if (!await _loadGate.WaitAsync(0))
        {
            return;
        }
        SetBusy(true);
        try
        {
            using var cts = RequestTimeout();
            var prompts = await _client.ListPromptsAsync(_promptCategory, SearchBox.Text, cts.Token);
            _prompts.Clear();
            foreach (var p in prompts)
            {
                _prompts.Add(p);
            }
            StatusText.Text = $"常用提示词库 / 已加载 {_prompts.Count} 条模板";
        }
        catch (Exception ex)
        {
            StatusText.Text = CleanError(ex.Message);
        }
        finally
        {
            SetBusy(false);
            _loadGate.Release();
        }
    }

    private async Task WatchAgentEventsAsync(CancellationToken cancellationToken)
    {
        while (!cancellationToken.IsCancellationRequested)
        {
            try
            {
                await using var stream = await _client.OpenEventsAsync(cancellationToken);
                using var reader = new StreamReader(stream);
                while (!reader.EndOfStream && !cancellationToken.IsCancellationRequested)
                {
                    var line = await reader.ReadLineAsync(cancellationToken);
                    if (line?.StartsWith("data: ", StringComparison.Ordinal) == true)
                    {
                        var shouldSkip = false;
                        var payload = line.Substring(6).Trim();
                        try
                        {
                            using var doc = JsonDocument.Parse(payload);
                            if (doc.RootElement.TryGetProperty("record_id", out var rIdProp))
                            {
                                var rId = rIdProp.GetString();
                                if (!string.IsNullOrEmpty(rId) && _suppressedEventRecordIds.TryGetValue(rId, out var expireAt))
                                {
                                    if (DateTime.UtcNow <= expireAt)
                                    {
                                        shouldSkip = true;
                                    }
                                    else
                                    {
                                        _suppressedEventRecordIds.TryRemove(rId, out _);
                                    }
                                }
                            }
                        }
                        catch
                        {
                            // 忽略心跳或非 JSON 数据，回退正常处理
                        }

                        if (!shouldSkip)
                        {
                            QueueEventRefresh();
                        }
                    }
                }
            }
            catch when (cancellationToken.IsCancellationRequested)
            {
                return;
            }
            catch
            {
                await Task.Delay(TimeSpan.FromSeconds(2), cancellationToken);
            }
        }
    }

    private void QueueEventRefresh()
    {
        DispatcherQueue.TryEnqueue(() =>
        {
            if (_eventRefreshTimer is null || _lifetime?.IsCancellationRequested != false)
            {
                return;
            }
            _eventRefreshTimer.Stop();
            _eventRefreshTimer.Start();
        });
    }

    private async void EventRefreshTimer_Tick(DispatcherQueueTimer sender, object args)
    {
        await LoadCurrentViewDataAsync();
    }

    #region Navigation & Filtering

    private void UpdateNavButtons(MainViewMode mode)
    {
        _currentView = mode;
        NavAllButton.Style = mode == MainViewMode.All ? (Style)Resources["AccentButtonStyle"] : (Style)Application.Current.Resources["DefaultButtonStyle"];
        NavFavoritesButton.Style = mode == MainViewMode.Favorites ? (Style)Resources["AccentButtonStyle"] : (Style)Application.Current.Resources["DefaultButtonStyle"];
        NavPromptsButton.Style = mode == MainViewMode.Prompts ? (Style)Resources["AccentButtonStyle"] : (Style)Application.Current.Resources["DefaultButtonStyle"];
        NavSensitiveButton.Style = mode == MainViewMode.Sensitive ? (Style)Resources["AccentButtonStyle"] : (Style)Application.Current.Resources["DefaultButtonStyle"];

        if (mode == MainViewMode.Prompts)
        {
            RecordsList.Visibility = Visibility.Collapsed;
            PromptsContainer.Visibility = Visibility.Visible;
            NewPromptButton.Visibility = Visibility.Visible;
            DateFilterButton.Visibility = Visibility.Collapsed;
        }
        else
        {
            RecordsList.Visibility = Visibility.Visible;
            PromptsContainer.Visibility = Visibility.Collapsed;
            NewPromptButton.Visibility = Visibility.Collapsed;
            DateFilterButton.Visibility = Visibility.Visible;
        }
    }

    private async void NavAll_Click(object sender, RoutedEventArgs e)
    {
        _date = "";
        _sensitiveOnly = false;
        _favoriteOnly = false;
        _offset = 0;
        UpdateNavButtons(MainViewMode.All);
        await LoadRecordsAsync();
    }

    private async void NavFavorites_Click(object sender, RoutedEventArgs e)
    {
        _sensitiveOnly = false;
        _favoriteOnly = true;
        _offset = 0;
        UpdateNavButtons(MainViewMode.Favorites);
        await LoadRecordsAsync();
    }

    private async void NavPrompts_Click(object sender, RoutedEventArgs e)
    {
        UpdateNavButtons(MainViewMode.Prompts);
        await LoadPromptsAsync();
    }

    private async void NavSensitive_Click(object sender, RoutedEventArgs e)
    {
        _sensitiveOnly = true;
        _favoriteOnly = false;
        _offset = 0;
        UpdateNavButtons(MainViewMode.Sensitive);
        await LoadRecordsAsync();
    }

    private async void SearchBox_KeyDown(object sender, KeyRoutedEventArgs e)
    {
        if (e.Key == VirtualKey.Enter)
        {
            _offset = 0;
            await LoadCurrentViewDataAsync();
        }
    }

    private async void RefreshButton_Click(object sender, RoutedEventArgs e)
    {
        _offset = 0;
        await LoadCurrentViewDataAsync();
    }

    private async void DateButton_Click(object sender, RoutedEventArgs e)
    {
        var value = await PromptTextAsync("按时间筛选", "yyyyMMdd", _date);
        if (value is null)
        {
            return;
        }
        _date = value.Trim();
        _offset = 0;
        await LoadRecordsAsync();
    }

    #endregion

    #region Record Star & Actions

    private async Task ToggleFavoriteRecordAsync(ClipboardRecordView record)
    {
        var nextState = !record.IsFavorite;
        try
        {
            // 1. 本地模型即时原地更新，触发 INotifyPropertyChanged，星标平滑变化，无任何抖动
            record.IsFavorite = nextState;
            if (_favoriteOnly && !nextState)
            {
                _records.Remove(record);
            }

            // 2. 标记抑制此记录随后 3 秒内的 SSE 全量重载，避免页面清空重刷
            SuppressRecordRefresh(record.Id);

            // 3. 异步提交到本地 Agent 持久化
            using var cts = RequestTimeout();
            await _client.SetFavoriteAsync(record.Id, nextState, cts.Token);
            StatusText.Text = nextState ? "已添加至收藏夹" : "已从收藏夹移除";
        }
        catch (Exception ex)
        {
            // 若请求失败，回滚本地状态
            record.IsFavorite = !nextState;
            if (_favoriteOnly && !nextState)
            {
                _records.Insert(0, record);
            }
            StatusText.Text = CleanError(ex.Message);
        }
    }

    private async void RecordStarButton_Click(object sender, RoutedEventArgs e)
    {
        if (sender is not Button btn || btn.DataContext is not ClipboardRecordView record)
        {
            return;
        }
        await ToggleFavoriteRecordAsync(record);
    }

    private async void RecordConvertToPrompt_Click(object sender, RoutedEventArgs e)
    {
        if (sender is not Button btn || btn.DataContext is not ClipboardRecordView record)
        {
            return;
        }
        var initialContent = record.ContentText;
        if (string.IsNullOrWhiteSpace(initialContent))
        {
            StatusText.Text = "当前记录无文本内容，无法转为提示词";
            return;
        }
        await ShowEditPromptDialogAsync(new PromptView
        {
            Title = record.Summary.Length > 20 ? record.Summary[..20] : record.Summary,
            Category = "通用",
            TemplateContent = initialContent,
            Shortcut = ""
        });
    }

    private async void BatchDeleteButton_Click(object sender, RoutedEventArgs e)
    {
        var result = await ConfirmAsync("批量删除", "将删除当前结果中的普通剪贴内容（账户密码及已收藏记录会自动保留）。");
        if (!result)
        {
            return;
        }
        await RunAgentActionAsync(async token => await _client.BatchDeleteAsync(_records.Select(r => r.Id), skipSensitive: true, skipFavorite: true, token));
        await LoadRecordsAsync();
    }

    private async void CaptureButton_Click(object sender, RoutedEventArgs e)
    {
        await RunAgentActionAsync(async token => await _client.CaptureCurrentAsync(token));
        await LoadRecordsAsync();
    }

    private async void DeleteButton_Click(object sender, RoutedEventArgs e)
    {
        var record = SelectedOrNull();
        if (record is null)
        {
            return;
        }
        if (record.IsSensitive)
        {
            var ok = await ConfirmAsync("删除", $"该内容已设置为{record.KindLabel}，确定删除吗？");
            if (!ok)
            {
                return;
            }
        }
        await RunAgentActionAsync(async token => await _client.DeleteAsync(record.Id, token));
        await LoadRecordsAsync();
    }

    private async void PasswordButton_Click(object sender, RoutedEventArgs e)
    {
        var record = SelectedOrNull();
        if (record is null)
        {
            return;
        }
        if (record.IsImage)
        {
            StatusText.Text = "图片不能标记为账户或密码";
            return;
        }
        var input = await PromptSensitiveAsync("密码", "账户", "", allowGenerate: false);
        if (input is null)
        {
            return;
        }
        await RunAgentActionAsync(async token => await _client.MarkPasswordAsync(record.Id, input.Value, input.Remark, token));
        await LoadRecordsAsync();
    }

    private async void AccountButton_Click(object sender, RoutedEventArgs e)
    {
        var record = SelectedOrNull();
        if (record is null)
        {
            return;
        }
        if (record.IsImage)
        {
            StatusText.Text = "图片不能标记为账户或密码";
            return;
        }
        var input = await PromptSensitiveAsync("账户", "密码", GeneratePassword(), allowGenerate: true);
        if (input is null)
        {
            return;
        }
        await RunAgentActionAsync(async token => await _client.MarkAccountAsync(record.Id, input.Value, input.Remark, token));
        await LoadRecordsAsync();
    }

    private async void ViewButton_Click(object sender, RoutedEventArgs e)
    {
        var record = SelectedOrNull();
        if (record is null)
        {
            return;
        }
        await ShowRecordAsync(record);
    }

    private async void CopyButton_Click(object sender, RoutedEventArgs e)
    {
        var record = SelectedOrNull();
        if (record is null)
        {
            return;
        }
        var field = await SelectFieldAsync(record, defaultField: record.SensitiveType == "account" ? "account" : "password");
        if (field is null)
        {
            return;
        }
        await RunAgentActionAsync(async token => await _client.CopyAsync(record.Id, field, token));
    }

    private async void PasteButton_Click(object sender, RoutedEventArgs e)
    {
        await PasteSelectedAsync();
    }

    private ulong GetPasteTargetHandle()
    {
        if (App.Current is App app && app.MainWindow is MainWindow mw)
        {
            mw.FocusPasteTarget();
            return mw.PasteTargetHandle;
        }
        return 0;
    }

    private async Task PasteSelectedAsync()
    {
        var record = SelectedOrNull();
        if (record is null)
        {
            return;
        }
        var field = await SelectFieldAsync(record, defaultField: record.SensitiveType == "account" ? "account" : "password");
        if (field is null)
        {
            return;
        }
        var targetHwnd = GetPasteTargetHandle();
        await RunAgentActionAsync(async token => await _client.PasteAsync(record.Id, field, targetHwnd, token));
    }

    private void RecordGrid_ContextRequested(UIElement sender, ContextRequestedEventArgs args)
    {
        EnsureTargetSelected(sender);
    }

    private void RecordContextMenu_Copy_Click(object sender, RoutedEventArgs e)
    {
        EnsureTargetSelected(sender);
        CopyButton_Click(sender, e);
    }

    private void RecordContextMenu_Paste_Click(object sender, RoutedEventArgs e)
    {
        EnsureTargetSelected(sender);
        PasteButton_Click(sender, e);
    }

    private void RecordContextMenu_View_Click(object sender, RoutedEventArgs e)
    {
        EnsureTargetSelected(sender);
        ViewButton_Click(sender, e);
    }

    private async void RecordContextMenu_ToggleFavorite_Click(object sender, RoutedEventArgs e)
    {
        EnsureTargetSelected(sender);
        var record = SelectedOrNull();
        if (record is null) return;
        await ToggleFavoriteRecordAsync(record);
    }

    private void RecordContextMenu_ConvertToPrompt_Click(object sender, RoutedEventArgs e)
    {
        EnsureTargetSelected(sender);
        RecordConvertToPrompt_Click(sender, e);
    }

    private void RecordContextMenu_Password_Click(object sender, RoutedEventArgs e)
    {
        EnsureTargetSelected(sender);
        PasswordButton_Click(sender, e);
    }

    private void RecordContextMenu_Account_Click(object sender, RoutedEventArgs e)
    {
        EnsureTargetSelected(sender);
        AccountButton_Click(sender, e);
    }

    private void RecordContextMenu_Delete_Click(object sender, RoutedEventArgs e)
    {
        EnsureTargetSelected(sender);
        DeleteButton_Click(sender, e);
    }

    private void EnsureTargetSelected(object sender)
    {
        if (sender is FrameworkElement fe && fe.DataContext is ClipboardRecordView record)
        {
            _selected = record;
            RecordsList.SelectedItem = record;
        }
    }

    private void RecordsList_SelectionChanged(object sender, SelectionChangedEventArgs e)
    {
        _selected = RecordsList.SelectedItem as ClipboardRecordView;
        if (_selected is not null)
        {
            StatusText.Text = "已选择: " + ShortId(_selected.Id);
        }
    }

    private async void RecordsList_DoubleTapped(object sender, DoubleTappedRoutedEventArgs e)
    {
        await PasteSelectedAsync();
    }

    private async void RecordsList_KeyDown(object sender, KeyRoutedEventArgs e)
    {
        if (_selected is null)
        {
            return;
        }
        if (e.Key == VirtualKey.Enter)
        {
            e.Handled = true;
            CopyButton_Click(sender, e);
        }
        else if (e.Key == VirtualKey.Delete)
        {
            e.Handled = true;
            DeleteButton_Click(sender, e);
        }
        else if (e.Key == VirtualKey.S)
        {
            // S 键一键标星
            e.Handled = true;
            await ToggleFavoriteRecordAsync(_selected);
        }
    }

    private async Task CopyRecordAsync(ClipboardRecordView record)
    {
        if (!await _actionGate.WaitAsync(0))
        {
            return;
        }
        try
        {
            var field = await SelectFieldAsync(record, defaultField: record.SensitiveType == "account" ? "account" : "password");
            if (field is null)
            {
                return;
            }
            await RunAgentActionAsync(async token => await _client.CopyAsync(record.Id, field, token));
        }
        finally
        {
            _actionGate.Release();
        }
    }

    #endregion

    #region Prompts Management & Execution

    private async void PromptCatAll_Click(object sender, RoutedEventArgs e) => await SetPromptCategoryAsync("");
    private async void PromptCatDev_Click(object sender, RoutedEventArgs e) => await SetPromptCategoryAsync("代码开发");
    private async void PromptCatTrans_Click(object sender, RoutedEventArgs e) => await SetPromptCategoryAsync("翻译润色");
    private async void PromptCatOffice_Click(object sender, RoutedEventArgs e) => await SetPromptCategoryAsync("日常办公");

    private async Task SetPromptCategoryAsync(string category)
    {
        _promptCategory = category;
        await LoadPromptsAsync();
    }

    private void PromptsList_ItemClick(object sender, ItemClickEventArgs e)
    {
        _selectedPrompt = e.ClickedItem as PromptView;
    }

    private async void PromptsList_DoubleTapped(object sender, DoubleTappedRoutedEventArgs e)
    {
        if (_selectedPrompt is not null)
        {
            await ExecutePromptAsync(_selectedPrompt);
        }
    }

    private async void ExecutePromptButton_Click(object sender, RoutedEventArgs e)
    {
        if (sender is Button btn && btn.DataContext is PromptView prompt)
        {
            await ExecutePromptAsync(prompt);
        }
    }

    private async Task ExecutePromptAsync(PromptView prompt)
    {
        try
        {
            var finalContent = prompt.TemplateContent;
            // 提取当前系统剪贴板文本
            var clipPackage = Clipboard.GetContent();
            var clipText = "";
            if (clipPackage != null && clipPackage.Contains(StandardDataFormats.Text))
            {
                clipText = await clipPackage.GetTextAsync();
            }

            // 识别模板变量
            var variables = prompt.Variables;
            var values = new Dictionary<string, string>();

            // 优先替换内置剪贴板变量
            if (variables.Contains("clip") || variables.Contains("input"))
            {
                values["clip"] = clipText;
                values["input"] = clipText;
            }

            // 过滤出需要人工输入的变量
            var requiredVars = variables.Where(v => v != "clip" && v != "input").ToList();
            if (requiredVars.Count > 0)
            {
                var inputValues = await PromptWithVariablesDialogAsync(prompt, requiredVars, clipText);
                if (inputValues == null)
                {
                    return; // 用户取消
                }
                foreach (var pair in inputValues)
                {
                    values[pair.Key] = pair.Value;
                }
            }

            // 执行模板字符串插值
            foreach (var kvp in values)
            {
                var regex = new Regex(@"\{" + Regex.Escape(kvp.Key) + @"(?::[^}]+)?\}");
                finalContent = regex.Replace(finalContent, kvp.Value);
            }

            // 复制组装后的提示词至系统剪贴板
            var dp = new DataPackage();
            dp.SetText(finalContent);
            Clipboard.SetContent(dp);

            // 自增使用频次统计
            _ = Task.Run(async () =>
            {
                using var cts = RequestTimeout();
                await _client.UsePromptAsync(prompt.Id, cts.Token);
            });

            StatusText.Text = $"提示词「{prompt.Title}」已组装并写入剪贴板！";
        }
        catch (Exception ex)
        {
            StatusText.Text = CleanError(ex.Message);
        }
    }

    private async Task<Dictionary<string, string>?> PromptWithVariablesDialogAsync(PromptView prompt, List<string> variables, string defaultClip)
    {
        var panel = new StackPanel { Spacing = 10, MinWidth = 360 };
        var inputs = new Dictionary<string, TextBox>();

        foreach (var v in variables)
        {
            var label = new TextBlock { Text = $"参数 {{{v}}}:", FontWeight = Microsoft.UI.Text.FontWeights.SemiBold };
            var tb = new TextBox { PlaceholderText = $"请输入 {v} 的值" };
            panel.Children.Add(label);
            panel.Children.Add(tb);
            inputs[v] = tb;
        }

        var dialog = new ContentDialog
        {
            XamlRoot = XamlRoot,
            Title = $"填充提示词参数 - {prompt.Title}",
            Content = panel,
            PrimaryButtonText = "组装并复制",
            CloseButtonText = "取消",
            DefaultButton = ContentDialogButton.Primary
        };

        var result = await dialog.ShowAsync();
        if (result != ContentDialogResult.Primary)
        {
            return null;
        }
        var dict = new Dictionary<string, string>();
        foreach (var kvp in inputs)
        {
            dict[kvp.Key] = kvp.Value.Text;
        }
        return dict;
    }

    private async void NewPromptButton_Click(object sender, RoutedEventArgs e)
    {
        await ShowEditPromptDialogAsync(new PromptView
        {
            Title = "",
            Category = "通用",
            TemplateContent = "",
            Shortcut = ""
        });
    }

    private async void EditPromptButton_Click(object sender, RoutedEventArgs e)
    {
        if (sender is Button btn && btn.DataContext is PromptView prompt)
        {
            await ShowEditPromptDialogAsync(prompt);
        }
    }

    private async void DeletePromptButton_Click(object sender, RoutedEventArgs e)
    {
        if (sender is Button btn && btn.DataContext is PromptView prompt)
        {
            if (!await ConfirmAsync("删除提示词", $"确定要删除提示词「{prompt.Title}」吗？"))
            {
                return;
            }
            try
            {
                using var cts = RequestTimeout();
                await _client.DeletePromptAsync(prompt.Id, cts.Token);
                _prompts.Remove(prompt);
                StatusText.Text = "提示词已删除";
            }
            catch (Exception ex)
            {
                StatusText.Text = CleanError(ex.Message);
            }
        }
    }

    private async Task ShowEditPromptDialogAsync(PromptView prompt)
    {
        var titleBox = new TextBox { Text = prompt.Title, PlaceholderText = "提示词标题，例如：Code Review 专家" };
        var categoryBox = new TextBox { Text = prompt.Category, PlaceholderText = "分类，例如：代码开发 / 翻译润色" };
        var shortcutBox = new TextBox { Text = prompt.Shortcut, PlaceholderText = "快捷代号，例如：/cr" };
        var contentBox = new TextBox
        {
            Text = prompt.TemplateContent,
            PlaceholderText = "提示词正文模板，支持 {clip}、{target_lang} 等插槽变量",
            AcceptsReturn = true,
            MinHeight = 120,
            TextWrapping = TextWrapping.Wrap
        };

        var panel = new StackPanel { Spacing = 10, MinWidth = 420 };
        panel.Children.Add(new TextBlock { Text = "标题", FontWeight = Microsoft.UI.Text.FontWeights.SemiBold });
        panel.Children.Add(titleBox);
        panel.Children.Add(new TextBlock { Text = "分类", FontWeight = Microsoft.UI.Text.FontWeights.SemiBold });
        panel.Children.Add(categoryBox);
        panel.Children.Add(new TextBlock { Text = "快捷指令代号 (可选)", FontWeight = Microsoft.UI.Text.FontWeights.SemiBold });
        panel.Children.Add(shortcutBox);
        panel.Children.Add(new TextBlock { Text = "模板正文 ({clip} 代表剪贴板内容)", FontWeight = Microsoft.UI.Text.FontWeights.SemiBold });
        panel.Children.Add(contentBox);

        var dialog = new ContentDialog
        {
            XamlRoot = XamlRoot,
            Title = string.IsNullOrEmpty(prompt.Id) ? "新建常用提示词" : "编辑提示词",
            Content = panel,
            PrimaryButtonText = "保存",
            CloseButtonText = "取消",
            DefaultButton = ContentDialogButton.Primary
        };

        if (await dialog.ShowAsync() == ContentDialogResult.Primary)
        {
            if (string.IsNullOrWhiteSpace(titleBox.Text) || string.IsNullOrWhiteSpace(contentBox.Text))
            {
                StatusText.Text = "标题与模板正文不能为空";
                return;
            }
            prompt.Title = titleBox.Text.Trim();
            prompt.Category = string.IsNullOrWhiteSpace(categoryBox.Text) ? "通用" : categoryBox.Text.Trim();
            prompt.Shortcut = shortcutBox.Text.Trim();
            prompt.TemplateContent = contentBox.Text;

            try
            {
                using var cts = RequestTimeout();
                var saved = await _client.SavePromptAsync(prompt, cts.Token);
                await LoadPromptsAsync();
                StatusText.Text = "提示词保存成功";
            }
            catch (Exception ex)
            {
                StatusText.Text = CleanError(ex.Message);
            }
        }
    }

    #endregion

    #region Scroll & List Helpers

    private ScrollViewer? _recordsScrollViewer;
    private void RecordsList_Loaded(object sender, RoutedEventArgs e)
    {
        DispatcherQueue.TryEnqueue(() =>
        {
            _recordsScrollViewer = FindScrollViewer(RecordsList);
            if (_recordsScrollViewer is not null)
            {
                _recordsScrollViewer.ViewChanged -= RecordsList_ViewChanged;
                _recordsScrollViewer.ViewChanged += RecordsList_ViewChanged;
            }
        });
    }

    private static ScrollViewer? FindScrollViewer(DependencyObject root)
    {
        for (int i = 0; i < VisualTreeHelper.GetChildrenCount(root); i++)
        {
            var child = VisualTreeHelper.GetChild(root, i);
            if (child is ScrollViewer sv) return sv;
            var found = FindScrollViewer(child);
            if (found is not null) return found;
        }
        return null;
    }

    private void RecordsList_PointerWheelChanged(object sender, PointerRoutedEventArgs e)
    {
        if (e.GetCurrentPoint(RecordsList).Properties.MouseWheelDelta >= 0) return;
        DispatcherQueue.TryEnqueue(async () => await LoadNextPageAsync());
    }

    private async Task LoadNextPageAsync()
    {
        if (_loadingMore || _records.Count < 10 || _offset > 1000000) return;
        _loadingMore = true;
        try
        {
            using var cts = RequestTimeout();
            var more = await _client.ListAsync(SearchBox.Text, _date, _sensitiveOnly, _favoriteOnly, _offset + 10, cts.Token);
            foreach (var r in more) _records.Add(r);
            if (more.Count > 0) _offset += 10;
        }
        finally
        {
            _loadingMore = false;
        }
    }

    private async void RecordsList_ViewChanged(object? sender, ScrollViewerViewChangedEventArgs e)
    {
        if (_loadingMore || sender is not ScrollViewer sv || sv.VerticalOffset < sv.ScrollableHeight - 80 || _offset > 1000000) return;
        _loadingMore = true;
        try
        {
            using var cts = RequestTimeout();
            var more = await _client.ListAsync(SearchBox.Text, _date, _sensitiveOnly, _favoriteOnly, _offset + 10, cts.Token);
            foreach (var r in more) _records.Add(r);
            if (more.Count > 0) _offset += 10;
        }
        finally
        {
            _loadingMore = false;
        }
    }

    private void RecordsList_ContainerContentChanging(ListViewBase sender, ContainerContentChangingEventArgs args)
    {
    }

    private ClipboardRecordView? SelectedOrNull()
    {
        if (_selected is not null)
        {
            return _selected;
        }
        StatusText.Text = "请先选择一条记录";
        return null;
    }

    private async Task<string?> SelectFieldAsync(ClipboardRecordView record, string defaultField)
    {
        if (!record.IsSensitive)
        {
            return "content";
        }
        var options = new ComboBox { SelectedValuePath = "Tag", MinWidth = 240 };
        options.Items.Add(new ComboBoxItem { Content = "原始内容", Tag = "content" });
        options.Items.Add(new ComboBoxItem { Content = "账户", Tag = "account" });
        options.Items.Add(new ComboBoxItem { Content = "密码", Tag = "password" });
        options.Items.Add(new ComboBoxItem { Content = "备注", Tag = "remark" });
        foreach (ComboBoxItem item in options.Items)
        {
            if ((string)item.Tag == defaultField)
            {
                options.SelectedItem = item;
                break;
            }
        }
        var dialog = new ContentDialog
        {
            XamlRoot = XamlRoot,
            Title = "选择字段",
            Content = options,
            PrimaryButtonText = "确定",
            CloseButtonText = "取消",
            DefaultButton = ContentDialogButton.Primary
        };
        var result = await dialog.ShowAsync();
        return result == ContentDialogResult.Primary && options.SelectedItem is ComboBoxItem selected
            ? selected.Tag?.ToString()
            : null;
    }

    private async Task RunAgentActionAsync(Func<CancellationToken, Task> action)
    {
        SetBusy(true);
        try
        {
            using var cts = RequestTimeout();
            await action(cts.Token);
            StatusText.Text = "操作完成";
        }
        catch (Exception ex)
        {
            StatusText.Text = CleanError(ex.Message);
        }
        finally
        {
            SetBusy(false);
        }
    }

    private void SetBusy(bool value)
    {
        BusyRing.IsActive = value;
    }

    private CancellationTokenSource RequestTimeout()
    {
        return new CancellationTokenSource(TimeSpan.FromSeconds(12));
    }

    private string BuildStatus(AgentStatus status)
    {
        var filter = _favoriteOnly ? "⭐ 收藏夹" : (_sensitiveOnly ? "🔒 账户密码" : "全部记录");
        if (!string.IsNullOrWhiteSpace(_date))
        {
            filter += " / " + _date;
        }
        return $"Agent {status.Status} / {status.DeviceId} / {filter}";
    }

    private static string CleanError(string message)
    {
        return message.Replace("\r", " ").Replace("\n", " ").Trim();
    }

    private async Task<bool> ConfirmAsync(string title, string message)
    {
        var dialog = new ContentDialog
        {
            XamlRoot = XamlRoot,
            Title = title,
            Content = message,
            PrimaryButtonText = "确定",
            CloseButtonText = "取消",
            DefaultButton = ContentDialogButton.Primary
        };
        return await dialog.ShowAsync() == ContentDialogResult.Primary;
    }

    private async Task<string?> PromptTextAsync(string title, string label, string defaultValue)
    {
        var box = new TextBox { Text = defaultValue, PlaceholderText = label };
        var dialog = new ContentDialog
        {
            XamlRoot = XamlRoot,
            Title = title,
            Content = box,
            PrimaryButtonText = "确定",
            CloseButtonText = "取消",
            DefaultButton = ContentDialogButton.Primary
        };
        var result = await dialog.ShowAsync();
        return result == ContentDialogResult.Primary ? box.Text : null;
    }

    private async Task ShowRecordAsync(ClipboardRecordView record)
    {
        if (record.IsImage)
        {
            if (!Uri.TryCreate(record.ImageUrl, UriKind.Absolute, out var imageUri))
            {
                StatusText.Text = "图片地址无效";
                return;
            }
            var image = new Image
            {
                Source = new BitmapImage(imageUri),
                MaxWidth = 760,
                MaxHeight = 540,
                Stretch = Microsoft.UI.Xaml.Media.Stretch.Uniform
            };
            var imageDialog = new ContentDialog
            {
                XamlRoot = XamlRoot,
                Title = "查看图片",
                Content = new ScrollViewer { Content = image },
                PrimaryButtonText = "确定",
                DefaultButton = ContentDialogButton.Primary
            };
            await imageDialog.ShowAsync();
            return;
        }
        using var cts = RequestTimeout();
        var revealed = await _client.RevealAsync(record.Id, cts.Token);
        if (!record.IsSensitive)
        {
            await ShowMessageAsync("查看", revealed.Content);
            return;
        }

        var accountBox = ReadonlyTextBox(revealed.Account);
        var passwordBox = ReadonlyTextBox(revealed.Password);
        var remarkBox = new TextBox
        {
            Text = revealed.Remark,
            AcceptsReturn = true,
            MinHeight = 70,
            TextWrapping = TextWrapping.Wrap
        };

        var panel = new StackPanel { Spacing = 10 };
        panel.Children.Add(FieldWithCopy("账户", accountBox, async () => await RunAgentActionAsync(async token => await _client.CopyAsync(record.Id, "account", token))));
        panel.Children.Add(FieldWithCopy("密码", passwordBox, async () => await RunAgentActionAsync(async token => await _client.CopyAsync(record.Id, "password", token))));
        panel.Children.Add(new TextBlock { Text = "备注", FontWeight = Microsoft.UI.Text.FontWeights.SemiBold });
        panel.Children.Add(remarkBox);

        var dialog = new ContentDialog
        {
            XamlRoot = XamlRoot,
            Title = "查看",
            Content = panel,
            PrimaryButtonText = "确定",
            CloseButtonText = "关闭",
            DefaultButton = ContentDialogButton.Primary
        };
        dialog.Closing += async (_, args) =>
        {
            if (remarkBox.Text == revealed.Remark)
            {
                return;
            }
            var deferral = args.GetDeferral();
            try
            {
                using var saveCts = RequestTimeout();
                await _client.UpdateRemarkAsync(record.Id, remarkBox.Text, saveCts.Token);
            }
            finally
            {
                deferral.Complete();
            }
        };
        await dialog.ShowAsync();
        await LoadRecordsAsync();
    }

    private static FrameworkElement FieldWithCopy(string label, TextBox valueBox, Func<Task> copy)
    {
        var grid = new Grid { ColumnSpacing = 8 };
        grid.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        grid.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
        var stack = new StackPanel { Spacing = 4 };
        stack.Children.Add(new TextBlock { Text = label, FontWeight = Microsoft.UI.Text.FontWeights.SemiBold });
        stack.Children.Add(valueBox);
        var button = new Button { Content = "复制", MinWidth = 64, VerticalAlignment = VerticalAlignment.Bottom };
        button.Click += async (_, _) => await copy();
        Grid.SetColumn(button, 1);
        grid.Children.Add(stack);
        grid.Children.Add(button);
        return grid;
    }

    private static TextBox ReadonlyTextBox(string text)
    {
        return new TextBox
        {
            Text = text,
            IsReadOnly = true,
            FontFamily = new Microsoft.UI.Xaml.Media.FontFamily("Cascadia Mono, Consolas")
        };
    }

    private async Task<SensitiveInput?> PromptSensitiveAsync(string title, string valueLabel, string defaultValue, bool allowGenerate)
    {
        var valueBox = new TextBox
        {
            Text = defaultValue,
            FontFamily = new Microsoft.UI.Xaml.Media.FontFamily("Cascadia Mono, Consolas")
        };
        var remarkBox = new TextBox { PlaceholderText = "备注" };
        var panel = new StackPanel { Spacing = 10 };
        var header = new Grid { ColumnSpacing = 8 };
        header.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        header.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
        header.Children.Add(new TextBlock { Text = valueLabel, FontWeight = Microsoft.UI.Text.FontWeights.SemiBold, VerticalAlignment = VerticalAlignment.Center });
        if (allowGenerate)
        {
            var generate = new Button { Content = "生成密码" };
            generate.Click += (_, _) => valueBox.Text = GeneratePassword();
            Grid.SetColumn(generate, 1);
            header.Children.Add(generate);
        }
        panel.Children.Add(header);
        panel.Children.Add(valueBox);
        panel.Children.Add(new TextBlock { Text = "备注", FontWeight = Microsoft.UI.Text.FontWeights.SemiBold });
        panel.Children.Add(remarkBox);

        var dialog = new ContentDialog
        {
            XamlRoot = XamlRoot,
            Title = title,
            Content = panel,
            PrimaryButtonText = "确定",
            CloseButtonText = "取消",
            DefaultButton = ContentDialogButton.Primary
        };
        var result = await dialog.ShowAsync();
        return result == ContentDialogResult.Primary
            ? new SensitiveInput(valueBox.Text, remarkBox.Text)
            : null;
    }

    private async Task ShowMessageAsync(string title, string message)
    {
        var dialog = new ContentDialog
        {
            XamlRoot = XamlRoot,
            Title = title,
            Content = new TextBlock
            {
                Text = message,
                TextWrapping = TextWrapping.Wrap,
                FontFamily = new Microsoft.UI.Xaml.Media.FontFamily("Cascadia Mono, Consolas")
            },
            PrimaryButtonText = "确定",
            DefaultButton = ContentDialogButton.Primary
        };
        await dialog.ShowAsync();
    }

    private static string ShortId(string id)
    {
        return id.Length <= 22 ? id : id[..22] + "...";
    }

    private static string GeneratePassword()
    {
        const string upper = "ABCDEFGHJKLMNPQRSTUVWXYZ";
        const string lower = "abcdefghijkmnopqrstuvwxyz";
        const string digits = "23456789";
        const string symbols = "!@#$%^&*()-_=+[]{};:,.?";
        var classes = new[] { upper, lower, digits, symbols };
        var all = string.Concat(classes);
        var chars = new char[20];
        for (var i = 0; i < classes.Length; i++)
        {
            chars[i] = Pick(classes[i]);
        }
        for (var i = classes.Length; i < chars.Length; i++)
        {
            chars[i] = Pick(all);
        }
        for (var i = chars.Length - 1; i > 0; i--)
        {
            var j = RandomNumberGenerator.GetInt32(i + 1);
            (chars[i], chars[j]) = (chars[j], chars[i]);
        }
        return new string(chars);

        static char Pick(string alphabet)
        {
            return alphabet[RandomNumberGenerator.GetInt32(alphabet.Length)];
        }
    }

    #endregion
}

public sealed record SensitiveInput(string Value, string Remark);
