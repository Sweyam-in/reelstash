-- Daily streaming totals for admin analytics: bytes sent by direct play and
-- by HLS transcode, and the most streams playing at once that day.
CREATE TABLE IF NOT EXISTS stream_daily (
    day          DATE PRIMARY KEY,
    direct_bytes BIGINT NOT NULL DEFAULT 0,
    hls_bytes    BIGINT NOT NULL DEFAULT 0,
    peak_streams INTEGER NOT NULL DEFAULT 0
);
