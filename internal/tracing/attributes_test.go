package tracing

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSpanNames_NotEmpty(t *testing.T) {
	names := []string{
		// WS / handler spans
		SpanWSConnection, SpanWSMessageReceive,
		SpanHandlerInvoke, SpanBrokerEnqueue, SpanBrokerProcess,
		SpanHandlerBroadcast, SpanAgentExecute, SpanAgentBuild,
		SpanAgentRun, SpanAgentLLMCall, SpanAgentToolCall,
		SpanAgentCheckpointSave, SpanAgentStream,
		// DB layer spans
		SpanDBConversationCreate, SpanDBConversationGet,
		SpanDBConversationGetByUsers, SpanDBConversationGetByUser,
		SpanDBConversationUpdate, SpanDBConversationDelete,
		SpanDBConversationRestore, SpanDBConversationUpdateLastMessage,
		SpanDBConversationSearchByTitle, SpanDBConversationGetUnscoped,
		SpanDBConversationUpdateLastRead, SpanDBConversationUpdateAgentStatus,
		SpanDBConversationClearAgentStatus, SpanDBConversationListStaleHITL,
		SpanDBMessageCreate, SpanDBMessageGet,
		SpanDBMessageListByConversation, SpanDBMessageDelete,
		SpanDBMessageGetByClientMessageID, SpanDBMessageSearchByConversation,
		SpanDBMessageListByTimeRange, SpanDBMessageRestore,
		SpanDBMessageDeleteByConversation, SpanDBMessageCountUnread,
		SpanDBMessageRestoreByConversation, SpanDBMessageListRecentByConversation,
		SpanDBUserUpdateCreate, SpanDBUserUpdateListByUser,
		SpanDBUserUpdateListByUserRange, SpanDBUserUpdateGetLatestSeq,
		SpanDBUserUpdateCleanupExpiredBefore,
		SpanDBRemoteCallingCreate, SpanDBRemoteCallingGetByID,
		SpanDBRemoteCallingGetPendingByConversation, SpanDBRemoteCallingGetPendingByCheckpoint,
		SpanDBRemoteCallingGetByCheckpoint, SpanDBRemoteCallingResolveResult,
		SpanDBRemoteCallingResolveError, SpanDBRemoteCallingCancelByCheckpoint,
		SpanDBRemoteCallingDeleteByConversation, SpanDBRemoteCallingDeleteByCheckpoint,
		SpanDBRemoteCallingCountPendingByCheckpoint, SpanDBRemoteCallingListExpired,
		SpanDBRemoteCallingMarkExpired, SpanDBRemoteCallingMarkExpiredByCheckpoint,
		SpanDBStoreSendMessage, SpanDBStoreTransaction, SpanDBStoreBeginTx,
		SpanDBStoreAutoMigrate, SpanDBStorePing, SpanDBStoreHealthCheck,
		// Redis layer spans
		SpanRedisConnectionAdd, SpanRedisConnectionGet,
		SpanRedisConnectionRemove, SpanRedisConnectionExists,
		SpanRedisConnectionUpdate, SpanRedisConnectionPatch,
		SpanRedisConnectionRefresh, SpanRedisConnectionListByUser,
		SpanRedisConnectionCountByUser, SpanRedisConnectionCountAll,
		SpanRedisConnectionRemoveByUser, SpanRedisConnectionPing,
		SpanRedisPendingSave, SpanRedisPendingList,
		SpanRedisPendingRemove, SpanRedisPendingUpdate,
		SpanRedisPendingRemoveByDevice,
		SpanRedisBroadcasterPublish, SpanRedisBroadcasterSubscribe,
	}
	for _, name := range names {
		assert.NotEmpty(t, name, "span name constant must not be empty")
	}
}

