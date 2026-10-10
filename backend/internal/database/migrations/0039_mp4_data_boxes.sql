-- How many top-level 'mdat' boxes an MP4/MOV file has (NULL: not checked yet,
-- or not an MP4). Some downloaders write one per few seconds of video; a
-- browser opening such a file seeks to every box header first, which pulls
-- nearly the whole file before the first frame. MP4_REPAIR rewrites them.
ALTER TABLE media_file ADD COLUMN IF NOT EXISTS mp4_data_boxes INTEGER;
