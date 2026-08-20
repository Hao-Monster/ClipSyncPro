package pro.clipsync

import android.content.Intent
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.viewModels
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.AssistChip
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import pro.clipsync.clipboard.ClipboardForegroundService
import pro.clipsync.model.ClipboardRecord

class MainActivity : ComponentActivity() {
    private val viewModel by viewModels<MainViewModel>()

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        startForegroundService(Intent(this, ClipboardForegroundService::class.java))
        setContent {
            ClipSyncTheme {
                val state by viewModel.state.collectAsState()
                ClipSyncScreen(
                    state = state,
                    onServerUrl = viewModel::updateServerUrl,
                    onSecret = viewModel::updateSecret,
                    onSearch = viewModel::updateSearch,
                    onSave = viewModel::saveConfig,
                    onRefresh = viewModel::refresh,
                    onUpload = viewModel::uploadCurrentClipboard,
                    onReveal = viewModel::revealSelected,
                    onCopy = viewModel::copySelected,
                    onDelete = viewModel::requestDeleteSelected,
                    onMarkPassword = viewModel::markSelectedPassword,
                    onMarkAccount = viewModel::markSelectedAccount,
                    onGeneratePassword = { viewModel.generatePassword() },
                    onSelect = viewModel::select,
                    onDismissReveal = viewModel::dismissReveal,
                    onConfirmDelete = viewModel::confirmDelete,
                    onCancelDelete = viewModel::cancelDelete,
                )
            }
        }
        viewModel.refresh()
    }
}

@Composable
private fun ClipSyncTheme(content: @Composable () -> Unit) {
    MaterialTheme(
        colorScheme = androidx.compose.material3.lightColorScheme(
            primary = androidx.compose.ui.graphics.Color(0xFF2563EB),
            secondary = androidx.compose.ui.graphics.Color(0xFF0F766E),
            surface = androidx.compose.ui.graphics.Color(0xFFF8FAFC),
            surfaceVariant = androidx.compose.ui.graphics.Color(0xFFE2E8F0),
        ),
        typography = MaterialTheme.typography,
        content = content,
    )
}

