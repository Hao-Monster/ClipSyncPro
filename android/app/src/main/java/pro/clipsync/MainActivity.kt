package pro.clipsync

import android.app.Activity
import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.content.Intent
import android.os.Bundle
import android.view.ViewGroup
import android.widget.ArrayAdapter
import android.widget.Button
import android.widget.EditText
import android.widget.LinearLayout
import android.widget.ListView
import android.widget.TextView
import android.widget.Toast
import pro.clipsync.clipboard.ClipboardForegroundService
import pro.clipsync.crypto.SecretBox
import pro.clipsync.data.ClipboardRepository
import pro.clipsync.model.ClipboardRecord
import kotlin.concurrent.thread

class MainActivity : Activity() {
    private lateinit var repository: ClipboardRepository
    private lateinit var adapter: ArrayAdapter<String>
    private var records: List<ClipboardRecord> = emptyList()
    private var selected: ClipboardRecord? = null
    private lateinit var search: EditText
    private lateinit var serverUrl: EditText
    private lateinit var secret: EditText
    private lateinit var status: TextView

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        repository = ClipboardRepository(this)
        setContentView(buildView())
        startForegroundService(Intent(this, ClipboardForegroundService::class.java))
        refresh()
    }

    private fun buildView(): LinearLayout {
        adapter = ArrayAdapter(this, android.R.layout.simple_list_item_1, mutableListOf())
        serverUrl = EditText(this).apply {
            hint = "服务器地址"
            setText(repository.serverUrl)
        }
        secret = EditText(this).apply {
            hint = "加密密钥"
            setText(repository.secret)
        }
        search = EditText(this).apply { hint = "搜索" }
        status = TextView(this).apply { text = "未选择" }
        val list = ListView(this).apply {
            adapter = this@MainActivity.adapter
            setOnItemClickListener { _, _, position, _ ->
                selected = records.getOrNull(position)
                status.text = selected?.id ?: "未选择"
            }
            setOnItemLongClickListener { _, _, position, _ ->
                selected = records.getOrNull(position)
                copySelected()
                true
            }
        }
        val buttons = LinearLayout(this).apply {
            orientation = LinearLayout.HORIZONTAL
            addView(button("保存配置") { saveConfig() })
            addView(button("刷新") { refresh() })
            addView(button("上传剪贴板") { uploadCurrentClipboard() })
            addView(button("查看") { revealSelected() })
            addView(button("删除") { deleteSelected() })
        }
        return LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(16, 16, 16, 16)
            addView(serverUrl, LinearLayout.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT)
            addView(secret, LinearLayout.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT)
            addView(search, LinearLayout.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT)
            addView(buttons, LinearLayout.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT)
            addView(list, LinearLayout.LayoutParams.MATCH_PARENT, 0, 1f)
            addView(status, LinearLayout.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT)
        }
    }

    private fun saveConfig() {
        repository.serverUrl = serverUrl.text.toString()
        repository.secret = secret.text.toString()
        Toast.makeText(this, "已保存", Toast.LENGTH_SHORT).show()
    }

    private fun button(text: String, action: () -> Unit): Button {
        return Button(this).apply {
            this.text = text
            setOnClickListener { action() }
        }
    }

    private fun refresh() {
        thread(name = "clipsync-refresh") {
            val result = runCatching { repository.api.list(keyword = search.text.toString()) }
            runOnUiThread {
                result.onSuccess {
                    records = it
                    adapter.clear()
                    adapter.addAll(it.map { rec -> "${rec.clientCreatedAt}  ${rec.summary}" })
                    adapter.notifyDataSetChanged()
                    status.text = "${it.size} 条"
                }.onFailure {
                    Toast.makeText(this, it.message ?: "加载失败", Toast.LENGTH_SHORT).show()
                }
            }
        }
    }

    private fun uploadCurrentClipboard() {
        val cm = getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager
        val text = cm.primaryClip?.getItemAt(0)?.coerceToText(this)?.toString().orEmpty()
        thread(name = "clipsync-upload-manual") {
            val result = runCatching { repository.uploadClipboardText(text) }
            runOnUiThread {
                result.onSuccess { refresh() }
                    .onFailure { Toast.makeText(this, it.message ?: "上传失败", Toast.LENGTH_SHORT).show() }
            }
        }
    }

    private fun revealSelected() {
        val rec = selected ?: return
        if (!rec.isSensitive) {
            showText(rec.contentText.orEmpty())
            return
        }
        val account = SecretBox.decrypt(repository.secret, rec.accountCipher)
        val password = SecretBox.decrypt(repository.secret, rec.passwordCipher)
        val remark = SecretBox.decrypt(repository.secret, rec.remarkCipher)
        showText("账户:\n$account\n\n密码:\n$password\n\n备注:\n$remark")
    }

    private fun copySelected() {
        val rec = selected ?: return
        val cm = getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager
        val text = if (rec.isSensitive) SecretBox.decrypt(repository.secret, rec.passwordCipher) else rec.contentText.orEmpty()
        cm.setPrimaryClip(ClipData.newPlainText("ClipSync Pro", text))
    }

    private fun deleteSelected() {
        val rec = selected ?: return
        thread(name = "clipsync-delete") {
            val result = runCatching { repository.api.delete(rec.id) }
            runOnUiThread {
                result.onSuccess { refresh() }
                    .onFailure { Toast.makeText(this, it.message ?: "删除失败", Toast.LENGTH_SHORT).show() }
            }
        }
    }

    private fun showText(text: String) {
        android.app.AlertDialog.Builder(this)
            .setTitle("查看")
            .setMessage(text)
            .setPositiveButton("确定", null)
            .show()
    }
}
