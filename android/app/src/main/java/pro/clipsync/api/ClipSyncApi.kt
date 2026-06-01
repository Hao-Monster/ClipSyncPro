package pro.clipsync.api

import org.json.JSONObject
import pro.clipsync.model.ClipboardRecord
import java.io.BufferedReader
import java.io.OutputStreamWriter
import java.net.HttpURLConnection
import java.net.URL

class ClipSyncApi(private val baseUrl: String) {
    fun list(keyword: String = "", date: String = "", sensitive: Boolean = false): List<ClipboardRecord> {
        val query = buildList {
            if (keyword.isNotBlank()) add("keyword=${encode(keyword)}")
            if (date.isNotBlank()) add("date=${encode(date)}")
            if (sensitive) add("sensitive=true")
        }.joinToString("&")
        val suffix = if (query.isBlank()) "" else "?$query"
        val json = request("GET", "/api/v1/clipboard/records$suffix")
        return ClipboardRecord.listFromJson(JSONObject(json).getJSONArray("records"))
    }

    fun create(recordJson: JSONObject) {
        request("POST", "/api/v1/clipboard/records", recordJson.toString())
    }

    fun delete(id: String) {
        request("DELETE", "/api/v1/clipboard/records/${encode(id)}")
    }

    private fun request(method: String, path: String, body: String? = null): String {
        val conn = (URL(baseUrl.trimEnd('/') + path).openConnection() as HttpURLConnection).apply {
            requestMethod = method
            connectTimeout = 5000
            readTimeout = 10000
            setRequestProperty("Content-Type", "application/json; charset=utf-8")
            doInput = true
            doOutput = body != null
        }
        if (body != null) {
            OutputStreamWriter(conn.outputStream, Charsets.UTF_8).use { it.write(body) }
        }
        val stream = if (conn.responseCode in 200..299) conn.inputStream else conn.errorStream
        val text = BufferedReader(stream.reader(Charsets.UTF_8)).use { it.readText() }
        if (conn.responseCode !in 200..299) error(text)
        return text
    }

    private fun encode(value: String): String = java.net.URLEncoder.encode(value, Charsets.UTF_8.name())
}
