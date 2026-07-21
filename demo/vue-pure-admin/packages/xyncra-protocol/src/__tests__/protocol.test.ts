/**
 * @file Comprehensive type validation tests for @xyncra/protocol package.
 * Tests cover PackageType, UpdateType, ResponseCode, ErrorCode, HandlerError,
 * factory functions, and JSON serialization round-trips.
 */

import type {
  FunctionInfo,
  Package,
  PackageDataRequest,
  PackageDataResponse,
  PackageDataUpdate,
  PackageDataUpdates,
  ReturnInfo,
} from '../index';
import {
  ErrorCode,
  HandlerError,
  isEphemeralUpdateType,
  newDuplicateError,
  newHandlerError,
  newInternalError,
  newNotFoundError,
  newPermissionDeniedError,
  newValidationError,
  PackageType,
  packageTypeName,
  ResponseCode,
  responseCodeName,
  UpdateType,
  wrapError,
} from '../index';

describe('PackageType', () => {
  it('should have correct numeric values', () => {
    expect(PackageType.Request).toBe(0);
    expect(PackageType.Response).toBe(1);
    expect(PackageType.Updates).toBe(2);
  });

  it('should return correct names via packageTypeName()', () => {
    expect(packageTypeName(PackageType.Request)).toBe('Request');
    expect(packageTypeName(PackageType.Response)).toBe('Response');
    expect(packageTypeName(PackageType.Updates)).toBe('Updates');
  });

  it('should return Unknown for invalid PackageType', () => {
    expect(packageTypeName(99 as PackageType)).toBe('Unknown(99)');
  });
});

describe('UpdateType', () => {
  it('should have correct string values', () => {
    expect(UpdateType.Message).toBe('message');
    expect(UpdateType.DeleteMessage).toBe('delete_message');
    expect(UpdateType.MarkRead).toBe('mark_read');
    expect(UpdateType.Conversation).toBe('conversation');
    expect(UpdateType.Gap).toBe('gap');
    expect(UpdateType.Typing).toBe('typing');
    expect(UpdateType.Streaming).toBe('streaming');
    expect(UpdateType.AgentStatus).toBe('agent_status');
    expect(UpdateType.AgentTimeout).toBe('agent_timeout');
  });

  describe('isEphemeralUpdateType()', () => {
    it('should return true for ephemeral types', () => {
      expect(isEphemeralUpdateType(UpdateType.Typing)).toBe(true);
      expect(isEphemeralUpdateType(UpdateType.Streaming)).toBe(true);
      expect(isEphemeralUpdateType(UpdateType.AgentStatus)).toBe(true);
      expect(isEphemeralUpdateType(UpdateType.AgentTimeout)).toBe(true);
    });

    it('should return false for non-ephemeral types', () => {
      expect(isEphemeralUpdateType(UpdateType.Message)).toBe(false);
      expect(isEphemeralUpdateType(UpdateType.DeleteMessage)).toBe(false);
      expect(isEphemeralUpdateType(UpdateType.MarkRead)).toBe(false);
      expect(isEphemeralUpdateType(UpdateType.Conversation)).toBe(false);
      expect(isEphemeralUpdateType(UpdateType.Gap)).toBe(false);
    });

    it('should return false for unknown update type values', () => {
      expect(isEphemeralUpdateType('unknown_type' as UpdateType)).toBe(false);
      expect(isEphemeralUpdateType('' as UpdateType)).toBe(false);
    });
  });

  it('should have distinct agent ephemeral types', () => {
    const agentTypes = [UpdateType.AgentStatus, UpdateType.AgentTimeout];
    const seen = new Set<string>();
    for (const type of agentTypes) {
      expect(seen.has(type)).toBe(false);
      seen.add(type);
    }
  });
});

describe('Package serialization', () => {
  it('should round-trip a complete Package object', () => {
    const pkg: Package = {
      version: 1,
      type: PackageType.Request,
      data: { id: 'test-1', method: 'ping', params: null },
    };

    const json = JSON.stringify(pkg);
    const parsed = JSON.parse(json) as Package;

    expect(parsed.version).toBe(1);
    expect(parsed.type).toBe(PackageType.Request);
    expect(parsed.data).toEqual({ id: 'test-1', method: 'ping', params: null });
  });
});

