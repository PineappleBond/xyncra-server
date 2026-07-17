package cli

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/PineappleBond/xyncra-server/pkg/store"
	"github.com/PineappleBond/xyncra-server/pkg/store/model"
)

// newDraftCommand creates the "draft" parent command with three subcommands:
// save, get, and delete.
func newDraftCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "draft",
		Short: "Manage message drafts (local only)",
	}

	cmd.AddCommand(newDraftSaveCommand())
	cmd.AddCommand(newDraftGetCommand())
	cmd.AddCommand(newDraftDeleteCommand())

	return cmd
}

// ---------------------------------------------------------------------------
// draft save
// ---------------------------------------------------------------------------

// newDraftSaveCommand creates the "draft save" subcommand.
func newDraftSaveCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "save",
		Short: "Save or update a draft for a conversation",
		RunE:  runDraftSave,
	}

	cmd.Flags().StringP("conversation-id", "c", "", "Conversation ID (required)")
	cmd.Flags().StringP("content", "m", "", "Draft content (required)")

	_ = cmd.MarkFlagRequired("conversation-id")
	_ = cmd.MarkFlagRequired("content")

	return cmd
}

// runDraftSave saves a draft to the local SQLite database. The draft is
// upserted on the conversation_id unique index, so any existing draft for the
// conversation is overwritten.
func runDraftSave(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
	defer cancel()

	cliCtx, err := NewCLIContext(cmd)
	if err != nil {
		return fmt.Errorf("draft save: %w", err)
	}

	convID, _ := cmd.Flags().GetString("conversation-id")
	content, _ := cmd.Flags().GetString("content")

	if convID == "" {
		return errors.New("draft save: --conversation-id is required")
	}
	if content == "" {
		return errors.New("draft save: --content is required")
	}

	db, err := store.New(cliCtx.DBPath)
	if err != nil {
		return fmt.Errorf("draft save: open db: %w", err)
	}
	defer func() { _ = db.Close() }()

	draft := &model.Draft{
		ID:             uuid.New().String(),
		ConversationID: convID,
		Content:        content,
	}

	if err := db.Drafts.Save(ctx, draft); err != nil {
		return fmt.Errorf("draft save: %w", err)
	}

	fmt.Println("Draft saved.")
	return nil
}

// ---------------------------------------------------------------------------
// draft get
// ---------------------------------------------------------------------------

// newDraftGetCommand creates the "draft get" subcommand.
func newDraftGetCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get",
		Short: "Retrieve the draft for a conversation",
		RunE:  runDraftGet,
	}

	cmd.Flags().StringP("conversation-id", "c", "", "Conversation ID (required)")

	_ = cmd.MarkFlagRequired("conversation-id")

	return cmd
}

// runDraftGet retrieves the draft for the given conversation from the local
// SQLite database and prints its content.
func runDraftGet(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
	defer cancel()

	cliCtx, err := NewCLIContext(cmd)
	if err != nil {
		return fmt.Errorf("draft get: %w", err)
	}

	convID, _ := cmd.Flags().GetString("conversation-id")

	if convID == "" {
		return errors.New("draft get: --conversation-id is required")
	}

	db, err := store.New(cliCtx.DBPath)
	if err != nil {
		return fmt.Errorf("draft get: open db: %w", err)
	}
	defer func() { _ = db.Close() }()

	draft, err := db.Drafts.GetByConversation(ctx, convID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			fmt.Println("No draft found for this conversation.")
			return nil
		}
		return fmt.Errorf("draft get: %w", err)
	}

	fmt.Printf("Draft for conversation %s:\n", convID)
	fmt.Println(draft.Content)
	return nil
}

// ---------------------------------------------------------------------------
// draft delete
// ---------------------------------------------------------------------------

// newDraftDeleteCommand creates the "draft delete" subcommand.
func newDraftDeleteCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete the draft for a conversation",
		RunE:  runDraftDelete,
	}

	cmd.Flags().StringP("conversation-id", "c", "", "Conversation ID (required)")

	_ = cmd.MarkFlagRequired("conversation-id")

	return cmd
}

// runDraftDelete removes the draft for the given conversation from the local
// SQLite database. Uses a writable store connection.
func runDraftDelete(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
	defer cancel()

	cliCtx, err := NewCLIContext(cmd)
	if err != nil {
		return fmt.Errorf("draft delete: %w", err)
	}

	convID, _ := cmd.Flags().GetString("conversation-id")

	if convID == "" {
		return errors.New("draft delete: --conversation-id is required")
	}

	db, err := store.New(cliCtx.DBPath)
	if err != nil {
		return fmt.Errorf("draft delete: open db: %w", err)
	}
	defer func() { _ = db.Close() }()

	if err := db.Drafts.DeleteByConversation(ctx, convID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			fmt.Println("No draft found for this conversation.")
			return nil
		}
		return fmt.Errorf("draft delete: %w", err)
	}

	fmt.Println("Draft deleted.")
	return nil
}
