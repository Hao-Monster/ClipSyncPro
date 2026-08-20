package pro.clipsync.data

import android.content.Context
import android.provider.Settings
import org.json.JSONObject
import pro.clipsync.api.ClipSyncApi
import pro.clipsync.crypto.SecretBox
import pro.clipsync.model.ClipboardRecord
import java.security.MessageDigest
import java.time.Instant
import java.util.UUID

class ClipboardRepository(private val context: Context) {
    private val prefs = context.getSharedPreferences("clipsync", Context.MODE_PRIVATE)

    var serverUrl: String
        get() = prefs.getString("server_url", "http://127.0.0.1:37617") ?: "http://127.0.0.1:37617"
        set(value) = prefs.edit().putString("server_url", value).apply()

    var secret: String
        get() = prefs.getString("secret", "") ?: ""
        set(value) = prefs.edit().putString("secret", value).apply()

    val api: ClipSyncApi
        get() = ClipSyncApi(serverUrl)

    fun uploadClipboardText(text: String) {
        val now = Instant.now().toString()
        val json = JSONObject()
            .put("id", "rec_${System.currentTimeMillis()}_${UUID.randomUUID()}")
            .put("user_id", "local")
            .put("content_type", detectContentType(text))
            .put("content_text", text)
            .put("content_hash", sha256(text))
            .put("is_sensitive", false)
            .put("source_device_id", deviceId())
            .put("source_platform", "android")
            .put("client_created_at", now)
            .put("updated_at", now)
            .put("sync_status", "pending")
        api.create(json)
    }

    fun markPassword(record: ClipboardRecord, account: String, remark: String) {
        val cleanAccount = account.trim()
        require(cleanAccount.isNotEmpty()) { "账户不能为空" }
        val content = revealContent(record)
        upsertSensitive(
            record = record,
            sensitiveType = "password",
            content = content,
            account = cleanAccount,
            password = content,
            remark = remark,
        )
    }

    fun markAccount(record: ClipboardRecord, password: String, remark: String) {
        val cleanPassword = password.trim()
        require(cleanPassword.isNotEmpty()) { "密码不能为空" }
        val content = revealContent(record)
        upsertSensitive(
            record = record,
            sensitiveType = "account",
            content = content,
            account = content,
            password = cleanPassword,
            remark = remark,
        )
    }

    private fun upsertSensitive(
        record: ClipboardRecord,
        sensitiveType: String,
        content: String,
        account: String,
        password: String,
        remark: String,
    ) {
        val now = Instant.now().toString()
        val json = JSONObject()
            .put("id", record.id)
            .put("user_id", "local")
            .put("content_type", record.contentType.ifBlank { detectContentType(content) })
            .put("content_hash", record.contentHash.ifBlank { sha256(content) })
            .put("content_cipher", SecretBox.encrypt(secret, content).toJson())
            .put("is_sensitive", true)
            .put("sensitive_type", sensitiveType)
            .put("account_cipher", SecretBox.encrypt(secret, account).toJson())
            .put("password_cipher", SecretBox.encrypt(secret, password).toJson())
            .put("remark_cipher", SecretBox.encrypt(secret, remark).toJson())
            .put("search_index", org.json.JSONArray(SecretBox.searchIndex(secret, content, account, password, remark)))
            .put("source_device_id", record.sourceDeviceId.ifBlank { deviceId() })
            .put("source_platform", record.sourcePlatform.ifBlank { "android" })
            .put("client_created_at", record.clientCreatedAt.ifBlank { now })
            .put("updated_at", now)
            .put("sync_status", "pending")
        api.create(json)
    }

    private fun revealContent(record: ClipboardRecord): String {
        return if (record.isSensitive) {
            SecretBox.decrypt(secret, record.contentCipher)
        } else {
            record.contentText.orEmpty()
        }
    }

    private fun detectContentType(text: String): String {
        val trimmed = text.trim()
        if (trimmed.matches(Regex("^[+-]?([0-9]+|[0-9]*\\.[0-9]+)$"))) return "number"
        if (trimmed.startsWith("http://") || trimmed.startsWith("https://")) return "url"
        return "text"
    }

    private fun sha256(text: String): String {
        return MessageDigest.getInstance("SHA-256")
            .digest(text.toByteArray(Charsets.UTF_8))
            .joinToString("") { "%02x".format(it) }
    }

    private fun deviceId(): String {
        val id = Settings.Secure.getString(context.contentResolver, Settings.Secure.ANDROID_ID) ?: "android"
        return "android_$id"
    }

    companion object {
        const val INTERNAL_CLIP_LABEL = "pro.clipsync.internal"
    }
}
