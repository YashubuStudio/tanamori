'use strict';

const $ = (selector) => document.querySelector(selector);
const state = { folders: [], classifications: [], settings: null, timer: null };

async function api(path, options = {}) {
  const init = { ...options, headers: { ...(options.headers || {}) } };
  if (init.body && typeof init.body !== 'string') {
    init.headers['Content-Type'] = 'application/json';
    init.body = JSON.stringify(init.body);
  }
  const response = await fetch(path, init);
  const contentType = response.headers.get('content-type') || '';
  const payload = contentType.includes('application/json') ? await response.json() : await response.text();
  if (!response.ok) {
    const message = payload && payload.error ? payload.error : String(payload || `HTTP ${response.status}`);
    throw new Error(message);
  }
  return payload;
}

function node(tag, className, text) {
  const element = document.createElement(tag);
  if (className) element.className = className;
  if (text !== undefined) element.textContent = text;
  return element;
}

function empty(container, message) {
  container.replaceChildren(node('div', 'empty-card', message));
}

let toastTimer;
function toast(message, error = false) {
  const element = $('#toast');
  element.textContent = message;
  element.className = `toast show${error ? ' error' : ''}`;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => { element.className = 'toast'; }, 4200);
}

function setMessage(selector, message, error = false) {
  const element = $(selector);
  element.textContent = message;
  element.classList.toggle('error', error);
}

function buttonBusy(form, busy) {
  const button = form.querySelector('button[type="submit"]');
  if (button) button.disabled = busy;
}

function shortPath(path, maximum = 78) {
  if (!path || path.length <= maximum) return path || '—';
  return `…${path.slice(-(maximum - 1))}`;
}

function percent(value) {
  return `${Math.round((Number(value) || 0) * 100)}%`;
}

function statusLabel(value) {
  return ({ queued: '待機中', running: '処理中', succeeded: '完了', failed: '失敗' })[value] || value;
}

async function refreshApp() {
  const info = await api('/v1/app');
  $('#appVersion').textContent = info.version || '';
  $('#modelName').textContent = info.model_id || '軽量キーワード分類';
  $('#modelState').textContent = info.model_ready ? `準備完了・${info.workers}ワーカー` : (info.model_message || 'モデル未導入・軽量モード');
  $('#modelState').style.color = info.model_ready ? '#cddd76' : '#f1c886';
  if (info.test_path && !$('#discoverPath').value) $('#discoverPath').value = info.test_path;
}

async function refreshQueue() {
  const counts = await api('/v1/queue');
  $('#countQueued').textContent = counts.queued;
  $('#countRunning').textContent = counts.running;
  $('#countSucceeded').textContent = counts.succeeded;
  $('#countFailed').textContent = counts.failed;
  return counts;
}

async function refreshJobs() {
  const jobs = await api('/v1/jobs?limit=60');
  const body = $('#jobsBody');
  body.replaceChildren();
  if (!jobs.length) {
    const row = node('tr');
    const cell = node('td', 'empty', 'まだジョブがありません。');
    cell.colSpan = 5;
    row.append(cell); body.append(row);
    return;
  }
  jobs.forEach((job) => {
    const row = node('tr');
    const statusCell = node('td');
    statusCell.append(node('span', `badge ${job.status}`, statusLabel(job.status)));
    const pathCell = node('td', 'path-cell', shortPath(job.path));
    pathCell.title = job.path;
    const detail = job.error || (job.file_id ? `file: ${job.file_id}` : '—');
    row.append(statusCell, pathCell, node('td', '', job.kind === 'scan' ? 'フォルダー走査' : 'ファイル'), node('td', '', String(job.attempts)), node('td', '', detail));
    body.append(row);
  });
}

