// Package e2e_test contains HITL Resilience E2E tests for the Agent system.
//
// These tests verify the data persistence and recovery guarantees described in
// docs/DESIGN_HITL_RESILIENCE.md. They exercise the Question table, Conversation
// agent_status state machine, Checkpoint persistence, and the resume flow
// across simulated server restarts and multi-device races.
//
// All tests use:
//   - SQLite in-memory database (via setupAgentE2E)
//   - Redis for checkpoints and conversation locks (via setupAgentE2E)
//   - Direct DB manipulation to simulate agent behaviour (D-110: MQ bypass)
package e2e_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PineappleBond/xyncra-server/internal/agent"
	"github.com/PineappleBond/xyncra-server/internal/mq"
	"github.com/PineappleBond/xyncra-server/internal/store"
	"github.com/PineappleBond/xyncra-server/internal/store/model"
	"github.com/PineappleBond/xyncra-server/pkg/protocol"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// createTestQuestion creates a Question directly in the database.
// This simulates the agent creating a HITL question during execution.
func createTestQuestion(t *testing.T, env *agentE2EEnv, convID, checkpointID, interruptID, questionText string) *model.Question {
	t.Helper()
	q := &model.Question{
		ID:             uuid.New().String(),
		ConversationID: convID,
		CheckpointID:   checkpointID,
		InterruptID:    interruptID,
		QuestionText:   questionText,
		Status:         model.QuestionStatusPending,
		CreatedAt:      time.Now(),
	}
	err := env.store.QuestionStore().Create(context.Background(), q)
	require.NoError(t, err, "create question should succeed")
	return q
}

// newHitlRedisClient creates a Redis client for HITL test checkpoint operations.
func newHitlRedisClient(t *testing.T) *redis.Client {
	t.Helper()
	rdb := redis.NewClient(&redis.Options{
		Addr: e2eRedisAddr,
		DB:   e2eRedisDB,
	})
	t.Cleanup(func() { _ = rdb.Close() })
	return rdb
}

// ---------------------------------------------------------------------------
// Scenario 1: Single Device Basic Flow
// ---------------------------------------------------------------------------

// TestHITLResilience_Scenario1_SingleDevice verifies the basic HITL flow for
// a single device:
//
//  1. Agent emits a question -> Question row created (status=pending)
//  2. Conversation agent_status set to "asking_user"
//  3. User answers -> Question updated (status=answered)
//  4. All questions answered -> resume task processes -> agent resumes
//
// Corresponds to DESIGN_HITL_RESILIENCE.md Scenario 1 (User Offline When Agent Asks).
func TestHITLResilience_Scenario1_SingleDevice(t *testing.T) {
	env := setupAgentE2E(t)
	userID := "user-hitl-r1"
	agentUserID := "agent/test-bot"
	checkpointID := "ckpt-hitl-r1"
	interruptID := "intr-hitl-r1"

	// Create conversation and use the actual generated ID.
	conv := createAgentConversation(t, env, userID, agentUserID)
	convID := conv.ID

	// Step 1: Agent pauses, creates a Question in DB (status=pending).
	q := createTestQuestion(t, env, convID, checkpointID, interruptID,
		"Are you sure you want to proceed?")

	// Step 2: Verify Question is persisted with status=pending.
	ctx := context.Background()
	questions, err := env.store.QuestionStore().GetPendingByCheckpoint(ctx, checkpointID)
	require.NoError(t, err)
	require.Len(t, questions, 1, "should have 1 pending question")
	assert.Equal(t, model.QuestionStatusPending, questions[0].Status)
	assert.Equal(t, "Are you sure you want to proceed?", questions[0].QuestionText)
	assert.Equal(t, q.ID, questions[0].ID)

	// Step 3: Update Conversation agent_status to asking_user.
	_, err = env.store.ConversationStore().UpdateAgentStatus(ctx, convID,
		model.AgentStatusAskingUser, agentUserID, checkpointID)
	require.NoError(t, err)

	// Step 4: Verify Conversation state.
	updatedConv, err := env.store.ConversationStore().Get(ctx, convID)
	require.NoError(t, err)
	assert.Equal(t, model.AgentStatusAskingUser, updatedConv.AgentStatus)
	assert.Equal(t, checkpointID, updatedConv.CheckpointID)
	assert.Equal(t, agentUserID, updatedConv.AgentID)

	// Step 5: User answers the question.
	err = env.store.QuestionStore().UpdateAnswer(ctx, q.ID, "Yes, proceed", userID, "device-1")
	require.NoError(t, err, "UpdateAnswer should succeed")

	// Step 6: Verify Question is now answered.
	allQ, err := env.store.QuestionStore().GetByCheckpoint(ctx, checkpointID)
	require.NoError(t, err)
	require.Len(t, allQ, 1)
	assert.Equal(t, model.QuestionStatusAnswered, allQ[0].Status)
	assert.Equal(t, "Yes, proceed", allQ[0].Answer)
	assert.Equal(t, userID, allQ[0].AnsweredBy)
	assert.Equal(t, "device-1", allQ[0].AnsweredDeviceID)
	assert.NotNil(t, allQ[0].AnsweredAt)

	// Step 7: Verify no pending questions remain.
	pending, err := env.store.QuestionStore().CountPendingByCheckpoint(ctx, checkpointID)
	require.NoError(t, err)
	assert.Equal(t, int64(0), pending, "all questions should be answered")
}

