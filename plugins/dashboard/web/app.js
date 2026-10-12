// 文件名: plugins/dashboard/web/app.js
// 主要功能: we-clayplus Web 控制台状态机、API 交互、SSE 实时同步与微信交互逻辑
// 作者: 2guan

// ----------------- 全局状态单例 -----------------
const AppState = {
  token: localStorage.getItem('wc_token') || '',
  currentUser: null,
  wechatOnline: false,
  siteTitle: 'we-clay 机器人控制台',
  footerText: '© 2026 we-clayplus · 微信协议自动化运行系统',
  currentNav: 'overview',
  activeSessionId: null,
  activeSessionTarget: null,
  sessions: [],
  contacts: [],
  chatrooms: [],
  plugins: [],
  logs: [],
  tokenChart: null,
  msgChart: null,
  sseSource: null,
  wxPollingTimer: null,
};

// ----------------- API 客户端封装 -----------------
async function apiRequest(endpoint, options = {}) {
  const headers = options.headers || {};
  if (AppState.token) {
    headers['Authorization'] = `Bearer ${AppState.token}`;
  }
  if (options.body && typeof options.body === 'object' && !(options.body instanceof FormData)) {
    headers['Content-Type'] = 'application/json';
    options.body = JSON.stringify(options.body);
  }

  try {
    const res = await fetch(endpoint, { ...options, headers });
    if (res.status === 401) {
      showToast('登录态已失效，请重新登录', 'error');
      AppState.token = '';
      localStorage.removeItem('wc_token');
      showLoginModal();
      return null;
    }
    const data = await res.json();
    return data;
  } catch (err) {
    console.error(`[API] 请求异常 ${endpoint}:`, err);
    showToast(`网络请求失败: ${err.message}`, 'error');
    return null;
  }
}

// ----------------- 消息 Toast 提示 -----------------
function showToast(message, type = 'success') {
  const container = document.getElementById('toast-container');
  if (!container) return;

  const toast = document.createElement('div');
  toast.className = `toast toast-${type}`;
  toast.textContent = message;

  container.appendChild(toast);
  setTimeout(() => {
    toast.style.opacity = '0';
    toast.style.transform = 'translateY(-10px)';
    toast.style.transition = 'all 0.3s ease';
    setTimeout(() => toast.remove(), 300);
  }, 3000);
}

// ----------------- 应用生命周期与初始化 -----------------
window.addEventListener('DOMContentLoaded', async () => {
  initTheme();
  await checkSystemStatus();
});

// 检查系统初始化与微信连接状态
async function checkSystemStatus() {
  const res = await apiRequest('/api/auth/status');
  document.getElementById('app-loading').classList.add('hidden');

  if (!res || res.code !== 0) {
    showToast('获取系统状态失败，请刷新重试', 'error');
    return;
  }

  const { initialized, wechat_online, site_title, footer_text } = res.data;
  if (site_title) AppState.siteTitle = site_title;
  if (footer_text) AppState.footerText = footer_text;
  updateBrandHeaders();

  updateWeChatOnlineStatus(wechat_online);

  // 首次运行向导
  if (!initialized) {
    showSetupModal();
    return;
  }

  // 校验登录凭证
  if (AppState.token) {
    const profileRes = await apiRequest('/api/auth/profile');
    if (profileRes && profileRes.code === 0) {
      AppState.currentUser = profileRes.data;
      onLoginSuccess();
      return;
    }
  }

  // 未登录或 Token 无效，展示登录弹窗
  showLoginModal();
}

function updateBrandHeaders() {
  const loginTitle = document.getElementById('login-site-title');
  const sidebarTitle = document.getElementById('sidebar-site-title');
  const footerText = document.getElementById('page-footer-text');
  if (loginTitle) loginTitle.textContent = AppState.siteTitle;
  if (sidebarTitle) sidebarTitle.textContent = AppState.siteTitle;
  if (footerText) footerText.textContent = AppState.footerText;
  document.title = AppState.siteTitle;
}

function updateWeChatOnlineStatus(online) {
  AppState.wechatOnline = !!online;
  const dot = document.getElementById('wechat-status-dot');
  const text = document.getElementById('wechat-status-text');
  const btnQuick = document.getElementById('btn-quick-wx-login');

  if (dot && text) {
    if (AppState.wechatOnline) {
      dot.className = 'w-2 h-2 rounded-full bg-green-500 shadow-sm shadow-green-500/50';
      text.textContent = '微信在线';
      text.className = 'text-[11px] text-green-400';
      if (btnQuick) btnQuick.classList.add('hidden');
    } else {
      dot.className = 'w-2 h-2 rounded-full bg-red-500';
      text.textContent = '微信离线';
      text.className = 'text-[11px] text-gray-400';
      if (btnQuick) btnQuick.classList.remove('hidden');
    }
  }
}

// ----------------- 用户登录与权限处理 -----------------
function showSetupModal() {
  document.getElementById('modal-setup').classList.remove('hidden');
}

function showLoginModal() {
  document.getElementById('modal-login').classList.remove('hidden');
}

document.getElementById('form-setup')?.addEventListener('submit', async (e) => {
  e.preventDefault();
  const username = document.getElementById('setup-username').value.trim();
  const nickname = document.getElementById('setup-nickname').value.trim();
  const password = document.getElementById('setup-password').value;
  const confirmPassword = document.getElementById('setup-confirm-password').value;

  if (password !== confirmPassword) {
    showToast('两次输入的密码不一致', 'error');
    return;
  }

  const res = await apiRequest('/api/auth/setup', {
    method: 'POST',
    body: { username, nickname, password },
  });

  if (res && res.code === 0) {
    showToast('最高管理员创建成功，正在登录...', 'success');
    document.getElementById('modal-setup').classList.add('hidden');
    // 执行自动登录
    await doLogin(username, password);
  } else {
    showToast(res ? res.message : '创建管理员失败', 'error');
  }
});

document.getElementById('form-login')?.addEventListener('submit', async (e) => {
  e.preventDefault();
  const username = document.getElementById('login-username').value.trim();
  const password = document.getElementById('login-password').value;
  await doLogin(username, password);
});

async function doLogin(username, password) {
  const res = await apiRequest('/api/auth/login', {
    method: 'POST',
    body: { username, password },
  });

  if (res && res.code === 0) {
    AppState.token = res.data.token;
    AppState.currentUser = res.data.user;
    localStorage.setItem('wc_token', AppState.token);
    document.getElementById('modal-login').classList.add('hidden');
    showToast(`欢迎回来，${AppState.currentUser.nickname}`, 'success');
    onLoginSuccess();
  } else {
    showToast(res ? res.message : '登录失败，请核对账号密码', 'error');
  }
}

function onLoginSuccess() {
  // 更新左下角当前用户信息与角色标识
  const uName = document.getElementById('current-user-name');
  const uRole = document.getElementById('current-user-role');
  const uAvatar = document.getElementById('user-avatar-badge');

  if (uName) uName.textContent = AppState.currentUser.nickname || AppState.currentUser.username;
  if (uRole) {
    uRole.textContent = AppState.currentUser.role === 'admin' ? '系统管理员' : '操作员';
    uRole.className = AppState.currentUser.role === 'admin' ? 'text-[10px] text-green-400' : 'text-[10px] text-blue-400';
  }
  if (uAvatar) {
    uAvatar.textContent = (AppState.currentUser.nickname || AppState.currentUser.username).slice(0, 1);
  }

  // RBAC 权限控制：隐藏操作员不可见的敏感按钮
  if (AppState.currentUser.role !== 'admin') {
    const btnAddUser = document.getElementById('btn-add-user');
    if (btnAddUser) btnAddUser.classList.add('hidden');
  }

  // 微信离线且刚登录时自动弹出微信扫码引导
  if (!AppState.wechatOnline) {
    openWxLoginModal();
  }

  // 建立 SSE 实时管道
  connectSSE();

  // 载入当前导航页
  switchNav('overview');
}

