-- SQLite schema for local ReelStash installations.
PRAGMA foreign_keys = ON;
PRAGMA journal_mode = WAL;

CREATE TABLE IF NOT EXISTS "admin_audit_event" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  "actor_id" INTEGER,
  "actor_name" TEXT NOT NULL DEFAULT '',
  "action" TEXT NOT NULL,
  "method" TEXT NOT NULL DEFAULT '',
  "route" TEXT NOT NULL DEFAULT '',
  "target_kind" TEXT NOT NULL DEFAULT '',
  "target_id" INTEGER,
  "target_label" TEXT NOT NULL DEFAULT '',
  "reason" TEXT NOT NULL DEFAULT '',
  "detail" TEXT
);

CREATE TABLE IF NOT EXISTS "app_lock" (
  "user_id" INTEGER NOT NULL,
  "pin_hash" TEXT NOT NULL,
  "idle_minutes" INTEGER NOT NULL DEFAULT 30,
  "failed_attempts" INTEGER NOT NULL DEFAULT 0,
  "locked_until" TIMESTAMP,
  "created_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  "updated_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  "library_ids" TEXT,
  CONSTRAINT "app_lock_pkey" PRIMARY KEY (user_id),
  CONSTRAINT "app_lock_user_id_fkey" FOREIGN KEY (user_id) REFERENCES app_user(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS "app_setting" (
  "key" TEXT NOT NULL,
  "value" TEXT NOT NULL,
  "updated_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT "app_setting_pkey" PRIMARY KEY (key)
);

CREATE TABLE IF NOT EXISTS "app_user" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "auth_subject" TEXT NOT NULL,
  "email" TEXT,
  "display_name" TEXT,
  "avatar_url" TEXT,
  "role" TEXT NOT NULL,
  "disabled" BOOLEAN NOT NULL DEFAULT 0,
  "created_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  "updated_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  "last_login_at" TIMESTAMP,
  "default_library_id" INTEGER,
  "preferred_audio_language" TEXT,
  "preferred_subtitle_language" TEXT,
  "subtitle_mode" TEXT NOT NULL DEFAULT 'off',
  "username" TEXT,
  "password_hash" TEXT,
  CONSTRAINT "app_user_auth_subject_key" UNIQUE (auth_subject),
  CONSTRAINT "app_user_default_library_id_fkey" FOREIGN KEY (default_library_id) REFERENCES library(id) ON DELETE SET NULL,
  CONSTRAINT "app_user_role_check" CHECK ((role IN ('ADMIN', 'USER'))),
  CONSTRAINT "app_user_subtitle_mode_check" CHECK ((subtitle_mode IN ('off', 'on', 'foreign')))
);

CREATE TABLE IF NOT EXISTS "artwork" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "kind" TEXT NOT NULL,
  "storage_key" TEXT NOT NULL,
  "mime_type" TEXT NOT NULL,
  "width" INTEGER,
  "height" INTEGER,
  "created_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  "custom" BOOLEAN NOT NULL DEFAULT 0,
  CONSTRAINT "artwork_kind_check" CHECK ((kind IN ('POSTER', 'BACKDROP', 'PROFILE', 'LOGO'))),
  CONSTRAINT "artwork_storage_key_key" UNIQUE (storage_key)
);

CREATE TABLE IF NOT EXISTS "auth_throttle" (
  "bucket" TEXT NOT NULL,
  "fails" INTEGER NOT NULL,
  "until_at" TIMESTAMP NOT NULL,
  CONSTRAINT "auth_throttle_pkey" PRIMARY KEY (bucket)
);

CREATE TABLE IF NOT EXISTS "background_job" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "kind" TEXT NOT NULL,
  "status" TEXT NOT NULL,
  "payload" TEXT NOT NULL DEFAULT '{}',
  "result" TEXT,
  "error" TEXT,
  "attempts" INTEGER NOT NULL DEFAULT 0,
  "created_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  "started_at" TIMESTAMP,
  "finished_at" TIMESTAMP,
  "progress_done" INTEGER NOT NULL DEFAULT 0,
  "progress_total" INTEGER NOT NULL DEFAULT 0,
  "progress_label" TEXT,
  CONSTRAINT "background_job_status_check" CHECK ((status IN ('PENDING', 'RUNNING', 'COMPLETED', 'FAILED', 'CANCELLED')))
);

CREATE TABLE IF NOT EXISTS "category" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "name" TEXT NOT NULL,
  "slug" TEXT NOT NULL,
  "parent_id" INTEGER,
  CONSTRAINT "category_parent_id_fkey" FOREIGN KEY (parent_id) REFERENCES category(id) ON DELETE SET NULL,
  CONSTRAINT "category_slug_key" UNIQUE (slug)
);

CREATE TABLE IF NOT EXISTS "collection" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "name" TEXT NOT NULL,
  "description" TEXT,
  "kind" TEXT NOT NULL,
  "rules" TEXT,
  "created_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  "updated_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  "pinned" BOOLEAN NOT NULL DEFAULT 0,
  "user_id" INTEGER,
  "library_id" INTEGER,
  CONSTRAINT "collection_kind_check" CHECK ((kind IN ('MANUAL', 'SMART'))),
  CONSTRAINT "collection_library_id_fkey" FOREIGN KEY (library_id) REFERENCES library(id) ON DELETE SET NULL,
  CONSTRAINT "collection_user_id_fkey" FOREIGN KEY (user_id) REFERENCES app_user(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS "collection_media_item" (
  "collection_id" INTEGER NOT NULL,
  "media_item_id" INTEGER NOT NULL,
  "position" INTEGER NOT NULL DEFAULT 0,
  CONSTRAINT "collection_media_item_collection_id_fkey" FOREIGN KEY (collection_id) REFERENCES collection(id) ON DELETE CASCADE,
  CONSTRAINT "collection_media_item_media_item_id_fkey" FOREIGN KEY (media_item_id) REFERENCES media_item(id) ON DELETE CASCADE,
  CONSTRAINT "collection_media_item_pkey" PRIMARY KEY (collection_id, media_item_id)
);

CREATE TABLE IF NOT EXISTS "device_pairing" (
  "device_code" TEXT NOT NULL,
  "user_code" TEXT NOT NULL,
  "device_name" TEXT NOT NULL,
  "device_platform" TEXT NOT NULL,
  "approved_user_id" INTEGER,
  "created_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  "expires_at" TIMESTAMP NOT NULL,
  CONSTRAINT "device_pairing_approved_user_id_fkey" FOREIGN KEY (approved_user_id) REFERENCES app_user(id) ON DELETE CASCADE,
  CONSTRAINT "device_pairing_pkey" PRIMARY KEY (device_code),
  CONSTRAINT "device_pairing_user_code_key" UNIQUE (user_code)
);

CREATE TABLE IF NOT EXISTS "download_move" (
  "hash" TEXT NOT NULL,
  "name" TEXT NOT NULL DEFAULT '',
  "rule_id" INTEGER,
  "rule_name" TEXT NOT NULL DEFAULT '',
  "status" TEXT NOT NULL,
  "from_path" TEXT NOT NULL DEFAULT '',
  "dest_path" TEXT NOT NULL DEFAULT '',
  "message" TEXT NOT NULL DEFAULT '',
  "scan_pending" BOOLEAN NOT NULL DEFAULT 0,
  "library_id" INTEGER,
  "remove_after" TEXT NOT NULL DEFAULT 'never',
  "remove_ratio" REAL NOT NULL DEFAULT 0,
  "remove_seed_minutes" INTEGER NOT NULL DEFAULT 0,
  "created_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  "updated_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  "moved_at" TIMESTAMP,
  CONSTRAINT "download_move_library_id_fkey" FOREIGN KEY (library_id) REFERENCES library(id) ON DELETE SET NULL,
  CONSTRAINT "download_move_pkey" PRIMARY KEY (hash),
  CONSTRAINT "download_move_rule_id_fkey" FOREIGN KEY (rule_id) REFERENCES download_rule(id) ON DELETE SET NULL,
  CONSTRAINT "download_move_status_check" CHECK ((status IN ('MOVING', 'MOVED', 'FAILED', 'SKIPPED', 'REMOVED')))
);

CREATE TABLE IF NOT EXISTS "download_rule" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "name" TEXT NOT NULL,
  "position" INTEGER NOT NULL DEFAULT 0,
  "enabled" BOOLEAN NOT NULL DEFAULT 1,
  "match_category" TEXT NOT NULL DEFAULT '',
  "match_tag" TEXT NOT NULL DEFAULT '',
  "match_tracker" TEXT NOT NULL DEFAULT '',
  "match_name" TEXT NOT NULL DEFAULT '',
  "min_size_mb" INTEGER NOT NULL DEFAULT 0,
  "video_only" BOOLEAN NOT NULL DEFAULT 0,
  "dest_storage_location_id" INTEGER NOT NULL,
  "dest_path" TEXT NOT NULL DEFAULT '',
  "subfolder" TEXT NOT NULL DEFAULT '',
  "scan_after" BOOLEAN NOT NULL DEFAULT 1,
  "remove_after" TEXT NOT NULL DEFAULT 'never',
  "remove_ratio" REAL NOT NULL DEFAULT 0,
  "remove_seed_minutes" INTEGER NOT NULL DEFAULT 0,
  "created_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  "updated_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT "download_rule_dest_storage_location_id_fkey" FOREIGN KEY (dest_storage_location_id) REFERENCES storage_location(id) ON DELETE CASCADE,
  CONSTRAINT "download_rule_remove_after_check" CHECK ((remove_after IN ('never', 'moved', 'seeded')))
);

