package httpapi

import "net/http"

func (a *API) getSettingValue(w http.ResponseWriter, r *http.Request, key string) (string, bool) {
	var value string
	err := a.DB.QueryRow(r.Context(), `SELECT value FROM app_setting WHERE key = $1`, key).Scan(&value)
	if err != nil {
		return "", false
	}
	return value, true
}

func maskToken(token string) string {
	if token == "" {
		return ""
	}
	if len(token) <= 4 {
		return "****"
	}
	return "****" + token[len(token)-4:]
}