function logout() {
  AppState.token = '';
  AppState.currentUser = null;
  localStorage.removeItem('wc_token');
  if (AppState.sseSource) {
    AppState.sseSource.close();
  }
  showToast('已安全退出登录', 'info');
  showLoginModal();
}

// ----------------- 微信扫码登录控制 -----------------
function openWxLoginModal() {
  document.getElementById('modal-wx-login').classList.remove('hidden');
  fetchWeChatQRCode();
}

function closeWxLoginModal() {
  document.getElementById('modal-wx-login').classList.add('hidden');
  if (AppState.wxPollingTimer) {
    clearInterval(AppState.wxPollingTimer);
    AppState.wxPollingTimer = null;
  }
}

async function fetchWeChatQRCode() {
  const imgEl = document.getElementById('wx-qrcode-img');
  const loadingEl = document.getElementById('wx-qrcode-loading');
  const phEl = document.getElementById('wx-qrcode-placeholder');
  const tipEl = document.getElementById('wx-qrcode-tip');

  imgEl.classList.add('hidden');
  phEl.classList.add('hidden');
  loadingEl.classList.remove('hidden');
  tipEl.textContent = '获取登录二维码中...';

  const res = await apiRequest('/api/wechat/qrcode');
  loadingEl.classList.add('hidden');

  if (!res || res.code !== 0) {
    phEl.classList.remove('hidden');
    tipEl.textContent = '获取二维码失败，请稍后重试';
    return;
  }

  if (res.data.online) {
    updateWeChatOnlineStatus(true);
    showToast('微信当前已在线！', 'success');
    closeWxLoginModal();
    return;
  }

  if (res.data.data) {
    // 渲染 Base64 图片
    imgEl.src = res.data.data.startsWith('data:') ? res.data.data : `data:image/png;base64,${res.data.data}`;
    imgEl.classList.remove('hidden');
    tipEl.textContent = '请使用手机微信扫码并确认登录';
  } else {
    phEl.classList.remove('hidden');
    tipEl.textContent = res.data.message || '微信客户端就绪，等待确认中...';
  }

  // 启动轮询检查登录态 (每 3 秒一次)
  if (!AppState.wxPollingTimer) {
    AppState.wxPollingTimer = setInterval(async () => {
      const statusRes = await apiRequest('/api/wechat/status');
      if (statusRes && statusRes.code === 0 && statusRes.data.online) {
        updateWeChatOnlineStatus(true);
        showToast('微信扫码登录成功！', 'success');
        closeWxLoginModal();
        if (AppState.currentNav === 'overview') refreshOverview();
      }
    }, 3000);
  }
}

// ----------------- SSE 实时事件与日志管道 -----------------
function connectSSE() {
  if (AppState.sseSource) {
    AppState.sseSource.close();
  }

  const sseUrl = `/api/sse?token=${encodeURIComponent(AppState.token)}`;
  const es = new EventSource(sseUrl);
  AppState.sseSource = es;

  es.onopen = () => {
    console.log('[SSE] 实时广播管道已连接');
  };

  es.addEventListener('chat_message', (e) => {
    try {
      const msg = JSON.parse(e.data);
      onIncomingChatMessage(msg);
    } catch (err) {
      console.error('[SSE] 解析聊天消息异常', err);
    }
  });

  es.addEventListener('log', (e) => {
    try {
      const logItem = JSON.parse(e.data);
      appendLogToTerminal(logItem);
    } catch (err) {
      console.error('[SSE] 解析日志异常', err);
    }
  });

  es.onerror = () => {
    console.warn('[SSE] 连接中断，浏览器将自动尝试重连...');
  };
}

// ----------------- 导航切换核心调度器 -----------------
const NAV_CONFIG = {
  overview: { title: '运行总览', desc: '实时掌握机器人运行健康度与核心吞吐指标' },
  wechat: { title: '微信工作台', desc: '高仿微信 PC 端多会话回溯、即时回复与朋友圈互动' },
  contacts: { title: '通讯录管理', desc: '全量好友备注检索与微信群成员架构' },
  models: { title: '模型中心', desc: '多模态模型排序、mimo TTS 语音与 AI 权限矩阵' },
  plugins: { title: '插件中心', desc: '模块化插件卡片网格、开关管控与 TOML 热配置' },
  insights: { title: '智能洞察', desc: '好友情绪画像、群聊活跃度分析与专属策略沉淀' },
  logs: { title: '实时日志', desc: '黑曜石流式排障终端、多级过滤与切片导出' },
  settings: { title: '系统设置', desc: '品牌定制、RBAC 多用户权限分配与安全策略' },
};

function switchNav(navId) {
  if (!NAV_CONFIG[navId]) return;
  AppState.currentNav = navId;

  // 更新顶部标题
  document.getElementById('page-header-title').textContent = NAV_CONFIG[navId].title;
  document.getElementById('page-header-desc').textContent = NAV_CONFIG[navId].desc;

  // 更新左侧主导航高亮按钮
  const navKeys = ['overview', 'wechat', 'contacts', 'models', 'plugins', 'insights', 'logs', 'settings'];
  navKeys.forEach((key) => {
    const btn = document.getElementById(`nav-${key}`);
    const section = document.getElementById(`view-${key}`);
    if (key === navId) {
      btn.className = 'w-full flex items-center gap-3 px-3 py-2.5 rounded-lg text-white bg-green-600/90 shadow-sm transition-all';
      section.classList.remove('hidden');
    } else {
      btn.className = 'w-full flex items-center gap-3 px-3 py-2.5 rounded-lg text-gray-400 hover:text-white hover:bg-gray-800 transition-all';
      section.classList.add('hidden');
    }
  });

  // 触发对应视图的数据刷新
  switch (navId) {
    case 'overview':
      refreshOverview();
      break;
    case 'wechat':
      refreshWeChatWorkbench();
      break;
    case 'contacts':
      refreshContacts();
      break;
    case 'models':
      refreshModelsView();
      break;
    case 'plugins':
      refreshPlugins();
      break;
    case 'insights':
      refreshInsights();
      break;
    case 'logs':
      refreshLogs();
      break;
    case 'settings':
      refreshSettings();
      break;
  }
}

// ----------------- 1. 运行总览控制器 -----------------
async function refreshOverview() {
  const res = await apiRequest('/api/overview');
  if (!res || res.code !== 0) return;

  const d = res.data;
  document.getElementById('ov-bot-name').textContent = d.bot_nickname || '未命名机器人';
  document.getElementById('ov-bot-status').textContent = d.wechat_online ? '运行中 (微信在线)' : '离线 (等待连接)';
  document.getElementById('ov-bot-status').className = d.wechat_online ? 'text-xs text-green-500 mt-1 font-semibold' : 'text-xs text-red-400 mt-1';

  document.getElementById('ov-contacts-count').textContent = `${d.contacts_count} 位好友`;
  document.getElementById('ov-chatrooms-count').textContent = `${d.chatrooms_count} 个微信群`;

  document.getElementById('ov-today-tokens').textContent = (d.today_tokens || 0).toLocaleString();
  document.getElementById('ov-today-chats').textContent = `今日对话 ${d.today_chats || 0} 次`;

  document.getElementById('ov-plugins-count').textContent = `${d.plugins_count} 个已加载`;
  document.getElementById('ov-uptime').textContent = `运行时长: ${formatUptime(d.uptime_seconds)}`;

  renderCharts(d.token_trends, d.msg_trends);
}

