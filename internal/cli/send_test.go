package cli

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// send command — content flag validation
// ---------------------------------------------------------------------------

// TestSend_ContentFlagNotProvided verifies that omitting --content entirely
// results in Changed("content") == false, which runSend interprets as
// "content is required". We test at the flag level (ParseFlags) to avoid
// needing a full CLIContext setup.
func TestSend_ContentFlagNotProvided(t *testing.T) {
	cmd := newSendCommand()
	// Only set the required --conversation-id; leave --content unset.
	if err := cmd.ParseFlags([]string{"--conversation-id", "conv-1"}); err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}

	if cmd.Flags().Changed("content") {
		t.Error("Changed(\"content\") = true when flag is omitted, want false")
	}

	// Verify the default value is still "" (which is NOT the same as Changed).
	val, _ := cmd.Flags().GetString("content")
	if val != "" {
		t.Errorf("GetString(\"content\") = %q, want \"\"", val)
	}
}

// TestSend_ContentFlagEmptyString verifies that passing --content "" (empty
// string) sets Changed("content") = true, allowing empty messages.
func TestSend_ContentFlagEmptyString(t *testing.T) {
	cmd := newSendCommand()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.ParseFlags([]string{"--conversation-id", "conv-1", "--content", ""}); err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}

	if !cmd.Flags().Changed("content") {
		t.Error("Changed(\"content\") = false, want true when --content is explicitly set to empty string")
	}

	val, _ := cmd.Flags().GetString("content")
	if val != "" {
		t.Errorf("GetString(\"content\") = %q, want \"\"", val)
	}
}

// TestSend_ContentFlagWithText verifies that --content "hello" passes both
// Changed() and value checks.
func TestSend_ContentFlagWithText(t *testing.T) {
	cmd := newSendCommand()

	if err := cmd.ParseFlags([]string{"--conversation-id", "conv-1", "--content", "hello"}); err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}

	if !cmd.Flags().Changed("content") {
		t.Error("Changed(\"content\") = false, want true")
	}

	val, _ := cmd.Flags().GetString("content")
	if val != "hello" {
		t.Errorf("GetString(\"content\") = %q, want \"hello\"", val)
	}
}

// TestSend_ContentFlagChangedSemantics is a focused test on cobra's
// Changed() behaviour: it returns true only when the flag was explicitly set
// on the command line, regardless of whether the value equals the default.
func TestSend_ContentFlagChangedSemantics(t *testing.T) {
	// Case 1: --content not in args at all -> Changed() == false.
	t.Run("omitted", func(t *testing.T) {
		cmd := newSendCommand()
		_ = cmd.ParseFlags([]string{"--conversation-id", "conv-1"})
		if cmd.Flags().Changed("content") {
			t.Error("Changed() = true when flag omitted, want false")
		}
	})

	// Case 2: --content "" -> Changed() == true (explicitly set).
	t.Run("empty_string", func(t *testing.T) {
		cmd := newSendCommand()
		_ = cmd.ParseFlags([]string{"--conversation-id", "conv-1", "--content", ""})
		if !cmd.Flags().Changed("content") {
			t.Error("Changed() = false when --content \"\", want true")
		}
	})

	// Case 3: --content "text" -> Changed() == true.
	t.Run("with_text", func(t *testing.T) {
		cmd := newSendCommand()
		_ = cmd.ParseFlags([]string{"--conversation-id", "conv-1", "--content", "hi"})
		if !cmd.Flags().Changed("content") {
			t.Error("Changed() = false when --content \"hi\", want true")
		}
	})
}

// TestSend_ConversationIDRequired verifies that --conversation-id is a
// required flag (MarkFlagRequired).
func TestSend_ConversationIDRequired(t *testing.T) {
	cmd := newSendCommand()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"--content", "hello"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when --conversation-id is omitted, got nil")
	}
	if !strings.Contains(err.Error(), "conversation-id") {
		t.Errorf("error %q should mention --conversation-id", err.Error())
	}
}
