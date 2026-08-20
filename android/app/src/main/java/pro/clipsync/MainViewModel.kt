package pro.clipsync

import android.app.Application
import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import java.security.SecureRandom
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import pro.clipsync.crypto.SecretBox
import pro.clipsync.data.ClipboardRepository
import pro.clipsync.model.ClipboardRecord

data class ClipSyncUiState(
    val serverUrl: String = "",
    val secret: String = "",
    val search: String = "",
    val records: List<ClipboardRecord> = emptyList(),
    val selectedId: String? = null,
    val status: String = "未选择",
    val loading: Boolean = false,
    val revealText: String? = null,
    val pendingDelete: ClipboardRecord? = null,
)

class MainViewModel(application: Application) : AndroidViewModel(application) {
    private val repository = ClipboardRepository(application.applicationContext)
    private val random = SecureRandom()
    private val _state = MutableStateFlow(
        ClipSyncUiState(
            serverUrl = repository.serverUrl,
            secret = repository.secret,
        )
    )
    val state: StateFlow<ClipSyncUiState> = _state

    fun updateServerUrl(value: String) {
        _state.update { it.copy(serverUrl = value) }
    }

    fun updateSecret(value: String) {
        _state.update { it.copy(secret = value) }
    }

    fun updateSearch(value: String) {
        _state.update { it.copy(search = value) }
    }

    fun saveConfig() {
        repository.serverUrl = state.value.serverUrl
        repository.secret = state.value.secret
        _state.update { it.copy(status = "配置已保存") }
    }

    fun refresh() {
        viewModelScope.launch {
            _state.update { it.copy(loading = true) }
            val result = withContext(Dispatchers.IO) {
                runCatching { repository.api.list(keyword = state.value.search) }
            }
            result.onSuccess { records ->
                _state.update {
                    it.copy(
                        records = records,
                        loading = false,
                        selectedId = it.selectedId?.takeIf { id -> records.any { rec -> rec.id == id } },
                        status = if (records.isEmpty()) "暂无剪贴记录" else "已加载 ${records.size} 条",
                    )
                }
            }.onFailure { error ->
                _state.update { it.copy(loading = false, status = error.message ?: "加载失败") }
            }
        }
    }

    fun select(record: ClipboardRecord) {
        _state.update { it.copy(selectedId = record.id, status = "已选择: ${shortId(record.id)}") }
    }

    fun uploadCurrentClipboard() {
        val clipboard = getApplication<Application>().getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager
        val text = clipboard.primaryClip?.getItemAt(0)?.coerceToText(getApplication())?.toString().orEmpty()
        if (text.isBlank()) {
            _state.update { it.copy(status = "当前剪贴板没有文本") }
            return
        }
        viewModelScope.launch {
            _state.update { it.copy(loading = true) }
            val result = withContext(Dispatchers.IO) {
                runCatching { repository.uploadClipboardText(text) }
            }
            result.onSuccess {
                _state.update { it.copy(loading = false, status = "已上传当前剪贴板") }
                refresh()
            }.onFailure { error ->
                _state.update { it.copy(loading = false, status = error.message ?: "上传失败") }
            }
        }
    }

    fun copySelected() {
        val record = selectedRecord() ?: return
        val text = runCatching {
            if (record.isSensitive) {
                SecretBox.decrypt(state.value.secret, record.passwordCipher)
            } else {
                record.contentText.orEmpty()
            }
        }.getOrElse {
            _state.update { current -> current.copy(status = it.message ?: "复制失败") }
            return
        }
        val clipboard = getApplication<Application>().getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager
        clipboard.setPrimaryClip(ClipData.newPlainText("ClipSync Pro", text))
        _state.update { it.copy(status = "已复制") }
    }

    fun revealSelected() {
        val record = selectedRecord() ?: return
        val text = runCatching {
            if (!record.isSensitive) {
                record.contentText.orEmpty()
            } else {
                val account = SecretBox.decrypt(state.value.secret, record.accountCipher)
                val password = SecretBox.decrypt(state.value.secret, record.passwordCipher)
                val remark = SecretBox.decrypt(state.value.secret, record.remarkCipher)
                "账户:\n$account\n\n密码:\n$password\n\n备注:\n$remark"
            }
        }.getOrElse {
            _state.update { current -> current.copy(status = it.message ?: "查看失败") }
            return
        }
        _state.update { it.copy(revealText = text) }
    }

    fun markSelectedPassword(account: String, remark: String) {
        val record = selectedRecord() ?: return
        viewModelScope.launch {
            _state.update { it.copy(loading = true) }
            val result = withContext(Dispatchers.IO) {
                runCatching { repository.markPassword(record, account, remark) }
            }
            result.onSuccess {
                _state.update { it.copy(loading = false, status = "已设置为密码") }
                refresh()
            }.onFailure { error ->
                _state.update { it.copy(loading = false, status = error.message ?: "设置失败") }
            }
        }
    }

    fun markSelectedAccount(password: String, remark: String) {
        val record = selectedRecord() ?: return
        viewModelScope.launch {
            _state.update { it.copy(loading = true) }
            val result = withContext(Dispatchers.IO) {
                runCatching { repository.markAccount(record, password, remark) }
            }
            result.onSuccess {
                _state.update { it.copy(loading = false, status = "已设置为账户") }
                refresh()
            }.onFailure { error ->
                _state.update { it.copy(loading = false, status = error.message ?: "设置失败") }
            }
        }
    }

    fun generatePassword(length: Int = 20): String {
        val classes = listOf(
            "ABCDEFGHJKLMNPQRSTUVWXYZ",
            "abcdefghijkmnopqrstuvwxyz",
            "23456789",
            "!@#$%^&*()-_=+[]{};:,.?",
        )
        val all = classes.joinToString("")
        val chars = CharArray(length.coerceAtLeast(12))
        classes.forEachIndexed { index, alphabet ->
            chars[index] = alphabet[random.nextInt(alphabet.length)]
        }
        for (i in classes.size until chars.size) {
            chars[i] = all[random.nextInt(all.length)]
        }
        for (i in chars.lastIndex downTo 1) {
            val j = random.nextInt(i + 1)
            val tmp = chars[i]
            chars[i] = chars[j]
            chars[j] = tmp
        }
        return String(chars)
    }

    fun requestDeleteSelected() {
        val record = selectedRecord() ?: return
        if (record.isSensitive) {
            _state.update { it.copy(pendingDelete = record) }
        } else {
            delete(record)
        }
    }

    fun confirmDelete() {
        val record = state.value.pendingDelete ?: return
        _state.update { it.copy(pendingDelete = null) }
        delete(record)
    }

    fun cancelDelete() {
        _state.update { it.copy(pendingDelete = null) }
    }

    fun dismissReveal() {
        _state.update { it.copy(revealText = null) }
    }

    private fun delete(record: ClipboardRecord) {
        viewModelScope.launch {
            _state.update { it.copy(loading = true) }
            val result = withContext(Dispatchers.IO) {
                runCatching { repository.api.delete(record.id) }
            }
            result.onSuccess {
                _state.update { it.copy(loading = false, status = "已删除", selectedId = null) }
                refresh()
            }.onFailure { error ->
                _state.update { it.copy(loading = false, status = error.message ?: "删除失败") }
            }
        }
    }

    private fun selectedRecord(): ClipboardRecord? {
        val record = state.value.records.firstOrNull { it.id == state.value.selectedId }
        if (record == null) {
            _state.update { it.copy(status = "请先选择一条记录") }
        }
        return record
    }

    private fun shortId(id: String): String = if (id.length <= 18) id else id.take(18) + "..."
}
