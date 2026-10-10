package app.reelstash.shared

import app.reelstash.shared.ServerAddress.Result.Invalid
import app.reelstash.shared.ServerAddress.Result.Ok
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertIs

class ServerAddressTest {
    private fun ok(input: String) = (ServerAddress.normalize(input) as Ok).url

    @Test
    fun publicHostsDefaultToHttps() {
        assertEquals("https://media.example.com", ok("media.example.com"))
        assertEquals("https://media.example.com", ok(" Media.Example.com/ "))
    }

    @Test
    fun homeNetworkHostsDefaultToHttp() {
        assertEquals("http://media.local:8000", ok("media.local:8000"))
        assertEquals("http://192.168.1.20:8000", ok("192.168.1.20:8000"))
        assertEquals("http://localhost:3000", ok("localhost:3000"))
        assertEquals("http://[fd00::1]:8000", ok("[fd00::1]:8000"))
    }

    @Test
    fun keepsExplicitSchemeAndPathButDropsQuery() {
        assertEquals("http://media.example.com/reelstash", ok("HTTP://media.example.com/reelstash/?x=1#top"))
        assertEquals("https://10.0.0.5", ok("https://10.0.0.5"))
    }

    @Test
    fun rejectsNonsense() {
        assertIs<Invalid>(ServerAddress.normalize(""))
        assertIs<Invalid>(ServerAddress.normalize("ftp://host"))
        assertIs<Invalid>(ServerAddress.normalize("my server"))
        assertIs<Invalid>(ServerAddress.normalize("host:99999"))
        assertIs<Invalid>(ServerAddress.normalize("https://"))
    }
}