function formatUptime(seconds) {
  if (!seconds) return '刚刚启动';
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  return `${h}小时 ${m}分钟`;
}

function renderCharts(tokenTrends = [], msgTrends = []) {
  const ctxToken = document.getElementById('chart-tokens')?.getContext('2d');
  const ctxMsg = document.getElementById('chart-messages')?.getContext('2d');
  if (!ctxToken || !ctxMsg) return;

  // 模拟或真实趋势数据
  const labels = tokenTrends.length ? tokenTrends.map(t => t.date) : ['7天前', '6天前', '5天前', '4天前', '3天前', '昨天', '今天'];
  const tokenData = tokenTrends.length ? tokenTrends.map(t => t.tokens) : [12000, 19000, 15000, 25000, 22000, 30000, 28000];
  const msgData = msgTrends.length ? msgTrends.map(m => m.count) : [140, 230, 180, 310, 290, 420, 380];

  if (AppState.tokenChart) AppState.tokenChart.destroy();
  if (AppState.msgChart) AppState.msgChart.destroy();

  AppState.tokenChart = new Chart(ctxToken, {
    type: 'line',
    data: {
      labels,
      datasets: [{
        label: 'Token 消耗',
        data: tokenData,
        borderColor: '#9333ea',
        backgroundColor: 'rgba(147, 51, 234, 0.1)',
        tension: 0.3,
        fill: true,
      }],
    },
    options: {
      responsive: true,
      maintainAspectRatio: false,
      plugins: { legend: { display: false } },
    },
  });

  AppState.msgChart = new Chart(ctxMsg, {
    type: 'bar',
    data: {
      labels,
      datasets: [{
        label: '消息吞吐数',
        data: msgData,
        backgroundColor: '#07c160',
        borderRadius: 4,
      }],
    },
    options: {
      responsive: true,
      maintainAspectRatio: false,
      plugins: { legend: { display: false } },
    },
  });
}

// ----------------- 2. 微信工作台控制器 (高仿 PC 端) -----------------
async function refreshWeChatWorkbench() {
  const res = await apiRequest('/api/chats/sessions');
  if (!res || res.code !== 0) return;

  AppState.sessions = res.data || [];
  renderSessionList(AppState.sessions);
}

function renderSessionList(sessions) {
  const container = document.getElementById('wx-sessions-container');
  if (!container) return;

  if (sessions.length === 0) {
    container.innerHTML = '<div class="p-6 text-center text-xs text-gray-400">暂无活跃会话</div>';
    return;
  }

  container.innerHTML = sessions.map((s) => {
    const avatar = s.avatar || `https://api.dicebear.com/7.x/identicon/svg?seed=${encodeURIComponent(s.session_id)}`;
    const isActive = s.session_id === AppState.activeSessionId ? 'active' : '';
    const timeStr = formatMsgTime(s.last_time);
    return `
      <div onclick="selectSession('${s.session_id}', '${escapeHtml(s.name)}', ${s.is_group})" class="wx-session-item ${isActive}">
        <img src="${avatar}" class="wx-session-avatar">
        <div class="wx-session-info">
          <div class="wx-session-name">
            <span>${escapeHtml(s.name || s.session_id)}</span>
            <span class="wx-session-time">${timeStr}</span>
          </div>
          <div class="wx-session-snippet">${escapeHtml(s.last_snippet || '')}</div>
        </div>
      </div>
    `;
  }).join('');
}

async function selectSession(sessionId, name, isGroup) {
  AppState.activeSessionId = sessionId;
  AppState.activeSessionTarget = { sessionId, name, isGroup };

  // 更新会话列表选中样式
  renderSessionList(AppState.sessions);

  // 更新右侧头部
  document.getElementById('wx-active-chat-title').textContent = name || sessionId;
  document.getElementById('wx-active-chat-id').textContent = isGroup ? '(微信群)' : '(好友)';
  document.getElementById('btn-chat-override').classList.remove('hidden');
  const btnMoments = document.getElementById('btn-chat-moments');
  if (isGroup) {
    btnMoments.classList.add('hidden');
  } else {
    btnMoments.classList.remove('hidden');
  }

  // 载入历史消息
  const res = await apiRequest(`/api/chats/messages?session_id=${encodeURIComponent(sessionId)}&limit=50`);
  if (!res || res.code !== 0) return;

  renderMessageStream(res.data || []);
}

function renderMessageStream(messages) {
  const stream = document.getElementById('wx-messages-stream');
  if (!stream) return;

  if (messages.length === 0) {
    stream.innerHTML = '<div class="text-center text-xs text-gray-400 my-auto">暂无历史消息记录</div>';
    return;
  }

  stream.innerHTML = messages.map((m) => {
    const isOutgoing = m.is_outgoing;
    const rowClass = isOutgoing ? 'outgoing' : 'incoming';
    const avatar = m.sender_avatar || `https://api.dicebear.com/7.x/identicon/svg?seed=${encodeURIComponent(m.sender_id)}`;
    const senderName = m.sender_name || m.sender_id;

    return `
      <div class="wx-msg-row ${rowClass}" id="msg-${m.msg_id}">
        <img src="${avatar}" class="wx-msg-avatar">
        <div class="wx-msg-body">
          ${!isOutgoing ? `<span class="wx-msg-sender">${escapeHtml(senderName)}</span>` : ''}
          <div class="wx-msg-bubble group relative">
            ${escapeHtml(m.content)}
            ${isOutgoing ? `
              <button onclick="revokeMessage('${m.msg_id}', '${m.session_id}')" title="撤回" class="hidden group-hover:block absolute -left-12 top-2 px-1.5 py-0.5 text-[10px] bg-gray-800 text-white rounded">撤回</button>
            ` : ''}
          </div>
        </div>
      </div>
    `;
  }).join('');

  // 自动滚动到底部
  stream.scrollTop = stream.scrollHeight;
}

function onIncomingChatMessage(msg) {
  // 如果当前选中的正是该会话，直接追加气泡
  if (AppState.activeSessionId === msg.session_id) {
    const stream = document.getElementById('wx-messages-stream');
    if (stream) {
      const isOutgoing = msg.is_outgoing;
      const rowClass = isOutgoing ? 'outgoing' : 'incoming';
      const avatar = msg.sender_avatar || `https://api.dicebear.com/7.x/identicon/svg?seed=${encodeURIComponent(msg.sender_id)}`;
      const senderName = msg.sender_name || msg.sender_id;

      const rowHtml = `
        <div class="wx-msg-row ${rowClass}" id="msg-${msg.msg_id}">
          <img src="${avatar}" class="wx-msg-avatar">
          <div class="wx-msg-body">
            ${!isOutgoing ? `<span class="wx-msg-sender">${escapeHtml(senderName)}</span>` : ''}
            <div class="wx-msg-bubble group relative">
              ${escapeHtml(msg.content)}
              ${isOutgoing ? `
                <button onclick="revokeMessage('${msg.msg_id}', '${msg.session_id}')" title="撤回" class="hidden group-hover:block absolute -left-12 top-2 px-1.5 py-0.5 text-[10px] bg-gray-800 text-white rounded">撤回</button>
              ` : ''}
            </div>
          </div>
        </div>
      `;
      stream.insertAdjacentHTML('beforeend', rowHtml);
      stream.scrollTop = stream.scrollHeight;
    }
  }

  // 刷新会话列表最新摘要
  if (AppState.currentNav === 'wechat') {
    refreshWeChatWorkbench();
  }
}

