-- A series can carry its own backdrop image (uploaded in the series settings),
-- shown behind the series page header.
ALTER TABLE series ADD COLUMN IF NOT EXISTS backdrop_image_id BIGINT REFERENCES artwork(id) ON DELETE SET NULL;