func TestSpanNames_Unique(t *testing.T) {
	names := []string{
		// WS / handler spans
		SpanWSConnection, SpanWSMessageReceive,
		SpanHandlerInvoke, SpanBrokerEnqueue, SpanBrokerProcess,
		SpanHandlerBroadcast, SpanAgentExecute, SpanAgentBuild,
		SpanAgentRun, SpanAgentLLMCall, SpanAgentToolCall,
		SpanAgentCheckpointSave, SpanAgentStream,
		// DB layer spans
		SpanDBConversationCreate, SpanDBConversationGet,
		SpanDBConversationGetByUsers, SpanDBConversationGetByUser,
		SpanDBConversationUpdate, SpanDBConversationDelete,
		SpanDBConversationRestore, SpanDBConversationUpdateLastMessage,
		SpanDBConversationSearchByTitle, SpanDBConversationGetUnscoped,
		SpanDBConversationUpdateLastRead, SpanDBConversationUpdateAgentStatus,
		SpanDBConversationClearAgentStatus, SpanDBConversationListStaleHITL,
		SpanDBMessageCreate, SpanDBMessageGet,
		SpanDBMessageListByConversation, SpanDBMessageDelete,
		SpanDBMessageGetByClientMessageID, SpanDBMessageSearchByConversation,
		SpanDBMessageListByTimeRange, SpanDBMessageRestore,
		SpanDBMessageDeleteByConversation, SpanDBMessageCountUnread,
		SpanDBMessageRestoreByConversation, SpanDBMessageListRecentByConversation,
		SpanDBUserUpdateCreate, SpanDBUserUpdateListByUser,
		SpanDBUserUpdateListByUserRange, SpanDBUserUpdateGetLatestSeq,
		SpanDBUserUpdateCleanupExpiredBefore,
		SpanDBRemoteCallingCreate, SpanDBRemoteCallingGetByID,
		SpanDBRemoteCallingGetPendingByConversation, SpanDBRemoteCallingGetPendingByCheckpoint,
		SpanDBRemoteCallingGetByCheckpoint, SpanDBRemoteCallingResolveResult,
		SpanDBRemoteCallingResolveError, SpanDBRemoteCallingCancelByCheckpoint,
		SpanDBRemoteCallingDeleteByConversation, SpanDBRemoteCallingDeleteByCheckpoint,
		SpanDBRemoteCallingCountPendingByCheckpoint, SpanDBRemoteCallingListExpired,
		SpanDBRemoteCallingMarkExpired, SpanDBRemoteCallingMarkExpiredByCheckpoint,
		SpanDBStoreSendMessage, SpanDBStoreTransaction, SpanDBStoreBeginTx,
		SpanDBStoreAutoMigrate, SpanDBStorePing, SpanDBStoreHealthCheck,
		// Redis layer spans
		SpanRedisConnectionAdd, SpanRedisConnectionGet,
		SpanRedisConnectionRemove, SpanRedisConnectionExists,
		SpanRedisConnectionUpdate, SpanRedisConnectionPatch,
		SpanRedisConnectionRefresh, SpanRedisConnectionListByUser,
		SpanRedisConnectionCountByUser, SpanRedisConnectionCountAll,
		SpanRedisConnectionRemoveByUser, SpanRedisConnectionPing,
		SpanRedisPendingSave, SpanRedisPendingList,
		SpanRedisPendingRemove, SpanRedisPendingUpdate,
		SpanRedisPendingRemoveByDevice,
		SpanRedisBroadcasterPublish, SpanRedisBroadcasterSubscribe,
	}
	seen := make(map[string]bool, len(names))
	for _, name := range names {
		assert.False(t, seen[name], "duplicate span name: %s", name)
		seen[name] = true
	}
}

func TestAttributeKeys_NotEmpty(t *testing.T) {
	keys := []string{
		AttrUserID, AttrDeviceID, AttrConnID, AttrMethod,
		AttrAgentID, AttrConversationID, AttrTaskType,
		AttrTaskID, AttrIteration, AttrToolName,
		AttrModel, AttrInputTokens, AttrOutputTokens,
		AttrTotalTokens, AttrDurationMs, AttrCheckpointID,
		AttrChunkCount, AttrTotalChars, AttrDebug,
		AttrSizeBytes, AttrTargetUserID, AttrTargetType,
	}
	for _, key := range keys {
		assert.NotEmpty(t, key, "attribute key constant must not be empty")
	}
}

func TestAttributeKeys_HaveXyncraPrefix(t *testing.T) {
	keys := []string{
		AttrUserID, AttrDeviceID, AttrConnID, AttrMethod,
		AttrAgentID, AttrConversationID, AttrTaskType,
		AttrTaskID, AttrIteration, AttrToolName,
		AttrModel, AttrInputTokens, AttrOutputTokens,
		AttrTotalTokens, AttrDurationMs, AttrCheckpointID,
		AttrChunkCount, AttrTotalChars, AttrDebug,
		AttrSizeBytes, AttrTargetUserID, AttrTargetType,
	}
	for _, key := range keys {
		assert.True(t, strings.HasPrefix(key, "xyncra."),
			"attribute key %q should have xyncra. prefix", key)
	}
}
