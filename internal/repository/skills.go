package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"9router-gateway/internal/entity"
)

// ListSkills returns all skills, optionally filtered by a search term across name, description, and tags.
func (r *SQLiteRepo) ListSkills(ctx context.Context, search string) ([]entity.Skill, error) {
	var rows *sql.Rows
	var err error

	if search == "" {
		rows, err = r.db.QueryContext(ctx,
			`SELECT id, name, description, tags, content, created_at, updated_at
			 FROM skills ORDER BY name ASC`)
	} else {
		q := "%" + strings.ToLower(search) + "%"
		rows, err = r.db.QueryContext(ctx,
			`SELECT id, name, description, tags, content, created_at, updated_at
			 FROM skills
			 WHERE lower(name) LIKE ? OR lower(description) LIKE ? OR lower(tags) LIKE ?
			 ORDER BY name ASC`,
			q, q, q)
	}
	if err != nil {
		return nil, fmt.Errorf("list skills: %w", err)
	}
	defer rows.Close()

	var skills []entity.Skill
	for rows.Next() {
		var s entity.Skill
		var createdAt, updatedAt string
		if err := rows.Scan(&s.ID, &s.Name, &s.Description, &s.Tags, &s.Content, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan skill: %w", err)
		}
		s.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
		s.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", updatedAt)
		skills = append(skills, s)
	}
	return skills, rows.Err()
}

// GetSkillByID returns a single skill by its UUID.
func (r *SQLiteRepo) GetSkillByID(ctx context.Context, id string) (*entity.Skill, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, name, description, tags, content, created_at, updated_at
		 FROM skills WHERE id = ?`, id)
	return scanSkill(row)
}

// GetSkillByName returns a single skill by its unique name slug.
func (r *SQLiteRepo) GetSkillByName(ctx context.Context, name string) (*entity.Skill, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, name, description, tags, content, created_at, updated_at
		 FROM skills WHERE name = ?`, name)
	return scanSkill(row)
}

// CreateSkill inserts a new skill row.
func (r *SQLiteRepo) CreateSkill(ctx context.Context, s *entity.Skill) error {
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO skills (id, name, description, tags, content, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		s.ID, s.Name, s.Description, s.Tags, s.Content, now, now)
	if err != nil {
		return fmt.Errorf("create skill: %w", err)
	}
	return nil
}

// UpdateSkill updates name, description, tags, and content for an existing skill.
func (r *SQLiteRepo) UpdateSkill(ctx context.Context, s *entity.Skill) error {
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	res, err := r.db.ExecContext(ctx,
		`UPDATE skills SET name = ?, description = ?, tags = ?, content = ?, updated_at = ?
		 WHERE id = ?`,
		s.Name, s.Description, s.Tags, s.Content, now, s.ID)
	if err != nil {
		return fmt.Errorf("update skill: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("skill %q not found", s.ID)
	}
	return nil
}

// DeleteSkill removes a skill by ID.
func (r *SQLiteRepo) DeleteSkill(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM skills WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete skill: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("skill %q not found", id)
	}
	return nil
}

func scanSkill(row *sql.Row) (*entity.Skill, error) {
	var s entity.Skill
	var createdAt, updatedAt string
	if err := row.Scan(&s.ID, &s.Name, &s.Description, &s.Tags, &s.Content, &createdAt, &updatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("scan skill: %w", err)
	}
	s.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
	s.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", updatedAt)
	return &s, nil
}