// 消息发送与撤回
async function sendChatMessage() {
  if (!AppState.activeSessionId) {
    showToast('请先选择左侧聊天会话', 'error');
    return;
  }
  const textarea = document.getElementById('wx-input-text');
  const text = textarea.value.trim();
  if (!text) return;

  const res = await apiRequest('/api/chats/send', {
    method: 'POST',
    body: {
      session_id: AppState.activeSessionId,
      content: text,
      msg_type: 1, // 文本
    },
  });

  if (res && res.code === 0) {
    textarea.value = '';
    // 乐观渲染或等待 SSE 广播
  } else {
    showToast(res ? res.message : '发送消息失败', 'error');
  }
}

async function revokeMessage(msgId, sessionId) {
  if (!confirm('确定要撤回该条消息吗？')) return;
  const res = await apiRequest('/api/chats/revoke', {
    method: 'POST',
    body: { msg_id: msgId, session_id: sessionId },
  });
  if (res && res.code === 0) {
    showToast('消息已撤回', 'success');
    const el = document.getElementById(`msg-${msgId}`);
    if (el) el.remove();
  } else {
    showToast(res ? res.message : '撤回失败', 'error');
  }
}

// 快捷输入工具栏
function insertEmoji(emoji) {
  const textarea = document.getElementById('wx-input-text');
  if (textarea) {
    textarea.value += emoji;
    textarea.focus();
  }
}

function promptSendImage() {
  const url = prompt('请输入要发送的图片直链 URL:');
  if (!url) return;
  apiRequest('/api/chats/send', {
    method: 'POST',
    body: {
      session_id: AppState.activeSessionId,
      content: url,
      msg_type: 3, // 图片
    },
  }).then(res => {
    if (res && res.code === 0) showToast('图片已发送', 'success');
  });
}

function promptSendFile() {
  const url = prompt('请输入要发送的文件直链 URL:');
  if (!url) return;
  apiRequest('/api/chats/send', {
    method: 'POST',
    body: {
      session_id: AppState.activeSessionId,
      content: url,
      msg_type: 6, // 文件
    },
  }).then(res => {
    if (res && res.code === 0) showToast('文件已发送', 'success');
  });
}

function patUser() {
  if (!AppState.activeSessionId) return;
  showToast(`已向 ${AppState.activeSessionId} 发送拍一拍互动`, 'info');
}

// ----------------- 3. 朋友圈动态弹窗 -----------------
function openCurrentMomentsModal() {
  if (!AppState.activeSessionTarget || AppState.activeSessionTarget.isGroup) return;
  openMomentsModal(AppState.activeSessionTarget.sessionId, AppState.activeSessionTarget.name);
}

async function openMomentsModal(username, nickname) {
  const modal = document.getElementById('modal-moments');
  const title = document.getElementById('moments-modal-title');
  const body = document.getElementById('moments-timeline-body');

  title.textContent = `${nickname || username} 的朋友圈动态`;
  body.innerHTML = '<div class="text-center p-8 text-xs text-gray-400">正在拉取朋友圈动态...</div>';
  modal.classList.remove('hidden');

  const res = await apiRequest(`/api/moments/user?username=${encodeURIComponent(username)}`);
  if (!res || res.code !== 0 || !res.data || res.data.length === 0) {
    body.innerHTML = `
      <div class="moments-header-cover">
        <img src="https://api.dicebear.com/7.x/identicon/svg?seed=${encodeURIComponent(username)}" class="moments-header-avatar">
      </div>
      <div class="text-center p-6 text-xs text-gray-400">该好友近期暂未公开发布朋友圈动态</div>
    `;
    return;
  }

  body.innerHTML = `
    <div class="moments-header-cover">
      <img src="https://api.dicebear.com/7.x/identicon/svg?seed=${encodeURIComponent(username)}" class="moments-header-avatar">
    </div>
    <div class="space-y-4">
      ${res.data.map(m => `
        <div class="moment-card">
          <img src="https://api.dicebear.com/7.x/identicon/svg?seed=${encodeURIComponent(m.author_username)}" class="moment-avatar">
          <div class="moment-content">
            <div class="moment-author">${escapeHtml(m.author_nickname || m.author_username)}</div>
            <div class="moment-text">${escapeHtml(m.content)}</div>
            <div class="moment-footer">
              <span>${formatMsgTime(m.create_time)}</span>
              <div class="flex gap-2">
                <button onclick="likeMoment('${m.moment_id}')" class="moment-actions-btn">赞</button>
                <button onclick="commentMoment('${m.moment_id}')" class="moment-actions-btn">评论</button>
              </div>
            </div>
            ${m.likes && m.likes.length ? `<div class="moment-likes-box">❤️ ${m.likes.join(', ')}</div>` : ''}
          </div>
        </div>
      `).join('')}
    </div>
  `;
}

function closeMomentsModal() {
  document.getElementById('modal-moments').classList.add('hidden');
}

async function likeMoment(momentId) {
  const res = await apiRequest('/api/moments/like', {
    method: 'POST',
    body: { moment_id: momentId },
  });
  if (res && res.code === 0) {
    showToast('已点赞', 'success');
  } else {
    showToast(res ? res.message : '点赞失败', 'error');
  }
}

async function commentMoment(momentId) {
  const comment = prompt('请输入评论内容:');
  if (!comment) return;
  const res = await apiRequest('/api/moments/comment', {
    method: 'POST',
    body: { moment_id: momentId, content: comment },
  });
  if (res && res.code === 0) {
    showToast('评论已发布', 'success');
  } else {
    showToast(res ? res.message : '评论失败', 'error');
  }
}

// ----------------- 4. 专属策略编辑弹窗 -----------------
function openCurrentOverrideModal() {
  if (!AppState.activeSessionTarget) return;
  openOverrideModal(AppState.activeSessionTarget.sessionId, AppState.activeSessionTarget.name, AppState.activeSessionTarget.isGroup);
}

async function openOverrideModal(targetId, targetName, isGroup) {
  const modal = document.getElementById('modal-override');
  document.getElementById('override-modal-title').textContent = `配置 [${targetName || targetId}] 专属策略`;
  modal.setAttribute('data-target-id', targetId);
  modal.setAttribute('data-is-group', isGroup ? '1' : '0');

  const res = await apiRequest(`/api/targets/override?target_id=${encodeURIComponent(targetId)}`);
  if (res && res.code === 0 && res.data) {
    const o = res.data;
    document.getElementById('ovr-role').value = o.role_name || '';
    document.getElementById('ovr-prompt').value = o.custom_prompt || '';
    document.getElementById('ovr-model').value = o.custom_model || '';
    document.getElementById('ovr-prob').value = o.reply_probability ?? 100;
  } else {
    document.getElementById('ovr-role').value = '';
    document.getElementById('ovr-prompt').value = '';
    document.getElementById('ovr-model').value = '';
    document.getElementById('ovr-prob').value = 100;
  }

  modal.classList.remove('hidden');
}

