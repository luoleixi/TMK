import { useState, useEffect, useRef } from 'react';
import { SessionService, CaptureService } from '../bindings/changeme';
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

const DEVICE_SYSTEM_AUDIO = -2;

function App() {
  const [sourceLang, setSourceLang] = useState('zh');
  const [targetLang, setTargetLang] = useState('en');
  const [running, setRunning] = useState(false);
  const [sourceText, setSourceText] = useState('');
  const [translatedText, setTranslatedText] = useState('');
  const [records, setRecords] = useState<RecordEntry[]>([]);
  const [status, setStatus] = useState('就绪');
  const [devices, setDevices] = useState<DeviceInfo[]>([]);
  const [selectedDevice, setSelectedDevice] = useState(DEVICE_SYSTEM_AUDIO);
  const transitioning = useRef(false);
  const sourceTextRef = useRef('');
  const lastSourceRef = useRef('');

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

    CaptureService.ListCaptureDevices().then((list: DeviceInfo[]) => {
      setDevices(list);
    });

    return () => {
      offTranscript();
      offTranslation();
    };
  }, []);

  const handleToggle = async () => {
    if (transitioning.current) return;
    transitioning.current = true;
    if (running) {
      setStatus('停止中...');
      CaptureService.StopCapture();
      await SessionService.StopInterpret();
      setStatus('已停止');
      setRunning(false);
    } else {
      setStatus('创建会话中...');
      try {
        const inputType = selectedDevice === DEVICE_SYSTEM_AUDIO ? 'system_audio' : 'microphone';
        lastSourceRef.current = '';
        await SessionService.CreateSession(sourceLang, targetLang, inputType);
        await SessionService.StartInterpret();
        await CaptureService.StartCapture(inputType);
        setStatus('翻译中...');
        setRunning(true);
      } catch (e: any) {
        setStatus('连接失败: ' + e.message);
      }
    }
    transitioning.current = false;
  };

  const handleDeviceChange = async (deviceID: number) => {
    setSelectedDevice(deviceID);
    await CaptureService.SetMicrophoneDevice(deviceID);
  };

  return (
    <div style={{ maxWidth: 600, margin: '0 auto', padding: 24, fontFamily: 'sans-serif' }}>
      <h1 style={{ textAlign: 'center' }}>TMK 同声传译</h1>

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

      <div style={{ textAlign: 'center', marginBottom: 16 }}>
        <button onClick={handleToggle}
          style={{
            padding: '12px 48px', fontSize: 18, cursor: 'pointer',
            background: running ? '#e74c3c' : '#2ecc71', color: '#fff',
            border: 'none', borderRadius: 8,
          }}>
          {running ? '停止翻译' : '开始翻译'}
        </button>
        <p style={{ color: '#888', marginTop: 8 }}>{status}</p>
      </div>

      <div style={{ background: '#1e1e1e', color: '#fff', borderRadius: 8, padding: 16, minHeight: 80, marginBottom: 16 }}>
        <p style={{ margin: 0, fontSize: 20 }}>{sourceText || '等待语音输入...'}</p>
        <p style={{ margin: '4px 0 0', fontSize: 18, color: '#4ec9b0' }}>
          {translatedText || ''}
        </p>
      </div>

      <div>
        <h3>翻译记录</h3>
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
    </div>
  );
}

export default App;