CREATE TABLE IF NOT EXISTS "duplicate_group" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "kind" TEXT NOT NULL,
  "confidence" REAL,
  "dismissed" BOOLEAN NOT NULL DEFAULT 0,
  "created_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT "duplicate_group_confidence_check" CHECK (((confidence >= (0)) AND (confidence <= (1)))),
  CONSTRAINT "duplicate_group_kind_check" CHECK ((kind IN ('EXACT', 'PROBABLE', 'VERSION')))
);

CREATE TABLE IF NOT EXISTS "duplicate_group_file" (
  "duplicate_group_id" INTEGER NOT NULL,
  "media_file_id" INTEGER NOT NULL,
  CONSTRAINT "duplicate_group_file_duplicate_group_id_fkey" FOREIGN KEY (duplicate_group_id) REFERENCES duplicate_group(id) ON DELETE CASCADE,
  CONSTRAINT "duplicate_group_file_media_file_id_fkey" FOREIGN KEY (media_file_id) REFERENCES media_file(id) ON DELETE CASCADE,
  CONSTRAINT "duplicate_group_file_pkey" PRIMARY KEY (duplicate_group_id, media_file_id)
);

CREATE TABLE IF NOT EXISTS "library" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "name" TEXT NOT NULL,
  "mode" TEXT NOT NULL DEFAULT 'entertainment',
  "default_access" BOOLEAN NOT NULL DEFAULT 0,
  "position" INTEGER NOT NULL DEFAULT 0,
  "created_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  "updated_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  "watch_enabled" BOOLEAN NOT NULL DEFAULT 0,
  CONSTRAINT "library_mode_check" CHECK ((mode IN ('entertainment', 'private'))),
  CONSTRAINT "library_name_check" CHECK ((name <> '')),
  CONSTRAINT "library_name_key" UNIQUE (name)
);

CREATE TABLE IF NOT EXISTS "library_access" (
  "user_id" INTEGER NOT NULL,
  "library_id" INTEGER NOT NULL,
  CONSTRAINT "library_access_library_id_fkey" FOREIGN KEY (library_id) REFERENCES library(id) ON DELETE CASCADE,
  CONSTRAINT "library_access_pkey" PRIMARY KEY (user_id, library_id),
  CONSTRAINT "library_access_user_id_fkey" FOREIGN KEY (user_id) REFERENCES app_user(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS "library_provider" (
  "library_id" INTEGER NOT NULL,
  "provider" TEXT NOT NULL,
  "position" INTEGER NOT NULL DEFAULT 0,
  CONSTRAINT "library_provider_library_id_fkey" FOREIGN KEY (library_id) REFERENCES library(id) ON DELETE CASCADE,
  CONSTRAINT "library_provider_pkey" PRIMARY KEY (library_id, provider)
);

CREATE TABLE IF NOT EXISTS "library_root" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "library_id" INTEGER NOT NULL,
  "storage_location_id" INTEGER NOT NULL,
  "path" TEXT NOT NULL DEFAULT '',
  CONSTRAINT "library_root_library_id_fkey" FOREIGN KEY (library_id) REFERENCES library(id) ON DELETE CASCADE,
  CONSTRAINT "library_root_path_check" CHECK (((path NOT LIKE '/%') AND (path NOT LIKE '%/'))),
  CONSTRAINT "library_root_storage_location_id_fkey" FOREIGN KEY (storage_location_id) REFERENCES storage_location(id) ON DELETE CASCADE,
  CONSTRAINT "library_root_storage_location_id_path_key" UNIQUE (storage_location_id, path)
);

CREATE TABLE IF NOT EXISTS "match_link" (
  "kind" TEXT NOT NULL,
  "entity_id" INTEGER NOT NULL,
  "provider" TEXT NOT NULL,
  "external_id" TEXT NOT NULL,
  "matched_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  "remote_metadata" TEXT,
  CONSTRAINT "match_link_kind_check" CHECK ((kind IN ('media', 'performer', 'studio', 'series'))),
  CONSTRAINT "match_link_pkey" PRIMARY KEY (kind, entity_id, provider)
);

CREATE TABLE IF NOT EXISTS "media_file" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "media_item_id" INTEGER NOT NULL,
  "storage_location_id" INTEGER NOT NULL,
  "relative_path" TEXT NOT NULL,
  "file_name" TEXT NOT NULL,
  "extension" TEXT NOT NULL,
  "file_size" INTEGER NOT NULL,
  "content_hash" TEXT,
  "partial_hash" TEXT,
  "media_fingerprint" TEXT,
  "duration_seconds" REAL,
  "width" INTEGER,
  "height" INTEGER,
  "frame_rate" REAL,
  "video_codec" TEXT,
  "audio_codec" TEXT,
  "container" TEXT,
  "video_bitrate" INTEGER,
  "audio_bitrate" INTEGER,
  "overall_bitrate" INTEGER,
  "created_time" TIMESTAMP,
  "modified_time" TIMESTAMP,
  "discovered_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  "last_scanned_at" TIMESTAMP,
  "available" BOOLEAN NOT NULL DEFAULT 1,
  "missing_since" TIMESTAMP,
  "created_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  "updated_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  "nfo_uid" TEXT,
  "original_file_name" TEXT,
  "library_id" INTEGER,
  "tracks_probed_at" TIMESTAMP,
  "mp4_data_boxes" INTEGER,
  "sidecar_subtitles" INTEGER,
  "rotation" INTEGER,
  "portrait" BOOLEAN GENERATED ALWAYS AS (
CASE
    WHEN (rotation IN (90, 270)) THEN (width > height)
    ELSE (height > width)
END) STORED,
  CONSTRAINT "media_file_file_size_check" CHECK ((file_size >= 0)),
  CONSTRAINT "media_file_library_id_fkey" FOREIGN KEY (library_id) REFERENCES library(id) ON DELETE SET NULL,
  CONSTRAINT "media_file_media_item_id_fkey" FOREIGN KEY (media_item_id) REFERENCES media_item(id) ON DELETE RESTRICT,
  CONSTRAINT "media_file_relative_path_check" CHECK (((relative_path <> '') AND (relative_path NOT LIKE '/%'))),
  CONSTRAINT "media_file_storage_location_id_fkey" FOREIGN KEY (storage_location_id) REFERENCES storage_location(id) ON DELETE RESTRICT,
  CONSTRAINT "media_file_storage_location_id_relative_path_key" UNIQUE (storage_location_id, relative_path)
);

