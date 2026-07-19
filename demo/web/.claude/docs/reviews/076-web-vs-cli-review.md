# Code Review Report: xyncra-client-web vs xyncra-client-cli Baseline

**Review ID:** 076-web-vs-cli-review
**Date:** 2026-07-19
**Scope:** `demo/web/packages/xyncra-client-web` (web) vs `demo/web/packages/xyncra-client-cli` (CLI baseline)
**Integration points:** `demo/web/src/`
**Reference docs:** `_step1-baseline.md`, `_step2-matrix.md`, `_step3-checklist.md`, `_step4-compliance.md`, `_step5-issues.md`, `_step5b-core-fix.md`, `PRODUCT_DECISIONS.md`

---

## 1. Overview

The CLI package was used as the trusted behavioral baseline (verified working). The web package was audited for behavioral parity, consistency, compliance with product decisions, and test coverage across 13 functional areas (message send/receive, delete, mark-read, conversation update, gap, typing, streaming, agent status, HITL, RPC, storage, websocket lifecycle, function registration).

A fix loop (steps 5 & 6) ran 2 rounds, then a cross-scope core fix (step 5b) resolved the last blocker. **Result: 5 bugs fixed (1 cross-scope), 0 remaining blockers.**

---

## 2. Feature Comparison Matrix Summary

Source: `_step2-matrix.md`. Verdict legend: consistent / deviation / reasonable-diff / missing.

| Verdict | Count | Items |
|---------|-------|-------|
| Consistent | 5 | delete message, streaming, agent status, storage, RPC (no-IPC) |
| Web-only (reasonable) | 4 | message→event, onGap empty, RPC via browser WS, factory-only storage |
| Deviation (bug) | 4 | mark-read no consumer, conversation events never emit, HITL broken, state-machine stuck |
| Missing / recorded | — | builtin functions not registered (EXP-3, deferred) |

Of 13 areas, 9 consistent or reasonably-differentiated; 4 deviations confirmed as bugs (all fixed).

---

## 3. Issues List

| ID | File:line | Difference vs CLI | Severity | Status |
|----|-----------|-------------------|----------|--------|
| BUG-1a | `ReactUpdateHandler.ts:139` | `hitl:question` missing `questionId/checkpointId/interruptId` (CLI `agent_resume` needs them) | bug P0 | fixed |
| BUG-1b | `useHITL.ts:79` | `answer()` sends only `{question_id, answer}` | bug P0 | fixed |
| BUG-1c | `HITLDialog.tsx:27` | uses `pendingQuestion.userId` as question id | bug P0 | fixed |
| BUG-1d | `useHITL.ts:62` | `pendingQuestion` drops recovery fields | bug P0 | fixed |
| BUG-2 | `ReactUpdateHandler.ts:70` + `useConversations.ts` | `read:updated` emitted but never consumed | bug P1 | fixed |
| BUG-3 | `ReactUpdateHandler.ts:76` | `conversation:updated`/`removed` never emitted | bug P1 | fixed (cross-scope, see §7) |
| BUG-4 | `XyncraProvider.tsx:311` | state machine stuck in `syncing` on empty library | bug P2 | fixed |
| CON-1 | `ReactUpdateHandler.ts:94` | typing not split agent/user (CLI uses `isAgentUser`) | consistency P2 | recorded (no fix) |
| CON-2 | `ReactUpdateHandler.ts:139` | timeout mapped directly to HITL question | consistency P2 | recorded (no fix) |
| EXP-1 | `XyncraProvider.tsx:218` | `error:rpc` has no subscribable hook | ux P2 | recorded (no fix) |
| EXP-2 | `useHITL.test.ts:85` / `hitl-flow.test.ts:85` | tests locked wrong HITL contract | ux P2 | fixed (tests corrected) |
| EXP-3 | (web package) | builtin `ping`/`get_device_info`/`get_time` not registered | ux P2 | recorded (architect TBD) |