async function refreshFolders() {
  state.folders = await api('/v1/folders');
  const list = $('#folderList');
  list.replaceChildren();
  if (!state.folders.length) {
    empty(list, '分類先が未登録です。');
    return;
  }
  state.folders.forEach((folder) => {
    const item = node('div', 'folder-item');
    item.append(node('strong', '', folder.name));
    const purpose = folder.purpose ? ` — ${folder.purpose}` : '';
    item.append(node('p', '', `${folder.current_path}${purpose}`));
    list.append(item);
  });
}

async function refreshSettings() {
  state.settings = await api('/v1/settings');
  const fields = { 会計: 'priorityAccounting', 会議: 'priorityMeeting', 顧客名: 'priorityCustomer', ホームページ: 'priorityWebsite', 修正依頼: 'priorityRevision' };
  Object.entries(fields).forEach(([tag, id]) => { $(`#${id}`).value = state.settings.priorities[tag]; });
  $('#renamePattern').value = state.settings.rename_pattern;
}

async function refreshClassifications() {
  state.classifications = await api('/v1/classifications?limit=100');
  const list = $('#classificationList');
  const summary = $('#classificationSummary');
  list.replaceChildren(); summary.replaceChildren();
  if (!state.classifications.length) {
    empty(list, 'まだ分類結果がありません。取り込みを開始するとここへ表示されます。');
    return;
  }
  let matches = 0, mismatches = 0, proposals = 0;
  state.classifications.forEach((item) => {
    const top = item.top;
    const hasExpected = Boolean(item.current_folder_id);
    const isMatch = Boolean(top && hasExpected && top.folder_id === item.current_folder_id);
    if (isMatch) matches += 1;
    else if (top && hasExpected) mismatches += 1;
    if (item.proposal_id) proposals += 1;

    const row = node('article', 'result-row');
    const file = node('div');
    file.append(node('h3', '', item.name));
    const path = node('p', '', shortPath(item.path)); path.title = item.path; file.append(path);
    if (item.tags && item.tags.length) file.append(node('small', '', `関連タグ: ${item.tags.join('、')}`));
    const destination = node('div', 'destination');
    destination.append(node('strong', '', top ? top.folder_name : '候補なし'));
    destination.append(node('small', '', item.current_folder ? `現在: ${item.current_folder}` : '現在の分類先: 未登録'));
    if (top && top.priority) destination.append(node('small', '', `タグ優先度: ${top.priority}`));
    const score = node('div', 'score', top ? percent(top.score) : '—');
    score.append(node('small', '', ' 関連度'));
    let label = '未判定', className = 'queued';
    if (item.extract_status !== 'ok') { label = '抽出失敗'; className = 'failed'; }
    else if (isMatch) { label = '一致'; className = 'match'; }
    else if (item.proposal_id) { label = '移動提案'; className = 'proposal'; }
    else if (top && hasExpected) { label = '不一致'; className = 'mismatch'; }
    const status = node('div'); status.append(node('span', `badge ${className}`, label));
    row.append(file, destination, score, status);
    list.append(row);
  });
  const evaluated = matches + mismatches;
  if (evaluated) summary.append(node('span', 'summary-chip', `正解率 ${Math.round(matches / evaluated * 100)}%（${matches}/${evaluated}）`));
  summary.append(node('span', 'summary-chip', `表示 ${state.classifications.length}件`));
  if (proposals) summary.append(node('span', 'summary-chip', `移動提案 ${proposals}件`));
}

