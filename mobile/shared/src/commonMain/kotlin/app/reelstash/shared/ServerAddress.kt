package app.reelstash.shared

/**
 * Turns what someone types into a server base URL: "nas.local:8000",
 * "https://media.example.com/", " HTTP://Host " all become a clean
 * scheme://host[:port][/path] with no trailing slash.
 *
 * A bare host defaults to https, except for addresses that are almost always
 * plain HTTP at home: IP literals, .local/.lan/.home names and localhost.
 */
object ServerAddress {
    sealed interface Result {
        data class Ok(val url: String) : Result
        data class Invalid(val reason: String) : Result
    }

    fun normalize(input: String): Result {
        var text = input.trim()
        if (text.isEmpty()) return Result.Invalid("Enter your server's address")
        if (text.any { it.isWhitespace() }) return Result.Invalid("The address can't contain spaces")

        val schemeEnd = text.indexOf("://")
        val scheme: String
        if (schemeEnd >= 0) {
            scheme = text.substring(0, schemeEnd).lowercase()
            if (scheme != "http" && scheme != "https") return Result.Invalid("Use an http:// or https:// address")
            text = text.substring(schemeEnd + 3)
        } else {
            scheme = if (looksLocal(hostOf(text))) "http" else "https"
        }
        // Drop query and fragment, then trailing slashes.
        text = text.substringBefore('#').substringBefore('?').trimEnd('/')
        val host = hostOf(text)
        if (host.isEmpty()) return Result.Invalid("That address has no host name")
        val port = text.substringBefore('/').removePrefix(hostPart(text)).removePrefix(":")
        if (port.isNotEmpty() && (port.toIntOrNull() == null || port.toInt() !in 1..65535)) {
            return Result.Invalid("That port number isn't valid")
        }
        val authority = text.substringBefore('/')
        val path = if (text.contains('/')) "/" + text.substringAfter('/') else ""
        return Result.Ok("$scheme://${authority.lowercase()}$path")
    }

    private fun hostPart(text: String): String {
        val authority = text.substringBefore('/')
        return if (authority.startsWith("[")) authority.substringBefore(']') + "]" else authority.substringBefore(':')
    }

    private fun hostOf(text: String): String = hostPart(text).removePrefix("[").removeSuffix("]").lowercase()

    private fun looksLocal(host: String): Boolean {
        if (host == "localhost" || host.endsWith(".local") || host.endsWith(".lan") || host.endsWith(".home") || host.endsWith(".internal")) return true
        if (host.contains(':')) return true // IPv6 literal
        val parts = host.split('.')
        return parts.size == 4 && parts.all { p -> p.toIntOrNull()?.let { it in 0..255 } == true }
    }
}
