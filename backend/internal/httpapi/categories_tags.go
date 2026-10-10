package httpapi

import (
	"net/http"
	"strconv"
	"strings"

	"reelstash/backend/internal/slug"
)

type category struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Slug       string `json:"slug"`
	ParentID   *int64 `json:"parentId,omitempty"`
	MediaCount int64  `json:"mediaCount"`
}

func (a *API) listCategories(w http.ResponseWriter, r *http.Request) {
	// Users see categories (and counts) of the titles in their libraries.
	args := []any{}
	arg := func(v any) string { args = append(args, v); return "$" + strconv.Itoa(len(args)) }
	join, having := `LEFT JOIN media_item_category mic ON mic.category_id = c.id`, ""
	if f := libraryFilter(r, "mi.library_id", arg); f != "" {
		join = `LEFT JOIN (media_item_category mic JOIN media_item mi ON mi.id = mic.media_item_id AND ` + f + `) ON mic.category_id = c.id`
		having = ` HAVING COUNT(mic.media_item_id) > 0`
	}
	rows, err := a.DB.Query(r.Context(), `
		SELECT c.id, c.name, c.slug, c.parent_id, COUNT(mic.media_item_id)
		FROM category c
		`+join+`
		GROUP BY c.id`+having+`
		ORDER BY c.name`, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load categories")
		return
	}
	defer rows.Close()

	items := []category{}
	for rows.Next() {
		var c category
		if err := rows.Scan(&c.ID, &c.Name, &c.Slug, &c.ParentID, &c.MediaCount); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read category")
			return
		}
		items = append(items, c)
	}

	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

type createCategoryRequest struct {
	Name     string `json:"name"`
	ParentID *int64 `json:"parentId"`
}

// slugifyName is the category slug stored beside the display name.
func slugifyName(name string) string { return slug.Name(name) }

func (a *API) createCategory(w http.ResponseWriter, r *http.Request) {
	var req createCategoryRequest
	if err := decodeJSON(r, &req); err != nil || req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	var id int64
	err := a.DB.QueryRow(r.Context(), `
		INSERT INTO category (name, slug, parent_id) VALUES ($1, $2, $3)
		RETURNING id
	`, req.Name, slugifyName(req.Name), req.ParentID).Scan(&id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create category (name/slug may already exist)")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

type updateCategoryRequest struct {
	Name     string `json:"name"`
	ParentID *int64 `json:"parentId"`
}

func (a *API) updateCategory(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid category id")
		return
	}
	var req updateCategoryRequest
	if err := decodeJSON(r, &req); err != nil || req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	tag, err := a.DB.Exec(r.Context(), `
		UPDATE category SET name = $2, slug = $3, parent_id = $4 WHERE id = $1
	`, id, req.Name, slugifyName(req.Name), req.ParentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update category (name/slug may already exist)")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "category not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id})
}

func (a *API) deleteCategory(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid category id")
		return
	}
	tag, err := a.DB.Exec(r.Context(), `DELETE FROM category WHERE id = $1`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete category")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "category not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type tag struct {
	ID            int64   `json:"id"`
	Name          string  `json:"name"`
	Description   *string `json:"description,omitempty"`
	OptionalColor *string `json:"optionalColor,omitempty"`
	MediaCount    int64   `json:"mediaCount"`
}

func (a *API) listTags(w http.ResponseWriter, r *http.Request) {
	rows, err := a.DB.Query(r.Context(), `
		SELECT t.id, t.name, t.description, t.optional_color,
		       (SELECT COUNT(*) FROM media_item_tag mit WHERE mit.tag_id = t.id)
		FROM tag t ORDER BY t.name`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load tags")
		return
	}
	defer rows.Close()

	items := []tag{}
	for rows.Next() {
		var t tag
		if err := rows.Scan(&t.ID, &t.Name, &t.Description, &t.OptionalColor, &t.MediaCount); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read tag")
			return
		}
		items = append(items, t)
	}

	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

type createTagRequest struct {
	Name          string `json:"name"`
	Description   string `json:"description"`
	OptionalColor string `json:"optionalColor"`
}

func (a *API) createTag(w http.ResponseWriter, r *http.Request) {
	var req createTagRequest
	if err := decodeJSON(r, &req); err != nil || req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	var id int64
	err := a.DB.QueryRow(r.Context(), `
		INSERT INTO tag (name, description, optional_color) VALUES ($1, NULLIF($2, ''), NULLIF($3, ''))
		RETURNING id
	`, req.Name, req.Description, req.OptionalColor).Scan(&id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create tag")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

type updateTagRequest struct {
	Name string `json:"name"`
}

// updateTag renames a tag. Admin only.
func (a *API) updateTag(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid tag id")
		return
	}
	var req updateTagRequest
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	res, err := a.DB.Exec(r.Context(), `UPDATE tag SET name = $2 WHERE id = $1`, id, strings.TrimSpace(req.Name))
	if err != nil {
		writeError(w, http.StatusConflict, "a tag with that name already exists")
		return
	}
	if res.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "tag not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// deleteTag removes a tag from every title and deletes it. Admin only.
func (a *API) deleteTag(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid tag id")
		return
	}
	res, err := a.DB.Exec(r.Context(), `DELETE FROM tag WHERE id = $1`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete tag")
		return
	}
	if res.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "tag not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// emptyCategories matches categories with no titles and no child categories.
const emptyCategories = `NOT EXISTS (SELECT 1 FROM media_item_category m WHERE m.category_id = category.id)
	AND NOT EXISTS (SELECT 1 FROM category ch WHERE ch.parent_id = category.id)`

// countEmptyCategories reports how many categories cleanupCategories would delete.
func (a *API) countEmptyCategories(w http.ResponseWriter, r *http.Request) {
	var n int
	if err := a.DB.QueryRow(r.Context(), `SELECT COUNT(*) FROM category WHERE `+emptyCategories).Scan(&n); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count categories")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": n})
}

// cleanupCategories deletes every category that has no titles.
func (a *API) cleanupCategories(w http.ResponseWriter, r *http.Request) {
	tag, err := a.DB.Exec(r.Context(), `DELETE FROM category WHERE `+emptyCategories)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clean up categories")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": tag.RowsAffected()})
}
