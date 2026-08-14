import type { Envelope, TokenPair, User } from "./types";

function apiBase(): string {
  if (import.meta.env.VITE_API_BASE) return import.meta.env.VITE_API_BASE.replace(/\/$/, "");
  const marker = window.location.pathname.indexOf("/admin");
  const deploymentPrefix = marker >= 0 ? window.location.pathname.slice(0, marker) : "";
  return `${deploymentPrefix}/api/v1`;
}

export class ApiError extends Error {
  constructor(public status: number, message: string) {
    super(message);
  }
}

class ApiClient {
  private accessToken = "";
  private refreshToken = "";
  private refreshPromise: Promise<boolean> | null = null;
  onAuthLost?: () => void;

  async login(email: string, password: string): Promise<User> {
    const response = await this.raw<TokenPair>("/auth/login", { method: "POST", body: JSON.stringify({ email, password }) }, false);
    this.applyTokens(response);
    return response.user;
  }

  async logout(): Promise<void> {
    if (!this.accessToken) return;
    try {
      await this.raw("/auth/logout", { method: "POST", body: JSON.stringify({ refresh_token: this.refreshToken }) }, true, false);
    } finally {
      this.clearTokens();
    }
  }

  async request<T>(path: string, init: RequestInit = {}): Promise<T> {
    return this.raw<T>(path, init, true, true);
  }

  async upload(kind: "audio" | "text", file: File): Promise<unknown> {
    const body = new FormData();
    body.append("file", file);
    return this.request(`/admin/objects?kind=${kind}`, { method: "POST", body });
  }

  async download(path: string): Promise<Blob> {
    const headers = new Headers({ Authorization: `Bearer ${this.accessToken}` });
    let response = await fetch(`${apiBase()}${path}`, { headers });
    if (response.status === 401 && await this.refresh()) {
      headers.set("Authorization", `Bearer ${this.accessToken}`);
      response = await fetch(`${apiBase()}${path}`, { headers });
    }
    if (!response.ok) throw new ApiError(response.status, "下载失败");
    return response.blob();
  }

  private async raw<T>(path: string, init: RequestInit, authenticate: boolean, retry = true): Promise<T> {
    const headers = new Headers(init.headers);
    if (!(init.body instanceof FormData)) headers.set("Content-Type", "application/json");
    if (authenticate && this.accessToken) headers.set("Authorization", `Bearer ${this.accessToken}`);
    const response = await fetch(`${apiBase()}${path}`, { ...init, headers });
    if (response.status === 401 && authenticate && retry && await this.refresh()) {
      return this.raw<T>(path, init, authenticate, false);
    }
    const payload = await response.json().catch(() => ({ message: "请求失败" })) as Envelope<T>;
    if (!response.ok) {
      if (response.status === 401 && authenticate) {
        this.clearTokens();
        this.onAuthLost?.();
      }
      throw new ApiError(response.status, payload.message || "请求失败");
    }
    return payload.data;
  }

  private async refresh(): Promise<boolean> {
    if (!this.refreshToken) return false;
    if (!this.refreshPromise) {
      this.refreshPromise = this.raw<TokenPair>("/auth/refresh", {
        method: "POST",
        body: JSON.stringify({ refresh_token: this.refreshToken }),
      }, false).then((pair) => {
        this.applyTokens(pair);
        return true;
      }).catch(() => {
        this.clearTokens();
        return false;
      }).finally(() => {
        this.refreshPromise = null;
      });
    }
    return this.refreshPromise;
  }

  private applyTokens(pair: TokenPair) {
    this.accessToken = pair.access_token;
    this.refreshToken = pair.refresh_token;
  }

  private clearTokens() {
    this.accessToken = "";
    this.refreshToken = "";
  }
}

export const api = new ApiClient();
