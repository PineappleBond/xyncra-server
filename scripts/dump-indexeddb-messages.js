/**
 * 浏览器控制台脚本：打印 IndexedDB 中 messages 表的所有数据
 *
 * 使用方法：
 * 1. 打开浏览器开发者工具 (F12)
 * 2. 切换到 Console 标签
 * 3. 粘贴此脚本并执行
 *
 * 输出：JSON 格式的 messages 数组，可复制保存
 */

(async function dumpMessages() {
  console.log('🔍 正在读取 IndexedDB messages 表...\n');

  try {
    // 打开 xyncra 数据库
    const db = await new Promise((resolve, reject) => {
      const request = indexedDB.open('xyncra-agent-c2993800-9274-48c1-8b0d-454cbe66d743');

      request.onerror = () => {
        reject(new Error(`无法打开数据库: ${request.error?.message}`));
      };

      request.onsuccess = () => {
        resolve(request.result);
      };

      request.onupgradeneeded = (event) => {
        // 如果数据库不存在，会触发这个事件
        console.warn('⚠️ 数据库版本升级中，可能数据库尚未创建');
      };
    });

    // 检查 messages 表是否存在
    if (!db.objectStoreNames.contains('messages')) {
      console.error('❌ 错误：messages 表不存在');
      console.log('可用的表:', Array.from(db.objectStoreNames));
      db.close();
      return;
    }

    // 读取所有 messages
    const messages = await new Promise((resolve, reject) => {
      const transaction = db.transaction(['messages'], 'readonly');
      const store = transaction.objectStore('messages');
      const request = store.getAll();

      request.onerror = () => {
        reject(new Error(`读取 messages 失败: ${request.error?.message}`));
      };

      request.onsuccess = () => {
        resolve(request.result);
      };
    });

    db.close();

    // 格式化输出
    console.log(`✅ 共找到 ${messages.length} 条消息\n`);
    console.log('='.repeat(80));
    console.log('📊 Messages 数据 (JSON 格式，可复制):');
    console.log('='.repeat(80));

    // 格式化日期字段
    const formattedMessages = messages.map(msg => ({
      ...msg,
      created_at: msg.created_at instanceof Date ? msg.created_at.toISOString() : msg.created_at,
      deleted_at: msg.deleted_at instanceof Date ? msg.deleted_at.toISOString() : msg.deleted_at,
    }));

    // 输出 JSON
    const jsonOutput = JSON.stringify(formattedMessages, null, 2);
    console.log(jsonOutput);

    console.log('='.repeat(80));
    console.log('📝 统计信息:');
    console.log(`- 总消息数: ${messages.length}`);

    // 按类型统计
    const typeStats = {};
    messages.forEach(msg => {
      const type = msg.type || 'unknown';
      typeStats[type] = (typeStats[type] || 0) + 1;
    });
    console.log('- 按类型统计:', typeStats);

    // 按 conversation_id 统计
    const convStats = {};
    messages.forEach(msg => {
      const convId = msg.conversation_id || 'unknown';
      convStats[convId] = (convStats[convId] || 0) + 1;
    });
    console.log('- 按会话统计 (conversation_id -> 消息数):', convStats);

    console.log('='.repeat(80));
    console.log('💡 提示：复制上面的 JSON 输出，保存到文件中发给我分析');

    // 返回数据供进一步使用
    return formattedMessages;

  } catch (error) {
    console.error('❌ 错误:', error.message);
    console.log('\n🔍 调试信息:');
    console.log('- 确保页面已完全加载');
    console.log('- 确保已登录并选择了会话');
    console.log('- 尝试刷新页面后重试');
  }
})();
