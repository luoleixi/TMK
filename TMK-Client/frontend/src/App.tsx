import { useState, useEffect, useRef } from 'react';
import { SessionService } from '../bindings/tmk-client/internal/client/session';
import { CaptureService } from '../bindings/tmk-client/internal/client/capture';
import { SettingsService } from '../bindings/tmk-client/internal/client/settings';
import { ExportService } from '../bindings/tmk-client/internal/client/export';
import { WindowService } from '../bindings/tmk-client/internal/client/window';
import { Events } from '@wailsio/runtime';
import {
  ArrowRightLeft,
  Captions,
  Download,
  FileText,
  History,
  MessageSquareText,
  Mic,
  Pause,
  Play,
  Radio,
  Search,
  Sparkles,
  Square,
  Trash2,
  Volume2,
  X,
} from 'lucide-react';
import './App.css';

const LANGUAGES = [
  { code: 'zh', name: '中文' },
  { code: 'en', name: 'English' },
  { code: 'ja', name: '日本語' },
  { code: 'ko', name: '한국어' },
  { code: 'fr', name: 'Français' },
  { code: 'de', name: 'Deutsch' },
  { code: 'es', name: 'Español' },
  { code: 'ru', name: 'Русский' },
];

type RecordEntry = {
  id: number;
  sourceText: string;
  translatedText: string;
};

type DeviceInfo = {
  id: number;
  name: string;
  type: string;
};

type HistorySession = {
  id: string;
  source_lang: string;
  target_lang: string;
  record_count: number;
  brief?: string;
  summary?: string;
  created_at: string;
};

type HistoryRecord = {
  id: number;
  sequence: number;
  source_text: string;
  translated_text: string;
};

type HistoryDetail = {
  summary?: string;
  records?: HistoryRecord[];
};

type ExportRecord = {
  source_text: string;
  translated_text: string;
  sequence: number;
};

type StreamMessage = {
  seq?: number;
  segment_id?: number;
  revision?: number;
  text: string;
  is_final?: boolean;
  reason?: string;
  timestamp?: number;
};

type SubtitleSegment = {
  id: number;
  source: string;
  translation: string;
  sourceRevision: number;
  translationRevision: number;
  isFinal: boolean;
};

const DEVICE_SYSTEM_AUDIO = -2;

const streamSegmentID = (message: StreamMessage) =>
  Number(message.segment_id || message.seq || message.timestamp || Date.now());

const updateSubtitleSegments = (
  segments: SubtitleSegment[],
  message: StreamMessage,
  field: 'source' | 'translation',
) => {
  const id = streamSegmentID(message);
  const revision = Number(message.revision || message.seq || 0);
  const revisionField = field === 'source' ? 'sourceRevision' : 'translationRevision';
  const existing = segments.find(segment => segment.id === id);
  if (existing && revision < existing[revisionField]) return segments;

  const updated: SubtitleSegment = {
    id,
    source: existing?.source || '',
    translation: existing?.translation || '',
    sourceRevision: existing?.sourceRevision || 0,
    translationRevision: existing?.translationRevision || 0,
    isFinal: Boolean(existing?.isFinal || message.is_final),
    [field]: message.text,
    [revisionField]: revision,
  };
  return [...segments.filter(segment => segment.id !== id), updated]
    .sort((left, right) => left.id - right.id)
    .slice(-2);
};

function SubtitleWindow() {
  const [segments, setSegments] = useState<SubtitleSegment[]>([]);

  useEffect(() => {
    const offTranscript = Events.On('transcript', (event: any) => {
      setSegments(previous => updateSubtitleSegments(previous, event.data, 'source'));
    });
    const offTranslation = Events.On('translation', (event: any) => {
      setSegments(previous => updateSubtitleSegments(previous, event.data, 'translation'));
    });
    const offReset = Events.On('stream-reset', () => {
      setSegments([]);
    });
    return () => {
      offTranscript();
      offTranslation();
      offReset();
    };
  }, []);

  const previous = segments.length > 1 ? segments[0] : undefined;
  const current = segments[segments.length - 1];

  return (
    <div className="subtitle-surface">
      <button
        type="button"
        className="subtitle-close"
        title="关闭悬挂字幕"
        aria-label="关闭悬挂字幕"
        onClick={() => void WindowService.HideSubtitle()}
      >
        <X size={17} />
      </button>
      <div className="subtitle-content" aria-live="polite">
        {previous && (
          <div className="subtitle-previous">
            {previous.translation || previous.source}
          </div>
        )}
        <div className="subtitle-translation">
          {current?.translation || '等待翻译...'}
        </div>
        <div className="subtitle-source">
          {current?.source || '等待语音输入...'}
        </div>
      </div>
    </div>
  );
}

