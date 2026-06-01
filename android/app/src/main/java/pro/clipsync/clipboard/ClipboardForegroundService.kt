package pro.clipsync.clipboard

import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.Service
import android.content.ClipboardManager
import android.content.Context
import android.content.Intent
import android.os.IBinder
import androidx.core.app.NotificationCompat
import pro.clipsync.data.ClipboardRepository
import kotlin.concurrent.thread

class ClipboardForegroundService : Service() {
    private lateinit var clipboard: ClipboardManager
    private lateinit var repository: ClipboardRepository
    private var lastText: String = ""

    override fun onCreate() {
        super.onCreate()
        clipboard = getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager
        repository = ClipboardRepository(this)
        ensureChannel()
        startForeground(
            100,
            NotificationCompat.Builder(this, CHANNEL_ID)
                .setSmallIcon(android.R.drawable.ic_menu_save)
                .setContentTitle("ClipSync Pro")
                .setContentText("正在监听系统允许范围内的剪贴板")
                .setOngoing(true)
                .build()
        )
        clipboard.addPrimaryClipChangedListener(listener)
    }

    override fun onDestroy() {
        clipboard.removePrimaryClipChangedListener(listener)
        super.onDestroy()
    }

    override fun onBind(intent: Intent?): IBinder? = null

    private val listener = ClipboardManager.OnPrimaryClipChangedListener {
        val text = clipboard.primaryClip?.getItemAt(0)?.coerceToText(this)?.toString().orEmpty()
        if (text == lastText) return@OnPrimaryClipChangedListener
        lastText = text
        thread(name = "clipsync-upload") {
            runCatching { repository.uploadClipboardText(text) }
        }
    }

    private fun ensureChannel() {
        val manager = getSystemService(NotificationManager::class.java)
        manager.createNotificationChannel(
            NotificationChannel(CHANNEL_ID, "ClipSync Pro", NotificationManager.IMPORTANCE_LOW)
        )
    }

    companion object {
        private const val CHANNEL_ID = "clipsync_clipboard"
    }
}