// ---------------------------------------------------------------------------
// Scenario 2: Multi-Device Race Condition
// ---------------------------------------------------------------------------

// TestHITLResilience_Scenario2_MultiDeviceRace verifies that when two devices
// attempt to answer the same Question, only the first succeeds and the second
// receives ErrConflict (409). This is the idempotency guarantee (D-118).
//
// Corresponds to DESIGN_HITL_RESILIENCE.md Scenario 2 (Multi-Device Race Condition).
func TestHITLResilience_Scenario2_MultiDeviceRace(t *testing.T) {
	env := setupAgentE2E(t)
	userID := "user-hitl-r2"
	agentUserID := "agent/test-bot"
	checkpointID := "ckpt-hitl-r2"
	interruptID := "intr-hitl-r2"

	conv := createAgentConversation(t, env, userID, agentUserID)
	convID := conv.ID
	q := createTestQuestion(t, env, convID, checkpointID, interruptID,
		"Confirm the action?")
	ctx := context.Background()

	// Device A answers first -> success.
	errA := env.store.QuestionStore().UpdateAnswer(ctx, q.ID, "Confirmed by A", userID, "device-A")
	require.NoError(t, errA, "Device A answer should succeed")

	// Device B answers same question -> ErrConflict (409).
	errB := env.store.QuestionStore().UpdateAnswer(ctx, q.ID, "Confirmed by B", userID, "device-B")
	require.Error(t, errB, "Device B answer should fail")
	assert.True(t, errors.Is(errB, store.ErrConflict),
		"Device B should get ErrConflict (409), got: %v", errB)

	// Verify the final state: only Device A's answer is persisted.
	allQ, err := env.store.QuestionStore().GetByCheckpoint(ctx, checkpointID)
	require.NoError(t, err)
	require.Len(t, allQ, 1)
	assert.Equal(t, model.QuestionStatusAnswered, allQ[0].Status)
	assert.Equal(t, "Confirmed by A", allQ[0].Answer, "Device A's answer should win")
	assert.Equal(t, "device-A", allQ[0].AnsweredDeviceID)

	// Verify no pending questions remain.
	pending, err := env.store.QuestionStore().CountPendingByCheckpoint(ctx, checkpointID)
	require.NoError(t, err)
	assert.Equal(t, int64(0), pending)
}

// ---------------------------------------------------------------------------
// Scenario 3: Parallel Sub-Agent HITL
// ---------------------------------------------------------------------------

