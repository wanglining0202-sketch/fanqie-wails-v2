// ═══════════════════════════════════════════════════════════
// 番茄小说下载器 — Wails 桌面版
// Go 函数调用: window.go.main.App.Method(args).then(JSON.parse)
// ═══════════════════════════════════════════════════════════

const $ = (s) => document.querySelector(s);
const $$ = (s) => document.querySelectorAll(s);

// ── 状态 ──
const STORE = "fanqie-wails";
const state = {
  activeTab: "search",
  searchResults: [],
  booksCache: {},
  selectedBook: null,
  shelf: [],
  history: [],
  format: "TXT",
  defaultDir: "",
  chapterRange: 6,
  diagnostics: false,
};

function loadState() {
  try {
    const raw = localStorage.getItem(STORE);
    if (raw) Object.assign(state, JSON.parse(raw));
  } catch {}
}
function persist() {
  localStorage.setItem(STORE, JSON.stringify({
    shelf: state.shelf, history: state.history.slice(0, 200),
    format: state.format, defaultDir: state.defaultDir,
    chapterRange: state.chapterRange, diagnostics: state.diagnostics,
  }));
}

// ── Wails API 封装 ──

function goCall(method, ...args) {
  return window.go.main.App[method](...args).then(r => {
    try { return JSON.parse(r); }
    catch { return { error: "解析失败" }; }
  }).catch(e => {
    return { error: "调用失败: " + (e.message || e) };
  });
}

async function searchBooks(q) {
  return goCall("Search", q);
}

async function getBookInfo(id) {
  return goCall("GetBookInfo", id);
}

async function getTrending() {
  return goCall("GetTrending");
}

async function downloadBook(id, dir) {
  return goCall("DownloadBook", id, dir || state.defaultDir);
}

async function selectDir() {
  const dir = await window.go.main.App.SelectDirectory();
  if (dir) {
    state.defaultDir = dir;
    $("#defaultDir").value = dir;
    persist();
  }
}

// ── 激活流程 ──

function setupActivation() {
  // 激活函数 — onclick 直接绑定
  window._doActivate = async function () {
    var msg = document.getElementById("activationMsg");
    if (!msg) return;
    msg.textContent = "正在验证...";
    msg.style.color = "#4cc9f0";

    var input = document.getElementById("activationCode");
    var raw = (input && input.value) || "";
    var code = raw.replace(/-/g, "");

    if (code.length < 16) {
      msg.textContent = "请输入完整注册码";
      msg.style.color = "var(--orange)";
      return;
    }

    var btn = document.getElementById("activationBtn");
    if (btn) { btn.disabled = true; btn.textContent = "验证中..."; }

    var result = await goCall("Activate", raw);
    if (result.error) {
      msg.textContent = result.error;
      msg.style.color = "#ff6b6b";
      if (btn) { btn.disabled = false; btn.textContent = "激活"; }
    } else if (result.activated) {
      msg.textContent = "激活成功！正在启动...";
      msg.style.color = "#4cc9f0";
      setTimeout(function () {
        document.getElementById("activationOverlay").style.display = "none";
        initMain();
      }, 800);
    }
  };

  // 自动格式化
  var inp = document.getElementById("activationCode");
  if (inp) {
    inp.addEventListener("input", function () {
      var raw = inp.value.replace(/[^A-Za-z0-9]/g, "").toUpperCase();
      if (raw.length > 16) raw = raw.slice(0, 16);
      var f = "";
      for (var i = 0; i < raw.length; i++) {
        if (i > 0 && i % 4 === 0) f += "-";
        f += raw[i];
      }
      inp.value = f;
    });
  }
}

// ── 入口：检查激活 → 进主界面 ──

