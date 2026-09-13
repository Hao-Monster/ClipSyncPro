using System.Collections.ObjectModel;
using System.Security.Cryptography;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Input;
using Microsoft.UI.Xaml.Media.Imaging;
using Windows.System;
using DispatcherQueueTimer = Microsoft.UI.Dispatching.DispatcherQueueTimer;

namespace ClipSyncPro_WinUI;

public sealed partial class MainPage : Page
{
    private readonly AgentClient _client = new();
    private readonly ObservableCollection<ClipboardRecordView> _records = [];
    private readonly SemaphoreSlim _loadGate = new(1, 1);
    private CancellationTokenSource? _lifetime;
    private DispatcherQueueTimer? _eventRefreshTimer;
    private ClipboardRecordView? _selected;
    private string _date = "";
    private bool _sensitiveOnly;

    public MainPage()
    {
        InitializeComponent();
        RecordsList.ItemsSource = _records;
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
        await LoadRecordsAsync();
    }

    private void Page_Unloaded(object sender, RoutedEventArgs e)
    {
        _eventRefreshTimer?.Stop();
        _lifetime?.Cancel();
        _lifetime?.Dispose();
        _lifetime = null;
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
            var records = await _client.ListAsync(SearchBox.Text, _date, _sensitiveOnly, cts.Token);

            _records.Clear();
            foreach (var record in records)
            {
                _records.Add(record);
            }
            StatusText.Text = BuildStatus(status);
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
                        QueueEventRefresh();
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
        await LoadRecordsAsync();
    }

    private async void RefreshButton_Click(object sender, RoutedEventArgs e)
    {
        await LoadRecordsAsync();
    }

    private async void AllButton_Click(object sender, RoutedEventArgs e)
    {
        _date = "";
        _sensitiveOnly = false;
        SearchBox.Text = "";
        await LoadRecordsAsync();
    }

    private async void SensitiveButton_Click(object sender, RoutedEventArgs e)
    {
        _sensitiveOnly = !_sensitiveOnly;
        SensitiveButton.Opacity = _sensitiveOnly ? 1.0 : 0.72;
        await LoadRecordsAsync();
    }

    private async void DateButton_Click(object sender, RoutedEventArgs e)
    {
        var value = await PromptTextAsync("按时间筛选", "yyyyMMdd", _date);
        if (value is null)
        {
            return;
        }
        _date = value.Trim();
        await LoadRecordsAsync();
    }

    private async void BatchDeleteButton_Click(object sender, RoutedEventArgs e)
    {
        var result = await ConfirmAsync("批量删除", "将删除当前结果中的普通剪贴内容，账户/密码记录会自动跳过。");
        if (!result)
        {
            return;
        }
        await RunAgentActionAsync(async token => await _client.BatchDeleteAsync(_records.Select(r => r.Id), true, token));
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

    private async void CaptureButton_Click(object sender, RoutedEventArgs e)
    {
        await RunAgentActionAsync(async token => await _client.CaptureCurrentAsync(token));
        await LoadRecordsAsync();
    }

    private async void RecordsList_DoubleTapped(object sender, DoubleTappedRoutedEventArgs e)
    {
        await PasteSelectedAsync();
    }

    private void RecordsList_KeyDown(object sender, KeyRoutedEventArgs e)
    {
        if (e.Key == VirtualKey.Enter)
        {
            e.Handled = true;
            CopyButton_Click(sender, e);
        }
        if (e.Key == VirtualKey.Delete)
        {
            e.Handled = true;
            DeleteButton_Click(sender, e);
        }
    }

    private async void SearchBox_KeyDown(object sender, KeyRoutedEventArgs e)
    {
        if (e.Key == VirtualKey.Enter)
        {
            e.Handled = true;
            await LoadRecordsAsync();
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
        await RunAgentActionAsync(async token => await _client.PasteAsync(record.Id, field, token));
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

    private ClipboardRecordView? SelectedOrNull()
    {
        if (_selected is not null)
        {
            return _selected;
        }
        StatusText.Text = "请先选择一条记录";
        return null;
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
        var filter = _sensitiveOnly ? "账户密码" : "全部";
        if (!string.IsNullOrWhiteSpace(_date))
        {
            filter += " / " + _date;
        }
        return $"Agent {status.Status} / {status.DeviceId} / {filter}";
    }

    private static string ShortId(string id)
    {
        return id.Length <= 22 ? id : id[..22] + "...";
    }

    private static string CleanError(string message)
    {
        return message.Replace("\r", " ").Replace("\n", " ").Trim();
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
}

public sealed record SensitiveInput(string Value, string Remark);
