package app.reelstash.shared

import app.reelstash.shared.api.ApiException
import app.reelstash.shared.api.DeviceDescription
import app.reelstash.shared.api.LockedException
import app.reelstash.shared.api.ReelStashClient
import app.reelstash.shared.api.ServerCredentials
import app.reelstash.shared.api.SessionEvent
import app.reelstash.shared.api.UnauthorizedException
import app.reelstash.shared.model.normalizeColorTheme
import io.ktor.client.engine.mock.MockEngine
import io.ktor.client.engine.mock.MockRequestHandleScope
import io.ktor.client.engine.mock.respond
import io.ktor.client.request.HttpRequestData
import io.ktor.client.request.HttpResponseData
import io.ktor.http.HttpStatusCode
import io.ktor.http.content.TextContent
import io.ktor.http.headersOf
import kotlinx.coroutines.async
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.test.runTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertNull
import kotlin.test.assertTrue

class ReelStashClientTest {
    private val requests = mutableListOf<HttpRequestData>()

    private fun client(creds: ServerCredentials = ServerCredentials(token = "tok", libraryId = 8), handler: MockRequestHandleScope.(HttpRequestData) -> HttpResponseData) =
        ReelStashClient("http://nas.local:8000/", creds, MockEngine { req -> requests += req; handler(req) })

    private fun MockRequestHandleScope.json(body: String, status: HttpStatusCode = HttpStatusCode.OK, extra: Map<String, String> = emptyMap()) =
        respond(body, status, headersOf(*(extra + ("Content-Type" to "application/json")).map { it.key to listOf(it.value) }.toTypedArray()))

    @Test
    fun sendsBearerTokenAndLibrary() = runTest {
        val c = client { json("""{"id":3,"displayName":"Ann","isAdmin":false,"unknown":1}""") }
        val me = c.me()
        assertEquals("Ann", me.displayName)
        val req = requests.single()
        assertEquals("http://nas.local:8000/api/me", req.url.toString())
        assertEquals("Bearer tok", req.headers["Authorization"])
        assertEquals("8", req.headers["X-Library"])
    }

    @Test
    fun signInCallsDoNotSendCredentials() = runTest {
        val c = client { json("""{"token":"new","user":{"id":1}}""") }
        val session = c.passwordSignIn("ann", "pw", DeviceDescription("Pixel", "android"))
        assertEquals("new", session.token)
        val req = requests.single()
        assertNull(req.headers["Authorization"])
        val body = (req.body as TextContent).text
        assertTrue(body.contains("\"deviceName\":\"Pixel\"") && body.contains("\"platform\":\"android\""), body)
    }

    @Test
    fun pairingPollIsNullWhilePending() = runTest {
        var approved = false
        val c = client { if (approved) json("""{"token":"t2","user":{"id":2}}""") else json("""{"status":"pending"}""", HttpStatusCode.Accepted) }
        assertNull(c.pollPairing("dc"))
        approved = true
        assertEquals("t2", c.pollPairing("dc")?.token)
    }

    @Test
    fun unauthorizedAndLockedRaiseEvents() = runTest {
        val creds = ServerCredentials(token = "tok", unlockToken = "old")
        var status = HttpStatusCode.Unauthorized
        val c = client(creds) { json("""{"error":"nope"}""", status) }
        val signedOut = async { c.events.first() }
        assertFailsWith<UnauthorizedException> { c.home() }
        assertEquals(SessionEvent.SignedOut, signedOut.await())

        status = HttpStatusCode.Locked
        val locked = async { c.events.first() }
        val e = assertFailsWith<LockedException> { c.home() }
        assertEquals("nope", e.message)
        assertEquals(SessionEvent.Locked, locked.await())
        assertNull(creds.unlockToken, "a lock drops the stale unlock token")

        status = HttpStatusCode.NotFound
        assertEquals(404, assertFailsWith<ApiException> { c.home() }.status)
    }

    @Test
    fun unlockTokensAreKeptFromBodyAndRefreshedFromHeaders() = runTest {
        val creds = ServerCredentials(token = "tok")
        val c = client(creds) { req ->
            if (req.url.encodedPath.endsWith("/verify")) json("""{"valid":true,"unlockToken":"u1"}""")
            else json("""{"sections":[]}""", extra = mapOf("X-Reelstash-Unlock" to "u2"))
        }
        assertTrue(c.unlock("123456"))
        assertEquals("u1", creds.unlockToken)
        c.home()
        assertEquals("u1", requests.last().headers["X-Reelstash-Unlock"])
        assertEquals("u2", creds.unlockToken)
    }

    @Test
    fun readsTheAdminColorTheme() = runTest {
        val c = client(ServerCredentials()) { json("""{"colorTheme":"iris","plugins":[]}""") }
        assertEquals("iris", c.authConfig().colorTheme)
        assertEquals("/auth/config", requests.last().url.encodedPath)
        assertEquals("grove", normalizeColorTheme(null))
        assertEquals("grove", normalizeColorTheme("pink"))
        assertEquals("aurora", normalizeColorTheme("aurora"))
    }
}
