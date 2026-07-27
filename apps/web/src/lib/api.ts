const TOKEN_KEY = "token";

export function getApiBaseUrl(): string {
  return process.env.NEXT_PUBLIC_API_BASE_URL ?? "http://localhost:8080";
}

function storage(): Storage | null {
  try {
    if (typeof localStorage === "undefined") return null;
    return localStorage;
  } catch {
    return null;
  }
}

export function getToken(): string | null {
  return storage()?.getItem(TOKEN_KEY) ?? null;
}

export function setToken(token: string): void {
  storage()?.setItem(TOKEN_KEY, token);
}

export function clearToken(): void {
  storage()?.removeItem(TOKEN_KEY);
}

type QueryValue = string | number | boolean | null | undefined;

function buildUrl(
  path: string,
  query?: Record<string, QueryValue>,
): string {
  const base = getApiBaseUrl().replace(/\/$/, "");
  const normalized = path.startsWith("/") ? path : `/${path}`;
  const url = new URL(`${base}${normalized}`);
  if (query) {
    for (const [key, value] of Object.entries(query)) {
      if (value === undefined || value === null) continue;
      url.searchParams.set(key, String(value));
    }
  }
  return url.toString();
}

async function request<T>(
  method: "GET" | "POST",
  path: string,
  body?: unknown,
): Promise<T> {
  const headers: Record<string, string> = {
    Accept: "application/json",
  };

  const token = getToken();
  if (token) {
    headers.Authorization = `Bearer ${token}`;
  }

  let url: string;
  let fetchBody: string | undefined;

  if (method === "GET") {
    const query =
      body && typeof body === "object" && !Array.isArray(body)
        ? (body as Record<string, QueryValue>)
        : undefined;
    url = buildUrl(path, query);
  } else {
    url = buildUrl(path);
    if (body !== undefined) {
      headers["Content-Type"] = "application/json";
      fetchBody = JSON.stringify(body);
    }
  }

  const res = await fetch(url, { method, headers, body: fetchBody });
  if (!res.ok) {
    const text = await res.text().catch(() => "");
    throw new Error(
      `API ${method} ${path} failed: ${res.status}${text ? ` ${text}` : ""}`,
    );
  }
  if (res.status === 204) {
    return undefined as T;
  }
  return (await res.json()) as T;
}

export const api = {
  get<T>(path: string, body?: Record<string, QueryValue>): Promise<T> {
    return request<T>("GET", path, body);
  },
  post<T>(path: string, body?: unknown): Promise<T> {
    return request<T>("POST", path, body);
  },
};
