package cli

import (
	"context"
	"testing"
	"time"

	"github.com/PineappleBond/xyncra-server/pkg/store"
	"github.com/PineappleBond/xyncra-server/pkg/store/model"
)

// ---------------------------------------------------------------------------
// Draft Save (UPSERT)
// ---------------------------------------------------------------------------

func TestDraftSave_New(t *testing.T) {
	cliCtx := newTestCLIContext(t)
	db := openTestDB(t, cliCtx)

	draft := &model.Draft{
		ID:             "draft-1",
		ConversationID: "conv-1",
		Content:        "Hello draft",
	}
	if err := db.Drafts.Save(context.Background(), draft); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := db.Drafts.GetByConversation(context.Background(), "conv-1")
	if err != nil {
		t.Fatalf("GetByConversation: %v", err)
	}
	if got.Content != "Hello draft" {
		t.Errorf("Content = %q, want %q", got.Content, "Hello draft")
	}
}

func TestDraftSave_Upsert(t *testing.T) {
	cliCtx := newTestCLIContext(t)
	db := openTestDB(t, cliCtx)

	// Create initial draft.
	draft1 := &model.Draft{
		ID:             "draft-orig",
		ConversationID: "conv-upsert",
		Content:        "First version",
	}
	if err := db.Drafts.Save(context.Background(), draft1); err != nil {
		t.Fatalf("Save (1st): %v", err)
	}

	// Verify created_at is set.
	got1, err := db.Drafts.GetByConversation(context.Background(), "conv-upsert")
	if err != nil {
		t.Fatalf("GetByConversation (1st): %v", err)
	}
	originalCreatedAt := got1.CreatedAt

	// Give a tiny delay so UpdatedAt differs from CreatedAt.
	time.Sleep(10 * time.Millisecond)

	// Update the draft (UPSERT).
	draft2 := &model.Draft{
		ID:             "draft-new-id",
		ConversationID: "conv-upsert",
		Content:        "Updated version",
	}
	if err := db.Drafts.Save(context.Background(), draft2); err != nil {
		t.Fatalf("Save (2nd): %v", err)
	}

	got2, err := db.Drafts.GetByConversation(context.Background(), "conv-upsert")
	if err != nil {
		t.Fatalf("GetByConversation (2nd): %v", err)
	}
	if got2.Content != "Updated version" {
		t.Errorf("Content = %q, want %q", got2.Content, "Updated version")
	}
	// CreatedAt should be preserved from the original insert.
	if got2.CreatedAt.Before(originalCreatedAt) {
		t.Errorf("CreatedAt changed: was %v, now %v", originalCreatedAt, got2.CreatedAt)
	}
}

// ---------------------------------------------------------------------------
// Draft Get
// ---------------------------------------------------------------------------

func TestDraftGet_Found(t *testing.T) {
	cliCtx := newTestCLIContext(t)
	db := openTestDB(t, cliCtx)

	seedDraft(t, db, &model.Draft{
		ID:             "draft-get",
		ConversationID: "conv-get",
		Content:        "My draft content",
	})

	draft, err := db.Drafts.GetByConversation(context.Background(), "conv-get")
	if err != nil {
		t.Fatalf("GetByConversation: %v", err)
	}
	if draft.Content != "My draft content" {
		t.Errorf("Content = %q, want %q", draft.Content, "My draft content")
	}
}

func TestDraftGet_NotFound(t *testing.T) {
	cliCtx := newTestCLIContext(t)
	db := openTestDB(t, cliCtx)

	_, err := db.Drafts.GetByConversation(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent draft")
	}
	if err != store.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Draft Delete
// ---------------------------------------------------------------------------

func TestDraftDelete_Success(t *testing.T) {
	cliCtx := newTestCLIContext(t)
	db := openTestDB(t, cliCtx)

	seedDraft(t, db, &model.Draft{
		ID:             "draft-del",
		ConversationID: "conv-del",
		Content:        "to be deleted",
	})

	if err := db.Drafts.DeleteByConversation(context.Background(), "conv-del"); err != nil {
		t.Fatalf("DeleteByConversation: %v", err)
	}

	_, err := db.Drafts.GetByConversation(context.Background(), "conv-del")
	if err != store.ErrNotFound {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestDraftDelete_NotFound(t *testing.T) {
	cliCtx := newTestCLIContext(t)
	db := openTestDB(t, cliCtx)

	err := db.Drafts.DeleteByConversation(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent draft")
	}
	if err != store.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Draft command structure
// ---------------------------------------------------------------------------

func TestNewDraftCommand_Subcommands(t *testing.T) {
	cmd := newDraftCommand()
	if cmd.Use != "draft" {
		t.Errorf("Use = %q, want %q", cmd.Use, "draft")
	}

	subCmds := cmd.Commands()
	names := make(map[string]bool)
	for _, sc := range subCmds {
		names[sc.Use] = true
	}
	if !names["save"] {
		t.Error("missing 'save' subcommand")
	}
	if !names["get"] {
		t.Error("missing 'get' subcommand")
	}
	if !names["delete"] {
		t.Error("missing 'delete' subcommand")
	}
}

func TestNewDraftSaveCommand_RequiredFlags(t *testing.T) {
	cmd := newDraftSaveCommand()
	if cmd.Flags().Lookup("conversation-id") == nil {
		t.Error("missing --conversation-id flag")
	}
	if cmd.Flags().Lookup("content") == nil {
		t.Error("missing --content flag")
	}
}

func TestNewDraftGetCommand_RequiredFlags(t *testing.T) {
	cmd := newDraftGetCommand()
	if cmd.Flags().Lookup("conversation-id") == nil {
		t.Error("missing --conversation-id flag")
	}
}

func TestNewDraftDeleteCommand_RequiredFlags(t *testing.T) {
	cmd := newDraftDeleteCommand()
	if cmd.Flags().Lookup("conversation-id") == nil {
		t.Error("missing --conversation-id flag")
	}
}

// ---------------------------------------------------------------------------
// Draft List
// ---------------------------------------------------------------------------

func TestDraftList(t *testing.T) {
	cliCtx := newTestCLIContext(t)
	db := openTestDB(t, cliCtx)

	seedDraft(t, db, &model.Draft{
		ID:             "draft-list-1",
		ConversationID: "conv-a",
		Content:        "Draft A",
	})
	seedDraft(t, db, &model.Draft{
		ID:             "draft-list-2",
		ConversationID: "conv-b",
		Content:        "Draft B",
	})

	drafts, err := db.Drafts.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(drafts) != 2 {
		t.Errorf("expected 2 drafts, got %d", len(drafts))
	}
}