describe('PackageDataRequest serialization', () => {
  it('should round-trip with optional fields', () => {
    const req: PackageDataRequest = {
      id: 'req-123',
      method: 'chat.send_message',
      params: { text: 'hello' },
      idempotency_key: 'req-123',
      seq: 42,
    };

    const json = JSON.stringify(req);
    const parsed = JSON.parse(json) as PackageDataRequest;

    expect(parsed.id).toBe('req-123');
    expect(parsed.method).toBe('chat.send_message');
    expect(parsed.params).toEqual({ text: 'hello' });
    expect(parsed.idempotency_key).toBe('req-123');
    expect(parsed.seq).toBe(42);
  });

  it('should omit optional fields when not set (omitempty behavior)', () => {
    const req: PackageDataRequest = {
      id: 'req-456',
      method: 'ping',
      params: null,
    };

    const json = JSON.stringify(req);
    const parsed = JSON.parse(json) as Record<string, unknown>;

    expect(parsed).toHaveProperty('id');
    expect(parsed).toHaveProperty('method');
    expect(parsed).toHaveProperty('params');
    expect(parsed).not.toHaveProperty('idempotency_key');
    expect(parsed).not.toHaveProperty('seq');
  });

  it('should deserialize old format JSON without optional fields', () => {
    const oldJson = '{"id":"1","method":"ping","params":null}';
    const parsed = JSON.parse(oldJson) as PackageDataRequest;

    expect(parsed.id).toBe('1');
    expect(parsed.method).toBe('ping');
    expect(parsed.params).toBeNull();
    expect(parsed.idempotency_key).toBeUndefined();
    expect(parsed.seq).toBeUndefined();
  });
});

describe('PackageDataResponse serialization', () => {
  it('should round-trip response fields', () => {
    const res: PackageDataResponse = {
      id: 'res-789',
      code: ResponseCode.OK,
      msg: 'success',
      data: { result: 'pong' },
    };

    const json = JSON.stringify(res);
    const parsed = JSON.parse(json) as PackageDataResponse;

    expect(parsed.id).toBe('res-789');
    expect(parsed.code).toBe(ResponseCode.OK);
    expect(parsed.msg).toBe('success');
    expect(parsed.data).toEqual({ result: 'pong' });
  });

  it('should handle error response codes', () => {
    const res: PackageDataResponse = {
      id: 'res-err',
      code: ErrorCode.NotFound,
      msg: 'user not found',
      data: null,
    };

    const json = JSON.stringify(res);
    const parsed = JSON.parse(json) as PackageDataResponse;

    expect(parsed.code).toBe(ErrorCode.NotFound);
    expect(parsed.msg).toBe('user not found');
    expect(parsed.data).toBeNull();
  });
});

describe('PackageDataUpdate serialization', () => {
  it('should round-trip update fields', () => {
    const update: PackageDataUpdate = {
      seq: 100,
      type: UpdateType.Message,
      payload: { message_id: 'm1', text: 'hello' },
      created_at: '2026-07-18T10:00:00Z',
    };

    const json = JSON.stringify(update);
    const parsed = JSON.parse(json) as PackageDataUpdate;

    expect(parsed.seq).toBe(100);
    expect(parsed.type).toBe('message');
    expect(parsed.payload).toEqual({ message_id: 'm1', text: 'hello' });
    expect(parsed.created_at).toBe('2026-07-18T10:00:00Z');
  });

  it('should handle optional created_at field', () => {
    const update: PackageDataUpdate = {
      seq: 0,
      type: UpdateType.Typing,
      payload: { user_id: 'u1' },
    };

    const json = JSON.stringify(update);
    const parsed = JSON.parse(json) as PackageDataUpdate;

    expect(parsed.seq).toBe(0);
    expect(parsed.type).toBe('typing');
    expect(parsed.created_at).toBeUndefined();
  });

  it('should round-trip PackageDataUpdates wrapper', () => {
    const updates: PackageDataUpdates = {
      updates: [
        { seq: 1, type: UpdateType.Message, payload: { id: 'm1' } },
        { seq: 2, type: UpdateType.DeleteMessage, payload: { id: 'm2' } },
      ],
    };

    const json = JSON.stringify(updates);
    const parsed = JSON.parse(json) as PackageDataUpdates;

    expect(parsed.updates).toHaveLength(2);
    expect(parsed.updates[0].seq).toBe(1);
    expect(parsed.updates[1].seq).toBe(2);
  });

  it('should round-trip empty updates array', () => {
    const updates: PackageDataUpdates = { updates: [] };

    const json = JSON.stringify(updates);
    const parsed = JSON.parse(json) as PackageDataUpdates;

    expect(parsed.updates).toHaveLength(0);
  });
});

