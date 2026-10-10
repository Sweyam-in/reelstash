plugins {
    alias(libs.plugins.kotlin.multiplatform)
    alias(libs.plugins.android.kmp.library)
    alias(libs.plugins.kotlin.serialization)
}

kotlin {
    androidLibrary {
        namespace = "app.reelstash.shared"
        compileSdk = 37
        minSdk = 26
        withHostTestBuilder {}
    }

    // Off unless building the Apple app (`-Preelstash.ios=true`, which the Xcode project passes).
    val apple = providers.gradleProperty("reelstash.ios").orNull == "true"
    if (apple) {
        listOf(iosArm64(), iosSimulatorArm64()).forEach { target ->
            target.binaries.framework {
                baseName = "Shared"
                isStatic = true
            }
        }
    }

    sourceSets {
        commonMain.dependencies {
            api(libs.kotlinx.coroutines.core)
            api(libs.kotlinx.serialization.json)
            api(libs.ktor.client.core)
        }
        commonTest.dependencies {
            implementation(kotlin("test"))
            implementation(libs.kotlinx.coroutines.test)
            implementation(libs.ktor.client.mock)
        }
        androidMain.dependencies {
            api(libs.ktor.client.okhttp)
        }
        if (apple) {
            iosMain.dependencies {
                implementation(libs.ktor.client.darwin)
            }
        }
    }
}