async function init() {
  // 兜底：3秒后如果还没反应，强制显示激活界面
  var fallbackTimer = setTimeout(function () {
    var overlay = document.getElementById("activationOverlay");
    if (overlay && overlay.style.display !== "none") return;
    if (overlay) overlay.style.display = "flex";
    setupActivation();
  }, 3000);

  try {
    var status = await goCall("CheckActivation");
    clearTimeout(fallbackTimer);
    if (!status || !status.activated) {
      document.getElementById("activationOverlay").style.display = "flex";
      setupActivation();
      return;
    }
    initMain();
  } catch (e) {
    clearTimeout(fallbackTimer);
    document.getElementById("activationOverlay").style.display = "flex";
    setupActivation();
  }
}

// ── 主界面初始化 ──

async function initMain() {
  loadState();
  if (state.defaultDir) $("#defaultDir").value = state.defaultDir;

  switchTab(state.activeTab);
  syncSegments();
  renderShelf();
  renderHistory();

  // 事件
  // 搜索按钮 — 直接 click 处理（WebView2 中 form submit 可能不触发）
  const doSearch = async () => {
    const q = $("#searchInput").value.trim();
    if (!q) return;
    $("#resultList").innerHTML = '<div class="empty-box">搜索中...</div>';

    const data = await searchBooks(q);
    if (data.error) {
      // 搜索失败 → 如果输入看起来是 book_id，直接载入
      if (/^\d{10,}$/.test(q)) {
        $("#resultList").innerHTML = '<div class="empty-box">搜索无结果，尝试直接载入书籍...</div>';
        await loadBookById(q);
        return;
      }
      $("#resultList").innerHTML = `<div class="empty-box">搜索失败: ${data.error}</div>`;
      return;
    }
    const results = data.results || [];
    if (results.length === 0 && /^\d{10,}$/.test(q)) {
      // ixdzs8 无结果 + 看起来是 book_id → 直接载入
      $("#resultList").innerHTML = '<div class="empty-box">搜索无结果，尝试直接载入书籍...</div>';
      await loadBookById(q);
      return;
    }
    state.searchResults = [];
    state.booksCache = {};
    results.forEach(r => {
      const book = { id: r.book_id, title: r.title, author: r.author,
        source: r.source, description: r.description, status: "未知" };
      state.searchResults.push(book.id);
      state.booksCache[book.id] = book;
    });
    if (results.length === 0) {
      $("#resultList").innerHTML = '<div class="empty-box">未找到相关书籍<br/>试试直接输入 book_id 点"载入"</div>';
    }
    $("#resultCount").textContent = results.length;
    renderSearchResults();
  };

  $("#searchForm").addEventListener("submit", (e) => { e.preventDefault(); doSearch(); });
  // 直接给搜索按钮绑 click（兜底 WebView2 不触发 submit）
  document.querySelector("#searchForm button.primary")?.addEventListener("click", (e) => {
    e.preventDefault();
    doSearch();
  });

  $("#loadButton").addEventListener("click", async () => {
    const id = $("#searchInput").value.trim();
    if (!id) return;
    $("#resultList").innerHTML = '<div class="empty-box">载入中...</div>';
    await loadBookById(id);
  });

  document.body.addEventListener("click", handleBodyClick);

  $$(".tab").forEach(b => b.addEventListener("click", () => switchTab(b.dataset.tab)));

  $("#refreshHistory").addEventListener("click", () => historyRefresher());
  $("#clearHistory").addEventListener("click", () => {
    state.history = []; persist(); renderHistory();
  });
  $("#saveSettings").addEventListener("click", () => {
    state.defaultDir = $("#defaultDir").value;
    state.chapterRange = Number($("#chapterRange").value) || 6;
    state.diagnostics = $("#diagnostics").checked;
    persist();
    toastTask("设置", "已保存", 100, "完成");
  });
  $("#chapterRange").addEventListener("input", () => {
    $("#chapterOutput").textContent = $("#chapterRange").value;
  });
  $("#chooseDefaultDir").addEventListener("click", selectDir);
  $("#dockClose").addEventListener("click", () => {
    $("#taskDock").style.display = "none";
  });

  // 自动载入热榜
  loadTrending();
}

