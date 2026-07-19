import { useRegisterFunctions } from '@xyncra/client-web';
import type { FunctionEntry } from '@xyncra/client-web';
import {
  createClickFunction,
  createInputFunction,
} from '../utils/factory';

const chatbotFunctions: FunctionEntry[] = [
  createInputFunction(
    'pg_chatbot_send_input',
    '在聊天机器人页发送消息到输入框',
    '.ant-input',
    ['page:chatbot'],
  ),
  createClickFunction(
    'pg_chatbot_new_conversation',
    '在聊天机器人页创建新对话',
    '.ant-btn:contains("New Conversation")',
    ['page:chatbot'],
  ),
  createClickFunction(
    'pg_chatbot_conversation_item_1',
    '在聊天机器人页选择第1个历史对话',
    '.ant-conversations .ant-conversation-item:nth-child(1)',
    ['page:chatbot'],
  ),
  createClickFunction(
    'pg_chatbot_conversation_item_2',
    '在聊天机器人页选择第2个历史对话',
    '.ant-conversations .ant-conversation-item:nth-child(2)',
    ['page:chatbot'],
  ),
  createClickFunction(
    'pg_chatbot_conversation_item_3',
    '在聊天机器人页选择第3个历史对话',
    '.ant-conversations .ant-conversation-item:nth-child(3)',
    ['page:chatbot'],
  ),
  createClickFunction(
    'pg_chatbot_conversation_item_4',
    '在聊天机器人页选择第4个历史对话',
    '.ant-conversations .ant-conversation-item:nth-child(4)',
    ['page:chatbot'],
  ),
  createClickFunction(
    'pg_chatbot_conversation_item_5',
    '在聊天机器人页选择第5个历史对话',
    '.ant-conversations .ant-conversation-item:nth-child(5)',
    ['page:chatbot'],
  ),
  createClickFunction(
    'pg_chatbot_conversation_item_6',
    '在聊天机器人页选择第6个历史对话',
    '.ant-conversations .ant-conversation-item:nth-child(6)',
    ['page:chatbot'],
  ),
  createClickFunction(
    'pg_chatbot_conversation_item_7',
    '在聊天机器人页选择第7个历史对话',
    '.ant-conversations .ant-conversation-item:nth-child(7)',
    ['page:chatbot'],
  ),
  createClickFunction(
    'pg_chatbot_conversation_item_8',
    '在聊天机器人页选择第8个历史对话',
    '.ant-conversations .ant-conversation-item:nth-child(8)',
    ['page:chatbot'],
  ),
  createClickFunction(
    'pg_chatbot_conversation_item_9',
    '在聊天机器人页选择第9个历史对话',
    '.ant-conversations .ant-conversation-item:nth-child(9)',
    ['page:chatbot'],
  ),
  createClickFunction(
    'pg_chatbot_conversation_delete',
    '在聊天机器人页删除当前选中对话',
    '.ant-conversations .ant-conversation-item-actions .ant-btn',
    ['page:chatbot'],
  ),
];

export function ChatbotFunctions() {
  useRegisterFunctions(chatbotFunctions);
  return null;
}