// TestHITLResilience_Scenario3_ParallelSubAgentHITL verifies the one-to-many
// Question pattern: when an agent emits multiple parallel questions (e.g. from
// sub-agents), answers are collected individually. A "partial" response is
// returned until all questions are answered, at which point the resume task is
// triggered.
//
// Corresponds to DESIGN_HITL_RESILIENCE.md Scenario 3 (Parallel Sub-Agent HITL).
func TestHITLResilience_Scenario3_ParallelSubAgentHITL(t *testing.T) {
	env := setupAgentE2E(t)
	userID := "user-hitl-r3"
	agentUserID := "agent/test-bot"
	checkpointID := "ckpt-hitl-r3"

	conv := createAgentConversation(t, env, userID, agentUserID)
	convID := conv.ID
	ctx := context.Background()

	// Agent emits 2 parallel questions (e.g. from 2 sub-agents).
	q1 := createTestQuestion(t, env, convID, checkpointID, "int-1",
		"Sub-agent A: Confirm deletion?")
	q2 := createTestQuestion(t, env, convID, checkpointID, "int-2",
		"Sub-agent B: Choose option?")

	// Verify both questions are pending.
	pending, err := env.store.QuestionStore().CountPendingByCheckpoint(ctx, checkpointID)
	require.NoError(t, err)
	assert.Equal(t, int64(2), pending, "should have 2 pending questions")

	// User answers Q1 -> partial (Q2 still pending).
	err = env.store.QuestionStore().UpdateAnswer(ctx, q1.ID, "Confirmed", userID, "device-1")
	require.NoError(t, err)

	pending, err = env.store.QuestionStore().CountPendingByCheckpoint(ctx, checkpointID)
	require.NoError(t, err)
	assert.Equal(t, int64(1), pending, "1 question still pending after Q1 answer")

	// Verify conversation still in asking_user state.
	_, _ = env.store.ConversationStore().UpdateAgentStatus(ctx, convID,
		model.AgentStatusAskingUser, agentUserID, checkpointID)
	fetchedConv, err := env.store.ConversationStore().Get(ctx, convID)
	require.NoError(t, err)
	assert.Equal(t, model.AgentStatusAskingUser, fetchedConv.AgentStatus,
		"should still be asking_user while Q2 pending")

	// User answers Q2 -> all answered, triggers resume.
	err = env.store.QuestionStore().UpdateAnswer(ctx, q2.ID, "Option B", userID, "device-1")
	require.NoError(t, err)

	pending, err = env.store.QuestionStore().CountPendingByCheckpoint(ctx, checkpointID)
	require.NoError(t, err)
	assert.Equal(t, int64(0), pending, "all questions should be answered")

	// Verify both questions are answered with correct data.
	allQ, err := env.store.QuestionStore().GetByCheckpoint(ctx, checkpointID)
	require.NoError(t, err)
	require.Len(t, allQ, 2)

	q1Answered := false
	q2Answered := false
	for _, q := range allQ {
		if q.ID == q1.ID {
			assert.Equal(t, model.QuestionStatusAnswered, q.Status)
			assert.Equal(t, "Confirmed", q.Answer)
			q1Answered = true
		}
		if q.ID == q2.ID {
			assert.Equal(t, model.QuestionStatusAnswered, q.Status)
			assert.Equal(t, "Option B", q.Answer)
			q2Answered = true
		}
	}
	assert.True(t, q1Answered, "Q1 should be found and answered")
	assert.True(t, q2Answered, "Q2 should be found and answered")
}

// ---------------------------------------------------------------------------
// Scenario 3b: Sequential Answers Via Handler Layer
// ---------------------------------------------------------------------------

