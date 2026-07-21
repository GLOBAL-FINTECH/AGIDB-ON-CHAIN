export type AgidbClientOptions = { baseUrl?: string; token?: string; fetchImpl?: typeof fetch };
export type TxOperation = { type: "put" | "delete"; key: string; value?: string };

export class AgidbClient {
  readonly baseUrl: string;
  private readonly token?: string;
  private readonly fetchImpl: typeof fetch;

  constructor(options: AgidbClientOptions = {}) {
    this.baseUrl = (options.baseUrl ?? "http://127.0.0.1:7319").replace(/\/$/, "");
    this.token = options.token;
    this.fetchImpl = options.fetchImpl ?? fetch;
  }

  private async request<T>(path: string, init: RequestInit = {}): Promise<T> {
    const headers = new Headers(init.headers);
    headers.set("accept", "application/json");
    if (init.body) headers.set("content-type", "application/json");
    if (this.token) headers.set("authorization", `Bearer ${this.token}`);
    const response = await this.fetchImpl(`${this.baseUrl}${path}`, { ...init, headers });
    const text = await response.text();
    if (!response.ok) throw new Error(`AGIDB ${response.status}: ${text}`);
    return (text ? JSON.parse(text) : {}) as T;
  }

  status<T = unknown>(): Promise<T> { return this.request("/v1/status"); }
  verify<T = unknown>(): Promise<T> { return this.request("/v1/verify"); }

  getState<T = unknown>(key: string, height?: number): Promise<T> {
    const query = height === undefined ? "" : `?height=${height}`;
    return this.request(`/v1/state/${encodeURIComponent(key)}${query}`);
  }

  getBlock<T = unknown>(height: number): Promise<T> {
    return this.request(`/v1/block/${height}`);
  }

  commit<T = unknown>(operations: TxOperation[]): Promise<T> {
    return this.request("/v1/tx", {
      method: "POST",
      body: JSON.stringify({ operations })
    });
  }
}