async function refreshProposals() {
  const proposals = await api('/v1/proposals?state=pending&limit=100');
  const list = $('#proposalList');
  list.replaceChildren();
  if (!proposals.length) {
    empty(list, '保留中の移動提案はありません。');
    return;
  }
  proposals.forEach((proposal) => {
    const fileName = proposal.file_name || (proposal.file_path || '').split(/[\\/]/).pop() || '元ファイル名を取得できません';
    const folderName = proposal.folder_name || '移動先の登録がありません';
    const card = node('article', 'proposal-card');
    card.append(node('h3', '', fileName));
    card.append(node('p', '', `移動先: ${folderName}`));
    card.append(node('p', '', proposal.folder_path || '移動先のパスを取得できません'));
    const actions = node('div', 'actions');
    actions.append(node('span', 'score', percent(proposal.score)));
    const button = node('button', 'primary', '確認して移動');
    button.type = 'button';
    button.addEventListener('click', async () => {
      button.disabled = true;
      try {
        const base = `/v1/proposals/${encodeURIComponent(proposal.proposal_id)}`;
        const preview = await api(`${base}/preview`);
        const apply = async (mode, renamePattern = '') => {
          try {
            if (mode === 'skip') {
              await api(`${base}/reject`, { method: 'POST', body: { reason: '同名ファイルがあるため保存しない' } });
              toast('この移動提案は保存せず終了しました。');
            } else {
              await api(`${base}/apply`, { method: 'POST', body: { reason: 'Web UIで確認して承認', on_conflict: mode, rename_pattern: renamePattern } });
              toast('ファイルを移動しました。');
            }
            await refreshAll();
          } catch (error) { toast(error.message, true); button.disabled = false; }
        };
        if (!preview.conflict) {
          if (!window.confirm(`「${preview.file_name}」を\n${preview.destination_path}\nへ移動しますか？`)) { button.disabled = false; return; }
          await apply('rename');
          return;
        }
        const panel = node('div', 'conflict-panel');
        panel.append(node('strong', '', `同名ファイルがあります: ${preview.destination_path}`));
        panel.append(node('p', '', '保存方法を選んでください。上書きは既存ファイルを置き換えます。'));
        const pattern = node('input'); pattern.type = 'text'; pattern.value = state.settings?.rename_pattern || '{name} ({n}){ext}';
        pattern.setAttribute('aria-label', '名前を変えて保存する命名ルール');
        panel.append(pattern);
        const choices = node('div', 'conflict-options');
        const overwrite = node('button', 'danger-button', '上書き保存'); overwrite.type = 'button';
        overwrite.addEventListener('click', () => { if (window.confirm(`既存の「${fileName}」を上書きしますか？この操作は元に戻せません。`)) apply('overwrite'); });
        const rename = node('button', 'secondary', '名前を変えて保存'); rename.type = 'button';
        rename.addEventListener('click', () => apply('rename', pattern.value.trim()));
        const skip = node('button', 'secondary', '保存しない'); skip.type = 'button';
        skip.addEventListener('click', () => apply('skip'));
        choices.append(overwrite, rename, skip); panel.append(choices); card.append(panel);
      } catch (error) {
        toast(error.message, true);
        button.disabled = false;
      }
    });
    actions.append(button); card.append(actions); list.append(card);
  });
}

async function refreshAll() {
  const button = $('#refreshAll');
  button.classList.add('loading');
  try {
    await Promise.all([refreshApp(), refreshQueue(), refreshJobs(), refreshFolders(), refreshSettings()]);
    await Promise.all([refreshClassifications(), refreshProposals()]);
    $('#serverStatus').textContent = 'サーバー稼働中';
    $('#serverStatus').className = 'status ok';
  } catch (error) {
    $('#serverStatus').textContent = '接続エラー';
    $('#serverStatus').className = 'status error';
    toast(error.message, true);
  } finally {
    button.classList.remove('loading');
  }
}

$('#discoverForm').addEventListener('submit', async (event) => {
  event.preventDefault(); const form = event.currentTarget; buttonBusy(form, true);
  setMessage('#discoverMessage', '分類先を登録し、キューへ投入しています…');
  try {
    const result = await api('/v1/folders/discover', { method: 'POST', body: { path: $('#discoverPath').value.trim(), enqueue: true } });
    setMessage('#discoverMessage', `${result.folders.length}分類を登録し、${result.jobs.length}件の走査を開始しました。`);
    toast('分類テストを開始しました。結果は順次表示されます。');
    await refreshAll();
  } catch (error) { setMessage('#discoverMessage', error.message, true); toast(error.message, true); }
  finally { buttonBusy(form, false); }
});

