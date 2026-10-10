package app.reelstash.android.ui.connect

import android.content.Intent
import android.net.Uri
import androidx.activity.compose.BackHandler
import androidx.compose.foundation.Image
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawingPadding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.outlined.Dns
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import app.reelstash.android.R
import app.reelstash.android.container
import app.reelstash.android.ui.common.GradientButton
import app.reelstash.android.ui.common.ambientGlow
import app.reelstash.android.ui.theme.Rs

/** Adding a server: its address, then signing in by password or by a code approved on the web. */
@Composable
fun ConnectScreen(canCancel: Boolean, onCancel: () -> Unit) {
    val context = LocalContext.current
    val vm = viewModel { ConnectViewModel(context.container) }
    val state by vm.state.collectAsStateWithLifecycle()

    BackHandler(enabled = state.server != null || canCancel) {
        if (state.server != null) vm.back() else onCancel()
    }

    Box(Modifier.fillMaxSize().ambientGlow(Rs.colors.glow).safeDrawingPadding().imePadding(), contentAlignment = Alignment.TopCenter) {
        Column(
            Modifier.widthIn(max = 480.dp).fillMaxWidth().verticalScroll(rememberScrollState()).padding(24.dp),
        ) {
            if (state.server != null || canCancel) {
                IconButton(onClick = { if (state.server != null) vm.back() else onCancel() }) {
                    Icon(Icons.AutoMirrored.Filled.ArrowBack, "Back")
                }
            } else Spacer(Modifier.height(48.dp))
            Spacer(Modifier.height(24.dp))
            val server = state.server
            if (server == null) AddressStep(state, vm) else SignInStep(state, server, vm)
        }
    }
}

@Composable
private fun AddressStep(state: ConnectUiState, vm: ConnectViewModel) {
    Image(painterResource(R.drawable.ic_logo), contentDescription = null, modifier = Modifier.size(64.dp))
    Spacer(Modifier.height(16.dp))
    Text("ReelStash", style = MaterialTheme.typography.headlineMedium)
    Spacer(Modifier.height(8.dp))
    Text("Connect to your server", style = MaterialTheme.typography.titleLarge)
    Spacer(Modifier.height(4.dp))
    Text(
        "Enter the address you open ReelStash at in a browser, like nas.local:8000 or media.example.com.",
        style = MaterialTheme.typography.bodyMedium,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
    )
    Spacer(Modifier.height(24.dp))
    OutlinedTextField(
        value = state.address,
        onValueChange = vm::setAddress,
        label = { Text("Server address") },
        leadingIcon = { Icon(Icons.Outlined.Dns, null) },
        singleLine = true,
        isError = state.error != null,
        supportingText = state.error?.let { { Text(it) } },
        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri, imeAction = ImeAction.Go, autoCorrectEnabled = false),
        keyboardActions = KeyboardActions(onGo = { vm.check() }),
        modifier = Modifier.fillMaxWidth(),
    )
    Spacer(Modifier.height(16.dp))
    GradientButton(if (state.checking) "Checking…" else "Continue", onClick = vm::check, enabled = !state.checking && state.address.isNotBlank(), modifier = Modifier.fillMaxWidth())
}