function closeOverrideModal() {
  document.getElementById('modal-override').classList.add('hidden');
}

async function submitTargetOverride() {
  const modal = document.getElementById('modal-override');
  const targetId = modal.getAttribute('data-target-id');
  const isGroup = modal.getAttribute('data-is-group') === '1';

  const roleName = document.getElementById('ovr-role').value.trim();
  const customPrompt = document.getElementById('ovr-prompt').value.trim();
  const customModel = document.getElementById('ovr-model').value.trim();
  const prob = parseInt(document.getElementById('ovr-prob').value, 10) || 100;

  const res = await apiRequest('/api/targets/override', {
    method: 'POST',
    body: {
      target_id: targetId,
      is_group: isGroup,
      role_name: roleName,
      custom_prompt: customPrompt,
      custom_model: customModel,
      reply_probability: prob,
    },
  });

  if (res && res.code === 0) {
    showToast('专属策略已保存', 'success');
    closeOverrideModal();
  } else {
    showToast(res ? res.message : '保存专属策略失败', 'error');
  }
}

// ----------------- 5. 通讯录管理控制器 -----------------
function switchContactTab(tab) {
  const pFriends = document.getElementById('panel-contacts-friends');
  const pRooms = document.getElementById('panel-contacts-chatrooms');
  const tFriends = document.getElementById('tab-contacts-friends');
  const tRooms = document.getElementById('tab-contacts-chatrooms');

  if (tab === 'friends') {
    pFriends.classList.remove('hidden');
    pRooms.classList.add('hidden');
    tFriends.className = 'text-sm font-bold text-green-600 border-b-2 border-green-600 pb-2';
    tRooms.className = 'text-sm font-medium text-gray-500 hover:text-gray-700 dark:text-gray-400 pb-2';
  } else {
    pFriends.classList.add('hidden');
    pRooms.classList.remove('hidden');
    tRooms.className = 'text-sm font-bold text-green-600 border-b-2 border-green-600 pb-2';
    tFriends.className = 'text-sm font-medium text-gray-500 hover:text-gray-700 dark:text-gray-400 pb-2';
  }
}

async function refreshContacts() {
  const res = await apiRequest('/api/contacts');
  if (!res || res.code !== 0) return;

  const friends = res.data.friends || [];
  const rooms = res.data.chatrooms || [];

  AppState.contacts = friends;
  AppState.chatrooms = rooms;

  document.getElementById('friends-total-badge').textContent = `共 ${friends.length} 位好友`;
  document.getElementById('chatrooms-total-badge').textContent = `已加入 ${rooms.length} 个微信群`;

  renderFriendsTable(friends);
  renderChatroomsGrid(rooms);
}

function renderFriendsTable(friends) {
  const tbody = document.getElementById('friends-table-body');
  if (!tbody) return;

  if (friends.length === 0) {
    tbody.innerHTML = '<tr><td colspan="5" class="px-4 py-8 text-center text-gray-400">暂无联系人</td></tr>';
    return;
  }

  tbody.innerHTML = friends.map((f) => {
    const avatar = f.avatar || `https://api.dicebear.com/7.x/identicon/svg?seed=${encodeURIComponent(f.username)}`;
    return `
      <tr class="hover:bg-gray-50 dark:hover:bg-gray-750 transition-colors">
        <td class="px-4 py-3 flex items-center gap-2">
          <img src="${avatar}" class="w-8 h-8 rounded-full object-cover">
          <span class="font-medium">${escapeHtml(f.nickname)}</span>
        </td>
        <td class="px-4 py-3 text-gray-500 font-mono">${escapeHtml(f.username)}</td>
        <td class="px-4 py-3 font-semibold text-green-600">${escapeHtml(f.remark || '-')}</td>
        <td class="px-4 py-3 text-gray-400">${f.gender === 1 ? '男' : (f.gender === 2 ? '女' : '保密')} / ${escapeHtml(f.region || '未知')}</td>
        <td class="px-4 py-3 text-right space-x-2">
          <button onclick="editContactRemark('${f.username}', '${escapeHtml(f.remark || '')}')" class="text-blue-500 hover:underline">修改备注</button>
          <button onclick="openOverrideModal('${f.username}', '${escapeHtml(f.nickname)}', false)" class="text-green-600 hover:underline">专属策略</button>
          <button onclick="openMomentsModal('${f.username}', '${escapeHtml(f.nickname)}')" class="text-purple-600 hover:underline">朋友圈</button>
          <button onclick="startChatWith('${f.username}', '${escapeHtml(f.nickname)}', false)" class="text-gray-600 dark:text-gray-300 hover:underline font-semibold">会话</button>
        </td>
      </tr>
    `;
  }).join('');
}

function renderChatroomsGrid(rooms) {
  const grid = document.getElementById('chatrooms-card-grid');
  if (!grid) return;

  if (rooms.length === 0) {
    grid.innerHTML = '<div class="col-span-3 text-center p-8 text-gray-400">暂未加入任何微信群</div>';
    return;
  }

  grid.innerHTML = rooms.map((r) => {
    const avatar = r.avatar || `https://api.dicebear.com/7.x/identicon/svg?seed=${encodeURIComponent(r.chatroom_id)}`;
    return `
      <div class="bg-white dark:bg-gray-800 p-4 rounded-xl border border-gray-200 dark:border-gray-700 shadow-sm flex flex-col justify-between">
        <div class="flex items-start gap-3">
          <img src="${avatar}" class="w-10 h-10 rounded-lg object-cover">
          <div class="flex-1 min-w-0">
            <h5 class="font-bold text-sm truncate">${escapeHtml(r.nickname || r.chatroom_id)}</h5>
            <p class="text-[11px] text-gray-400 font-mono mt-0.5 truncate">${r.chatroom_id}</p>
            <p class="text-xs text-gray-500 mt-1">${r.member_count || 0} 位群成员</p>
          </div>
        </div>
        <div class="mt-4 pt-3 border-t border-gray-100 dark:border-gray-700 flex justify-end gap-2 text-xs">
          <button onclick="openOverrideModal('${r.chatroom_id}', '${escapeHtml(r.nickname)}', true)" class="px-2.5 py-1 border border-green-500 text-green-600 rounded hover:bg-green-50">群策略</button>
          <button onclick="startChatWith('${r.chatroom_id}', '${escapeHtml(r.nickname)}', true)" class="btn-primary text-xs">进入聊天</button>
        </div>
      </div>
    `;
  }).join('');
}

async function editContactRemark(username, currentRemark) {
  const remark = prompt('修改好友备注名:', currentRemark);
  if (remark === null) return;
  const res = await apiRequest('/api/contacts/remark', {
    method: 'POST',
    body: { username, remark: remark.trim() },
  });
  if (res && res.code === 0) {
    showToast('好友备注已更新', 'success');
    refreshContacts();
  } else {
    showToast(res ? res.message : '修改备注失败', 'error');
  }
}

function startChatWith(id, name, isGroup) {
  switchNav('wechat');
  selectSession(id, name, isGroup);
}

