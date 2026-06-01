package pro.clipsync.model

import org.json.JSONArray
import org.json.JSONObject

data class CipherText(
    val algorithm: String,
    val kdf: String,
    val iterations: Int,
    val salt: String,
    val nonce: String,
    val ciphertext: String,
) {
    companion object {
        fun fromJson(json: JSONObject?): CipherText? {
            if (json == null) return null
            return CipherText(
                algorithm = json.optString("algorithm"),
                kdf = json.optString("kdf"),
                iterations = json.optInt("iterations"),
                salt = json.optString("salt"),
                nonce = json.optString("nonce"),
                ciphertext = json.optString("ciphertext"),
            )
        }
    }
}

data class ClipboardRecord(
    val id: String,
    val contentType: String,
    val contentText: String?,
    val contentCipher: CipherText?,
    val contentHash: String,
    val isSensitive: Boolean,
    val sensitiveType: String?,
    val accountCipher: CipherText?,
    val passwordCipher: CipherText?,
    val remarkCipher: CipherText?,
    val sourceDeviceId: String,
    val sourcePlatform: String,
    val clientCreatedAt: String,
    val syncStatus: String,
) {
    val summary: String
        get() = if (isSensitive) "已加密账户密码记录" else contentText.orEmpty().replace("\n", " ").take(120)

    companion object {
        fun fromJson(json: JSONObject): ClipboardRecord {
            return ClipboardRecord(
                id = json.getString("id"),
                contentType = json.optString("content_type"),
                contentText = nullableString(json, "content_text"),
                contentCipher = CipherText.fromJson(json.optJSONObject("content_cipher")),
                contentHash = json.optString("content_hash"),
                isSensitive = json.optBoolean("is_sensitive"),
                sensitiveType = nullableString(json, "sensitive_type"),
                accountCipher = CipherText.fromJson(json.optJSONObject("account_cipher")),
                passwordCipher = CipherText.fromJson(json.optJSONObject("password_cipher")),
                remarkCipher = CipherText.fromJson(json.optJSONObject("remark_cipher")),
                sourceDeviceId = json.optString("source_device_id"),
                sourcePlatform = json.optString("source_platform"),
                clientCreatedAt = json.optString("client_created_at"),
                syncStatus = json.optString("sync_status"),
            )
        }

        fun listFromJson(array: JSONArray): List<ClipboardRecord> {
            return buildList {
                for (i in 0 until array.length()) add(fromJson(array.getJSONObject(i)))
            }
        }

        private fun nullableString(json: JSONObject, name: String): String? {
            return if (json.has(name) && !json.isNull(name)) json.optString(name) else null
        }
    }
}

data class RevealedSensitive(
    val content: String,
    val account: String,
    val password: String,
    val remark: String,
)