function MainApp() {
  const [sourceLang, setSourceLang] = useState('zh');
  const [targetLang, setTargetLang] = useState('en');
  const [running, setRunning] = useState(false);
  const [paused, setPaused] = useState(false);
  const [sourceText, setSourceText] = useState('');
  const [translatedText, setTranslatedText] = useState('');
  const [records, setRecords] = useState<RecordEntry[]>([]);
  const [status, setStatus] = useState('就绪');
  const [devices, setDevices] = useState<DeviceInfo[]>([]);
  const [selectedDevice, setSelectedDevice] = useState(DEVICE_SYSTEM_AUDIO);
  const [subtitleMounted, setSubtitleMounted] = useState(true);
  const transitioning = useRef(false);
  const sessionIdRef = useRef('');
  const sourceBySegmentRef = useRef(new Map<number, string>());
  const committedSegmentsRef = useRef(new Set<number>());
  const currentSegmentIDRef = useRef(0);
  const settingsReady = useRef(false);
  const chatEndRef = useRef<HTMLDivElement | null>(null);

  // ---- history state ----
  const [view, setView] = useState<'live' | 'history'>('live');
  const [historySessions, setHistorySessions] = useState<HistorySession[]>([]);
  const [expandedId, setExpandedId] = useState<string | null>(null);
  const [historyRecords, setHistoryRecords] = useState<HistoryRecord[]>([]);
  const [historySummary, setHistorySummary] = useState('');
  const [historyLoading, setHistoryLoading] = useState(false);
  const [historyKeyword, setHistoryKeyword] = useState('');
  const [historyDateFrom, setHistoryDateFrom] = useState('');
  const [historyDateTo, setHistoryDateTo] = useState('');
  const [selectedHistoryIds, setSelectedHistoryIds] = useState<string[]>([]);
  const briefPollAttempts = useRef(0);

  useEffect(() => {
    const offTranscript = Events.On('transcript', (event: any) => {
      const message = event.data as StreamMessage;
      const segmentID = streamSegmentID(message);
      if (segmentID !== currentSegmentIDRef.current) {
        setTranslatedText('');
      }
      currentSegmentIDRef.current = segmentID;
      sourceBySegmentRef.current.set(segmentID, message.text);
      setSourceText(message.text);
    });
    const offTranslation = Events.On('translation', (event: any) => {
      const message = event.data as StreamMessage;
      const segmentID = streamSegmentID(message);
      if (segmentID === currentSegmentIDRef.current) {
        setTranslatedText(message.text);
      }
      const segmentSource = sourceBySegmentRef.current.get(segmentID);
      if (message.is_final && segmentSource && !committedSegmentsRef.current.has(segmentID)) {
        committedSegmentsRef.current.add(segmentID);
        setRecords(prev => [...prev, {
          id: segmentID,
          sourceText: segmentSource,
          translatedText: message.text,
        }]);
      }
    });
    const offSubtitleVisibility = Events.On('subtitle-visibility-changed', (event: any) => {
      setSubtitleMounted(Boolean(event.data));
    });

    Promise.all([
      CaptureService.ListCaptureDevices(),
      SettingsService.Load(),
    ]).then(([list, settings]: [DeviceInfo[], any]) => {
      setDevices(list);
      setSourceLang(settings.source_lang || 'zh');
      setTargetLang(settings.target_lang || 'en');
      const deviceID = typeof settings.selected_device === 'number' ? settings.selected_device : DEVICE_SYSTEM_AUDIO;
      setSelectedDevice(deviceID);
      setSubtitleMounted(settings.subtitle_mounted !== false);
      if (settings.subtitle_mounted !== false) {
        void WindowService.ShowSubtitle();
      } else {
        void WindowService.HideSubtitle();
      }
      setHistoryKeyword(settings.history_keyword || '');
      setHistoryDateFrom(settings.history_date_from || '');
      setHistoryDateTo(settings.history_date_to || '');
      CaptureService.SetMicrophoneDevice(deviceID);
      settingsReady.current = true;
    });

    return () => {
      offTranscript();
      offTranslation();
      offSubtitleVisibility();
    };
  }, []);

  useEffect(() => {
    if (!settingsReady.current) return;
    if (subtitleMounted) {
      void WindowService.ShowSubtitle();
    } else {
      void WindowService.HideSubtitle();
    }
    SettingsService.Save({
      source_lang: sourceLang,
      target_lang: targetLang,
      selected_device: selectedDevice,
      subtitle_mounted: subtitleMounted,
      history_keyword: historyKeyword,
      history_date_from: historyDateFrom,
      history_date_to: historyDateTo,
    }).catch((e: any) => setStatus('设置保存失败: ' + (e?.message || e)));
  }, [sourceLang, targetLang, selectedDevice, subtitleMounted, historyKeyword, historyDateFrom, historyDateTo]);

  // ---- live translate ----

  const inputType = () => selectedDevice === DEVICE_SYSTEM_AUDIO ? 'system_audio' : 'microphone';

  const handleStart = async () => {
    if (transitioning.current) return;
    transitioning.current = true;
    try {
      sourceBySegmentRef.current.clear();
      committedSegmentsRef.current.clear();
      currentSegmentIDRef.current = 0;
      if (paused) {
        await (SessionService as any).ResumeInterpret();
      } else {
        setStatus('创建会话中...');
        setRecords([]);
        setSourceText('');
        setTranslatedText('');
        sessionIdRef.current = await SessionService.CreateSession(sourceLang, targetLang, inputType());
        await SessionService.StartInterpret();
      }
      await CaptureService.StartCapture(inputType());
      setStatus('翻译中...');
      setRunning(true);
      setPaused(false);
    } catch (e: any) {
      setStatus('连接失败: ' + e.message);
    }
    transitioning.current = false;
  };

  const handlePause = async () => {
    if (transitioning.current) return;
    transitioning.current = true;
    try {
      await CaptureService.StopCapture();
      await (SessionService as any).PauseInterpret();
      setStatus('已暂停');
      setPaused(true);
      setRunning(false);
    } catch (e: any) {
      setStatus('暂停失败: ' + (e?.message || e));
    } finally {
      transitioning.current = false;
    }
  };

  const handleStop = async () => {
    if (transitioning.current) return;
    transitioning.current = true;
    setStatus('停止中...');
    try {
      await CaptureService.StopCapture();
      await SessionService.StopInterpret();
      sessionIdRef.current = '';
      setStatus('已停止');
      setRunning(false);
      setPaused(false);
    } catch (e: any) {
      setStatus('停止失败: ' + (e?.message || e));
    } finally {
      transitioning.current = false;
    }
  };

  const handleDeviceChange = async (deviceID: number) => {
    setSelectedDevice(deviceID);
    await CaptureService.SetMicrophoneDevice(deviceID);
  };

  // ---- history ----

  const loadHistoryList = async () => {
    briefPollAttempts.current = 0;
    setHistoryLoading(true);
    try {
      const from = historyDateFrom ? new Date(historyDateFrom + 'T00:00:00').toISOString() : '';
      const to = historyDateTo ? new Date(historyDateTo + 'T23:59:59').toISOString() : '';
      const result = await (SessionService as any).SearchHistory(0, 50, historyKeyword.trim(), from, to);
      setHistorySessions(result[0] || []);
      setSelectedHistoryIds([]);
    } finally {
      setHistoryLoading(false);
    }
  };

  useEffect(() => {
    const hasPendingBriefs = historySessions.some(session => !session.brief && session.record_count > 0);
    if (view !== 'history' || historyLoading || !hasPendingBriefs || briefPollAttempts.current >= 4) {
      return;
    }

    const timer = window.setTimeout(async () => {
      briefPollAttempts.current += 1;
      const from = historyDateFrom ? new Date(historyDateFrom + 'T00:00:00').toISOString() : '';
      const to = historyDateTo ? new Date(historyDateTo + 'T23:59:59').toISOString() : '';
      try {
        const result = await (SessionService as any).SearchHistory(0, 50, historyKeyword.trim(), from, to);
        setHistorySessions(result[0] || []);
      } catch {
        // The normal search action remains available if a background refresh fails.
      }
    }, 3500);

    return () => window.clearTimeout(timer);
  }, [view, historyLoading, historySessions, historyKeyword, historyDateFrom, historyDateTo]);

  const handleExpand = async (sessionId: string) => {
    if (expandedId === sessionId) {
      setExpandedId(null);
      setHistoryRecords([]);
      setHistorySummary('');
      return;
    }
    setExpandedId(sessionId);
    try {
      const detail = await SessionService.GetHistory(sessionId) as HistoryDetail;
      setHistoryRecords(detail.records || []);
      setHistorySummary(detail.summary || '');
    } catch {
      setHistoryRecords([]);
      setHistorySummary('');
    }
  };

  const handleViewChange = (v: 'live' | 'history') => {
    setView(v);
    if (v === 'history') {
      loadHistoryList();
    }
  };

  const langName = (code: string) => LANGUAGES.find(l => l.code === code)?.name || code;

  const formatTime = (iso: string) => {
    const d = new Date(iso);
    return d.toLocaleString();
  };

  const liveExportRecords = (): ExportRecord[] => records.map((r, index) => ({
    source_text: r.sourceText,
    translated_text: r.translatedText,
    sequence: index + 1,
  }));

  const historyExportRecords = (): ExportRecord[] => historyRecords.map(r => ({
    source_text: r.source_text,
    translated_text: r.translated_text,
    sequence: r.sequence,
  }));

  const exportRecords = async (format: 'txt' | 'srt', title: string, rows: ExportRecord[]) => {
    try {
      const path = format === 'txt'
        ? await ExportService.ExportTXT(title, rows)
        : await ExportService.ExportSRT(title, rows);
      setStatus('已导出: ' + path);
    } catch (e: any) {
      setStatus('导出失败: ' + (e?.message || e));
    }
  };

  const toggleHistorySelection = (id: string, selected: boolean) => {
    setSelectedHistoryIds(prev => selected ? [...new Set([...prev, id])] : prev.filter(item => item !== id));
  };

  const deleteHistory = async (ids: string[]) => {
    if (ids.length === 0) return;
    try {
      if (ids.length === 1) {
        await (SessionService as any).DeleteHistory(ids[0]);
      } else {
        await (SessionService as any).DeleteHistoryBatch(ids);
      }
      setStatus(`已删除 ${ids.length} 条历史`);
      setExpandedId(null);
      setHistoryRecords([]);
      setHistorySummary('');
      await loadHistoryList();
    } catch (e: any) {
      setStatus('删除失败: ' + (e?.message || e));
    }
  };

  const summarizeHistory = async (sessionId: string) => {
    try {
      setStatus('摘要生成中...');
      const summary = await (SessionService as any).SummarizeHistory(sessionId);
      setHistorySummary(summary);
      setStatus('摘要已生成');
    } catch (e: any) {
      setStatus('摘要失败: ' + (e?.message || e));
    }
  };

  useEffect(() => {
    const offShortcut = Events.On('shortcut', (event: any) => {
      const action = event.data;
      if (action === 'start' && !running) {
        void handleStart();
      }
      if (action === 'pause' && running && !paused) {
        void handlePause();
      }
      if (action === 'stop' && (running || paused)) {
        void handleStop();
      }
    });
    return () => offShortcut();
  }, [running, paused, sourceLang, targetLang, selectedDevice]);

  useEffect(() => {
    chatEndRef.current?.scrollIntoView({ behavior: 'smooth', block: 'end' });
  }, [records, sourceText, translatedText]);

  const swapLanguages = () => {
    setSourceLang(targetLang);
    setTargetLang(sourceLang);
  };

  const showLiveDraft = Boolean(sourceText) && !committedSegmentsRef.current.has(currentSegmentIDRef.current);
  const statusTone = running ? 'running' : paused ? 'paused' : 'idle';

  return (
    <div className="app-shell">
      <aside className="sidebar">
        <div className="brand-block">
          <div className="brand-mark"><Radio size={19} strokeWidth={2.4} /></div>
          <div>
            <strong>TMK</strong>
            <span>同声传译</span>
          </div>
        </div>

        <nav className="primary-nav" aria-label="功能导航">
          <button className={view === 'live' ? 'nav-item active' : 'nav-item'} onClick={() => handleViewChange('live')}>
            <MessageSquareText size={18} />
            <span>实时会话</span>
          </button>
          <button className={view === 'history' ? 'nav-item active' : 'nav-item'} onClick={() => handleViewChange('history')}>
            <History size={18} />
            <span>历史记录</span>
          </button>
        </nav>

        <div className="sidebar-footer">
          <span className={`status-dot ${statusTone}`} />
          <div>
            <strong>{running ? '正在翻译' : paused ? '会话已暂停' : '等待开始'}</strong>
            <span>{status}</span>
          </div>
        </div>
      </aside>

      <main className="workspace">
        {view === 'live' ? (
          <section className="chat-page">
            <header className="page-header">
              <div>
                <p className="eyebrow">实时会话</p>
                <h1>{langName(sourceLang)} <span>→</span> {langName(targetLang)}</h1>
              </div>
              <div className="header-actions">
                <button className="icon-command" title="导出 TXT" disabled={records.length === 0} onClick={() => exportRecords('txt', 'live-session', liveExportRecords())}>
                  <FileText size={17} /><span>TXT</span>
                </button>
                <button className="icon-command" title="导出 SRT" disabled={records.length === 0} onClick={() => exportRecords('srt', 'live-session', liveExportRecords())}>
                  <Download size={17} /><span>SRT</span>
                </button>
              </div>
            </header>

            <div className="message-stream" aria-live="polite">
              {records.length === 0 && !showLiveDraft ? (
                <div className="empty-chat">
                  <div className="empty-icon"><MessageSquareText size={26} /></div>
                  <h2>准备开始新的翻译</h2>
                  <p>语音识别后的每一句原文和译文会在这里成对呈现。</p>
                </div>
              ) : null}

              {records.map((record, index) => (
                <article className="message-pair" key={record.id}>
                  <div className="message-index">{String(index + 1).padStart(2, '0')}</div>
                  <div className="message-content">
                    <div className="message-line source-line">
                      <span className="message-label">原文 · {langName(sourceLang)}</span>
                      <p>{record.sourceText}</p>
                    </div>
                    <div className="message-line translated-line">
                      <span className="message-label">译文 · {langName(targetLang)}</span>
                      <p>{record.translatedText}</p>
                    </div>
                  </div>
                </article>
              ))}

              {showLiveDraft ? (
                <article className="message-pair live-draft">
                  <div className="message-index"><span className="listening-pulse" /></div>
                  <div className="message-content">
                    <div className="message-line source-line">
                      <span className="message-label">正在识别</span>
                      <p>{sourceText}</p>
                    </div>
                    <div className="message-line translated-line">
                      <span className="message-label">实时译文</span>
                      <p>{translatedText || '翻译处理中...'}</p>
                    </div>
                  </div>
                </article>
              ) : null}
              <div ref={chatEndRef} />
            </div>

            <footer className="control-dock">
              <div className="settings-strip">
                <label className="setting-control">
                  <span>源语言</span>
                  <select value={sourceLang} onChange={e => setSourceLang(e.target.value)} disabled={running}>
                    {LANGUAGES.map(language => <option key={language.code} value={language.code}>{language.name}</option>)}
                  </select>
                </label>
                <button className="swap-button" onClick={swapLanguages} disabled={running} title="交换语言">
                  <ArrowRightLeft size={17} />
                </button>
                <label className="setting-control">
                  <span>目标语言</span>
                  <select value={targetLang} onChange={e => setTargetLang(e.target.value)} disabled={running}>
                    {LANGUAGES.map(language => <option key={language.code} value={language.code}>{language.name}</option>)}
                  </select>
                </label>
                <label className="setting-control audio-setting">
                  <span>音频来源</span>
                  <div className="select-with-icon">
                    {selectedDevice === DEVICE_SYSTEM_AUDIO ? <Volume2 size={16} /> : <Mic size={16} />}
                    <select value={selectedDevice} onChange={e => void handleDeviceChange(Number(e.target.value))} disabled={running}>
                      {devices.map(device => <option key={device.id} value={device.id}>{device.name}</option>)}
                    </select>
                  </div>
                </label>
                <button
                  className={subtitleMounted ? 'subtitle-toggle enabled' : 'subtitle-toggle'}
                  onClick={() => setSubtitleMounted(value => !value)}
                  role="switch"
                  aria-checked={subtitleMounted}
                >
                  <Captions size={18} />
                  <span>悬浮字幕</span>
                  <i />
                </button>
              </div>

              <div className="session-controls">
                <span className="dock-status">{status}</span>
                {!running ? (
                  <button className="primary-action" onClick={handleStart}>
                    <Play size={18} fill="currentColor" />
                    <span>{paused ? '继续翻译' : '开始翻译'}</span>
                  </button>
                ) : (
                  <button className="secondary-action" onClick={handlePause}>
                    <Pause size={18} fill="currentColor" />
                    <span>暂停</span>
                  </button>
                )}
                {(running || paused) ? (
                  <button className="danger-action" onClick={handleStop} title="停止会话">
                    <Square size={16} fill="currentColor" />
                    <span>停止</span>
                  </button>
                ) : null}
              </div>
            </footer>
          </section>
        ) : (
          <section className="history-page">
            <header className="page-header history-header">
              <div>
                <p className="eyebrow">会话档案</p>
                <h1>历史记录</h1>
              </div>
              <button className="danger-text-button" onClick={() => deleteHistory(selectedHistoryIds)} disabled={selectedHistoryIds.length === 0}>
                <Trash2 size={17} /><span>删除选中</span>
              </button>
            </header>

            <div className="history-toolbar">
              <label className="search-field">
                <Search size={17} />
                <input value={historyKeyword} onChange={e => setHistoryKeyword(e.target.value)} placeholder="搜索原文或译文" />
              </label>
              <label className="date-field"><span>开始日期</span><input type="date" value={historyDateFrom} onChange={e => setHistoryDateFrom(e.target.value)} /></label>
              <label className="date-field"><span>结束日期</span><input type="date" value={historyDateTo} onChange={e => setHistoryDateTo(e.target.value)} /></label>
              <button className="search-button" onClick={loadHistoryList}><Search size={17} /><span>搜索</span></button>
            </div>

            <div className="history-list">
              {historyLoading ? <div className="history-empty">正在加载历史记录...</div> : null}
              {!historyLoading && historySessions.length === 0 ? <div className="history-empty">暂无历史记录</div> : null}
              {!historyLoading && historySessions.map(session => (
                <article className={expandedId === session.id ? 'history-item expanded' : 'history-item'} key={session.id}>
                  <div className="history-row">
                    <input
                      aria-label="选择会话"
                      type="checkbox"
                      checked={selectedHistoryIds.includes(session.id)}
                      onChange={event => toggleHistorySelection(session.id, event.target.checked)}
                    />
                    <button
                      className="history-main"
                      onClick={() => handleExpand(session.id)}
                      title={`${langName(session.source_lang)} → ${langName(session.target_lang)} · ${formatTime(session.created_at)}`}
                    >
                      <strong>{langName(session.source_lang)} <span>→</span> {langName(session.target_lang)}</strong>
                      <small>{formatTime(session.created_at)}</small>
                      <span className={session.brief ? 'history-brief' : 'history-brief pending'}>
                        <Sparkles size={12} />
                        <span title={session.brief || undefined}>
                          {session.brief || (session.record_count > 0 ? 'AI 总结生成中...' : '暂无可总结内容')}
                        </span>
                      </span>
                    </button>
                    <span className="record-count">{session.record_count} 条</span>
                    <button className="icon-only danger" title="删除会话" onClick={() => deleteHistory([session.id])}><Trash2 size={17} /></button>
                  </div>

                  {expandedId === session.id ? (
                    <div className="history-detail">
                      <div className="detail-actions">
                        <button onClick={() => exportRecords('txt', `history-${session.id}`, historyExportRecords())}><FileText size={16} /><span>导出 TXT</span></button>
                        <button onClick={() => exportRecords('srt', `history-${session.id}`, historyExportRecords())}><Download size={16} /><span>导出 SRT</span></button>
                        <button className="summary-button" onClick={() => summarizeHistory(session.id)}><Sparkles size={16} /><span>AI 摘要</span></button>
                      </div>
                      {historySummary ? <div className="summary-panel"><Sparkles size={17} /><p>{historySummary}</p></div> : null}
                      <div className="history-messages">
                        {historyRecords.length === 0 ? <p className="muted">暂无翻译记录</p> : null}
                        {historyRecords.map((record, index) => (
                          <div className="history-message" key={record.id}>
                            <span>{String(index + 1).padStart(2, '0')}</span>
                            <div className="history-message-copy">
                              <p title={record.source_text}>{record.source_text}</p>
                              <p title={record.translated_text}>{record.translated_text}</p>
                            </div>
                          </div>
                        ))}
                      </div>
                    </div>
                  ) : null}
                </article>
              ))}
            </div>
          </section>
        )}
      </main>
    </div>
  );
}

function App() {
  const isSubtitleWindow = new URLSearchParams(window.location.search).get('window') === 'subtitle';
  return isSubtitleWindow ? <SubtitleWindow /> : <MainApp />;
}

export default App;