describe('ResponseCode and ErrorCode', () => {
  it('should have correct numeric values', () => {
    expect(ResponseCode.OK).toBe(0);
    expect(ResponseCode.Error).toBe(-1);
    expect(ErrorCode.ValidationError).toBe(-100);
    expect(ErrorCode.NotFound).toBe(-101);
    expect(ErrorCode.Duplicate).toBe(-102);
    expect(ErrorCode.PermissionDenied).toBe(-200);
    expect(ErrorCode.Forbidden).toBe(-201);
    expect(ErrorCode.InternalError).toBe(-300);
    expect(ErrorCode.Unavailable).toBe(-301);
  });

  it('should return correct names via responseCodeName()', () => {
    expect(responseCodeName(ResponseCode.OK)).toBe('OK');
    expect(responseCodeName(ResponseCode.Error)).toBe('Error');
    expect(responseCodeName(ErrorCode.ValidationError)).toBe('ValidationError');
    expect(responseCodeName(ErrorCode.NotFound)).toBe('NotFound');
    expect(responseCodeName(ErrorCode.Duplicate)).toBe('Duplicate');
    expect(responseCodeName(ErrorCode.PermissionDenied)).toBe(
      'PermissionDenied',
    );
    expect(responseCodeName(ErrorCode.Forbidden)).toBe('Forbidden');
    expect(responseCodeName(ErrorCode.InternalError)).toBe('InternalError');
    expect(responseCodeName(ErrorCode.Unavailable)).toBe('Unavailable');
  });

  it('should return Unknown for invalid codes', () => {
    expect(responseCodeName(999 as ResponseCode)).toBe('Unknown(999)');
  });
});

describe('HandlerError', () => {
  it('should return message only when no cause', () => {
    const err = new HandlerError(ErrorCode.NotFound, 'user not found');
    expect(err.message).toBe('user not found');
    expect(err.toString()).toContain('user not found');
    expect(err.cause).toBeUndefined();
  });

  it('should include cause message when cause is provided', () => {
    const cause = new Error('database timeout');
    const err = new HandlerError(ErrorCode.InternalError, 'internal error', {
      cause,
    });
    expect(err.message).toBe('internal error');
    expect(err.cause).toBe(cause);
  });

  it('should be instance of Error and HandlerError', () => {
    const err = new HandlerError(ErrorCode.NotFound, 'not found');
    expect(err).toBeInstanceOf(Error);
    expect(err).toBeInstanceOf(HandlerError);
  });

  it('unwrap() should return cause when present', () => {
    const cause = new Error('db timeout');
    const err = new HandlerError(ErrorCode.InternalError, 'failed', { cause });
    expect(err.unwrap()).toBe(cause);
  });

  it('unwrap() should return undefined when no cause', () => {
    const err = new HandlerError(ErrorCode.NotFound, 'not found');
    expect(err.unwrap()).toBeUndefined();
  });

  it('should have correct name property', () => {
    const err = new HandlerError(ErrorCode.NotFound, 'not found');
    expect(err.name).toBe('HandlerError');
  });

  it('should preserve code property', () => {
    const err = new HandlerError(ErrorCode.InternalError, 'error');
    expect(err.code).toBe(ErrorCode.InternalError);
  });
});