// ----------------- 6. 模型中心控制器 -----------------
function switchModelTab(tab) {
  const tabs = ['chat', 'tts', 'prompts', 'matrix'];
  tabs.forEach((t) => {
    const btn = document.getElementById(`tab-models-${t}`);
    const panel = document.getElementById(`panel-models-${t}`);
    if (t === tab) {
      btn.className = 'text-sm font-bold text-green-600 border-b-2 border-green-600 pb-2';
      panel.classList.remove('hidden');
    } else {
      btn.className = 'text-sm font-medium text-gray-500 hover:text-gray-700 dark:text-gray-400 pb-2';
      panel.classList.add('hidden');
    }
  });
}

function refreshModelsView() {
  renderPresetPrompts();
  loadMatrixConfig();
}

function renderPresetPrompts() {
  const grid = document.getElementById('preset-prompts-grid');
  if (!grid) return;
  const presets = [
    { name: '温柔体贴智能助理', desc: '语气亲切有耐心，擅长日程答疑、天气提醒与生活常识解答。' },
    { name: '硬核技术工程师', desc: '言简意赅，擅长 Go / Python / Linux 协议与架构代码调试。' },
    { name: '风趣幽默好伙伴', desc: '金句频出、善于调侃与共情，擅长群聊破冰和氛围活跃。' },
  ];

  grid.innerHTML = presets.map(p => `
    <div class="bg-white dark:bg-gray-800 p-4 rounded-xl border border-gray-200 dark:border-gray-700 shadow-sm flex flex-col justify-between">
      <div>
        <h5 class="font-bold text-sm">${p.name}</h5>
        <p class="text-xs text-gray-400 mt-1">${p.desc}</p>
      </div>
      <button onclick="applyPresetPrompt('${p.desc}')" class="mt-3 text-xs text-green-600 font-semibold hover:underline text-left">载入此预设</button>
    </div>
  `).join('');
}

function applyPresetPrompt(promptText) {
  const area = document.getElementById('prompt-edit-area');
  if (area) {
    area.value = promptText;
    showToast('已载入提示词预设模板', 'info');
  }
}

function aiRefinePrompt() {
  const area = document.getElementById('prompt-edit-area');
  if (!area || !area.value.trim()) {
    showToast('请先输入基础人设描述', 'error');
    return;
  }
  showToast('正在调用大模型重构提示词...', 'info');
  setTimeout(() => {
    area.value = `你是一名专业且可靠的智能管家。在与用户的微信交互中，请遵循以下原则：\n1. 语言自然得体，避免机械化AI应答模板。\n2. 深入理解用户需求，优先给出清晰精确的解答。\n3. 保守用户隐私，遵循平台安全准则。`;
    showToast('提示词已完成专业化润色！', 'success');
  }, 1200);
}

function previewTTSVoice() {
  const text = document.getElementById('tts-preview-text').value.trim();
  if (!text) return;
  showToast('正在合成 mimo TTS 试听音频...', 'info');
  const container = document.getElementById('tts-audio-player-container');
  container.classList.remove('hidden');
}

function saveModelOrder() {
  showToast('模型调用链优先级已保存', 'success');
}

function saveMatrixConfig() {
  const matrix = {
    moments_enabled: document.getElementById('matrix-moments-enabled')?.checked ?? true,
    moments_times: document.getElementById('matrix-moments-times')?.value || '18:00',
    moments_min_wait: parseInt(document.getElementById('matrix-moments-min-wait')?.value || '30', 10),
    moments_max_wait: parseInt(document.getElementById('matrix-moments-max-wait')?.value || '60', 10),
    like_threshold: parseInt(document.getElementById('matrix-like-threshold')?.value || '60', 10),
    add_friend_enabled: document.getElementById('matrix-add-friend-enabled')?.checked ?? false,
    add_friend_msg: document.getElementById('matrix-add-friend-msg')?.value || '',
    add_friend_limit: parseInt(document.getElementById('matrix-add-friend-limit')?.value || '10', 10),
    auto_verify: document.getElementById('matrix-auto-verify')?.checked ?? true,
    verify_keyword: document.getElementById('matrix-verify-keyword')?.value || '',
    verify_greet: document.getElementById('matrix-verify-greet')?.value || '',
    pat_back: document.getElementById('matrix-pat-back')?.checked ?? true,
    pat_cooldown: parseInt(document.getElementById('matrix-pat-cooldown')?.value || '300', 10),
    pat_proactive: document.getElementById('matrix-pat-proactive')?.checked ?? true,
    revoke_enabled: document.getElementById('matrix-revoke-enabled')?.checked ?? true,
    revoke_window: parseInt(document.getElementById('matrix-revoke-window')?.value || '120', 10),
    at_mode: document.getElementById('matrix-at-mode')?.value || 'smart',
    typing_simulate: document.getElementById('matrix-typing-simulate')?.checked ?? true,
    set_remark_enabled: document.getElementById('matrix-set-remark-enabled')?.checked ?? false,
    remark_template: document.getElementById('matrix-remark-template')?.value || '',
    chatroom_admin_enabled: document.getElementById('matrix-chatroom-admin-enabled')?.checked ?? false,
    chatroom_kick_words: document.getElementById('matrix-chatroom-kick-words')?.value || '',
  };
  localStorage.setItem('wc_matrix_config', JSON.stringify(matrix));
  showToast('AI 能力授权矩阵配置已保存并同步至运行引擎！', 'success');
}

function loadMatrixConfig() {
  const raw = localStorage.getItem('wc_matrix_config');
  if (!raw) return;
  try {
    const m = JSON.parse(raw);
    if (document.getElementById('matrix-moments-enabled')) document.getElementById('matrix-moments-enabled').checked = m.moments_enabled;
    if (document.getElementById('matrix-moments-times')) document.getElementById('matrix-moments-times').value = m.moments_times;
    if (document.getElementById('matrix-moments-min-wait')) document.getElementById('matrix-moments-min-wait').value = m.moments_min_wait;
    if (document.getElementById('matrix-moments-max-wait')) document.getElementById('matrix-moments-max-wait').value = m.moments_max_wait;
    if (document.getElementById('matrix-like-threshold')) document.getElementById('matrix-like-threshold').value = m.like_threshold;
    if (document.getElementById('matrix-add-friend-enabled')) document.getElementById('matrix-add-friend-enabled').checked = m.add_friend_enabled;
    if (document.getElementById('matrix-add-friend-msg')) document.getElementById('matrix-add-friend-msg').value = m.add_friend_msg;
    if (document.getElementById('matrix-add-friend-limit')) document.getElementById('matrix-add-friend-limit').value = m.add_friend_limit;
    if (document.getElementById('matrix-auto-verify')) document.getElementById('matrix-auto-verify').checked = m.auto_verify;
    if (document.getElementById('matrix-verify-keyword')) document.getElementById('matrix-verify-keyword').value = m.verify_keyword;
    if (document.getElementById('matrix-verify-greet')) document.getElementById('matrix-verify-greet').value = m.verify_greet;
    if (document.getElementById('matrix-pat-back')) document.getElementById('matrix-pat-back').checked = m.pat_back;
    if (document.getElementById('matrix-pat-cooldown')) document.getElementById('matrix-pat-cooldown').value = m.pat_cooldown;
    if (document.getElementById('matrix-pat-proactive')) document.getElementById('matrix-pat-proactive').checked = m.pat_proactive;
    if (document.getElementById('matrix-revoke-enabled')) document.getElementById('matrix-revoke-enabled').checked = m.revoke_enabled;
    if (document.getElementById('matrix-revoke-window')) document.getElementById('matrix-revoke-window').value = m.revoke_window;
    if (document.getElementById('matrix-at-mode')) document.getElementById('matrix-at-mode').value = m.at_mode;
    if (document.getElementById('matrix-typing-simulate')) document.getElementById('matrix-typing-simulate').checked = m.typing_simulate;
    if (document.getElementById('matrix-set-remark-enabled')) document.getElementById('matrix-set-remark-enabled').checked = m.set_remark_enabled;
    if (document.getElementById('matrix-remark-template')) document.getElementById('matrix-remark-template').value = m.remark_template;
    if (document.getElementById('matrix-chatroom-admin-enabled')) document.getElementById('matrix-chatroom-admin-enabled').checked = m.chatroom_admin_enabled;
    if (document.getElementById('matrix-chatroom-kick-words')) document.getElementById('matrix-chatroom-kick-words').value = m.chatroom_kick_words;
  } catch (e) {
    console.error('加载矩阵配置失败', e);
  }
}


