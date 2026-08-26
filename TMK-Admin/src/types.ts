export type User = {
  id: string;
  email: string;
  display_name: string;
  role: "admin" | "user";
  status: "active" | "disabled";
  must_change_password: boolean;
  created_at: string;
  last_login_at?: string;
};
export type Environment = { id: "test" | "production"; name: string; enabled: boolean };

export type TokenPair = {
  access_token: string;
  refresh_token: string;
  expires_in_seconds: number;
  user: User;
};

export type StorageObject = {
  id: string;
  owner_user_id: string;
  kind: "audio" | "text";
  original_name: string;
  content_type: string;
  size_bytes: number;
  sha256: string;
  status: string;
  created_at: string;
};

export type ReferenceSegment = {
  text: string;
  begin_time_ms?: number;
  end_time_ms?: number;
};

export type DatasetItem = {
  id: string;
  sequence: number;
  audio_object_id: string;
  audio_original_name: string;
  reference_text_object_id: string;
  text_original_name: string;
  reference_segments?: ReferenceSegment[];
};

export type Dataset = {
  id: string;
  name: string;
  description: string;
  language: string;
  status: "draft" | "ready" | "archived";
  revision: number;
  item_count: number;
  updated_at: string;
};

export type EvaluationJob = {
  id: string;
  run_id?: string;
  variant?: "segmenter_on" | "segmenter_off";
  dataset_id: string;
  dataset_revision: number;
  dataset_language: string;
  status: string;
  total_items: number;
  completed_items: number;
  succeeded_items: number;
  failed_items: number;
  progress: number;
  asr_cer: number;
  asr_wer: number;
  segmented_cer: number;
  segmented_wer: number;
  segment_evaluable: boolean;
  segment_f1: number;
  segment_count_delta: number;
  error_message?: string;
  created_at: string;
  attempt_count: number;
  max_attempts: number;
  next_attempt_at?: string;
  lease_expires_at?: string;
};

export type EvaluationRun = {
  id: string;
  dataset_id: string;
  dataset_revision: number;
  dataset_language: string;
  mode: string;
  status: string;
  total_items: number;
  completed_items: number;
  created_at: string;
  jobs?: EvaluationJob[];
};

export type SegmenterRuntimeConfig = {
  enabled: boolean;
  rollout_percent: number;
  version: string;
  revision: number;
  status: string;
  changed_by?: string;
  change_reason?: string;
  created_at?: string;
  applied_at?: string;
};

export type EvaluationResult = {
  id: string;
  dataset_item_id: string;
  sequence: number;
  status: string;
  reference_text: string;
  asr_text: string;
  segmented_text: string;
  segment_count: number;
  asr_cer: number;
  asr_wer: number;
  segmented_cer: number;
  segmented_wer: number;
  segment_evaluable: boolean;
  segment_f1: number;
  error_message?: string;
};

export type DailyPoint = {
  date: string;
  sessions: number;
  evaluation_jobs: number;
  evaluation_items: number;
  failed_items: number;
};

export type Dashboard = {
  generated_at: string;
  window_days: number;
  users: { total: number; active: number; disabled: number; administrators: number };
  sessions: { total: number; in_window: number; ready: number; active: number; completed: number; failed: number; records: number };
  storage: { objects: number; audio_files: number; text_files: number; bytes: number; disk_free_bytes: number; quota_bytes: number; reserve_bytes: number };
  datasets: { total: number; draft: number; ready: number; archived: number; items: number };
  evaluations: EvaluationJob & { total: number; in_window: number; queued: number; running: number; completed_with_errors: number; cancelled: number; dead_lettered: number };
  daily: DailyPoint[];
  recent_evaluation_jobs: EvaluationJob[];
};

export type Governance = {
  generated_at: string;
  stale_draft_datasets: number;
  unreferenced_objects: number;
  unreferenced_object_bytes: number;
  ready_datasets_without_successful_evaluation: number;
  stuck_evaluation_jobs: number;
  expired_active_tokens: number;
  revoked_or_expired_tokens: number;
  sessions_past_retention: number;
  evaluation_jobs_past_retention: number;
  audit_events_past_retention: number;
  disk_free_bytes: number;
  disk_reserve_bytes: number;
  session_retention_days: number;
  evaluation_retention_days: number;
  audit_retention_days: number;
  stale_draft_days: number;
  stuck_job_minutes: number;
};

export type AuditEvent = {
  id: number;
  actor_user_id?: string;
  action: string;
  resource_type: string;
  resource_id: string;
  ip_address: string;
  user_agent: string;
  result: string;
  details: Record<string, unknown>;
  created_at: string;
};

export type Envelope<T> = { code: number; message: string; data: T };

export type MonitorAlert = {
  labels: Record<string, string>;
  annotations: Record<string, string>;
  state: string;
  activeAt?: string;
  value?: string;
};

export type MonitorMetric = { value: number; unit?: string; error?: string };

export type MonitoringSummary = {
  generated_at: string;
  environment: string;
  target: { url: string; up: boolean; status_code: number; latency_ms: number; error?: string };
  admin_target: { url: string; up: boolean; status_code: number; latency_ms: number; error?: string };
  alerts: MonitorAlert[];
  metrics: Record<string, MonitorMetric>;
};
