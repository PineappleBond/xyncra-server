package tracing

// Span names used throughout Xyncra Server.
const (
	SpanWSConnection        = "ws.connection"
	SpanWSMessageReceive    = "ws.message.receive"
	SpanWSMessageSend       = "ws.message.send"
	SpanHandlerInvoke       = "handler.invoke"
	SpanBrokerEnqueue       = "handler.broker.enqueue"
	SpanBrokerProcess       = "mq.process"
	SpanHandlerBroadcast    = "handler.broadcast"
	SpanAgentExecute        = "agent.execute"
	SpanAgentBuild          = "agent.build"
	SpanAgentRun            = "agent.run"
	SpanAgentLLMCall        = "agent.llm.call"
	SpanAgentToolCall       = "agent.tool.call"
	SpanAgentCheckpointSave = "agent.checkpoint.save"
	SpanAgentStream         = "agent.stream"
)

// Attribute keys for span attributes.
const (
	AttrUserID         = "xyncra.user.id"
	AttrDeviceID       = "xyncra.device.id"
	AttrConnID         = "xyncra.connection.id"
	AttrMethod         = "xyncra.method"
	AttrAgentID        = "xyncra.agent.id"
	AttrConversationID = "xyncra.conversation.id"
	AttrTaskType       = "xyncra.task.type"
	AttrTaskID         = "xyncra.task.id"
	AttrIteration      = "xyncra.iteration"
	AttrToolName       = "xyncra.tool.name"
	AttrModel          = "xyncra.llm.model"
	AttrInputTokens    = "xyncra.llm.input_tokens"
	AttrOutputTokens   = "xyncra.llm.output_tokens"
	AttrTotalTokens    = "xyncra.llm.total_tokens"
	AttrDurationMs     = "xyncra.duration_ms"
	AttrCheckpointID   = "xyncra.checkpoint.id"
	AttrChunkCount     = "xyncra.chunk_count"
	AttrTotalChars     = "xyncra.total_chars"
	AttrDebug          = "xyncra.debug"
	AttrSizeBytes      = "xyncra.size_bytes"
	AttrTargetUserID   = "xyncra.target_user_id"
	AttrTargetType     = "xyncra.target_type"
)