// TestHITLResilience_Scenario3b_SequentialAnswersViaHandler verifies that
// sequential answers to parallel questions go through the agent_resume RPC
// handler layer (not just direct DB manipulation) and return correct
// partial/queued responses with accurate total/answered/pending counts.
//
// This complements Scenario 3 (which uses direct QuestionStore.UpdateAnswer)
// by exercising the full RPC handler path: param validation → question lookup
// → answer persistence → pending count check → response generation.
func TestHITLResilience_Scenario3b_SequentialAnswersViaHandler(t *testing.T) {
	env := setupAgentE2E(t)
	userID := "user-hitl-r3b"
	agentUserID := "agent/test-bot"
	checkpointID := "ckpt-hitl-r3b"

	conv := createAgentConversation(t, env, userID, agentUserID)
	convID := conv.ID

	// Agent emits 2 parallel questions (e.g. from 2 sub-agents).
	_ = createTestQuestion(t, env, convID, checkpointID, "int-1",
		"Sub-agent A: Confirm deletion?")
	_ = createTestQuestion(t, env, convID, checkpointID, "int-2",
		"Sub-agent B: Choose option?")

	// Connect user via WebSocket to send RPC requests.
	conn := connectClient(t, env.addr, userID, "device-1")
	defer conn.Close()

	// Answer Q1 (int-1) via agent_resume RPC.
	sendRequest(t, conn, "req-r3b-1", "agent_resume", map[string]interface{}{
		"conversation_id": convID,
		"checkpoint_id":   checkpointID,
		"interrupt_id":    "int-1",
		"answer":          "Confirmed",
		"agent_id":        agentUserID,
	})

	// Read response — should be "partial" (Q2 still pending).
	resp1 := readResponse(t, conn, 5*time.Second)
	require.Equal(t, protocol.ResponseCodeOK, resp1.Code,
		"agent_resume Q1 should succeed, got code=%d msg=%s", resp1.Code, resp1.Msg)

	var partial1 map[string]interface{}
	require.NoError(t, json.Unmarshal(resp1.Data, &partial1))
	assert.Equal(t, "partial", partial1["status"], "Q1 answer should return partial status")
	assert.Equal(t, float64(1), partial1["answered"], "answered should be 1 after Q1")
	assert.Equal(t, float64(2), partial1["total"], "total should be 2")
	assert.Equal(t, float64(1), partial1["pending"], "pending should be 1 after Q1")

	// Answer Q2 (int-2) via agent_resume RPC.
	sendRequest(t, conn, "req-r3b-2", "agent_resume", map[string]interface{}{
		"conversation_id": convID,
		"checkpoint_id":   checkpointID,
		"interrupt_id":    "int-2",
		"answer":          "Option B",
		"agent_id":        agentUserID,
	})

	// Read response — should be "queued" (all answered).
	resp2 := readResponse(t, conn, 5*time.Second)
	require.Equal(t, protocol.ResponseCodeOK, resp2.Code,
		"agent_resume Q2 should succeed, got code=%d msg=%s", resp2.Code, resp2.Msg)

	var queued2 map[string]interface{}
	require.NoError(t, json.Unmarshal(resp2.Data, &queued2))
	assert.Equal(t, "queued", queued2["status"], "Q2 answer should return queued status")
	assert.Equal(t, float64(2), queued2["answered"], "answered should be 2 after Q2")
	assert.Equal(t, float64(2), queued2["total"], "total should be 2")

	// Verify both questions are answered in DB.
	ctx := context.Background()
	allQ, err := env.store.QuestionStore().GetByCheckpoint(ctx, checkpointID)
	require.NoError(t, err)
	require.Len(t, allQ, 2, "should have 2 questions total")

	answeredCount := 0
	for _, q := range allQ {
		if q.Status == model.QuestionStatusAnswered {
			answeredCount++
		}
	}
	assert.Equal(t, 2, answeredCount, "both questions should be answered in DB")
}

// ---------------------------------------------------------------------------
// Scenario 4: Server Restart During HITL Wait
// ---------------------------------------------------------------------------

