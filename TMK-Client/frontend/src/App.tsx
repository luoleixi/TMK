import { useState, useEffect, useRef } from 'react';
import { SessionService, CaptureService, SettingsService, ExportService } from '../bindings/changeme';
import { Events } from '@wailsio/runtime';

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

const DEVICE_SYSTEM_AUDIO = -2;

function App() {
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
  const sourceTextRef = useRef('');
  const lastSourceRef = useRef('');
  const settingsReady = useRef(false);

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

  useEffect(() => {
    const offTranscript = Events.On('transcript', (event: any) => {
      sourceTextRef.current = event.data.text;
      setSourceText(event.data.text);
    });
    const offTranslation = Events.On('translation', (event: any) => {
      setTranslatedText(event.data.text);
      if (event.data.is_final && sourceTextRef.current && sourceTextRef.current !== lastSourceRef.current) {
        lastSourceRef.current = sourceTextRef.current;
        setRecords(prev => [...prev, {
          id: Date.now(),
          sourceText: sourceTextRef.current,
          translatedText: event.data.text,
        }]);
      }
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
      setHistoryKeyword(settings.history_keyword || '');
      setHistoryDateFrom(settings.history_date_from || '');
      setHistoryDateTo(settings.history_date_to || '');
      CaptureService.SetMicrophoneDevice(deviceID);
      settingsReady.current = true;
    });

    return () => {
      offTranscript();
      offTranslation();
    };
  }, []);

  useEffect(() => {
    if (!settingsReady.current) return;
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
      lastSourceRef.current = '';
      if (paused) {
        await (SessionService as any).ResumeInterpret();
      } else {
        setStatus('创建会话中...');
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

  return (
    <div style={{ maxWidth: 600, margin: '0 auto', padding: 24, fontFamily: 'sans-serif' }}>
      <h1 style={{ textAlign: 'center' }}>TMK 同声传译</h1>

      {/* Tab bar */}
      <div style={{ display: 'flex', marginBottom: 16, borderBottom: '2px solid #333' }}>
        {(['live', 'history'] as const).map(v => (
          <button
            key={v}
            onClick={() => handleViewChange(v)}
            style={{
              flex: 1, padding: '8px 0', fontSize: 16, cursor: 'pointer',
              background: 'none', border: 'none',
              borderBottom: view === v ? '2px solid #4ec9b0' : '2px solid transparent',
              color: view === v ? '#4ec9b0' : '#888',
              marginBottom: -2,
            }}
          >
            {v === 'live' ? '实时翻译' : '历史记录'}
          </button>
        ))}
      </div>

      {view === 'live' && (
        <>
          <div style={{ display: 'flex', gap: 16, marginBottom: 16 }}>
            <label style={{ flex: 1 }}>
              源语言
              <select value={sourceLang} onChange={e => setSourceLang(e.target.value)}
                style={{ width: '100%', padding: 8, marginTop: 4 }}>
                {LANGUAGES.map(l => <option key={l.code} value={l.code}>{l.name}</option>)}
              </select>
            </label>
            <label style={{ flex: 1 }}>
              目标语言
              <select value={targetLang} onChange={e => setTargetLang(e.target.value)}
                style={{ width: '100%', padding: 8, marginTop: 4 }}>
                {LANGUAGES.map(l => <option key={l.code} value={l.code}>{l.name}</option>)}
              </select>
            </label>
          </div>

          <div style={{ marginBottom: 16 }}>
            <label>
              音频来源
              <select value={selectedDevice} onChange={e => handleDeviceChange(Number(e.target.value))}
                style={{ width: '100%', padding: 8, marginTop: 4 }}>
                {devices.map(d => (
                  <option key={d.id} value={d.id}>{d.name}</option>
                ))}
              </select>
            </label>
          </div>

          <label style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 16 }}>
            <input
              type="checkbox"
              checked={subtitleMounted}
              onChange={e => setSubtitleMounted(e.target.checked)}
            />
            挂载字幕
          </label>

          <div style={{ textAlign: 'center', marginBottom: 16 }}>
            {!running && !paused ? (
              <button onClick={handleStart}
                style={{
                  padding: '12px 48px', fontSize: 18, cursor: 'pointer',
                  background: '#2ecc71', color: '#fff', border: 'none', borderRadius: 8,
                }}>
                开始翻译
              </button>
            ) : null}

            {running && !paused ? (
              <>
                <button onClick={handlePause}
                  style={{
                    padding: '12px 48px', fontSize: 18, cursor: 'pointer',
                    background: '#f39c12', color: '#fff', border: 'none', borderRadius: 8,
                    marginRight: 16,
                  }}>
                  暂停
                </button>
                <button onClick={handleStop}
                  style={{
                    padding: '12px 48px', fontSize: 18, cursor: 'pointer',
                    background: '#e74c3c', color: '#fff', border: 'none', borderRadius: 8,
                  }}>
                  停止
                </button>
              </>
            ) : null}

            {paused ? (
              <>
                <button onClick={handleStart}
                  style={{
                    padding: '12px 48px', fontSize: 18, cursor: 'pointer',
                    background: '#2ecc71', color: '#fff', border: 'none', borderRadius: 8,
                    marginRight: 16,
                  }}>
                  继续
                </button>
                <button onClick={handleStop}
                  style={{
                    padding: '12px 48px', fontSize: 18, cursor: 'pointer',
                    background: '#e74c3c', color: '#fff', border: 'none', borderRadius: 8,
                  }}>
                  停止
                </button>
              </>
            ) : null}

            <p style={{ color: '#888', marginTop: 8 }}>{status}</p>
          </div>

          {subtitleMounted && (
            <div style={{ background: '#1e1e1e', color: '#fff', borderRadius: 8, padding: 16, minHeight: 80, marginBottom: 16 }}>
              <p style={{ margin: 0, fontSize: 20 }}>{sourceText || '等待语音输入...'}</p>
              <p style={{ margin: '4px 0 0', fontSize: 18, color: '#4ec9b0' }}>
                {translatedText || ''}
              </p>
            </div>
          )}

          <div>
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 8 }}>
              <h3>翻译记录</h3>
              <div style={{ display: 'flex', gap: 8 }}>
                <button onClick={() => exportRecords('txt', 'live-session', liveExportRecords())}>导出 TXT</button>
                <button onClick={() => exportRecords('srt', 'live-session', liveExportRecords())}>导出 SRT</button>
              </div>
            </div>
            {records.length === 0 ? (
              <p style={{ color: '#888' }}>暂无记录</p>
            ) : (
              records.map(r => (
                <div key={r.id} style={{ borderBottom: '1px solid #ddd', padding: '8px 0' }}>
                  <span>{r.sourceText}</span>
                  <span style={{ margin: '0 8px', color: '#aaa' }}>→</span>
                  <span style={{ color: '#2ecc71' }}>{r.translatedText}</span>
                </div>
              ))
            )}
          </div>
        </>
      )}

      {view === 'history' && (
        <div>
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 140px 140px auto', gap: 8, marginBottom: 16 }}>
            <input
              value={historyKeyword}
              onChange={e => setHistoryKeyword(e.target.value)}
              placeholder="关键词"
              style={{ padding: 8 }}
            />
            <input
              type="date"
              value={historyDateFrom}
              onChange={e => setHistoryDateFrom(e.target.value)}
              style={{ padding: 8 }}
            />
            <input
              type="date"
              value={historyDateTo}
              onChange={e => setHistoryDateTo(e.target.value)}
              style={{ padding: 8 }}
            />
            <button onClick={loadHistoryList}>搜索</button>
          </div>
          <div style={{ display: 'flex', justifyContent: 'flex-end', marginBottom: 12 }}>
            <button onClick={() => deleteHistory(selectedHistoryIds)} disabled={selectedHistoryIds.length === 0}>
              删除选中
            </button>
          </div>
          {historyLoading ? (
            <p style={{ color: '#888', textAlign: 'center' }}>加载中...</p>
          ) : historySessions.length === 0 ? (
            <p style={{ color: '#888', textAlign: 'center' }}>暂无历史记录</p>
          ) : (
            historySessions.map(ses => (
              <div key={ses.id}>
                <div
                  style={{
                    cursor: 'pointer', padding: 12, marginBottom: 8,
                    background: '#1e1e1e', borderRadius: 8,
                    border: expandedId === ses.id ? '1px solid #4ec9b0' : '1px solid #333',
                  }}
                >
                  <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                    <label style={{ display: 'flex', alignItems: 'center', gap: 8, color: '#fff', fontSize: 15 }}>
                      <input
                        type="checkbox"
                        checked={selectedHistoryIds.includes(ses.id)}
                        onChange={e => toggleHistorySelection(ses.id, e.target.checked)}
                      />
                      <span onClick={() => handleExpand(ses.id)}>
                        {langName(ses.source_lang)} → {langName(ses.target_lang)}
                      </span>
                    </label>
                    <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                      <span style={{ color: '#888', fontSize: 13 }}>
                        {ses.record_count} 条记录 · {formatTime(ses.created_at)}
                      </span>
                      <button onClick={() => deleteHistory([ses.id])}>删除</button>
                    </div>
                  </div>
                </div>

                {expandedId === ses.id && (
                  <div style={{ marginBottom: 16, paddingLeft: 12 }}>
                    <div style={{ display: 'flex', gap: 8, marginBottom: 8 }}>
                      <button onClick={() => exportRecords('txt', `history-${ses.id}`, historyExportRecords())}>导出 TXT</button>
                      <button onClick={() => exportRecords('srt', `history-${ses.id}`, historyExportRecords())}>导出 SRT</button>
                      <button onClick={() => summarizeHistory(ses.id)}>AI 摘要</button>
                    </div>
                    {historySummary && (
                      <p style={{ color: '#4ec9b0', background: '#111', padding: 12, borderRadius: 8 }}>{historySummary}</p>
                    )}
                    {historyRecords.length === 0 ? (
                      <p style={{ color: '#888' }}>暂无记录</p>
                    ) : (
                      historyRecords.map(r => (
                        <div key={r.id} style={{ borderBottom: '1px solid #333', padding: '8px 0' }}>
                          <span style={{ color: '#ccc' }}>{r.source_text}</span>
                          <span style={{ margin: '0 8px', color: '#666' }}>→</span>
                          <span style={{ color: '#4ec9b0' }}>{r.translated_text}</span>
                        </div>
                      ))
                    )}
                  </div>
                )}
              </div>
            ))
          )}
        </div>
      )}
    </div>
  );
}

export default App;
