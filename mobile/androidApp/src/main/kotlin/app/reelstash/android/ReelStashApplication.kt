package app.reelstash.android

import android.app.Application
import android.content.Context
import app.reelstash.android.data.AppContainer
import app.reelstash.android.lock.DeviceLock
import coil3.ImageLoader
import coil3.SingletonImageLoader
import coil3.network.okhttp.OkHttpNetworkFetcherFactory
import coil3.request.crossfade

class ReelStashApplication : Application(), SingletonImageLoader.Factory {
    lateinit var container: AppContainer
        private set
    lateinit var deviceLock: DeviceLock
        private set

    override fun onCreate() {
        super.onCreate()
        deviceLock = DeviceLock(this)
        container = AppContainer(this)
        container.start()
    }

    // Images load through the same authenticated client as the player.
    override fun newImageLoader(context: Context): ImageLoader =
        ImageLoader.Builder(context)
            .components { add(OkHttpNetworkFetcherFactory(callFactory = { container.mediaHttp })) }
            .crossfade(true)
            .build()
}

val Context.container: AppContainer get() = (applicationContext as ReelStashApplication).container

val Context.deviceLock: DeviceLock get() = (applicationContext as ReelStashApplication).deviceLock