$('#enqueueForm').addEventListener('submit', async (event) => {
  event.preventDefault(); const form = event.currentTarget; buttonBusy(form, true);
  try {
    const job = await api('/v1/jobs', { method: 'POST', body: { path: $('#enqueuePath').value.trim() } });
    setMessage('#enqueueMessage', `受付完了: ${job.job_id}`); toast('キューへ追加しました。');
    await Promise.all([refreshQueue(), refreshJobs()]);
  } catch (error) { setMessage('#enqueueMessage', error.message, true); toast(error.message, true); }
  finally { buttonBusy(form, false); }
});

async function* walkDroppedEntry(entry, prefix = '') {
  if (entry.isFile) {
    const file = await new Promise((resolve, reject) => entry.file(resolve, reject));
    yield { file, path: prefix + file.name };
  } else if (entry.isDirectory) {
    const reader = entry.createReader();
    while (true) {
      const entries = await new Promise((resolve, reject) => reader.readEntries(resolve, reject));
      if (!entries.length) break;
      for (const child of entries) yield* walkDroppedEntry(child, `${prefix}${entry.name}/`);
    }
  }
}

async function uploadItems(items) {
  const session = crypto.randomUUID().replaceAll('-', '');
  let accepted = 0, failed = 0;
  for await (const item of items) {
    if (item.file.size > 128 * 1024 * 1024) { failed += 1; continue; }
    const query = new URLSearchParams({ session, path: item.path });
    let uploaded = false;
    let failedThis = false;
    for (let attempt = 0; attempt < 60; attempt++) {
      const response = await fetch(`/v1/uploads?${query}`, { method: 'POST', headers: { 'Content-Type': 'application/octet-stream' }, body: item.file });
      const result = await response.json();
      if (response.ok) { accepted += 1; uploaded = true; break; }
      if (response.status !== 429) { failed += 1; failedThis = true; toast(`${item.path}: ${result.error || '取り込み失敗'}`, true); break; }
      setMessage('#uploadMessage', `${accepted}件登録済み。キューの空きを待っています…`);
      await new Promise((resolve) => setTimeout(resolve, 2000));
    }
    if (!uploaded && !failedThis) failed += 1;
    setMessage('#uploadMessage', `${accepted}件をキューへ登録、${failed}件失敗。`);
  }
  await Promise.all([refreshQueue(), refreshJobs()]);
  toast(`${accepted}件を取り込みました${failed ? `（${failed}件失敗）` : ''}。`, failed > 0);
}

async function* pickerItems(files) {
  for (const file of files) yield { file, path: file.webkitRelativePath || file.name };
}

for (const id of ['pickFiles', 'pickFolder']) {
  $(`#${id}`).addEventListener('change', async (event) => {
    try { await uploadItems(pickerItems(event.target.files)); }
    catch (error) { toast(error.message, true); }
    finally { event.target.value = ''; }
  });
}

const dropZone = $('#dropZone');
dropZone.addEventListener('dragover', (event) => { event.preventDefault(); dropZone.classList.add('drag-over'); });
dropZone.addEventListener('dragleave', () => dropZone.classList.remove('drag-over'));
dropZone.addEventListener('drop', async (event) => {
  event.preventDefault(); dropZone.classList.remove('drag-over');
  async function* droppedItems() {
    const entries = Array.from(event.dataTransfer.items || []).map((item) => item.webkitGetAsEntry?.()).filter(Boolean);
    if (entries.length) {
      for (const entry of entries) yield* walkDroppedEntry(entry);
    } else {
      for (const file of event.dataTransfer.files) yield { file, path: file.name };
    }
  }
  try { await uploadItems(droppedItems()); }
  catch (error) { toast(error.message, true); }
});

