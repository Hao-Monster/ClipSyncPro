package pro.clipsync.crypto

import android.util.Base64
import pro.clipsync.model.CipherText
import javax.crypto.Cipher
import javax.crypto.SecretKeyFactory
import javax.crypto.spec.GCMParameterSpec
import javax.crypto.spec.PBEKeySpec
import javax.crypto.spec.SecretKeySpec

object SecretBox {
    fun decrypt(secret: String, box: CipherText?): String {
        if (box == null || box.ciphertext.isEmpty()) return ""
        require(box.algorithm == "AES-256-GCM") { "Unsupported algorithm" }
        val salt = Base64.decode(box.salt, Base64.DEFAULT)
        val nonce = Base64.decode(box.nonce, Base64.DEFAULT)
        val encrypted = Base64.decode(box.ciphertext, Base64.DEFAULT)
        val keySpec = PBEKeySpec(secret.toCharArray(), salt, box.iterations, 256)
        val key = SecretKeyFactory.getInstance("PBKDF2WithHmacSHA256").generateSecret(keySpec).encoded
        val cipher = Cipher.getInstance("AES/GCM/NoPadding")
        cipher.init(Cipher.DECRYPT_MODE, SecretKeySpec(key, "AES"), GCMParameterSpec(128, nonce))
        return String(cipher.doFinal(encrypted), Charsets.UTF_8)
    }
}
