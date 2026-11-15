package v1

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// UpdateProfilePost handles profile display name updates from web forms.
func (h *Handler) UpdateProfilePost(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	ctx := r.Context()
	currentUser := GetUserFromContext(ctx)
	if currentUser == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		http.Redirect(w, r, "/profile?error="+url.QueryEscape("Display name cannot be empty"), http.StatusSeeOther)
		return
	}

	_, err := h.users.UpdateProfile(ctx, currentUser.ID, name)
	if err != nil {
		http.Redirect(w, r, "/profile?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/profile?msg="+url.QueryEscape("Profile updated successfully"), http.StatusSeeOther)
}

// APIUpdateProfile handles JSON profile updates.
func (h *Handler) APIUpdateProfile(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	currentUser := GetUserFromContext(ctx)
	if currentUser == nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	var payload struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	name := strings.TrimSpace(payload.Name)
	if name == "" {
		http.Error(w, `{"error":"display name cannot be empty"}`, http.StatusBadRequest)
		return
	}

	user, err := h.users.UpdateProfile(ctx, currentUser.ID, name)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"user": map[string]interface{}{
			"id":       user.ID,
			"username": user.Username,
			"name":     user.Name,
		},
	})
}

// ExportOwnLoginAudits streams the authenticated user's own login history as CSV or JSON.
func (h *Handler) ExportOwnLoginAudits(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	currentUser := GetUserFromContext(ctx)
	if currentUser == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	format := strings.ToLower(r.URL.Query().Get("format"))
	if format != "json" {
		format = "csv"
	}

	audits, err := h.repo.GetUserLoginAudits(ctx, currentUser.ID, 1000)
	if err != nil {
		http.Error(w, "Failed to retrieve login audits", http.StatusInternalServerError)
		return
	}

	timestamp := time.Now().Format("20060102_150405")

	if format == "json" {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=my_login_audits_%s.json", timestamp))
		_ = json.NewEncoder(w).Encode(audits)
		return
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=my_login_audits_%s.csv", timestamp))

	writer := csv.NewWriter(w)
	defer writer.Flush()

	_ = writer.Write([]string{
		"ID", "Timestamp_WIB", "Username", "Status", "Client_IP", "User_Agent", "Reason",
	})

	for _, a := range audits {
		timeWIB := a.CreatedAt.Add(7 * time.Hour).Format("2006-01-02 15:04:05")
		_ = writer.Write([]string{
			strconv.FormatInt(a.ID, 10),
			timeWIB,
			a.Username,
			a.Status,
			a.IP,
			a.UserAgent,
			a.Reason,
		})
	}
}