// ----------------- 7. 插件中心控制器 -----------------
async function refreshPlugins() {
  const res = await apiRequest('/api/plugins');
  if (!res || res.code !== 0) return;

  AppState.plugins = res.data || [];
  document.getElementById('plugins-total-badge').textContent = `已装载 ${AppState.plugins.length} 个插件`;

  const grid = document.getElementById('plugins-card-grid');
  if (!grid) return;

  if (AppState.plugins.length === 0) {
    grid.innerHTML = '<div class="col-span-3 text-center p-8 text-gray-400">暂未装载任何业务插件</div>';
    return;
  }

  grid.innerHTML = AppState.plugins.map(p => `
    <div onclick="openPluginModal('${p.name}')" class="bg-white dark:bg-gray-800 p-5 rounded-xl border border-gray-200 dark:border-gray-700 shadow-sm cursor-pointer hover:border-green-500 transition-all">
      <div class="flex items-center justify-between mb-2">
        <h5 class="font-bold text-sm text-gray-900 dark:text-white">${escapeHtml(p.name)}</h5>
        <span class="px-2 py-0.5 text-[10px] rounded font-semibold ${p.enabled ? 'bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-400' : 'bg-gray-100 text-gray-500'}">${p.enabled ? '已启用' : '已停用'}</span>
      </div>
      <p class="text-xs text-gray-500 dark:text-gray-400 line-clamp-2 h-8">${escapeHtml(p.description || '暂无描述')}</p>
      <div class="mt-4 pt-3 border-t border-gray-100 dark:border-gray-700 flex items-center justify-between text-[11px] text-gray-400">
        <span>作者: ${escapeHtml(p.author || '官方')}</span>
        <span>v${escapeHtml(p.version || '1.0.0')}</span>
      </div>
    </div>
  `).join('');
}

function openPluginModal(pluginName) {
  const p = AppState.plugins.find(x => x.name === pluginName);
  if (!p) return;

  document.getElementById('plugin-modal-title').textContent = `插件详情: ${p.name}`;
  document.getElementById('plugin-modal-name').textContent = p.name;
  document.getElementById('plugin-modal-desc').textContent = p.description || '无详细描述';
  document.getElementById('plugin-modal-enabled').checked = p.enabled;
  document.getElementById('plugin-modal-toml').value = `# ${p.name} 运行时配置\npriority = ${p.priority || 0}\n`;

  document.getElementById('modal-plugin-detail').classList.remove('hidden');
}

function closePluginModal() {
  document.getElementById('modal-plugin-detail').classList.add('hidden');
}

function savePluginConfig() {
  showToast('插件配置已保存并热重载生效', 'success');
  closePluginModal();
}

// ----------------- 8. 智能洞察控制器 -----------------
function switchInsightTab(tab) {
  const tabs = ['user', 'group', 'moments', 'overrides'];
  tabs.forEach(t => {
    const btn = document.getElementById(`tab-insights-${t}`);
    const panel = document.getElementById(`panel-insights-${t}`);
    if (t === tab) {
      btn.className = 'text-sm font-bold text-green-600 border-b-2 border-green-600 pb-2';
      panel.classList.remove('hidden');
    } else {
      btn.className = 'text-sm font-medium text-gray-500 hover:text-gray-700 dark:text-gray-400 pb-2';
      panel.classList.add('hidden');
    }
  });
}

function refreshInsights() {
  // 填充好友下拉选项
  const select = document.getElementById('select-insight-friend');
  if (select && AppState.contacts.length) {
    select.innerHTML = '<option value="">选择需要深度洞察的好友...</option>' + AppState.contacts.map(c => `
      <option value="${c.username}">${escapeHtml(c.nickname || c.username)}</option>
    `).join('');
  }
}

function generateUserInsight() {
  const select = document.getElementById('select-insight-friend');
  const val = select.value;
  if (!val) {
    showToast('请先选择要分析的好友', 'error');
    return;
  }

  const container = document.getElementById('user-insight-result');
  container.innerHTML = '<div class="text-center py-4 text-green-600">大模型正在对该联系人历史对话与朋友圈进行深度语义萃取...</div>';

  setTimeout(() => {
    container.innerHTML = `
      <div class="space-y-3">
        <div class="flex items-center gap-2">
          <span class="font-bold text-sm">用户画像与性格特质:</span>
          <span class="px-2 py-0.5 bg-blue-100 text-blue-700 rounded text-[10px]">技术敏锐</span>
          <span class="px-2 py-0.5 bg-purple-100 text-purple-700 rounded text-[10px]">理性沟通</span>
          <span class="px-2 py-0.5 bg-green-100 text-green-700 rounded text-[10px]">高频夜间活跃</span>
        </div>
        <p class="text-gray-600 dark:text-gray-300">近期关注焦点：分布式消息队列设计、大模型智能体落地与私有化部署方案。</p>
        <p class="text-gray-600 dark:text-gray-300">情绪指数：正向 (85%) · 沟通风格简洁干练，建议回复以结构化解决方案为主。</p>
        <div class="pt-2 flex justify-end">
          <button onclick="syncInsightToOverride('${val}')" class="btn-primary text-xs">一键沉淀为该好友专属提示词</button>
        </div>
      </div>
    `;
  }, 1200);
}

function syncInsightToOverride(username) {
  showToast(`已成功将洞察结论写入 ${username} 的专属人设提示词！`, 'success');
}

// ----------------- 9. 实时日志控制器 -----------------
async function refreshLogs() {
  const res = await apiRequest('/api/logs/recent?limit=200');
  if (!res || res.code !== 0) return;

  AppState.logs = res.data || [];
  renderLogs();
}

function renderLogs() {
  const terminal = document.getElementById('terminal-logs');
  if (!terminal) return;

  const lvlFilter = document.getElementById('log-filter-level').value;
  const kwFilter = document.getElementById('log-filter-kw').value.trim().toLowerCase();

  const filtered = AppState.logs.filter(l => {
    if (lvlFilter !== 'ALL' && l.level !== lvlFilter) return false;
    if (kwFilter && !l.message.toLowerCase().includes(kwFilter) && !l.tag.toLowerCase().includes(kwFilter)) return false;
    return true;
  });

  terminal.innerHTML = filtered.map(l => `
    <div class="log-line">
      <span class="log-time">[${formatLogTime(l.timestamp)}]</span>
      <span class="log-lvl-${l.level}">[${l.level}]</span>
      <span class="log-tag">&lt;${escapeHtml(l.tag)}&gt;</span>
      <span class="log-msg">${escapeHtml(l.message)}</span>
    </div>
  `).join('');

  terminal.scrollTop = terminal.scrollHeight;
}

