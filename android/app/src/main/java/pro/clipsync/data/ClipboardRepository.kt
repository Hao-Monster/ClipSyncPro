package pro.clipsync.data

import android.content.Context
import android.provider.Settings
import org.json.JSONObject
import pro.clipsync.api.ClipSyncApi
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
}
