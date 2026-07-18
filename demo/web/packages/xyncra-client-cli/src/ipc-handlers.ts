/**
 * IPC method handler registration.
 *
 * Binds JSON-RPC method handlers on the IPC server that forward requests
 * to the corresponding XyncraClient methods.
 *
 * Mirrors Go registerIPCHandlers in internal/cli/listen.go.
 *
 * @module
 */

import type { XyncraClient } from '@xyncra/client-core';
import { ClientError } from '@xyncra/client-core';
import type { IPCServer, IPCRequest, IPCResponse } from './ipc.js';
import { newIPCResponse, newIPCErrorResponse, ERR_SERVER, ERR_INVALID_PARAMS } from './ipc.js';

/**
 * Register all IPC method handlers on the server.
 * Each handler parses params, calls XyncraClient, and returns the result.
 */
export function registerIPCHandlers(
  server: IPCServer,
  client: XyncraClient,
  userID: string,
): void {
  // send_message
  server.register('send_message', async (req: IPCRequest): Promise<IPCResponse> => {
    const params = req.params as Record<string, unknown> | undefined;
    if (!params?.conversation_id) {
      return newIPCErrorResponse(req.id, ERR_INVALID_PARAMS, 'conversation_id is required');
    }
    try {
      const result = await client.sendMessage(
        params.conversation_id as string,
        (params.content as string) ?? '',
        params.client_message_id as string | undefined,
        params.reply_to as number | undefined,
      );
      return newIPCResponse(req.id, result);
    } catch (err) {
      return clientErrorToIPCResponse(req.id, err);
    }
  });

  // sync_updates
  server.register('sync_updates', async (req: IPCRequest): Promise<IPCResponse> => {
    try {
      await client.fullSync();
      return newIPCResponse(req.id, { status: 'ok' });
    } catch (err) {
      return clientErrorToIPCResponse(req.id, err);
    }
  });

  // create_conversation
  server.register('create_conversation', async (req: IPCRequest): Promise<IPCResponse> => {
    const params = req.params as Record<string, unknown> | undefined;
    if (!params?.user_id2) {
      return newIPCErrorResponse(req.id, ERR_INVALID_PARAMS, 'user_id2 is required');
    }
    try {
      const result = await client.createConversation(
        params.user_id2 as string,
        (params.title as string) ?? '',
      );
      return newIPCResponse(req.id, result);
    } catch (err) {
      return clientErrorToIPCResponse(req.id, err);
    }
  });

  // delete_conversation
  server.register('delete_conversation', async (req: IPCRequest): Promise<IPCResponse> => {
    const params = req.params as Record<string, unknown> | undefined;
    if (!params?.conversation_id) {
      return newIPCErrorResponse(req.id, ERR_INVALID_PARAMS, 'conversation_id is required');
    }
    try {
      const result = await client.deleteConversation(params.conversation_id as string);
      return newIPCResponse(req.id, result);
    } catch (err) {
      return clientErrorToIPCResponse(req.id, err);
    }
  });

  // restore_conversation
  server.register('restore_conversation', async (req: IPCRequest): Promise<IPCResponse> => {
    const params = req.params as Record<string, unknown> | undefined;
    if (!params?.conversation_id) {
      return newIPCErrorResponse(req.id, ERR_INVALID_PARAMS, 'conversation_id is required');
    }
    try {
      const result = await client.restoreConversation(params.conversation_id as string);
      return newIPCResponse(req.id, result);
    } catch (err) {
      return clientErrorToIPCResponse(req.id, err);
    }
  });

  // get_conversation (local DB read via XyncraClient)
  server.register('get_conversation', async (req: IPCRequest): Promise<IPCResponse> => {
    const params = req.params as Record<string, unknown> | undefined;
    if (!params?.conversation_id) {
      return newIPCErrorResponse(req.id, ERR_INVALID_PARAMS, 'conversation_id is required');
    }
    try {
      const result = await client.getConversation(params.conversation_id as string);
      return newIPCResponse(req.id, result);
    } catch (err) {
      return clientErrorToIPCResponse(req.id, err);
    }
  });

  // list_conversations (local DB read via XyncraClient)
  server.register('list_conversations', async (req: IPCRequest): Promise<IPCResponse> => {
    const params = (req.params as Record<string, unknown> | undefined) ?? {};
    try {
      const result = await client.listConversations(
        params.offset as number | undefined,
        params.limit as number | undefined,
      );
      return newIPCResponse(req.id, result);
    } catch (err) {
      return clientErrorToIPCResponse(req.id, err);
    }
  });

  // get_messages (local DB read via XyncraClient)
  server.register('get_messages', async (req: IPCRequest): Promise<IPCResponse> => {
    const params = req.params as Record<string, unknown> | undefined;
    if (!params?.conversation_id) {
      return newIPCErrorResponse(req.id, ERR_INVALID_PARAMS, 'conversation_id is required');
    }
    try {
      const result = await client.getMessages(
        params.conversation_id as string,
        params.after_message_id as number | undefined,
        params.limit as number | undefined,
      );
      return newIPCResponse(req.id, result);
    } catch (err) {
      return clientErrorToIPCResponse(req.id, err);
    }
  });

  // search_messages (local DB read via XyncraClient)
  server.register('search_messages', async (req: IPCRequest): Promise<IPCResponse> => {
    const params = req.params as Record<string, unknown> | undefined;
    if (!params?.conversation_id || !params?.query) {
      return newIPCErrorResponse(req.id, ERR_INVALID_PARAMS, 'conversation_id and query are required');
    }
    try {
      const result = await client.searchMessages(
        params.conversation_id as string,
        params.query as string,
        params.after_message_id as number | undefined,
        params.limit as number | undefined,
      );
      return newIPCResponse(req.id, result);
    } catch (err) {
      return clientErrorToIPCResponse(req.id, err);
    }
  });

  // delete_message
  server.register('delete_message', async (req: IPCRequest): Promise<IPCResponse> => {
    const params = req.params as Record<string, unknown> | undefined;
    if (!params?.message_id) {
      return newIPCErrorResponse(req.id, ERR_INVALID_PARAMS, 'message_id is required');
    }
    try {
      await client.deleteMessage(params.message_id as string);
      return newIPCResponse(req.id, null);
    } catch (err) {
      return clientErrorToIPCResponse(req.id, err);
    }
  });

  // mark_as_read
  server.register('mark_as_read', async (req: IPCRequest): Promise<IPCResponse> => {
    const params = req.params as Record<string, unknown> | undefined;
    if (!params?.conversation_id) {
      return newIPCErrorResponse(req.id, ERR_INVALID_PARAMS, 'conversation_id is required');
    }
    try {
      await client.markAsRead(
        params.conversation_id as string,
        params.message_id as number,
      );
      // Return a simple success response (server returns last_read_message_id).
      return newIPCResponse(req.id, { last_read_message_id: params.message_id });
    } catch (err) {
      return clientErrorToIPCResponse(req.id, err);
    }
  });

  // set_typing (fire-and-forget)
  server.register('set_typing', async (req: IPCRequest): Promise<IPCResponse> => {
    const params = req.params as Record<string, unknown> | undefined;
    if (!params?.conversation_id) {
      return newIPCErrorResponse(req.id, ERR_INVALID_PARAMS, 'conversation_id is required');
    }
    try {
      // set_typing is a direct RPC call, not exposed as a XyncraClient method.
      // We use the internal call mechanism.
      const result = await (client as unknown as { call: (m: string, p: unknown) => Promise<unknown> }).call('set_typing', {
        conversation_id: params.conversation_id,
        is_typing: params.is_typing,
      });
      return newIPCResponse(req.id, result);
    } catch (err) {
      return clientErrorToIPCResponse(req.id, err);
    }
  });

  // stream_text (fire-and-forget)
  server.register('stream_text', async (req: IPCRequest): Promise<IPCResponse> => {
    const params = req.params as Record<string, unknown> | undefined;
    if (!params?.conversation_id) {
      return newIPCErrorResponse(req.id, ERR_INVALID_PARAMS, 'conversation_id is required');
    }
    try {
      const result = await (client as unknown as { call: (m: string, p: unknown) => Promise<unknown> }).call('stream_text', {
        conversation_id: params.conversation_id,
        stream_id: params.stream_id,
        text: params.text,
        is_done: params.is_done,
      });
      return newIPCResponse(req.id, result);
    } catch (err) {
      return clientErrorToIPCResponse(req.id, err);
    }
  });

  // agent_resume
  server.register('agent_resume', async (req: IPCRequest): Promise<IPCResponse> => {
    const params = req.params as Record<string, unknown> | undefined;
    if (!params?.conversation_id) {
      return newIPCErrorResponse(req.id, ERR_INVALID_PARAMS, 'conversation_id is required');
    }
    try {
      const result = await (client as unknown as { call: (m: string, p: unknown) => Promise<unknown> }).call('agent_resume', params);
      return newIPCResponse(req.id, result);
    } catch (err) {
      return clientErrorToIPCResponse(req.id, err);
    }
  });

  // reload_agents
  server.register('reload_agents', async (req: IPCRequest): Promise<IPCResponse> => {
    try {
      const result = await (client as unknown as { call: (m: string, p: unknown) => Promise<unknown> }).call('reload_agents', null);
      return newIPCResponse(req.id, result);
    } catch (err) {
      return clientErrorToIPCResponse(req.id, err);
    }
  });
}

/** Convert a ClientError (or generic Error) to an IPC error response. */
function clientErrorToIPCResponse(id: string, err: unknown): IPCResponse {
  if (err instanceof ClientError) {
    return newIPCErrorResponse(id, err.code, err.message);
  }
  if (err instanceof Error) {
    return newIPCErrorResponse(id, ERR_SERVER, err.message);
  }
  return newIPCErrorResponse(id, ERR_SERVER, String(err));
}