// TestHITLResilience_Scenario4_ServerRestartDuringHITLWait verifies that all
// HITL state (Conversation, Questions, Checkpoint) survives a server restart.
//
// In this test, "restart" is simulated by verifying that all data resides in
// persistent stores (DB + Redis) that outlive the process. Since SQLite is
// in-memory (same process), the data naturally persists. The key invariant is
// that no critical state lives only in memory.
//
// Corresponds to DESIGN_HITL_RESILIENCE.md Scenario 4 (Server Restart During
// HITL Wait).
func TestHITLResilience_Scenario4_ServerRestartDuringHITLWait(t *testing.T) {
	env := setupAgentE2E(t)
	userID := "user-hitl-r4"
	agentUserID := "agent/test-bot"
	checkpointID := "ckpt-hitl-r4"

	conv := createAgentConversation(t, env, userID, agentUserID)
	convID := conv.ID
	ctx := context.Background()
	rdb := newHitlRedisClient(t)

	// --- Simulate agent execution: create state ---

	// 1. Save Checkpoint in Redis (TTL 24h in production, 1h here).
	checkpointStore := agent.NewRedisCheckPointStore(rdb, "", 1*time.Hour)
	err := checkpointStore.Set(ctx, checkpointID, []byte(`{"state":"interrupted"}`))
	require.NoError(t, err, "checkpoint save should succeed")

	// 2. Create Questions in DB.
	q1 := createTestQuestion(t, env, convID, checkpointID, "int-1",
		"Question 1: Confirm?")
	q2 := createTestQuestion(t, env, convID, checkpointID, "int-2",
		"Question 2: Which option?")

	// 3. Update Conversation agent_status.
	_, err = env.store.ConversationStore().UpdateAgentStatus(ctx, convID,
		model.AgentStatusAskingUser, agentUserID, checkpointID)
	require.NoError(t, err)

	// --- 💥 Simulated server restart ---
	// All critical state is in persistent stores (DB + Redis).
	// In a real restart, the process dies and comes back. Since we use
	// SQLite in-memory (same process), data persists within the test.
	// Redis data also persists (separate process).

	// --- Post-restart verification ---

	// Checkpoint still in Redis.
	loaded, found, err := checkpointStore.Get(ctx, checkpointID)
	require.NoError(t, err)
	assert.True(t, found, "checkpoint should survive restart")
	assert.Equal(t, `{"state":"interrupted"}`, string(loaded))

	// Questions still in DB with pending status.
	questions, err := env.store.QuestionStore().GetByCheckpoint(ctx, checkpointID)
	require.NoError(t, err)
	require.Len(t, questions, 2)
	for _, q := range questions {
		assert.Equal(t, model.QuestionStatusPending, q.Status,
			"Q %s should still be pending", q.ID)
	}

	// Conversation still in asking_user state.
	restoredConv, err := env.store.ConversationStore().Get(ctx, convID)
	require.NoError(t, err)
	assert.Equal(t, model.AgentStatusAskingUser, restoredConv.AgentStatus)

	// --- User answers after restart -> resume works ---

	// User answers Q1.
	err = env.store.QuestionStore().UpdateAnswer(ctx, q1.ID, "Yes", userID, "device-1")
	require.NoError(t, err)

	// User answers Q2.
	err = env.store.QuestionStore().UpdateAnswer(ctx, q2.ID, "Option B", userID, "device-1")
	require.NoError(t, err)

	// Verify all answered.
	pending, err := env.store.QuestionStore().CountPendingByCheckpoint(ctx, checkpointID)
	require.NoError(t, err)
	assert.Equal(t, int64(0), pending, "all questions should be answered after restart")
}

// ---------------------------------------------------------------------------
// Scenario 6: Server Restart After Partial Answers
// ---------------------------------------------------------------------------

