package app.reelstash.android.lock

import android.app.Activity
import android.app.Application
import android.content.Context
import android.os.Bundle
import android.os.SystemClock
import android.view.WindowManager
import androidx.biometric.BiometricManager
import androidx.biometric.BiometricManager.Authenticators.BIOMETRIC_WEAK
import androidx.biometric.BiometricManager.Authenticators.DEVICE_CREDENTIAL
import androidx.biometric.BiometricPrompt
import androidx.core.content.ContextCompat
import androidx.fragment.app.FragmentActivity
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

/**
 * The app's own lock, on this device: fingerprint, face, or the phone's screen
 * lock (PIN, pattern, password). It is separate from the account's PIN lock,
 * which the server enforces. When on, the app locks at launch and after it has
 * been in the background for the chosen time, and its screens are hidden from
 * the app switcher and screenshots.
 */
class DeviceLock(app: Application) {
    private val prefs = app.getSharedPreferences("device_lock", Context.MODE_PRIVATE)

    private val _enabled = MutableStateFlow(prefs.getBoolean(KEY_ENABLED, false))
    val enabled: StateFlow<Boolean> = _enabled.asStateFlow()

    private val _timeout = MutableStateFlow(prefs.getInt(KEY_TIMEOUT, 0))
    /** Minutes in the background before it locks again; 0 locks every time. */
    val timeoutMinutes: StateFlow<Int> = _timeout.asStateFlow()

    private val _locked = MutableStateFlow(_enabled.value)
    val locked: StateFlow<Boolean> = _locked.asStateFlow()

    private var started = 0
    private var backgroundedAt = 0L

    init {
        app.registerActivityLifecycleCallbacks(object : Application.ActivityLifecycleCallbacks {
            override fun onActivityCreated(a: Activity, b: Bundle?) = secure(a)
            override fun onActivityStarted(a: Activity) {
                if (started++ == 0 && _enabled.value && backgroundedAt > 0) {
                    val away = SystemClock.elapsedRealtime() - backgroundedAt
                    if (away >= _timeout.value * 60_000L) _locked.value = true
                }
            }
            override fun onActivityResumed(a: Activity) = secure(a)
            override fun onActivityPaused(a: Activity) {}
            override fun onActivityStopped(a: Activity) {
                // Configuration changes restart the activity without leaving the app.
                if (--started == 0 && !a.isChangingConfigurations) backgroundedAt = SystemClock.elapsedRealtime()
            }
            override fun onActivitySaveInstanceState(a: Activity, b: Bundle) {}
            override fun onActivityDestroyed(a: Activity) {}
        })
    }

    /** Keeps locked content out of the app switcher and screenshots. */
    private fun secure(a: Activity) {
        if (_enabled.value) a.window.addFlags(WindowManager.LayoutParams.FLAG_SECURE)
        else a.window.clearFlags(WindowManager.LayoutParams.FLAG_SECURE)
    }

    /** Whether this phone has a fingerprint, face or screen lock the app can use. */
    fun available(context: Context): Boolean =
        BiometricManager.from(context).canAuthenticate(AUTHENTICATORS) == BiometricManager.BIOMETRIC_SUCCESS

    fun setTimeout(minutes: Int) {
        prefs.edit().putInt(KEY_TIMEOUT, minutes).apply()
        _timeout.value = minutes
    }

    /** Turns the lock on or off, after the person proves they can unlock. */
    fun setEnabled(activity: FragmentActivity, on: Boolean, onDone: (Boolean) -> Unit) {
        authenticate(activity, if (on) "Turn on app lock" else "Turn off app lock") { ok ->
            if (ok) {
                prefs.edit().putBoolean(KEY_ENABLED, on).apply()
                _enabled.value = on
                secure(activity)
            }
            onDone(ok)
        }
    }

    fun unlock(activity: FragmentActivity) {
        authenticate(activity, "Unlock ReelStash") { ok -> if (ok) _locked.value = false }
    }

    private fun authenticate(activity: FragmentActivity, title: String, onResult: (Boolean) -> Unit) {
        val prompt = BiometricPrompt(activity, ContextCompat.getMainExecutor(activity), object : BiometricPrompt.AuthenticationCallback() {
            override fun onAuthenticationSucceeded(result: BiometricPrompt.AuthenticationResult) = onResult(true)
            override fun onAuthenticationError(code: Int, message: CharSequence) = onResult(false)
        })
        val info = BiometricPrompt.PromptInfo.Builder()
            .setTitle(title)
            .setSubtitle("Use your fingerprint, face or screen lock")
            .setAllowedAuthenticators(AUTHENTICATORS)
            .build()
        prompt.authenticate(info)
    }

    companion object {
        private const val KEY_ENABLED = "enabled"
        private const val KEY_TIMEOUT = "timeout_minutes"

        /** Biometrics (fingerprint or face) or the device's own PIN/pattern/password. */
        const val AUTHENTICATORS = BIOMETRIC_WEAK or DEVICE_CREDENTIAL

        val TIMEOUTS = listOf(0 to "Lock immediately", 1 to "Lock after 1 minute", 5 to "Lock after 5 minutes", 15 to "Lock after 15 minutes")
    }
}
