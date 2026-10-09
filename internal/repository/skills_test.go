package repository

import (
	"context"
	"testing"

	"9router-gateway/internal/entity"

	"github.com/google/uuid"
)

func TestSkillCRUD(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()

	ctx := context.Background()

	// 1. Create
	skill := &entity.Skill{
		ID:          uuid.New().String(),
		Name:        "test-skill",
		Description: "A test skill for unit testing",
		Tags:        "go,test",
		Content:     "---\nname: test-skill\ndescription: test\n---\n\n# Test Skill\nHello from test.",
	}
	if err := repo.CreateSkill(ctx, skill); err != nil {
		t.Fatalf("CreateSkill: %v", err)
	}

	// 2. GetByID
	got, err := repo.GetSkillByID(ctx, skill.ID)
	if err != nil {
		t.Fatalf("GetSkillByID: %v", err)
	}
	if got == nil {
		t.Fatal("GetSkillByID returned nil")
	}
	if got.Name != skill.Name {
		t.Errorf("name mismatch: got %q want %q", got.Name, skill.Name)
	}

	// 3. GetByName
	got2, err := repo.GetSkillByName(ctx, skill.Name)
	if err != nil {
		t.Fatalf("GetSkillByName: %v", err)
	}
	if got2 == nil || got2.ID != skill.ID {
		t.Errorf("GetSkillByName returned wrong skill")
	}

	// 4. List (no filter)
	skills, err := repo.ListSkills(ctx, "")
	if err != nil {
		t.Fatalf("ListSkills: %v", err)
	}
	if len(skills) == 0 {
		t.Fatal("ListSkills returned empty")
	}

	// 5. List (with search match)
	matched, err := repo.ListSkills(ctx, "test")
	if err != nil {
		t.Fatalf("ListSkills search: %v", err)
	}
	if len(matched) == 0 {
		t.Error("ListSkills search 'test' should have returned results")
	}

	// 6. List (with search no match)
	none, err := repo.ListSkills(ctx, "zzznomatch999")
	if err != nil {
		t.Fatalf("ListSkills no-match: %v", err)
	}
	if len(none) != 0 {
		t.Errorf("ListSkills no-match should return empty, got %d", len(none))
	}

	// 7. Update
	skill.Description = "Updated description"
	skill.Tags = "go,test,updated"
	skill.Content = "# Updated"
	if err := repo.UpdateSkill(ctx, skill); err != nil {
		t.Fatalf("UpdateSkill: %v", err)
	}
	updated, _ := repo.GetSkillByID(ctx, skill.ID)
	if updated.Description != "Updated description" {
		t.Errorf("UpdateSkill: description not updated, got %q", updated.Description)
	}

	// 8. Delete
	if err := repo.DeleteSkill(ctx, skill.ID); err != nil {
		t.Fatalf("DeleteSkill: %v", err)
	}
	gone, _ := repo.GetSkillByID(ctx, skill.ID)
	if gone != nil {
		t.Error("skill still exists after delete")
	}

	// 9. Delete non-existent
	if err := repo.DeleteSkill(ctx, "ghost-id"); err == nil {
		t.Error("DeleteSkill on non-existent should return error")
	}
}

func TestSkillDuplicateName(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()

	ctx := context.Background()

	s1 := &entity.Skill{
		ID:   uuid.New().String(),
		Name: "duplicate-skill",
	}
	s2 := &entity.Skill{
		ID:   uuid.New().String(),
		Name: "duplicate-skill",
	}
	if err := repo.CreateSkill(ctx, s1); err != nil {
		t.Fatalf("first CreateSkill: %v", err)
	}
	if err := repo.CreateSkill(ctx, s2); err == nil {
		t.Error("duplicate name should return error")
	}
}

func TestSkillTagList(t *testing.T) {
	s := &entity.Skill{Tags: "go, test, api "}
	tags := s.TagList()
	if len(tags) != 3 {
		t.Errorf("TagList expected 3 tags, got %d: %v", len(tags), tags)
	}
	if tags[0] != "go" || tags[1] != "test" || tags[2] != "api" {
		t.Errorf("unexpected tags: %v", tags)
	}

	empty := (&entity.Skill{Tags: ""}).TagList()
	if len(empty) != 0 {
		t.Errorf("empty Tags should return nil/empty slice")
	}
}