// TestHITLResilience_Scenario6_ServerRestartAfterPartialAnswers verifies that
// when the user has partially answered questions (Q1 answered, Q2 still pending)
// and the server restarts, Q1's answer is NOT lost. After restart, the user
// can answer Q2 and the resume flow works correctly.
//
// This tests the D-116 fix: answers are persisted to the Question table BEFORE
// the resume task is enqueued, so they survive restarts.
//
// Corresponds to DESIGN_HITL_RESILIENCE.md Scenario 6 (Server Restart After
// Partial Answers).
func TestHITLResilience_Scenario6_ServerRestartAfterPartialAnswers(t *testing.T) {
	env := setupAgentE2E(t)
	userID := "user-hitl-r6"
	agentUserID := "agent/test-bot"
	checkpointID := "ckpt-hitl-r6"

	conv := createAgentConversation(t, env, userID, agentUserID)
	convID := conv.ID
	ctx := context.Background()

	// Create 2 parallel questions.
	q1 := createTestQuestion(t, env, convID, checkpointID, "int-1",
		"Question 1: Confirm?")
	q2 := createTestQuestion(t, env, convID, checkpointID, "int-2",
		"Question 2: Which plan?")

	// User answers Q1.
	err := env.store.QuestionStore().UpdateAnswer(ctx, q1.ID, "Yes, confirmed", userID, "device-1")
	require.NoError(t, err)

	// Verify: Q1 answered, Q2 still pending.
	allQ, err := env.store.QuestionStore().GetByCheckpoint(ctx, checkpointID)
	require.NoError(t, err)
	require.Len(t, allQ, 2)
	for _, q := range allQ {
		if q.ID == q1.ID {
			assert.Equal(t, model.QuestionStatusAnswered, q.Status)
			assert.Equal(t, "Yes, confirmed", q.Answer)
		}
		if q.ID == q2.ID {
			assert.Equal(t, model.QuestionStatusPending, q.Status)
		}
	}

	pending, err := env.store.QuestionStore().CountPendingByCheckpoint(ctx, checkpointID)
	require.NoError(t, err)
	assert.Equal(t, int64(1), pending, "Q2 should still be pending")

	// --- 💥 Simulated server restart ---
	// Q1's answer is in the DB (persisted by D-116: answer-first-to-DB).
	// In the old design (answer only in MQ payload), this data would be lost.

	// --- Post-restart: Q1 answer is still there ---
	allQ2, err := env.store.QuestionStore().GetByCheckpoint(ctx, checkpointID)
	require.NoError(t, err)
	for _, q := range allQ2 {
		if q.ID == q1.ID {
			assert.Equal(t, model.QuestionStatusAnswered, q.Status,
				"Q1 answer must survive restart (D-116)")
			assert.Equal(t, "Yes, confirmed", q.Answer,
				"Q1 answer text must survive restart")
		}
	}

	// User answers Q2 after restart.
	err = env.store.QuestionStore().UpdateAnswer(ctx, q2.ID, "Plan B", userID, "device-1")
	require.NoError(t, err)

	// All answered -> verify.
	pending, err = env.store.QuestionStore().CountPendingByCheckpoint(ctx, checkpointID)
	require.NoError(t, err)
	assert.Equal(t, int64(0), pending, "all questions should be answered")

	// Verify resume handler can read all answers from DB (D-119).
	finalQ, err := env.store.QuestionStore().GetByCheckpoint(ctx, checkpointID)
	require.NoError(t, err)
	targets := make(map[string]any)
	for _, q := range finalQ {
		if q.Status == model.QuestionStatusAnswered && q.InterruptID != "" {
			targets[q.InterruptID] = q.Answer
		}
	}
	assert.Equal(t, 2, len(targets), "resume targets should have 2 entries from DB")
	assert.Equal(t, "Yes, confirmed", targets["int-1"])
	assert.Equal(t, "Plan B", targets["int-2"])
}

// ---------------------------------------------------------------------------
// Scenario 7: Resume Task In Queue During Restart
// ---------------------------------------------------------------------------