async function loadTrending() {
  try {
    // 热榜在浏览时加载
  } catch {}
}

async function loadBookById(id) {
  const data = await getBookInfo(id);
  if (data.error || !data.found) {
    toastTask("载入失败", data.error || "未找到书籍", 0, "失败");
    return;
  }
  const book = {
    id: data.book_id,
    title: data.title,
    author: data.author,
    source: data.source,
    status: data.status,
    description: data.description,
    chapterCount: data.chapter_count,
    wordCount: data.word_count,
    chapters: data.chapters,
    cover: data.cover,
    intro: data.description,
  };
  state.booksCache[book.id] = book;
  state.selectedBook = book;
  if (!state.searchResults.includes(book.id)) {
    state.searchResults.unshift(book.id);
    renderSearchResults();
  }
  renderDetail(book);
}

function handleBodyClick(event) {
  const action = event.target.closest("[data-action]");
  if (!action) return;
  const bookId = action.dataset.id;
  const book = state.booksCache[bookId] || state.selectedBook;
  if (!book) return;
  const name = action.dataset.action;
  if (name === "select") {
    state.selectedBook = book;
    if (!book.status || book.status === "未知") {
      loadBookById(book.id);
    } else {
      renderDetail(book);
    }
  }
  if (name === "download") downloadViaIX(book);
  if (name === "download-fanqie") downloadFanqie(book);
  if (name === "shelf") addToShelf(book);
  if (name === "read") readOnline(book);
  if (name === "clear-cache") toastTask(book.title, "缓存已清理", 100, "完成");
  if (name === "copy-id") navigator.clipboard?.writeText(book.id);
  if (name === "open-dir") {
    const historyItem = state.history.find(h => h.id === book.id);
    const filePath = historyItem?.filePath || "";
    const dirPath = state.defaultDir;
    const fullPath = filePath || `${dirPath}\\${book.title}.txt`;
    navigator.clipboard?.writeText(fullPath);
    window.go.main.App.OpenDirectory(dirPath || fullPath);
    toastTask(book.title, `路径已复制：${fullPath}`, 100, "打开");
  }
  if (name === "format") {
    state.format = action.dataset.format;
    syncSegments();
    renderDetail(book);
    persist();
  }
  if (name === "load-chapters") loadBookById(book.id);
}

// ═══════════════════════════════════════════════════════════
// 视图渲染
// ═══════════════════════════════════════════════════════════

function switchTab(tab) {
  state.activeTab = tab;
  $$(".tab").forEach(b => b.classList.toggle("is-active", b.dataset.tab === tab));
  $$(".view").forEach(v => v.classList.toggle("is-active", v.id === `view-${tab}`));
}

function renderSearchResults() {
  const ids = state.searchResults;
  $("#resultCount").textContent = ids.length;
  if (ids.length === 0) {
    $("#resultList").innerHTML = `<div class="empty-box">搜索书籍或输入 book_id<br/>直接载入</div>`;
    return;
  }
  $("#resultList").innerHTML = ids
    .map(id => state.booksCache[id])
    .filter(Boolean)
    .map(resultCard)
    .join("");
}

function resultCard(book) {
  const cover = book.cover || makeCover(book.title);
  const desc = (book.description || book.intro || "").slice(0, 80);
  const statusText = book.status || "未知";
  const sourceTag = book.source ? ` [${book.source}]` : "";
  return `
    <article class="result-card" data-action="select" data-id="${book.id}">
      <img class="cover" src="${cover}" alt="${book.title}" />
      <div>
        <div class="book-title">${book.title}</div>
        <div class="book-meta">${book.author} · ID ${book.id}${sourceTag}</div>
        <div class="book-desc">${desc}...</div>
      </div>
      <span class="status-pill">${statusText}</span>
    </article>
  `;
}

