package app.reelstash.shared.playback

/**
 * Counts how long someone actually watched, the way the web player does:
 * only continuous playback counts, so seeks and stalls (big jumps between
 * ticks) are ignored. It also says when to save the resume position.
 */
class WatchProgress(startPosition: Double = 0.0) {
    var watchedSeconds = 0.0
        private set
    var position = startPosition
        private set
    private var lastTick: Double? = null
    private var lastSaved = startPosition

    /** Records the player position; true when the resume point should be saved now. */
    fun tick(positionSeconds: Double): Boolean {
        lastTick?.let { last ->
            val delta = positionSeconds - last
            if (delta > 0 && delta < MAX_TICK_GAP) watchedSeconds += delta
        }
        lastTick = positionSeconds
        position = positionSeconds
        return kotlin.math.abs(positionSeconds - lastSaved) >= SAVE_EVERY
    }

    fun markSaved() {
        lastSaved = position
    }

    /** A seek: the next tick starts a new run instead of counting the jump. */
    fun seeked(positionSeconds: Double) {
        lastTick = null
        position = positionSeconds
    }

    companion object {
        const val SAVE_EVERY = 15.0
        const val MAX_TICK_GAP = 2.5

        /** Within this many seconds of the end, a title counts as finished. */
        const val END_MARGIN = 5.0

        /** Resume only from a point that leaves something to watch. */
        fun resumeFrom(positionSeconds: Double?, durationSeconds: Double?): Double {
            val pos = positionSeconds ?: return 0.0
            val dur = durationSeconds ?: return pos
            return if (pos > 0 && pos < dur - END_MARGIN) pos else 0.0
        }
    }
}