**Pre-existing unrelated failures:** `AgentSelector.test.tsx` (2 tests) fail before and after this work; excluded from all counts.

---

## 4. Test Coverage Conclusion

Source: `_step3-checklist.md` + `_step5-issues.md` + `_step5b-core-fix.md`.

Before fix: 7 of 9 deviation points had missing or contract-locking tests (BUG-1×4, BUG-2, BUG-3 emit path, BUG-4, EXP-2).

Improvements in the fix loop:
- `ReactUpdateHandler.test.ts`: added `onConversation` action branches (created/updated/removed); corrected `hitl:question` payload assertions.
- `useHITL.test.ts` / `hitl-flow.test.ts`: corrected to assert full 5-field `agent_resume`; `pendingQuestion` now retains recovery fields.
- `HITLDialog.test.tsx`: asserts `questionId` passed (not `userId`).
- `useConversations` read-state tests: `read:updated` consumption + stringified-numeric id comparison.
- `XyncraProvider` lifecycle: empty-library 2s → `connected` self-heal.

Final test counts: web **195 tests, 193 pass, 2 fail** (pre-existing `AgentSelector.test.tsx` only). Core **155/155 pass**. CLI **84 pass**.

Remaining gaps (documented, not in scope): no `useErrorRpc` hook; builtin functions unregistered; concurrent-HITL and remote-delete race not covered.

---

## 5. Compliance Conclusion

Source: `_step4-compliance.md`. All checked product decisions are compliant — no new decisions required.

| Decision | Verdict | Note |
|----------|---------|------|
| TS-D-001 (no Node code in browser) | ✅ | zero `ws`/`node:`/`fs`/`ipc`/`daemon` imports |
| TS-D-003 (IndexedDB factory only) | ✅ | `getIDBFactory()` returns native `indexedDB`, no CRUD |
| TS-D-007 (no IPC layer) | ✅ | browser-native WebSocket bridge only |
| TS-D-008 (npm workspace subpackage) | ✅ | `@xyncra/client-web` consumed by demo app |
| TS-D-012 (`--db-path` = IDB db name) | ✅ | web has no flag; db name derived from deviceID |

DX notes (non-blocking, recorded): `ConnectionStatus` component/type name collision, Internal modules over-exposed, `error:rpc` not programmatically subscribable, `useRegisterFunction` params lack runtime validation.

---

## 6. Code Quality Check

**Typecheck** (`npm run typecheck` in `packages/xyncra-client-web`):
```
> tsc --noEmit
(passed, no errors)
```

**Lint:** Not applicable — the web package `package.json` defines no `lint` script (only `build`, `test`, `typecheck`, `tsc`). The repository-level `npm run lint` (biome) lives in the demo app root, not the web package. Per task scope (web package only), lint was skipped; no lint errors are attributable to this fix.

No new type or format errors introduced by the fixes.

---

## 7. Cross-scope Change Note

BUG-3 required a core interface change (recorded in `_step5b-core-fix.md`):
- `xyncra-client-core/src/interfaces.ts`: `onConversation(conversation, action: ConversationAction)` — new `ConversationAction = 'created' | 'updated' | 'removed'`, backward-compatible trailing param.
- `sync-manager.ts`: 4 call sites now pass correct action (delete→`removed`, create→`created`, else `updated`).
- CLI `update-handler.ts`: prints action prefix (baseline behavior unchanged).
- Web `ReactUpdateHandler.ts`: dispatches `conversation:added`/`updated`/`removed` by action; `useConversations` subscriptions already existed and now take effect.

cli and web were synchronized in the same change. **BUG-3 status: unblocked / fixed.**

---

## 8. Summary

- **5 bugs fixed** (BUG-1a–1d, BUG-2, BUG-3 cross-scope, BUG-4) + EXP-2 test-lock corrected.
- **0 remaining blockers.**
- **typecheck: pass.** **lint: skipped** (no web-package lint script).
- **2 pre-existing failures** (`AgentSelector.test.tsx`) excluded.
