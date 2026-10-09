package v1

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"9router-gateway/internal/entity"
)

// SkillsRegistryPage renders the agent skills registry dashboard (admin-only).
func (h *Handler) SkillsRegistryPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	search := strings.TrimSpace(r.URL.Query().Get("q"))

	skills, err := h.repo.ListSkills(ctx, search)
	if err != nil {
		http.Error(w, "failed to load skills", http.StatusInternalServerError)
		return
	}

	h.render(w, r, "skills_registry.html", "base.html", map[string]interface{}{
		"ActivePage": "skills",
		"Skills":     skills,
		"Search":     search,
	})
}

// APIListSkills returns all skills as JSON (optionally filtered by ?q=).
func (h *Handler) APIListSkills(w http.ResponseWriter, r *http.Request) {
	search := strings.TrimSpace(r.URL.Query().Get("q"))
	skills, err := h.repo.ListSkills(r.Context(), search)
	if err != nil {
		http.Error(w, "failed to list skills", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"skills": skills, "count": len(skills)})
}

// APIGetSkillRaw returns the raw SKILL.md content of a skill by name slug.
// Used by: curl -s http://.../api/skills/my-skill/raw > ~/.hermes/skills/my-skill/SKILL.md
func (h *Handler) APIGetSkillRaw(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	skill, err := h.repo.GetSkillByName(r.Context(), name)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if skill == nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", `inline; filename="`+name+`.md"`)
	_, _ = w.Write([]byte(skill.Content))
}

// APIGetSkill returns a single skill by ID as JSON.
func (h *Handler) APIGetSkill(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	skill, err := h.repo.GetSkillByID(r.Context(), id)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if skill == nil {
		http.Error(w, "skill not found", http.StatusNotFound)
		return
	}
	writeJSON(w, skill)
}

// APICreateSkill creates a new skill entry.
func (h *Handler) APICreateSkill(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Tags        string `json:"tags"`
		Content     string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	skill := &entity.Skill{
		ID:          uuid.New().String(),
		Name:        req.Name,
		Description: req.Description,
		Tags:        req.Tags,
		Content:     req.Content,
	}
	if err := h.repo.CreateSkill(r.Context(), skill); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			http.Error(w, "skill name already exists", http.StatusConflict)
			return
		}
		http.Error(w, "failed to create skill", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, skill)
}

// APIUpdateSkill updates an existing skill by ID.
func (h *Handler) APIUpdateSkill(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	existing, err := h.repo.GetSkillByID(r.Context(), id)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if existing == nil {
		http.Error(w, "skill not found", http.StatusNotFound)
		return
	}

	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Tags        string `json:"tags"`
		Content     string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		req.Name = existing.Name
	}

	updated := &entity.Skill{
		ID:          id,
		Name:        strings.TrimSpace(req.Name),
		Description: req.Description,
		Tags:        req.Tags,
		Content:     req.Content,
	}
	if err := h.repo.UpdateSkill(r.Context(), updated); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			http.Error(w, "skill name already exists", http.StatusConflict)
			return
		}
		http.Error(w, "failed to update skill", http.StatusInternalServerError)
		return
	}
	writeJSON(w, updated)
}

// APIDeleteSkill deletes a skill by ID.
func (h *Handler) APIDeleteSkill(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.repo.DeleteSkill(r.Context(), id); err != nil {
		if strings.Contains(err.Error(), "not found") {
			http.Error(w, "skill not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to delete skill", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"status": "deleted"})
}