CREATE TABLE IF NOT EXISTS "media_item" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "title" TEXT NOT NULL,
  "original_title" TEXT,
  "sort_title" TEXT,
  "studio_id" INTEGER,
  "series_id" INTEGER,
  "description" TEXT,
  "release_date" DATE,
  "production_year" INTEGER,
  "duration_seconds" REAL,
  "favorite" BOOLEAN NOT NULL DEFAULT 0,
  "rating" INTEGER,
  "notes" TEXT,
  "watched" BOOLEAN NOT NULL DEFAULT 0,
  "watch_count" INTEGER NOT NULL DEFAULT 0,
  "last_watched_at" TIMESTAMP,
  "date_added" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  "poster_image_id" INTEGER,
  "backdrop_image_id" INTEGER,
  "metadata_source" TEXT NOT NULL DEFAULT 'FILENAME',
  "protected_fields" TEXT NOT NULL DEFAULT '[]',
  "created_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  "updated_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  "match_status" TEXT NOT NULL DEFAULT 'unmatched',
  "rating_count" INTEGER NOT NULL DEFAULT 0,
  "uid" TEXT NOT NULL DEFAULT (lower(hex(randomblob(4)) || '-' || hex(randomblob(2)) || '-4' || substr(hex(randomblob(2)),2) || '-8' || substr(hex(randomblob(2)),2) || '-' || hex(randomblob(6)))),
  "series_position" INTEGER,
  "library_id" INTEGER,
  "kind" TEXT,
  "season_number" INTEGER,
  "episode_number" INTEGER,
  "reel_override" BOOLEAN,
  CONSTRAINT "media_item_backdrop_image_id_fkey" FOREIGN KEY (backdrop_image_id) REFERENCES artwork(id) ON DELETE SET NULL,
  CONSTRAINT "media_item_episode_number_check" CHECK ((episode_number >= 0)),
  CONSTRAINT "media_item_kind_check" CHECK ((kind IN ('movie', 'episode'))),
  CONSTRAINT "media_item_library_id_fkey" FOREIGN KEY (library_id) REFERENCES library(id) ON DELETE SET NULL,
  CONSTRAINT "media_item_match_status_check" CHECK ((match_status IN ('unmatched', 'matched', 'rejected'))),
  CONSTRAINT "media_item_metadata_source_check" CHECK ((metadata_source IN ('MANUAL', 'NFO', 'FILENAME', 'FOLDER', 'INFERRED'))),
  CONSTRAINT "media_item_poster_image_id_fkey" FOREIGN KEY (poster_image_id) REFERENCES artwork(id) ON DELETE SET NULL,
  CONSTRAINT "media_item_rating_check" CHECK (((rating >= 0) AND (rating <= 10))),
  CONSTRAINT "media_item_season_number_check" CHECK ((season_number >= 0)),
  CONSTRAINT "media_item_series_id_fkey" FOREIGN KEY (series_id) REFERENCES series(id) ON DELETE SET NULL,
  CONSTRAINT "media_item_series_position_check" CHECK ((series_position >= 0)),
  CONSTRAINT "media_item_studio_id_fkey" FOREIGN KEY (studio_id) REFERENCES studio(id) ON DELETE SET NULL,
  CONSTRAINT "media_item_watch_count_check" CHECK ((watch_count >= 0))
);

CREATE TABLE IF NOT EXISTS "media_item_category" (
  "media_item_id" INTEGER NOT NULL,
  "category_id" INTEGER NOT NULL,
  CONSTRAINT "media_item_category_category_id_fkey" FOREIGN KEY (category_id) REFERENCES category(id) ON DELETE CASCADE,
  CONSTRAINT "media_item_category_media_item_id_fkey" FOREIGN KEY (media_item_id) REFERENCES media_item(id) ON DELETE CASCADE,
  CONSTRAINT "media_item_category_pkey" PRIMARY KEY (media_item_id, category_id)
);

CREATE TABLE IF NOT EXISTS "media_item_crew" (
  "media_item_id" INTEGER NOT NULL,
  "performer_id" INTEGER NOT NULL,
  "job" TEXT NOT NULL,
  "position" INTEGER NOT NULL DEFAULT 0,
  CONSTRAINT "media_item_crew_job_check" CHECK ((job IN ('director', 'writer'))),
  CONSTRAINT "media_item_crew_media_item_id_fkey" FOREIGN KEY (media_item_id) REFERENCES media_item(id) ON DELETE CASCADE,
  CONSTRAINT "media_item_crew_performer_id_fkey" FOREIGN KEY (performer_id) REFERENCES performer(id) ON DELETE CASCADE,
  CONSTRAINT "media_item_crew_pkey" PRIMARY KEY (media_item_id, performer_id, job)
);