function renderDetail(book) {
  if (!book) {
    $("#bookDetail").innerHTML = `<div class="empty-box">搜索或输入 book_id<br/>查看书籍详情</div>`;
    return;
  }
  state.selectedBook = book;
  const cover = book.cover || makeCover(book.title);
  const chapterText = book.chapterCount
    ? `${book.chapterCount.toLocaleString()} 章`
    : (book.chapters ? `${book.chapters.length} 章` : "");
  const wordText = book.wordCount
    ? `${(book.wordCount / 10000).toFixed(1)} 万字`
    : "";
  const statusText = book.status || "未知";
  const sourceTag = book.source ? ` [${book.source}]` : "";

  const fqSelected = state.format === "TXT" ? "is-active" : "";
  const epubSelected = state.format === "EPUB" ? "is-active" : "";

  $("#bookDetail").innerHTML = `
    <div class="detail-hero">
      <img class="detail-cover" src="${cover}" alt="${book.title}" />
      <div class="detail-info">
        <h3>${book.title}</h3>
        <div class="book-meta">${book.author} · ID ${book.id}${sourceTag}</div>
        <div class="detail-stats">
          <span>${chapterText}</span>
          <span>${wordText}</span>
          <span class="status-pill">${statusText}</span>
        </div>
        <p class="detail-desc">${(book.description || book.intro || "").slice(0, 300)}</p>
      </div>
    </div>
    <div class="detail-actions">
      <div class="segmented" data-setting="format">
        <button class="segment ${fqSelected}" type="button" data-action="format" data-format="TXT" data-id="${book.id}">TXT</button>
        <button class="segment ${epubSelected}" type="button" data-action="format" data-format="EPUB" data-id="${book.id}">EPUB</button>
      </div>
      <button class="primary" type="button" data-action="download" data-id="${book.id}">下载 (ixdzs8)</button>
      <button class="primary" type="button" data-action="download-fanqie" data-id="${book.id}" style="background: linear-gradient(135deg, #ff6b35, #f7c948);">番茄直链</button>
      <button class="ghost" type="button" data-action="shelf" data-id="${book.id}">加入书架</button>
      <button class="ghost" type="button" data-action="copy-id" data-id="${book.id}">复制 ID</button>
    </div>
  `;
}

function makeCover(title) {
  const hue = [...title].reduce((a, c) => a + c.charCodeAt(0), 0) % 360;
  const colors = [`hsl(${hue},60%,30%)`, `hsl(${(hue+40)%360},50%,20%)`];
  const text = title.slice(0, 4);
  return `data:image/svg+xml,${encodeURIComponent(
    `<svg xmlns="http://www.w3.org/2000/svg" width="160" height="210">
      <rect width="160" height="210" fill="${colors[0]}"/>
      <rect width="160" height="210" fill="${colors[1]}" opacity="0.6" rx="12"/>
      <text x="80" y="105" text-anchor="middle" fill="white" font-size="32" font-family="sans-serif">${text}</text>
    </svg>`
  )}`;
}

// ═══════════════════════════════════════════════════════════
// 下载
// ═══════════════════════════════════════════════════════════

function downloadViaIX(book) {
  // ixdzs8 下载 — 暂用番茄直链
  downloadFanqie(book);
}

async function downloadFanqie(book) {
  const record = {
    id: book.id, title: book.title, author: book.author,
    format: state.format,
    time: new Date().toLocaleString("zh-CN", { hour12: false }),
    status: "下载中", message: "代理API下载中...",
  };
  state.history.unshift(record);
  renderHistory();
  toastTask(book.title, "代理API极速下载中...", 10, "下载中");

  try {
    const result = await downloadBook(book.id, state.defaultDir);

    if (result.error) {
      record.status = "失败";
      record.message = result.error;
      toastTask(book.title, result.error, 0, "失败");
    } else if (result.success) {
      record.status = "完成";
      record.filePath = result.path;
      record.message = `已保存: ${result.path || ""} [${result.downloaded || "?"}/${result.total_chapters || "?"}章 · ${((result.cn_chars || 0) / 10000).toFixed(1)}万字 · ${result.elapsed_seconds || "?"}s]`;
      toastTask(book.title, `下载完成 · ${(result.cn_chars || 0).toLocaleString()} 字 · ${result.elapsed_seconds || "?"}s`, 100, "完成");
    } else {
      record.status = "失败";
      record.message = "下载返回异常";
      toastTask(book.title, "下载返回异常", 0, "失败");
    }
  } catch (e) {
    record.status = "失败";
    record.message = `请求失败: ${e.message}`;
    toastTask(book.title, `请求失败: ${e.message}`, 0, "失败");
  }

  persist();
  renderHistory();
}

