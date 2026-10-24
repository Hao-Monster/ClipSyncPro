package pro.clipsync.api

import org.json.JSONObject
import pro.clipsync.model.ClipboardRecord
import pro.clipsync.model.PromptTemplate
import java.io.BufferedReader
import java.io.OutputStreamWriter
import java.net.HttpURLConnection
import java.net.URL

class ClipSyncApi(private val baseUrl: String) {
    fun list(keyword: String = "", date: String = "", sensitive: Boolean = false, favorite: Boolean = false): List<ClipboardRecord> {
        val query = buildList {
            if (keyword.isNotBlank()) add("keyword=${encode(keyword)}")
            if (date.isNotBlank()) add("date=${encode(date)}")
            if (sensitive) add("sensitive=true")
            if (favorite) add("favorite=true")
        }.joinToString("&")
        val suffix = if (query.isBlank()) "" else "?$query"
        val json = request("GET", "/api/v1/clipboard/records$suffix")
        return ClipboardRecord.listFromJson(JSONObject(json).getJSONArray("records"))
    }

    fun create(recordJson: JSONObject) {
        request("POST", "/api/v1/clipboard/records", recordJson.toString())
    }

    fun setFavorite(id: String, isFavorite: Boolean) {
        val body = JSONObject().put("is_favorite", isFavorite).toString()
        request("POST", "/api/v1/clipboard/records/${encode(id)}/favorite", body)
    }

    fun delete(id: String) {
        request("DELETE", "/api/v1/clipboard/records/${encode(id)}")
    }

    fun listPrompts(category: String = "", keyword: String = ""): List<PromptTemplate> {
        val query = buildList {
            if (category.isNotBlank() && category != "全部") add("category=${encode(category)}")
            if (keyword.isNotBlank()) add("keyword=${encode(keyword)}")
        }.joinToString("&")
        val suffix = if (query.isBlank()) "" else "?$query"
        val json = request("GET", "/api/v1/clipboard/prompts$suffix")
        return PromptTemplate.listFromJson(JSONObject(json).getJSONArray("prompts"))
    }

    fun savePrompt(promptJson: JSONObject): PromptTemplate {
        val json = request("POST", "/api/v1/clipboard/prompts", promptJson.toString())
        return PromptTemplate.fromJson(JSONObject(json))
    }

    fun deletePrompt(id: String) {
        request("DELETE", "/api/v1/clipboard/prompts/${encode(id)}")
    }

    fun usePrompt(id: String) {
        request("POST", "/api/v1/clipboard/prompts/${encode(id)}/use")
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