$('#settingsForm').addEventListener('submit', async (event) => {
  event.preventDefault();
  const priorities = {
    会計: Number($('#priorityAccounting').value), 会議: Number($('#priorityMeeting').value),
    顧客名: Number($('#priorityCustomer').value), ホームページ: Number($('#priorityWebsite').value),
    修正依頼: Number($('#priorityRevision').value)
  };
  try {
    state.settings = await api('/v1/settings', { method: 'PUT', body: { priorities, rename_pattern: $('#renamePattern').value.trim() } });
    setMessage('#settingsMessage', '設定を保存しました。次に処理する文書から反映します。');
  } catch (error) { setMessage('#settingsMessage', error.message, true); }
});

$('#resetForm').addEventListener('submit', async (event) => {
  event.preventDefault();
  const body = { documents: $('#resetDocuments').checked, folders: $('#resetFolders').checked, confirm: $('#resetConfirm').value.trim() };
  if (body.confirm !== '削除') { setMessage('#resetMessage', '確認欄に「削除」と入力してください。', true); return; }
  if (!body.documents && !body.folders) { setMessage('#resetMessage', '削除対象を選択してください。', true); return; }
  if (!window.confirm('選択したTANAMORIの登録データを一括削除します。元ファイルは残ります。続けますか？')) return;
  try {
    const result = await api('/v1/data/reset', { method: 'POST', body });
    setMessage('#resetMessage', `削除完了: 文書${result.files}件、分類先${result.folders}件、処理履歴${result.jobs}件。`);
    $('#resetConfirm').value = '';
    await refreshAll();
  } catch (error) { setMessage('#resetMessage', error.message, true); }
});

$('#folderForm').addEventListener('submit', async (event) => {
  event.preventDefault(); const form = event.currentTarget; buttonBusy(form, true);
  try {
    const folder = await api('/v1/folders', { method: 'POST', body: {
      path: $('#folderPath').value.trim(), name: $('#folderName').value.trim(),
      purpose: $('#folderPurpose').value.trim(), description: $('#folderDescription').value.trim()
    }});
    setMessage('#folderMessage', `「${folder.name}」を登録しました。`); toast('分類先を登録しました。');
    await refreshFolders();
  } catch (error) { setMessage('#folderMessage', error.message, true); toast(error.message, true); }
  finally { buttonBusy(form, false); }
});

$('#searchForm').addEventListener('submit', async (event) => {
  event.preventDefault();
  const results = $('#searchResults'); results.replaceChildren(node('div', 'empty-card', '検索中…'));
  try {
    const hits = await api(`/v1/search?q=${encodeURIComponent($('#searchQuery').value.trim())}&limit=50`);
    results.replaceChildren();
    if (!hits.length) { empty(results, '該当する文書はありません。'); return; }
    hits.forEach((hit) => {
      const card = node('article', 'search-hit');
      card.append(node('strong', '', hit.name), node('small', '', hit.path), node('p', '', hit.snippet || '本文の抜粋はありません。'));
      results.append(card);
    });
  } catch (error) { empty(results, error.message); toast(error.message, true); }
});

document.querySelectorAll('[data-refresh]').forEach((button) => {
  button.addEventListener('click', async () => {
    const actions = { jobs: refreshJobs, folders: refreshFolders, classifications: refreshClassifications, proposals: refreshProposals };
    try { await actions[button.dataset.refresh](); } catch (error) { toast(error.message, true); }
  });
});
$('#refreshAll').addEventListener('click', refreshAll);

const observer = new IntersectionObserver((entries) => {
  entries.forEach((entry) => {
    if (!entry.isIntersecting) return;
    document.querySelectorAll('.nav-link').forEach((link) => link.classList.toggle('active', link.getAttribute('href') === `#${entry.target.id}`));
  });
}, { rootMargin: '-30% 0px -65%' });
document.querySelectorAll('main > section[id]').forEach((section) => observer.observe(section));

refreshAll();
state.timer = window.setInterval(async () => {
  try {
    const counts = await refreshQueue();
    await refreshJobs();
    if (counts.running || counts.queued) await Promise.all([refreshClassifications(), refreshProposals()]);
  } catch (_) { /* next cycle retries */ }
}, 3000);