function readOnline(book) {
  const desc = (book.description || book.intro || "").split("\n")[0];
  toastTask(book.title, desc, 100, "阅读");
}

// ═══════════════════════════════════════════════════════════
// 书架
// ═══════════════════════════════════════════════════════════

function addToShelf(book) {
  if (!state.shelf.find(s => s.id === book.id)) {
    state.shelf.push({ id: book.id, title: book.title, author: book.author, cover: book.cover });
    persist();
  }
  renderShelf();
  toastTask(book.title, "已加入书架", 100, "完成");
}

function renderShelf() {
  if (state.shelf.length === 0) {
    $("#shelfGrid").innerHTML = `<div class="empty-box">书架为空。搜索书籍后<br/>可添加到书架。</div>`;
    return;
  }
  $("#shelfGrid").innerHTML = state.shelf.map(book => {
    const cover = book.cover || makeCover(book.title);
    return `
      <div class="shelf-card" data-action="select" data-id="${book.id}">
        <img class="shelf-cover" src="${cover}" alt="${book.title}" />
        <div class="shelf-info">
          <strong>${book.title}</strong>
          <span>${book.author}</span>
        </div>
      </div>
    `;
  }).join("");
}

// ═══════════════════════════════════════════════════════════
// 历史
// ═══════════════════════════════════════════════════════════

function historyRefresher() {
  renderHistory();
}

function renderHistory() {
  const filter = $("#historyFilter")?.value?.trim() || "";
  const rows = state.history.filter(item =>
    [item.title, item.author, item.format].join(" ").includes(filter)
  );
  $("#statHistory").textContent = state.history.length;
  $("#statTasks").textContent = state.history.filter(item => item.status === "下载中").length;
  $("#statExists").textContent = state.history.filter(item => item.status === "完成").length;
  $("#statMissing").textContent = state.history.filter(item => item.status === "失败").length;
  $("#historyTable").innerHTML = `
    <div class="history-row header">
      <div>书籍</div><div>作者</div><div>格式</div><div>下载时间</div><div>状态</div><div>操作</div>
    </div>
    ${rows.length
      ? rows.map(item => `
      <div class="history-row">
        <div><strong>${item.title} / ID ${item.id}</strong><p>${item.message}</p></div>
        <div>${item.author}</div>
        <div>${item.format}</div>
        <div>${item.time}</div>
        <div><span class="${item.status === "失败" ? "fail" : "status-pill"}">${item.status}</span></div>
        <div><button class="ghost" type="button" data-action="open-dir" data-id="${item.id}">打开目录</button></div>
      </div>`).join("")
      : `<div class="history-row"><div>暂无下载记录</div><div></div><div></div><div></div><div></div><div></div></div>`
    }
  `;
}

function syncSegments() {
  $$("[data-format]").forEach(button =>
    button.classList.toggle("is-active", button.dataset.format === state.format)
  );
}

function toastTask(title, message, progress, status) {
  $("#taskDock").style.display = ""; // 恢复显示（关闭按钮会隐藏它）
  $("#taskTitle").textContent = title;
  $("#taskMessage").textContent = message;
  $("#taskStatus").textContent = status;
  $("#taskPercent").textContent = `进度 ${progress}%`;
  $("#taskProgress").style.width = `${progress}%`;
}

// ═══════════════════════════════════════════════════════════
// 启动
// ═══════════════════════════════════════════════════════════
init();