describe('Factory functions', () => {
  describe('newHandlerError', () => {
    it('should create error with correct code and message', () => {
      const err = newHandlerError(ErrorCode.NotFound, 'resource missing');
      expect(err.code).toBe(ErrorCode.NotFound);
      expect(err.message).toBe('resource missing');
      expect(err).toBeInstanceOf(HandlerError);
    });

    it('should accept optional cause', () => {
      const cause = new Error('underlying error');
      const err = newHandlerError(ErrorCode.InternalError, 'failed', cause);
      expect(err.cause).toBe(cause);
    });

    it('should handle empty string message', () => {
      const err = newHandlerError(ErrorCode.ValidationError, '');
      expect(err.message).toBe('');
    });
  });

  describe('wrapError', () => {
    it('should wrap Error with its message', () => {
      const original = new Error('database connection failed');
      const err = wrapError(ErrorCode.Unavailable, original);
      expect(err.code).toBe(ErrorCode.Unavailable);
      expect(err.message).toBe('database connection failed');
      expect(err.cause).toBe(original);
    });

    it('should convert non-Error to string', () => {
      const err = wrapError(ErrorCode.InternalError, 'string error');
      expect(err.message).toBe('string error');
      expect(err.cause).toBe('string error');
    });
  });

  describe('specific error factories', () => {
    it('newValidationError should return code -100', () => {
      const err = newValidationError('invalid input');
      expect(err.code).toBe(ErrorCode.ValidationError);
      expect(err.message).toBe('invalid input');
    });

    it('newNotFoundError should return code -101', () => {
      const err = newNotFoundError('not found');
      expect(err.code).toBe(ErrorCode.NotFound);
      expect(err.message).toBe('not found');
    });

    it('newDuplicateError should return code -102', () => {
      const err = newDuplicateError('already exists');
      expect(err.code).toBe(ErrorCode.Duplicate);
      expect(err.message).toBe('already exists');
    });

    it('newPermissionDeniedError should return code -200', () => {
      const err = newPermissionDeniedError('access denied');
      expect(err.code).toBe(ErrorCode.PermissionDenied);
      expect(err.message).toBe('access denied');
    });

    it('newInternalError should return code -300', () => {
      const cause = new Error('unexpected failure');
      const err = newInternalError(cause);
      expect(err.code).toBe(ErrorCode.InternalError);
      expect(err.message).toBe('unexpected failure');
      expect(err.cause).toBe(cause);
    });
  });
});

describe('FunctionInfo serialization', () => {
  it('should round-trip with all fields set', () => {
    const info: FunctionInfo = {
      name: 'send_message',
      description: 'Send a chat message',
      parameters: {
        type: 'object',
        properties: { text: { type: 'string' } },
        required: ['text'],
      },
      returns: { type: 'string', description: 'The sent message ID' },
      tags: ['chat', 'messaging'],
      timeout_ms: 5000,
    };

    const json = JSON.stringify(info);
    const parsed = JSON.parse(json) as FunctionInfo;

    expect(parsed.name).toBe('send_message');
    expect(parsed.description).toBe('Send a chat message');
    expect(parsed.parameters).toEqual({
      type: 'object',
      properties: { text: { type: 'string' } },
      required: ['text'],
    });
    expect(parsed.returns).toEqual({
      type: 'string',
      description: 'The sent message ID',
    });
    expect(parsed.tags).toEqual(['chat', 'messaging']);
    expect(parsed.timeout_ms).toBe(5000);
  });

  it('should round-trip with only name set (minimal fields)', () => {
    const info: FunctionInfo = { name: 'ping' };

    const json = JSON.stringify(info);
    const parsed = JSON.parse(json) as FunctionInfo;

    expect(parsed.name).toBe('ping');
    expect(parsed.description).toBeUndefined();
    expect(parsed.parameters).toBeUndefined();
    expect(parsed.returns).toBeUndefined();
    expect(parsed.tags).toBeUndefined();
    expect(parsed.timeout_ms).toBeUndefined();
  });

  it('should preserve nested ReturnInfo serialization', () => {
    const returns: ReturnInfo = {
      type: 'object',
      description: 'A complex result',
    };
    const info: FunctionInfo = { name: 'query', returns };

    const json = JSON.stringify(info);
    const parsed = JSON.parse(json) as FunctionInfo;

    expect(parsed.returns).toEqual(returns);
    expect(parsed.returns?.type).toBe('object');
    expect(parsed.returns?.description).toBe('A complex result');
  });
});