@OptIn(ExperimentalMaterial3Api::class, ExperimentalLayoutApi::class)
@Composable
private fun ClipSyncScreen(
    state: ClipSyncUiState,
    onServerUrl: (String) -> Unit,
    onSecret: (String) -> Unit,
    onSearch: (String) -> Unit,
    onSave: () -> Unit,
    onRefresh: () -> Unit,
    onUpload: () -> Unit,
    onReveal: () -> Unit,
    onCopy: () -> Unit,
    onDelete: () -> Unit,
    onMarkPassword: (String, String) -> Unit,
    onMarkAccount: (String, String) -> Unit,
    onGeneratePassword: () -> String,
    onSelect: (ClipboardRecord) -> Unit,
    onDismissReveal: () -> Unit,
    onConfirmDelete: () -> Unit,
    onCancelDelete: () -> Unit,
) {
    var markMode by remember { mutableStateOf<String?>(null) }
    var markValue by remember { mutableStateOf("") }
    var markRemark by remember { mutableStateOf("") }

    Scaffold(
        topBar = {
            TopAppBar(
                title = {
                    Text("ClipSync Pro", fontWeight = FontWeight.SemiBold)
                },
                colors = TopAppBarDefaults.topAppBarColors(
                    containerColor = MaterialTheme.colorScheme.surface,
                ),
            )
        },
    ) { padding ->
        Column(
            modifier = Modifier
                .fillMaxSize()
                .padding(padding)
                .padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            OutlinedTextField(
                value = state.serverUrl,
                onValueChange = onServerUrl,
                modifier = Modifier.fillMaxWidth(),
                singleLine = true,
                label = { Text("服务器地址") },
            )
            OutlinedTextField(
                value = state.secret,
                onValueChange = onSecret,
                modifier = Modifier.fillMaxWidth(),
                singleLine = true,
                label = { Text("加密密钥") },
            )
            OutlinedTextField(
                value = state.search,
                onValueChange = onSearch,
                modifier = Modifier.fillMaxWidth(),
                singleLine = true,
                label = { Text("搜索") },
            )
            FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Button(onClick = onSave) { Text("保存配置") }
                Button(onClick = onRefresh) { Text("刷新") }
                Button(onClick = onUpload) { Text("上传剪贴板") }
                Button(onClick = {
                    markMode = "password"
                    markValue = ""
                    markRemark = ""
                }) { Text("密码") }
                Button(onClick = {
                    markMode = "account"
                    markValue = onGeneratePassword()
                    markRemark = ""
                }) { Text("账户") }
                Button(onClick = onReveal) { Text("查看") }
                Button(onClick = onCopy) { Text("复制") }
                Button(onClick = onDelete) { Text("删除") }
            }
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    text = state.status,
                    modifier = Modifier.weight(1f),
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    style = MaterialTheme.typography.bodySmall,
                )
                if (state.loading) {
                    CircularProgressIndicator(modifier = Modifier.width(22.dp).height(22.dp), strokeWidth = 2.dp)
                }
            }
            LazyColumn(
                modifier = Modifier.fillMaxSize(),
                verticalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                items(state.records, key = { it.id }) { record ->
                    RecordRow(
                        record = record,
                        selected = record.id == state.selectedId,
                        onClick = { onSelect(record) },
                    )
                }
            }
        }
    }

    state.revealText?.let {
        AlertDialog(
            onDismissRequest = onDismissReveal,
            confirmButton = { TextButton(onClick = onDismissReveal) { Text("确定") } },
            title = { Text("查看") },
            text = { Text(it, fontFamily = FontFamily.Monospace) },
        )
    }

    state.pendingDelete?.let {
        AlertDialog(
            onDismissRequest = onCancelDelete,
            confirmButton = { TextButton(onClick = onConfirmDelete) { Text("确定") } },
            dismissButton = { TextButton(onClick = onCancelDelete) { Text("取消") } },
            title = { Text("删除") },
            text = { Text("该内容已设置为账户/密码，确定删除吗？") },
        )
    }

    markMode?.let { mode ->
        val isAccount = mode == "account"
        AlertDialog(
            onDismissRequest = { markMode = null },
            confirmButton = {
                TextButton(
                    onClick = {
                        if (isAccount) {
                            onMarkAccount(markValue, markRemark)
                        } else {
                            onMarkPassword(markValue, markRemark)
                        }
                        markMode = null
                    },
                ) { Text("确定") }
            },
            dismissButton = { TextButton(onClick = { markMode = null }) { Text("取消") } },
            title = { Text(if (isAccount) "账户" else "密码") },
            text = {
                Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                    OutlinedTextField(
                        value = markValue,
                        onValueChange = { markValue = it },
                        modifier = Modifier.fillMaxWidth(),
                        singleLine = true,
                        label = { Text(if (isAccount) "密码" else "账户") },
                    )
                    if (isAccount) {
                        TextButton(onClick = { markValue = onGeneratePassword() }) {
                            Text("生成密码")
                        }
                    }
                    OutlinedTextField(
                        value = markRemark,
                        onValueChange = { markRemark = it },
                        modifier = Modifier.fillMaxWidth(),
                        label = { Text("备注") },
                    )
                }
            },
        )
    }
}

@Composable
private fun RecordRow(record: ClipboardRecord, selected: Boolean, onClick: () -> Unit) {
    val container = if (selected) {
        MaterialTheme.colorScheme.primary.copy(alpha = 0.12f)
    } else {
        MaterialTheme.colorScheme.surface
    }
    Card(
        modifier = Modifier
            .fillMaxWidth()
            .clickable(onClick = onClick),
        shape = RoundedCornerShape(8.dp),
        colors = CardDefaults.cardColors(containerColor = container),
        elevation = CardDefaults.cardElevation(defaultElevation = if (selected) 1.dp else 0.dp),
    ) {
        Column(modifier = Modifier.padding(14.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(
                text = record.summary,
                maxLines = 2,
                overflow = TextOverflow.Ellipsis,
                fontFamily = FontFamily.Monospace,
                fontWeight = FontWeight.SemiBold,
            )
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
                AssistChip(onClick = {}, label = { Text(record.kindLabel) })
                AssistChip(onClick = {}, label = { Text(record.sourcePlatform) })
                Spacer(modifier = Modifier.weight(1f))
                Text(
                    text = record.clientCreatedAt.take(16).replace("T", " "),
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
    }
}

private val ClipboardRecord.kindLabel: String
    get() = if (!isSensitive) {
        contentType
    } else {
        when (sensitiveType) {
            "account" -> "账户"
            "password" -> "密码"
            else -> "敏感"
        }
    }