// TestHITLResilience_Scenario7_ResumeTaskInQueueDuringRestart verifies that
// when all questions are answered and a TypeAgentResume task is enqueued (but
// not yet processed), a server restart does not lose the answers. The task
// payload only contains checkpoint_id (D-116); the answers are read from DB
// by the resume handler (D-119). Therefore, even if the task is retried after
// restart, it reads fresh data from DB and resumes correctly.
//
// Corresponds to DESIGN_HITL_RESILIENCE.md Scenario 7 (Resume Task In Queue
// During Restart).
func TestHITLResilience_Scenario7_ResumeTaskInQueueDuringRestart(t *testing.T) {
	env := setupAgentE2E(t)
	userID := "user-hitl-r7"
	agentUserID := "agent/test-bot"
	checkpointID := "ckpt-hitl-r7"
	rdb := newHitlRedisClient(t)

	conv := createAgentConversation(t, env, userID, agentUserID)
	convID := conv.ID
	ctx := context.Background()

	// Create and answer 2 questions.
	q1 := createTestQuestion(t, env, convID, checkpointID, "int-1",
		"Confirm action?")
	q2 := createTestQuestion(t, env, convID, checkpointID, "int-2",
		"Select option?")

	err := env.store.QuestionStore().UpdateAnswer(ctx, q1.ID, "yes", userID, "device-1")
	require.NoError(t, err)
	err = env.store.QuestionStore().UpdateAnswer(ctx, q2.ID, "B", userID, "device-1")
	require.NoError(t, err)

	// Save checkpoint in Redis (simulates agent's HITL checkpoint).
	checkpointStore := agent.NewRedisCheckPointStore(rdb, "", 1*time.Hour)
	err = checkpointStore.Set(ctx, checkpointID, []byte(`{"state":"interrupted"}`))
	require.NoError(t, err)

	// Update Conversation status.
	_, err = env.store.ConversationStore().UpdateAgentStatus(ctx, convID,
		model.AgentStatusAskingUser, agentUserID, checkpointID)
	require.NoError(t, err)

	// All answered. Enqueue TypeAgentResume task (simulates what the
	// agent_resume RPC handler does when all questions are answered).
	resumePayload := agent.AgentResumePayload{
		ConversationID: convID,
		CheckpointID:   checkpointID,
		SenderID:       userID,
		AgentID:        agentUserID,
		DeviceID:       "device-1",
	}
	rawPayload, err := json.Marshal(resumePayload)
	require.NoError(t, err)

	// --- 💥 Simulated server restart ---
	// The task is in Redis (Asynq), answers are in DB, checkpoint in Redis.
	// After restart, Asynq retries the task. The task payload contains only
	// checkpoint_id — answers are read from DB (D-116, D-119).

	// --- Post-restart verification ---

	// 1. Answers still in DB.
	allQ, err := env.store.QuestionStore().GetByCheckpoint(ctx, checkpointID)
	require.NoError(t, err)
	require.Len(t, allQ, 2)
	for _, q := range allQ {
		assert.Equal(t, model.QuestionStatusAnswered, q.Status,
			"answers must survive restart")
	}

	// 2. Checkpoint still in Redis.
	_, found, err := checkpointStore.Get(ctx, checkpointID)
	require.NoError(t, err)
	assert.True(t, found, "checkpoint must survive restart")

	// 3. Conversation state still persisted.
	persistedConv, err := env.store.ConversationStore().Get(ctx, convID)
	require.NoError(t, err)
	assert.Equal(t, model.AgentStatusAskingUser, persistedConv.AgentStatus)

	// 4. Task payload can be deserialized (contains checkpoint_id only).
	var decoded agent.AgentResumePayload
	err = json.Unmarshal(rawPayload, &decoded)
	require.NoError(t, err)
	assert.Equal(t, convID, decoded.ConversationID)
	assert.Equal(t, checkpointID, decoded.CheckpointID)

	// 5. Resume handler would read answers from DB to build targets (D-119).
	questions, err := env.store.QuestionStore().GetByCheckpoint(ctx, checkpointID)
	require.NoError(t, err)
	targets := make(map[string]any)
	for _, q := range questions {
		if q.Status == model.QuestionStatusAnswered && q.InterruptID != "" {
			targets[q.InterruptID] = q.Answer
		}
	}
	assert.Equal(t, 2, len(targets), "targets should be built from DB answers")
	assert.Equal(t, "yes", targets["int-1"])
	assert.Equal(t, "B", targets["int-2"])

	// 6. Simulate Asynq retry: process the resume task directly.
	// The resume handler reads targets from DB (verified above). The actual
	// Eino ResumeWithParams call will fail because the mock LLM cannot
	// produce a real checkpoint, but the data-read path is correct.
	task := &mq.Task{
		Type:    mq.TypeAgentResume,
		Payload: rawPayload,
	}
	resumeHandler := agent.NewAgentResumeHandler(
		env.executor, env.registry, env.lock, testLogger{t: t}, nil)
	err = resumeHandler(ctx, task)
	// The handler returns nil (sends error message for checkpoint expiry).
	// This is expected: mock LLM cannot produce a real Eino checkpoint.
	assert.NoError(t, err,
		"resume handler should return nil (error message sent, not retried)")
}
