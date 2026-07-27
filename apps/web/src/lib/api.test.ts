import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { api, clearToken, setToken } from "./api";

describe("api client", () => {
  const store = new Map<string, string>();

  beforeEach(() => {
    store.clear();
    vi.stubGlobal("localStorage", {
      getItem: (key: string) => store.get(key) ?? null,
      setItem: (key: string, value: string) => {
        store.set(key, value);
      },
      removeItem: (key: string) => {
        store.delete(key);
      },
    });
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        new Response(JSON.stringify({ ok: true }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      ),
    );
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it("builds query string for GET params", async () => {
    await api.get("/api/v1/approvals", { status: "pending" });

    expect(fetch).toHaveBeenCalledTimes(1);
    const [url, init] = vi.mocked(fetch).mock.calls[0]!;
    expect(String(url)).toBe(
      "http://localhost:8080/api/v1/approvals?status=pending",
    );
    expect(init?.method).toBe("GET");
  });

  it("attaches Authorization Bearer header when token is set", async () => {
    setToken("test-jwt");

    await api.get("/api/v1/auth/me");

    const [, init] = vi.mocked(fetch).mock.calls[0]!;
    const headers = init?.headers as Record<string, string>;
    expect(headers.Authorization).toBe("Bearer test-jwt");
  });

  it("posts JSON body without query string", async () => {
    await api.post("/api/v1/auth/login", {
      username: "admin",
      password: "secret",
    });

    const [url, init] = vi.mocked(fetch).mock.calls[0]!;
    expect(String(url)).toBe("http://localhost:8080/api/v1/auth/login");
    expect(init?.method).toBe("POST");
    expect(init?.body).toBe(
      JSON.stringify({ username: "admin", password: "secret" }),
    );
  });

  it("omits Authorization when token cleared", async () => {
    setToken("x");
    clearToken();

    await api.post("/api/v1/runs/eod", {});

    const [, init] = vi.mocked(fetch).mock.calls[0]!;
    const headers = init?.headers as Record<string, string>;
    expect(headers.Authorization).toBeUndefined();
  });
});
