package pro.clipsync.crypto

import android.util.Base64
import pro.clipsync.model.CipherText
import java.security.SecureRandom
import javax.crypto.Cipher
import javax.crypto.Mac
import javax.crypto.SecretKeyFactory
import javax.crypto.spec.GCMParameterSpec
import javax.crypto.spec.PBEKeySpec
import javax.crypto.spec.SecretKeySpec

object SecretBox {
    private const val Algorithm = "AES-256-GCM"
    private const val Kdf = "PBKDF2-HMAC-SHA256"
    private const val Iterations = 120000
    private val random = SecureRandom()

    fun encrypt(secret: String, plaintext: String): CipherText {
        require(secret.isNotBlank()) { "加密密钥不能为空" }
        val salt = ByteArray(16).also(random::nextBytes)
        val nonce = ByteArray(12).also(random::nextBytes)
        val key = deriveKey(secret, salt, Iterations)
        val cipher = Cipher.getInstance("AES/GCM/NoPadding")
        cipher.init(Cipher.ENCRYPT_MODE, SecretKeySpec(key, "AES"), GCMParameterSpec(128, nonce))
        val encrypted = cipher.doFinal(plaintext.toByteArray(Charsets.UTF_8))
        return CipherText(
            algorithm = Algorithm,
            kdf = Kdf,
            iterations = Iterations,
            salt = Base64.encodeToString(salt, Base64.NO_WRAP),
            nonce = Base64.encodeToString(nonce, Base64.NO_WRAP),
            ciphertext = Base64.encodeToString(encrypted, Base64.NO_WRAP),
        )
    }

    fun decrypt(secret: String, box: CipherText?): String {
        if (box == null || box.ciphertext.isEmpty()) return ""
        require(secret.isNotBlank()) { "加密密钥不能为空" }
        require(box.algorithm == Algorithm) { "Unsupported algorithm" }
        require(box.kdf == Kdf) { "Unsupported KDF" }
        val salt = Base64.decode(box.salt, Base64.DEFAULT)
        val nonce = Base64.decode(box.nonce, Base64.DEFAULT)
        val encrypted = Base64.decode(box.ciphertext, Base64.DEFAULT)
        val key = deriveKey(secret, salt, box.iterations)
        val cipher = Cipher.getInstance("AES/GCM/NoPadding")
        cipher.init(Cipher.DECRYPT_MODE, SecretKeySpec(key, "AES"), GCMParameterSpec(128, nonce))
        return String(cipher.doFinal(encrypted), Charsets.UTF_8)
    }

    fun searchIndex(secret: String, vararg values: String): List<String> {
        val seen = linkedSetOf<String>()
        values.forEach { value ->
            searchTokens(value).forEach { token ->
                seen.add(hmacSha256Hex(secret, token))
            }
        }
        return seen.toList()
    }

    private fun deriveKey(secret: String, salt: ByteArray, iterations: Int): ByteArray {
        val keySpec = PBEKeySpec(secret.toCharArray(), salt, iterations, 256)
        return SecretKeyFactory.getInstance("PBKDF2WithHmacSHA256").generateSecret(keySpec).encoded
    }

    private fun hmacSha256Hex(secret: String, value: String): String {
        val mac = Mac.getInstance("HmacSHA256")
        mac.init(SecretKeySpec(secret.toByteArray(Charsets.UTF_8), "HmacSHA256"))
        return mac.doFinal(value.toByteArray(Charsets.UTF_8)).joinToString("") { "%02x".format(it) }
    }

    private fun searchTokens(value: String): Set<String> {
        val normalized = value.trim().lowercase()
        if (normalized.isEmpty()) return emptySet()
        val seen = linkedSetOf<String>()
        normalized.split(Regex("[\\s\\p{P}\\p{S}]+"))
            .filter { it.isNotEmpty() }
            .forEach { part ->
                seen.add(part)
                addNgrams(part, seen)
            }
        addNgrams(normalized, seen)
        return seen
    }

    private fun addNgrams(value: String, out: MutableSet<String>) {
        val chars = value.toList()
        for (n in 1..3) {
            if (chars.size < n) continue
            for (i in 0..(chars.size - n)) {
                out.add(chars.subList(i, i + n).joinToString(""))
            }
        }
    }
}
