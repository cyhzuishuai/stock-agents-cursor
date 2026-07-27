// @vitest-environment jsdom

import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { clearToken, getToken } from "@/lib/api";
import { LoginForm } from "./login-form";

const push = vi.fn();

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push }),
}));

describe("LoginForm", () => {
  const store = new Map<string, string>();

  beforeEach(() => {
    store.clear();
    push.mockClear();
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
        new Response(JSON.stringify({ token: "jwt-token" }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      ),
    );
  });

  afterEach(() => {
    clearToken();
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it("submit calls /api/v1/auth/login, stores token, redirects /", async () => {
    const user = userEvent.setup();
    render(<LoginForm />);

    await user.type(screen.getByLabelText(/username/i), "admin");
    await user.type(screen.getByLabelText(/password/i), "secret");
    await user.click(screen.getByRole("button", { name: /log in/i }));

    await waitFor(() => {
      expect(fetch).toHaveBeenCalledTimes(1);
    });

    const [url, init] = vi.mocked(fetch).mock.calls[0]!;
    expect(String(url)).toBe("http://localhost:8080/api/v1/auth/login");
    expect(init?.method).toBe("POST");
    expect(init?.body).toBe(
      JSON.stringify({ username: "admin", password: "secret" }),
    );
    expect(getToken()).toBe("jwt-token");
    expect(push).toHaveBeenCalledWith("/");
  });
});
