package handler

import (
	"github.com/PineappleBond/xyncra-server/internal/mq"
	"github.com/PineappleBond/xyncra-server/internal/server"
	"github.com/PineappleBond/xyncra-server/internal/store"
	"github.com/PineappleBond/xyncra-server/pkg/protocol"
)

// Dependencies holds all dependencies required by handlers.
type Dependencies struct {
	ConnStore   server.ConnectionStore
	Store       store.StoreAPI
	Broker      mq.Broker
	BroadcastFn func(userID string, updates *protocol.PackageDataUpdates) error
}

// RegisterAll registers all method handlers on the given DefaultMessageHandler.
// It registers:
//   - "heartbeat": heartbeat handler (passive TTL renewal)
//   - "send_message": message send handler (persistence + MQ fanout)
//   - "sync_updates": incremental update fetch handler
//   - "create_conversation": find-or-create conversation handler (D-011)
//   - "list_conversations": list conversations for the authenticated user
//   - "get_messages": fetch messages for a conversation
//   - "search_messages": full-text search across messages
//   - "get_conversation": get a single conversation with unread count
//   - "delete_conversation": cascade soft delete conversation and messages (D-013)
//   - "restore_conversation": cascade restore conversation and messages (D-015)
//   - "delete_message": sender-only message deletion (D-014)
//   - "mark_as_read": update read cursor with MAX semantics (D-012)
//   - "set_typing": ephemeral typing indicator broadcast (Seq=0, no persistence)
//
// Note: mq_send_message is a task handler (processed by the MQ worker), not a
// method handler (invoked by client RPC), and is therefore not registered here.
func RegisterAll(h *server.DefaultMessageHandler, deps Dependencies) {
	h.RegisterMethod("heartbeat", NewHeartbeatHandler(deps.ConnStore))
	h.RegisterMethod("send_message", NewSendMessageHandler(deps.Store, deps.Broker))
	h.RegisterMethod("sync_updates", NewSyncUpdatesHandler(deps.Store))
	h.RegisterMethod("create_conversation", NewCreateConversationHandler(deps.Store, deps.Broker))
	h.RegisterMethod("list_conversations", NewListConversationsHandler(deps.Store))
	h.RegisterMethod("get_messages", NewGetMessagesHandler(deps.Store))
	h.RegisterMethod("search_messages", NewSearchMessagesHandler(deps.Store))
	h.RegisterMethod("get_conversation", NewGetConversationHandler(deps.Store))
	h.RegisterMethod("delete_conversation", NewDeleteConversationHandler(deps.Store, deps.Broker))
	h.RegisterMethod("restore_conversation", NewRestoreConversationHandler(deps.Store, deps.Broker))
	h.RegisterMethod("delete_message", NewDeleteMessageHandler(deps.Store, deps.Broker))
	h.RegisterMethod("mark_as_read", NewMarkAsReadHandler(deps.Store, deps.Broker))
	h.RegisterMethod("set_typing", NewSetTypingHandler(deps.Store, deps.BroadcastFn))
}