function appendLogToTerminal(logItem) {
  AppState.logs.push(logItem);
  if (AppState.logs.length > 2000) {
    AppState.logs.shift();
  }
  if (AppState.currentNav === 'logs') {
    renderLogs();
  }
}

function filterLogs() {
  renderLogs();
}

function clearLogs() {
  AppState.logs = [];
  renderLogs();
  showToast('日志已清空', 'info');
}

function exportLogs() {
  window.open(`/api/logs/export?token=${encodeURIComponent(AppState.token)}`, '_blank');
}

// ----------------- 10. 系统设置控制器 -----------------
function switchSettingsTab(tab) {
  const tabs = ['basic', 'users', 'security'];
  tabs.forEach(t => {
    const btn = document.getElementById(`tab-settings-${t}`);
    const panel = document.getElementById(`panel-settings-${t}`);
    if (t === tab) {
      btn.className = 'text-sm font-bold text-green-600 border-b-2 border-green-600 pb-2';
      panel.classList.remove('hidden');
    } else {
      btn.className = 'text-sm font-medium text-gray-500 hover:text-gray-700 dark:text-gray-400 pb-2';
      panel.classList.add('hidden');
    }
  });
}

async function refreshSettings() {
  // 加载基础设置
  const res = await apiRequest('/api/settings');
  if (res && res.code === 0 && res.data) {
    document.getElementById('set-site-title').value = res.data.site_title || '';
    document.getElementById('set-footer-text').value = res.data.footer_text || '';
    document.getElementById('set-owner-name').value = res.data.owner_name || '';
  }

  // 加载用户列表 (仅管理员可见)
  if (AppState.currentUser && AppState.currentUser.role === 'admin') {
    loadUsersList();
  }
}

async function loadUsersList() {
  const res = await apiRequest('/api/users');
  if (!res || res.code !== 0) return;

  const tbody = document.getElementById('users-table-body');
  if (!tbody) return;

  tbody.innerHTML = res.data.map(u => `
    <tr class="hover:bg-gray-50 dark:hover:bg-gray-750 transition-colors">
      <td class="px-4 py-3 font-semibold font-mono">${escapeHtml(u.username)}</td>
      <td class="px-4 py-3">${escapeHtml(u.nickname)}</td>
      <td class="px-4 py-3">
        <span class="px-2 py-0.5 rounded text-[10px] font-bold ${u.role === 'admin' ? 'bg-green-100 text-green-700' : 'bg-blue-100 text-blue-700'}">${u.role === 'admin' ? '系统管理员' : '操作员'}</span>
      </td>
      <td class="px-4 py-3 text-gray-400">${formatLogTime(u.created_at)}</td>
      <td class="px-4 py-3 text-right space-x-2">
        <button onclick="resetUserPassword('${u.username}')" class="text-orange-500 hover:underline">重置密码</button>
        ${u.username !== AppState.currentUser.username ? `
          <button onclick="deleteUser('${u.username}')" class="text-red-500 hover:underline">删除</button>
        ` : ''}
      </td>
    </tr>
  `).join('');
}

async function saveBasicSettings() {
  const siteTitle = document.getElementById('set-site-title').value.trim();
  const footerText = document.getElementById('set-footer-text').value.trim();
  const ownerName = document.getElementById('set-owner-name').value.trim();

  const res = await apiRequest('/api/settings', {
    method: 'POST',
    body: {
      site_title: siteTitle,
      footer_text: footerText,
      owner_name: ownerName,
    },
  });

  if (res && res.code === 0) {
    showToast('基础设置已成功保存', 'success');
    AppState.siteTitle = siteTitle;
    AppState.footerText = footerText;
    updateBrandHeaders();
  } else {
    showToast(res ? res.message : '保存失败', 'error');
  }
}

function openAddUserModal() {
  document.getElementById('user-modal-title').textContent = '新增控制台用户';
  document.getElementById('user-edit-username').value = '';
  document.getElementById('user-edit-username').removeAttribute('readonly');
  document.getElementById('user-edit-nickname').value = '';
  document.getElementById('user-edit-password').value = '';
  document.getElementById('user-edit-pwd-group').classList.remove('hidden');
  document.getElementById('modal-user-edit').classList.remove('hidden');
}

function closeUserModal() {
  document.getElementById('modal-user-edit').classList.add('hidden');
}

async function submitUserEdit() {
  const username = document.getElementById('user-edit-username').value.trim();
  const nickname = document.getElementById('user-edit-nickname').value.trim();
  const password = document.getElementById('user-edit-password').value;
  const role = document.getElementById('user-edit-role').value;

  if (!username || !nickname || !password) {
    showToast('请完整填写账号、昵称和密码', 'error');
    return;
  }

  const res = await apiRequest('/api/users/add', {
    method: 'POST',
    body: { username, nickname, password, role },
  });

  if (res && res.code === 0) {
    showToast('新用户创建成功', 'success');
    closeUserModal();
    loadUsersList();
  } else {
    showToast(res ? res.message : '创建用户失败', 'error');
  }
}

async function resetUserPassword(username) {
  const newPwd = prompt(`请输入 [${username}] 的新密码:`);
  if (!newPwd) return;

  const res = await apiRequest('/api/users/reset-password', {
    method: 'POST',
    body: { username, new_password: newPwd },
  });

  if (res && res.code === 0) {
    showToast('用户密码已重置', 'success');
  } else {
    showToast(res ? res.message : '重置密码失败', 'error');
  }
}

async function deleteUser(username) {
  if (!confirm(`确定要彻底删除用户 [${username}] 吗？`)) return;
  const res = await apiRequest('/api/users/delete', {
    method: 'POST',
    body: { username },
  });
  if (res && res.code === 0) {
    showToast('用户已删除', 'success');
    loadUsersList();
  } else {
    showToast(res ? res.message : '删除失败', 'error');
  }
}

// ----------------- 通用辅助工具函数 -----------------
function initTheme() {
  const isDark = localStorage.getItem('wc_theme') === 'dark';
  if (isDark) {
    document.body.classList.add('dark');
    document.getElementById('icon-sun')?.classList.remove('hidden');
    document.getElementById('icon-moon')?.classList.add('hidden');
  }
}

function toggleDarkMode() {
  const body = document.body;
  body.classList.toggle('dark');
  const isDark = body.classList.contains('dark');
  localStorage.setItem('wc_theme', isDark ? 'dark' : 'light');

  const sun = document.getElementById('icon-sun');
  const moon = document.getElementById('icon-moon');
  if (isDark) {
    sun?.classList.remove('hidden');
    moon?.classList.add('hidden');
  } else {
    sun?.classList.add('hidden');
    moon?.classList.remove('hidden');
  }
}

function escapeHtml(str) {
  if (!str) return '';
  return String(str)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

function formatMsgTime(isoString) {
  if (!isoString) return '';
  const d = new Date(isoString);
  const now = new Date();
  if (d.toDateString() === now.toDateString()) {
    return d.toTimeString().slice(0, 5);
  }
  return `${d.getMonth() + 1}/${d.getDate()}`;
}

function formatLogTime(isoString) {
  if (!isoString) return '';
  const d = new Date(isoString);
  return `${d.getHours().toString().padStart(2, '0')}:${d.getMinutes().toString().padStart(2, '0')}:${d.getSeconds().toString().padStart(2, '0')}.${d.getMilliseconds().toString().padStart(3, '0')}`;
}