@Composable
private fun SignInStep(state: ConnectUiState, server: FoundServer, vm: ConnectViewModel) {
    var showPassword by rememberSaveable { mutableStateOf(server.info.passwordLogin) }
    Text("Sign in to ${server.info.name}", style = MaterialTheme.typography.titleLarge)
    Text(server.baseUrl, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
    Spacer(Modifier.height(24.dp))

    if (server.info.passwordLogin && showPassword) {
        PasswordForm(state, vm)
        Spacer(Modifier.height(24.dp))
        Row(verticalAlignment = Alignment.CenterVertically) {
            HorizontalDivider(Modifier.weight(1f))
            Text("  or  ", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            HorizontalDivider(Modifier.weight(1f))
        }
        Spacer(Modifier.height(16.dp))
        OutlinedButton(onClick = { showPassword = false; vm.startPairing() }, modifier = Modifier.fillMaxWidth()) {
            Text("Sign in with a code instead")
        }
    } else {
        PairingPanel(state, server, vm)
        if (server.info.passwordLogin) {
            Spacer(Modifier.height(12.dp))
            TextButton(onClick = { showPassword = true; vm.stopPairing() }, modifier = Modifier.fillMaxWidth()) {
                Text("Use a username and password")
            }
        }
    }
}

@Composable
private fun PasswordForm(state: ConnectUiState, vm: ConnectViewModel) {
    var username by rememberSaveable { mutableStateOf("") }
    var password by rememberSaveable { mutableStateOf("") }
    val submit = { if (username.isNotBlank() && password.isNotEmpty()) vm.signIn(username, password) }
    Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
        OutlinedTextField(
            value = username,
            onValueChange = { username = it },
            label = { Text("Username") },
            singleLine = true,
            keyboardOptions = KeyboardOptions(imeAction = ImeAction.Next, autoCorrectEnabled = false),
            modifier = Modifier.fillMaxWidth(),
        )
        OutlinedTextField(
            value = password,
            onValueChange = { password = it },
            label = { Text("Password") },
            singleLine = true,
            visualTransformation = PasswordVisualTransformation(),
            isError = state.error != null,
            supportingText = state.error?.let { { Text(it) } },
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password, imeAction = ImeAction.Go),
            keyboardActions = KeyboardActions(onGo = { submit() }),
            modifier = Modifier.fillMaxWidth(),
        )
        GradientButton(if (state.signingIn) "Signing in…" else "Sign in", onClick = submit, enabled = !state.signingIn && username.isNotBlank() && password.isNotEmpty(), modifier = Modifier.fillMaxWidth())
    }
}

@Composable
private fun PairingPanel(state: ConnectUiState, server: FoundServer, vm: ConnectViewModel) {
    val context = LocalContext.current
    val pairing = state.pairing
    Card(colors = CardDefaults.cardColors(containerColor = Rs.colors.surface), border = androidx.compose.foundation.BorderStroke(1.dp, Rs.colors.border), shape = androidx.compose.foundation.shape.RoundedCornerShape(18.dp), modifier = Modifier.fillMaxWidth()) {
        Column(Modifier.padding(20.dp), horizontalAlignment = Alignment.CenterHorizontally) {
            when {
                state.pairingError != null -> {
                    Text(state.pairingError, style = MaterialTheme.typography.bodyMedium, textAlign = TextAlign.Center)
                    Spacer(Modifier.height(12.dp))
                    GradientButton("Get a new code", onClick = vm::startPairing)
                }
                pairing == null -> CircularProgressIndicator()
                else -> {
                    Text("Sign in on the web, then enter this code:", style = MaterialTheme.typography.bodyMedium, textAlign = TextAlign.Center)
                    Spacer(Modifier.height(12.dp))
                    Text(
                        pairing.userCode,
                        fontFamily = FontFamily.Monospace,
                        fontWeight = FontWeight.Bold,
                        fontSize = 34.sp,
                        letterSpacing = 4.sp,
                        color = MaterialTheme.colorScheme.primary,
                    )
                    Spacer(Modifier.height(8.dp))
                    Text(pairing.verificationUrl, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, textAlign = TextAlign.Center)
                    Spacer(Modifier.height(16.dp))
                    GradientButton("Open in browser", onClick = {
                        val url = "${pairing.verificationUrl}?code=${Uri.encode(pairing.userCode)}"
                        context.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(url)))
                    })
                    Spacer(Modifier.height(12.dp))
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        CircularProgressIndicator(Modifier.size(14.dp), strokeWidth = 2.dp)
                        Text("  Waiting for approval…", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                }
            }
        }
    }
    if (!server.info.passwordLogin) {
        Spacer(Modifier.height(12.dp))
        Text(
            "This server signs people in through the web, so the app signs in with a code you approve there.",
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
}