CREATE TABLE IF NOT EXISTS "media_item_favorite" (
  "user_id" INTEGER NOT NULL,
  "media_item_id" INTEGER NOT NULL,
  "created_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT "media_item_favorite_media_item_id_fkey" FOREIGN KEY (media_item_id) REFERENCES media_item(id) ON DELETE CASCADE,
  CONSTRAINT "media_item_favorite_pkey" PRIMARY KEY (user_id, media_item_id),
  CONSTRAINT "media_item_favorite_user_id_fkey" FOREIGN KEY (user_id) REFERENCES app_user(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS "media_item_performer" (
  "media_item_id" INTEGER NOT NULL,
  "performer_id" INTEGER NOT NULL,
  "position" INTEGER NOT NULL DEFAULT 0,
  "character" TEXT,
  CONSTRAINT "media_item_performer_media_item_id_fkey" FOREIGN KEY (media_item_id) REFERENCES media_item(id) ON DELETE CASCADE,
  CONSTRAINT "media_item_performer_performer_id_fkey" FOREIGN KEY (performer_id) REFERENCES performer(id) ON DELETE CASCADE,
  CONSTRAINT "media_item_performer_pkey" PRIMARY KEY (media_item_id, performer_id)
);

CREATE TABLE IF NOT EXISTS "media_item_rating" (
  "user_id" INTEGER NOT NULL,
  "media_item_id" INTEGER NOT NULL,
  "rating" INTEGER NOT NULL,
  "updated_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT "media_item_rating_media_item_id_fkey" FOREIGN KEY (media_item_id) REFERENCES media_item(id) ON DELETE CASCADE,
  CONSTRAINT "media_item_rating_pkey" PRIMARY KEY (user_id, media_item_id),
  CONSTRAINT "media_item_rating_rating_check" CHECK (((rating >= 1) AND (rating <= 10))),
  CONSTRAINT "media_item_rating_user_id_fkey" FOREIGN KEY (user_id) REFERENCES app_user(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS "media_item_tag" (
  "media_item_id" INTEGER NOT NULL,
  "tag_id" INTEGER NOT NULL,
  CONSTRAINT "media_item_tag_media_item_id_fkey" FOREIGN KEY (media_item_id) REFERENCES media_item(id) ON DELETE CASCADE,
  CONSTRAINT "media_item_tag_pkey" PRIMARY KEY (media_item_id, tag_id),
  CONSTRAINT "media_item_tag_tag_id_fkey" FOREIGN KEY (tag_id) REFERENCES tag(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS "media_marker" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "media_item_id" INTEGER NOT NULL,
  "user_id" INTEGER NOT NULL,
  "seconds" REAL NOT NULL,
  "title" TEXT NOT NULL,
  "created_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT "media_marker_media_item_id_fkey" FOREIGN KEY (media_item_id) REFERENCES media_item(id) ON DELETE CASCADE,
  CONSTRAINT "media_marker_seconds_check" CHECK ((seconds >= (0))),
  CONSTRAINT "media_marker_user_id_fkey" FOREIGN KEY (user_id) REFERENCES app_user(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS "media_match_suggestion" (
  "media_item_id" INTEGER NOT NULL,
  "candidates" TEXT NOT NULL,
  "updated_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT "media_match_suggestion_media_item_id_fkey" FOREIGN KEY (media_item_id) REFERENCES media_item(id) ON DELETE CASCADE,
  CONSTRAINT "media_match_suggestion_pkey" PRIMARY KEY (media_item_id)
);

CREATE TABLE IF NOT EXISTS "media_rejected_match" (
  "media_item_id" INTEGER NOT NULL,
  "scene_uuid" TEXT NOT NULL,
  "created_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  "provider" TEXT NOT NULL,
  CONSTRAINT "media_rejected_match_media_item_id_fkey" FOREIGN KEY (media_item_id) REFERENCES media_item(id) ON DELETE CASCADE,
  CONSTRAINT "media_rejected_match_pkey" PRIMARY KEY (media_item_id, provider, scene_uuid)
);

CREATE TABLE IF NOT EXISTS "media_track" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "media_file_id" INTEGER NOT NULL,
  "kind" TEXT NOT NULL,
  "kind_index" INTEGER NOT NULL,
  "stream_index" INTEGER NOT NULL,
  "codec" TEXT,
  "language" TEXT,
  "title" TEXT,
  "channels" INTEGER,
  "is_default" BOOLEAN NOT NULL DEFAULT 0,
  "is_forced" BOOLEAN NOT NULL DEFAULT 0,
  CONSTRAINT "media_track_kind_check" CHECK ((kind IN ('audio', 'subtitle'))),
  CONSTRAINT "media_track_kind_index_check" CHECK ((kind_index >= 0)),
  CONSTRAINT "media_track_media_file_id_fkey" FOREIGN KEY (media_file_id) REFERENCES media_file(id) ON DELETE CASCADE,
  CONSTRAINT "media_track_media_file_id_kind_kind_index_key" UNIQUE (media_file_id, kind, kind_index)
);

CREATE TABLE IF NOT EXISTS "nfo_metadata" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "media_file_id" INTEGER NOT NULL,
  "relative_path" TEXT NOT NULL,
  "parsed_metadata" TEXT NOT NULL DEFAULT '{}',
  "content_hash" TEXT,
  "imported_at" TIMESTAMP,
  "updated_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT "nfo_metadata_media_file_id_fkey" FOREIGN KEY (media_file_id) REFERENCES media_file(id) ON DELETE CASCADE,
  CONSTRAINT "nfo_metadata_media_file_id_key" UNIQUE (media_file_id)
);

CREATE TABLE IF NOT EXISTS "notification" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "kind" TEXT NOT NULL,
  "title" TEXT NOT NULL,
  "body" TEXT,
  "link" TEXT,
  "admin_only" BOOLEAN NOT NULL DEFAULT 1,
  "created_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS "notification_state" (
  "user_id" INTEGER NOT NULL,
  "last_read_id" INTEGER NOT NULL DEFAULT 0,
  CONSTRAINT "notification_state_pkey" PRIMARY KEY (user_id),
  CONSTRAINT "notification_state_user_id_fkey" FOREIGN KEY (user_id) REFERENCES app_user(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS "organizer_keyword" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "name" TEXT NOT NULL,
  "keywords" TEXT NOT NULL DEFAULT '[]',
  "target_dir" TEXT NOT NULL,
  "priority" INTEGER NOT NULL DEFAULT 0,
  "created_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  "updated_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT "organizer_keyword_name_key" UNIQUE (name)
);

CREATE TABLE IF NOT EXISTS "organizer_rule" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "studio_id" INTEGER,
  "performer_id" INTEGER,
  "folder_name" TEXT,
  "enabled" BOOLEAN NOT NULL DEFAULT 1,
  "position" INTEGER NOT NULL DEFAULT 0,
  "created_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  "updated_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  "library_id" INTEGER NOT NULL,
  CONSTRAINT "organizer_rule_check" CHECK (((studio_id IS NULL) <> (performer_id IS NULL))),
  CONSTRAINT "organizer_rule_library_id_fkey" FOREIGN KEY (library_id) REFERENCES library(id) ON DELETE CASCADE,
  CONSTRAINT "organizer_rule_performer_id_fkey" FOREIGN KEY (performer_id) REFERENCES performer(id) ON DELETE CASCADE,
  CONSTRAINT "organizer_rule_studio_id_fkey" FOREIGN KEY (studio_id) REFERENCES studio(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS "performer" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "name" TEXT NOT NULL,
  "display_name" TEXT,
  "profile_image_id" INTEGER,
  "notes" TEXT,
  "favorite" BOOLEAN NOT NULL DEFAULT 0,
  "created_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  "updated_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  "match_status" TEXT NOT NULL DEFAULT 'unmatched',
  "remote_metadata" TEXT,
  "matched_at" TIMESTAMP,
  "photo_chosen" BOOLEAN NOT NULL DEFAULT 0,
  "field_sources" TEXT NOT NULL DEFAULT '{}',
  CONSTRAINT "performer_match_status_check" CHECK ((match_status IN ('unmatched', 'matched', 'no_match', 'ambiguous', 'rejected'))),
  CONSTRAINT "performer_name_key" UNIQUE (name),
  CONSTRAINT "performer_profile_image_id_fkey" FOREIGN KEY (profile_image_id) REFERENCES artwork(id) ON DELETE SET NULL
);

CREATE TABLE IF NOT EXISTS "performer_alias" (
  "performer_id" INTEGER NOT NULL,
  "alias" TEXT NOT NULL,
  CONSTRAINT "performer_alias_performer_id_fkey" FOREIGN KEY (performer_id) REFERENCES performer(id) ON DELETE CASCADE,
  CONSTRAINT "performer_alias_pkey" PRIMARY KEY (performer_id, alias)
);

CREATE TABLE IF NOT EXISTS "performer_distinct" (
  "a_id" INTEGER NOT NULL,
  "b_id" INTEGER NOT NULL,
  CONSTRAINT "performer_distinct_a_id_fkey" FOREIGN KEY (a_id) REFERENCES performer(id) ON DELETE CASCADE,
  CONSTRAINT "performer_distinct_b_id_fkey" FOREIGN KEY (b_id) REFERENCES performer(id) ON DELETE CASCADE,
  CONSTRAINT "performer_distinct_check" CHECK ((a_id < b_id)),
  CONSTRAINT "performer_distinct_pkey" PRIMARY KEY (a_id, b_id)
);

CREATE TABLE IF NOT EXISTS "performer_favorite" (
  "user_id" INTEGER NOT NULL,
  "performer_id" INTEGER NOT NULL,
  "created_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT "performer_favorite_performer_id_fkey" FOREIGN KEY (performer_id) REFERENCES performer(id) ON DELETE CASCADE,
  CONSTRAINT "performer_favorite_pkey" PRIMARY KEY (user_id, performer_id),
  CONSTRAINT "performer_favorite_user_id_fkey" FOREIGN KEY (user_id) REFERENCES app_user(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS "performer_notify" (
  "user_id" INTEGER NOT NULL,
  "performer_id" INTEGER NOT NULL,
  "created_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT "performer_notify_performer_id_fkey" FOREIGN KEY (performer_id) REFERENCES performer(id) ON DELETE CASCADE,
  CONSTRAINT "performer_notify_pkey" PRIMARY KEY (user_id, performer_id),
  CONSTRAINT "performer_notify_user_id_fkey" FOREIGN KEY (user_id) REFERENCES app_user(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS "performer_photo" (
  "performer_id" INTEGER NOT NULL,
  "artwork_id" INTEGER NOT NULL,
  "source" TEXT NOT NULL,
  "source_url" TEXT,
  "created_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT "performer_photo_artwork_id_fkey" FOREIGN KEY (artwork_id) REFERENCES artwork(id) ON DELETE CASCADE,
  CONSTRAINT "performer_photo_performer_id_fkey" FOREIGN KEY (performer_id) REFERENCES performer(id) ON DELETE CASCADE,
  CONSTRAINT "performer_photo_pkey" PRIMARY KEY (performer_id, artwork_id)
);

CREATE TABLE IF NOT EXISTS "performer_rejected_match" (
  "performer_id" INTEGER NOT NULL,
  "external_id" TEXT NOT NULL,
  "created_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  "provider" TEXT NOT NULL,
  CONSTRAINT "performer_rejected_match_performer_id_fkey" FOREIGN KEY (performer_id) REFERENCES performer(id) ON DELETE CASCADE,
  CONSTRAINT "performer_rejected_match_pkey" PRIMARY KEY (performer_id, provider, external_id)
);

CREATE TABLE IF NOT EXISTS "playback_state" (
  "user_id" INTEGER NOT NULL,
  "media_item_id" INTEGER NOT NULL,
  "position_seconds" REAL NOT NULL DEFAULT 0,
  "completed" BOOLEAN NOT NULL DEFAULT 0,
  "updated_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT "playback_state_media_item_id_fkey" FOREIGN KEY (media_item_id) REFERENCES media_item(id) ON DELETE CASCADE,
  CONSTRAINT "playback_state_pkey" PRIMARY KEY (user_id, media_item_id),
  CONSTRAINT "playback_state_user_id_fkey" FOREIGN KEY (user_id) REFERENCES app_user(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS "preview_clip" (
  "media_file_id" INTEGER NOT NULL,
  "storage_key" TEXT NOT NULL,
  "duration_seconds" REAL,
  "created_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT "preview_clip_media_file_id_fkey" FOREIGN KEY (media_file_id) REFERENCES media_file(id) ON DELETE CASCADE,
  CONSTRAINT "preview_clip_pkey" PRIMARY KEY (media_file_id),
  CONSTRAINT "preview_clip_storage_key_key" UNIQUE (storage_key)
);

CREATE TABLE IF NOT EXISTS "protected_path" (
  "path" TEXT NOT NULL,
  "owner" TEXT NOT NULL DEFAULT 'downloads',
  "created_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT "protected_path_pkey" PRIMARY KEY (path)
);

CREATE TABLE IF NOT EXISTS "saved_filter" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "user_id" INTEGER NOT NULL,
  "name" TEXT NOT NULL,
  "rules" TEXT NOT NULL,
  "created_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT "saved_filter_user_id_fkey" FOREIGN KEY (user_id) REFERENCES app_user(id) ON DELETE CASCADE,
  CONSTRAINT "saved_filter_user_id_name_key" UNIQUE (user_id, name)
);

CREATE TABLE IF NOT EXISTS "scan_job" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "storage_location_id" INTEGER NOT NULL,
  "background_job_id" INTEGER,
  "discovered_count" INTEGER NOT NULL DEFAULT 0,
  "changed_count" INTEGER NOT NULL DEFAULT 0,
  "missing_count" INTEGER NOT NULL DEFAULT 0,
  "started_at" TIMESTAMP,
  "completed_at" TIMESTAMP,
  "files_seen" INTEGER NOT NULL DEFAULT 0,
  "files_processed" INTEGER NOT NULL DEFAULT 0,
  "current_path" TEXT,
  "recent_paths" TEXT NOT NULL DEFAULT '[]',
  "phase" TEXT NOT NULL DEFAULT 'queued',
  "walk_complete" BOOLEAN NOT NULL DEFAULT 0,
  "library_id" INTEGER,
  CONSTRAINT "scan_job_background_job_id_fkey" FOREIGN KEY (background_job_id) REFERENCES background_job(id) ON DELETE SET NULL,
  CONSTRAINT "scan_job_library_id_fkey" FOREIGN KEY (library_id) REFERENCES library(id) ON DELETE SET NULL,
  CONSTRAINT "scan_job_storage_location_id_fkey" FOREIGN KEY (storage_location_id) REFERENCES storage_location(id) ON DELETE RESTRICT
);

CREATE TABLE IF NOT EXISTS "scan_rule" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "pattern" TEXT NOT NULL,
  "created_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  "library_id" INTEGER,
  CONSTRAINT "scan_rule_library_id_fkey" FOREIGN KEY (library_id) REFERENCES library(id) ON DELETE CASCADE,
  CONSTRAINT "scan_rule_pattern_check" CHECK ((pattern <> ''))
);

CREATE TABLE IF NOT EXISTS "scan_schedule" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "job_kind" TEXT NOT NULL DEFAULT 'SCAN',
  "cron_expr" TEXT NOT NULL,
  "enabled" BOOLEAN NOT NULL DEFAULT 1,
  "last_run_at" TIMESTAMP,
  "created_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  "library_id" INTEGER,
  CONSTRAINT "scan_schedule_job_kind_check" CHECK ((job_kind IN ('SCAN', 'DUPLICATE_SCAN', 'HOUSEKEEPING'))),
  CONSTRAINT "scan_schedule_library_id_fkey" FOREIGN KEY (library_id) REFERENCES library(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS "scrub_sprite" (
  "media_file_id" INTEGER NOT NULL,
  "storage_key" TEXT NOT NULL,
  "interval_seconds" REAL NOT NULL,
  "tile_width" INTEGER NOT NULL,
  "tile_height" INTEGER NOT NULL,
  "columns" INTEGER NOT NULL,
  "tile_count" INTEGER NOT NULL,
  "created_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT "scrub_sprite_media_file_id_fkey" FOREIGN KEY (media_file_id) REFERENCES media_file(id) ON DELETE CASCADE,
  CONSTRAINT "scrub_sprite_pkey" PRIMARY KEY (media_file_id),
  CONSTRAINT "scrub_sprite_storage_key_key" UNIQUE (storage_key)
);

CREATE TABLE IF NOT EXISTS "series" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "studio_id" INTEGER,
  "name" TEXT NOT NULL,
  "description" TEXT,
  "poster_image_id" INTEGER,
  "created_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  "updated_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  "release_date" DATE,
  "kind" TEXT,
  "backdrop_image_id" INTEGER,
  CONSTRAINT "series_backdrop_image_id_fkey" FOREIGN KEY (backdrop_image_id) REFERENCES artwork(id) ON DELETE SET NULL,
  CONSTRAINT "series_kind_check" CHECK ((kind IN ('show', 'collection'))),
  CONSTRAINT "series_poster_image_id_fkey" FOREIGN KEY (poster_image_id) REFERENCES artwork(id) ON DELETE SET NULL,
  CONSTRAINT "series_studio_id_fkey" FOREIGN KEY (studio_id) REFERENCES studio(id) ON DELETE SET NULL,
  CONSTRAINT "series_studio_id_name_key" UNIQUE (studio_id, name)
);

CREATE TABLE IF NOT EXISTS "storage_capacity_sample" (
  "storage_location_id" INTEGER NOT NULL,
  "sampled_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  "total_bytes" INTEGER NOT NULL,
  "used_bytes" INTEGER NOT NULL,
  "available_bytes" INTEGER NOT NULL,
  CONSTRAINT "storage_capacity_sample_pkey" PRIMARY KEY (storage_location_id, sampled_at),
  CONSTRAINT "storage_capacity_sample_storage_location_id_fkey" FOREIGN KEY (storage_location_id) REFERENCES storage_location(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS "storage_location" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "name" TEXT NOT NULL,
  "type" TEXT NOT NULL,
  "mount_path" TEXT NOT NULL,
  "total_capacity" INTEGER,
  "used_capacity" INTEGER,
  "available_capacity" INTEGER,
  "online" BOOLEAN NOT NULL DEFAULT 0,
  "last_seen_at" TIMESTAMP,
  "last_scan_at" TIMESTAMP,
  "created_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  "updated_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT "storage_location_available_capacity_check" CHECK ((available_capacity >= 0)),
  CONSTRAINT "storage_location_mount_path_key" UNIQUE (mount_path),
  CONSTRAINT "storage_location_name_key" UNIQUE (name),
  CONSTRAINT "storage_location_total_capacity_check" CHECK ((total_capacity >= 0)),
  CONSTRAINT "storage_location_type_check" CHECK ((type IN ('LOCAL', 'EXTERNAL', 'NAS', 'NETWORK_SHARE', 'ARCHIVE'))),
  CONSTRAINT "storage_location_used_capacity_check" CHECK ((used_capacity >= 0))
);

CREATE TABLE IF NOT EXISTS "stream_daily" (
  "day" DATE NOT NULL,
  "direct_bytes" INTEGER NOT NULL DEFAULT 0,
  "hls_bytes" INTEGER NOT NULL DEFAULT 0,
  "peak_streams" INTEGER NOT NULL DEFAULT 0,
  CONSTRAINT "stream_daily_pkey" PRIMARY KEY (day)
);

CREATE TABLE IF NOT EXISTS "studio" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "name" TEXT NOT NULL,
  "logo_image_id" INTEGER,
  "description" TEXT,
  "website" TEXT,
  "created_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  "updated_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  "match_status" TEXT NOT NULL DEFAULT 'unmatched',
  "remote_metadata" TEXT,
  "matched_at" TIMESTAMP,
  CONSTRAINT "studio_logo_image_id_fkey" FOREIGN KEY (logo_image_id) REFERENCES artwork(id) ON DELETE SET NULL,
  CONSTRAINT "studio_match_status_check" CHECK ((match_status IN ('unmatched', 'matched', 'no_match', 'ambiguous'))),
  CONSTRAINT "studio_name_key" UNIQUE (name)
);

CREATE TABLE IF NOT EXISTS "studio_alias" (
  "studio_id" INTEGER NOT NULL,
  "alias" TEXT NOT NULL,
  CONSTRAINT "studio_alias_pkey" PRIMARY KEY (studio_id, alias),
  CONSTRAINT "studio_alias_studio_id_fkey" FOREIGN KEY (studio_id) REFERENCES studio(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS "tag" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "name" TEXT NOT NULL,
  "description" TEXT,
  "optional_color" TEXT,
  CONSTRAINT "tag_name_key" UNIQUE (name)
);

CREATE TABLE IF NOT EXISTS "thumbnail" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "media_item_id" INTEGER,
  "media_file_id" INTEGER,
  "storage_key" TEXT NOT NULL,
  "timestamp_seconds" REAL,
  "width" INTEGER,
  "height" INTEGER,
  "preferred" BOOLEAN NOT NULL DEFAULT 0,
  "created_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT "thumbnail_check" CHECK (((media_item_id IS NOT NULL) OR (media_file_id IS NOT NULL))),
  CONSTRAINT "thumbnail_media_file_id_fkey" FOREIGN KEY (media_file_id) REFERENCES media_file(id) ON DELETE CASCADE,
  CONSTRAINT "thumbnail_media_item_id_fkey" FOREIGN KEY (media_item_id) REFERENCES media_item(id) ON DELETE CASCADE,
  CONSTRAINT "thumbnail_storage_key_key" UNIQUE (storage_key)
);

CREATE TABLE IF NOT EXISTS "trash_item" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "storage_location_id" INTEGER NOT NULL,
  "original_path" TEXT NOT NULL,
  "trash_path" TEXT NOT NULL,
  "title" TEXT NOT NULL DEFAULT '',
  "file_size" INTEGER NOT NULL DEFAULT 0,
  "created_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT "trash_item_storage_location_id_fkey" FOREIGN KEY (storage_location_id) REFERENCES storage_location(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS "watch_later" (
  "user_id" INTEGER NOT NULL,
  "media_item_id" INTEGER NOT NULL,
  "added_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT "watch_later_media_item_id_fkey" FOREIGN KEY (media_item_id) REFERENCES media_item(id) ON DELETE CASCADE,
  CONSTRAINT "watch_later_pkey" PRIMARY KEY (user_id, media_item_id),
  CONSTRAINT "watch_later_user_id_fkey" FOREIGN KEY (user_id) REFERENCES app_user(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS "watch_session" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "user_id" INTEGER NOT NULL,
  "media_item_id" INTEGER NOT NULL,
  "started_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  "last_activity_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  "watched_seconds" REAL NOT NULL DEFAULT 0,
  "max_position_seconds" REAL NOT NULL DEFAULT 0,
  "duration_seconds" REAL,
  "completed" BOOLEAN NOT NULL DEFAULT 0,
  CONSTRAINT "watch_session_media_item_id_fkey" FOREIGN KEY (media_item_id) REFERENCES media_item(id) ON DELETE CASCADE,
  CONSTRAINT "watch_session_user_id_fkey" FOREIGN KEY (user_id) REFERENCES app_user(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS "web_session" (
  "id" TEXT NOT NULL,
  "user_id" INTEGER NOT NULL,
  "created_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  "expires_at" TIMESTAMP NOT NULL,
  "auth_plugin" TEXT,
  "public_id" INTEGER NOT NULL DEFAULT (abs(random())),
  "device_name" TEXT,
  "device_platform" TEXT,
  "last_seen_at" TIMESTAMP,
  CONSTRAINT "web_session_user_id_fkey" FOREIGN KEY (user_id) REFERENCES app_user(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS "webhook" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "name" TEXT NOT NULL,
  "url" TEXT NOT NULL,
  "secret" TEXT,
  "events" TEXT NOT NULL DEFAULT '[]',
  "enabled" BOOLEAN NOT NULL DEFAULT 1,
  "last_status" INTEGER,
  "last_error" TEXT,
  "last_delivery_at" TIMESTAMP,
  "created_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS "webhook_delivery" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "webhook_id" INTEGER NOT NULL,
  "at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  "kind" TEXT NOT NULL DEFAULT '',
  "status" INTEGER,
  "error" TEXT,
  "duration_ms" INTEGER NOT NULL DEFAULT 0,
  CONSTRAINT "webhook_delivery_webhook_id_fkey" FOREIGN KEY (webhook_id) REFERENCES webhook(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS admin_audit_event_actor ON "admin_audit_event" (actor_id);
CREATE INDEX IF NOT EXISTS admin_audit_event_at ON "admin_audit_event" (at DESC, id DESC);
CREATE INDEX IF NOT EXISTS admin_audit_event_target ON "admin_audit_event" (target_kind, target_id);
CREATE UNIQUE INDEX IF NOT EXISTS app_user_username_key ON "app_user" (lower(username)) WHERE (username IS NOT NULL);
CREATE INDEX IF NOT EXISTS background_job_pending ON "background_job" (created_at) WHERE (status = 'PENDING');
CREATE INDEX IF NOT EXISTS background_job_status_id ON "background_job" (status, id DESC);
CREATE INDEX IF NOT EXISTS category_parent ON "category" (parent_id) WHERE (parent_id IS NOT NULL);
CREATE INDEX IF NOT EXISTS collection_media_item_reverse ON "collection_media_item" (media_item_id);
CREATE UNIQUE INDEX IF NOT EXISTS collection_user_library_name ON "collection" (user_id, library_id, name) WHERE (library_id IS NOT NULL);
CREATE UNIQUE INDEX IF NOT EXISTS collection_user_unassigned_name ON "collection" (user_id, name) WHERE (library_id IS NULL);
CREATE INDEX IF NOT EXISTS device_pairing_expires ON "device_pairing" (expires_at);
CREATE INDEX IF NOT EXISTS download_move_status_idx ON "download_move" (status, updated_at DESC);
CREATE INDEX IF NOT EXISTS duplicate_group_file_media_file ON "duplicate_group_file" (media_file_id);
CREATE INDEX IF NOT EXISTS idx_media_file_portrait ON "media_file" (media_item_id) WHERE (portrait AND available);
CREATE INDEX IF NOT EXISTS idx_media_item_rating_item ON "media_item_rating" (media_item_id);
CREATE INDEX IF NOT EXISTS library_access_library ON "library_access" (library_id);
CREATE INDEX IF NOT EXISTS match_link_external ON "match_link" (provider, external_id);
CREATE UNIQUE INDEX IF NOT EXISTS match_link_person_studio_unique ON "match_link" (kind, provider, external_id) WHERE (kind IN ('performer', 'studio'));
CREATE INDEX IF NOT EXISTS media_file_content_hash ON "media_file" (content_hash) WHERE (content_hash IS NOT NULL);
CREATE INDEX IF NOT EXISTS media_file_item ON "media_file" (media_item_id);
CREATE INDEX IF NOT EXISTS media_file_library ON "media_file" (library_id);
CREATE INDEX IF NOT EXISTS media_file_partial_hash ON "media_file" (partial_hash, file_size) WHERE (partial_hash IS NOT NULL);
CREATE INDEX IF NOT EXISTS media_item_backdrop ON "media_item" (backdrop_image_id) WHERE (backdrop_image_id IS NOT NULL);
CREATE INDEX IF NOT EXISTS media_item_category_reverse ON "media_item_category" (category_id, media_item_id);
CREATE INDEX IF NOT EXISTS media_item_crew_person ON "media_item_crew" (performer_id, job);
CREATE INDEX IF NOT EXISTS media_item_episode_order ON "media_item" (series_id, season_number, episode_number) WHERE (series_id IS NOT NULL);
CREATE INDEX IF NOT EXISTS media_item_favorite_item ON "media_item_favorite" (media_item_id);
CREATE INDEX IF NOT EXISTS media_item_kind ON "media_item" (library_id, kind);
CREATE INDEX IF NOT EXISTS media_item_library ON "media_item" (library_id);
CREATE INDEX IF NOT EXISTS media_item_library_added ON "media_item" (library_id, date_added DESC);
CREATE INDEX IF NOT EXISTS media_item_performer_reverse ON "media_item_performer" (performer_id, media_item_id);
CREATE INDEX IF NOT EXISTS media_item_poster ON "media_item" (poster_image_id) WHERE (poster_image_id IS NOT NULL);
CREATE INDEX IF NOT EXISTS media_item_series_position_idx ON "media_item" (series_id, series_position) WHERE (series_id IS NOT NULL);
CREATE INDEX IF NOT EXISTS media_item_studio ON "media_item" (studio_id) WHERE (studio_id IS NOT NULL);
CREATE INDEX IF NOT EXISTS media_item_tag_reverse ON "media_item_tag" (tag_id, media_item_id);
CREATE UNIQUE INDEX IF NOT EXISTS media_item_uid ON "media_item" (uid);
CREATE INDEX IF NOT EXISTS media_marker_item_user ON "media_marker" (media_item_id, user_id, seconds);
CREATE UNIQUE INDEX IF NOT EXISTS organizer_rule_library_performer ON "organizer_rule" (library_id, performer_id);
CREATE INDEX IF NOT EXISTS organizer_rule_library_position_idx ON "organizer_rule" (library_id, "position");
CREATE UNIQUE INDEX IF NOT EXISTS organizer_rule_library_studio ON "organizer_rule" (library_id, studio_id);
CREATE INDEX IF NOT EXISTS performer_favorite_performer ON "performer_favorite" (performer_id);
CREATE UNIQUE INDEX IF NOT EXISTS performer_photo_source_url ON "performer_photo" (performer_id, source_url) WHERE (source_url IS NOT NULL);
CREATE INDEX IF NOT EXISTS performer_profile_image ON "performer" (profile_image_id) WHERE (profile_image_id IS NOT NULL);
CREATE INDEX IF NOT EXISTS scan_job_library ON "scan_job" (library_id, completed_at);
CREATE UNIQUE INDEX IF NOT EXISTS scan_rule_library_unique ON "scan_rule" (COALESCE(library_id, (0)), pattern);
CREATE INDEX IF NOT EXISTS watch_later_user_added ON "watch_later" (user_id, added_at DESC);
CREATE INDEX IF NOT EXISTS watch_session_item ON "watch_session" (media_item_id);
CREATE INDEX IF NOT EXISTS watch_session_user_item ON "watch_session" (user_id, media_item_id);
CREATE INDEX IF NOT EXISTS watch_session_user_started ON "watch_session" (user_id, started_at DESC);
CREATE INDEX IF NOT EXISTS web_session_expires ON "web_session" (expires_at);
CREATE UNIQUE INDEX IF NOT EXISTS web_session_public_id ON "web_session" (public_id);
CREATE INDEX IF NOT EXISTS webhook_delivery_hook ON "webhook_delivery" (webhook_id, id DESC);

-- PostgreSQL uses trigger functions for these relationships. SQLite keeps the
-- same behavior with native triggers.
CREATE TRIGGER IF NOT EXISTS app_user_default_libraries AFTER INSERT ON app_user
BEGIN
  INSERT OR IGNORE INTO library_access (user_id, library_id)
  SELECT NEW.id, id FROM library WHERE default_access;
END;

CREATE TRIGGER IF NOT EXISTS media_item_match_link_cleanup AFTER DELETE ON media_item
BEGIN DELETE FROM match_link WHERE kind = 'media' AND entity_id = OLD.id; END;
CREATE TRIGGER IF NOT EXISTS performer_match_link_cleanup AFTER DELETE ON performer
BEGIN DELETE FROM match_link WHERE kind = 'performer' AND entity_id = OLD.id; END;
CREATE TRIGGER IF NOT EXISTS studio_match_link_cleanup AFTER DELETE ON studio
BEGIN DELETE FROM match_link WHERE kind = 'studio' AND entity_id = OLD.id; END;
CREATE TRIGGER IF NOT EXISTS series_match_link_cleanup AFTER DELETE ON series
BEGIN DELETE FROM match_link WHERE kind = 'series' AND entity_id = OLD.id; END;

CREATE TRIGGER IF NOT EXISTS media_file_library_assign_insert AFTER INSERT ON media_file
BEGIN
  UPDATE media_file SET library_id = (
    SELECT lr.library_id FROM library_root lr
    WHERE lr.storage_location_id = NEW.storage_location_id
      AND (lr.path = '' OR NEW.relative_path = lr.path OR substr(NEW.relative_path, 1, length(lr.path) + 1) = lr.path || '/')
    ORDER BY length(lr.path) DESC, lr.id LIMIT 1
  ) WHERE id = NEW.id;
  UPDATE media_item SET library_id = (
    SELECT mf.library_id FROM media_file mf WHERE mf.media_item_id = NEW.media_item_id
    ORDER BY mf.available DESC, mf.library_id IS NULL, mf.id LIMIT 1
  ) WHERE id = NEW.media_item_id;
END;
CREATE TRIGGER IF NOT EXISTS media_file_library_assign_update AFTER UPDATE OF storage_location_id, relative_path, media_item_id, available ON media_file
BEGIN
  UPDATE media_file SET library_id = (
    SELECT lr.library_id FROM library_root lr
    WHERE lr.storage_location_id = NEW.storage_location_id
      AND (lr.path = '' OR NEW.relative_path = lr.path OR substr(NEW.relative_path, 1, length(lr.path) + 1) = lr.path || '/')
    ORDER BY length(lr.path) DESC, lr.id LIMIT 1
  ) WHERE id = NEW.id;
  UPDATE media_item SET library_id = (
    SELECT mf.library_id FROM media_file mf WHERE mf.media_item_id = NEW.media_item_id
    ORDER BY mf.available DESC, mf.library_id IS NULL, mf.id LIMIT 1
  ) WHERE id = NEW.media_item_id;
  UPDATE media_item SET library_id = (
    SELECT mf.library_id FROM media_file mf WHERE mf.media_item_id = OLD.media_item_id
    ORDER BY mf.available DESC, mf.library_id IS NULL, mf.id LIMIT 1
  ) WHERE id = OLD.media_item_id AND OLD.media_item_id <> NEW.media_item_id;
END;
CREATE TRIGGER IF NOT EXISTS media_file_library_assign_delete AFTER DELETE ON media_file
BEGIN
  UPDATE media_item SET library_id = (
    SELECT mf.library_id FROM media_file mf WHERE mf.media_item_id = OLD.media_item_id
    ORDER BY mf.available DESC, mf.library_id IS NULL, mf.id LIMIT 1
  ) WHERE id = OLD.media_item_id;
END;

CREATE TABLE IF NOT EXISTS "dvd_disc" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "storage_location_id" INTEGER NOT NULL,
  "relative_path" TEXT NOT NULL,
  "title_count" INTEGER NOT NULL DEFAULT 0,
  "file_size" INTEGER NOT NULL DEFAULT 0,
  "available" INTEGER NOT NULL DEFAULT 1,
  "missing_since" TIMESTAMP,
  "last_seen_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  "created_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  "updated_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE (storage_location_id, relative_path),
  CONSTRAINT "dvd_disc_storage_location_id_fkey" FOREIGN KEY (storage_location_id) REFERENCES storage_location(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS dvd_disc_storage ON "dvd_disc" (storage_location_id);

CREATE TABLE IF NOT EXISTS "media_item_field_lock" (
  "media_item_id" INTEGER NOT NULL,
  "field" TEXT NOT NULL,
  "locked_at" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT "media_item_field_lock_field_check" CHECK ((field IN ('title', 'description', 'studio', 'categories', 'performers'))),
  CONSTRAINT "media_item_field_lock_media_item_id_fkey" FOREIGN KEY (media_item_id) REFERENCES media_item(id) ON DELETE CASCADE,
  CONSTRAINT "media_item_field_lock_pkey" PRIMARY KEY (media_item_id, field)
);
