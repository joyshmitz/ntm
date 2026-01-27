/**
 * API Client Tests
 *
 * Smoke tests for the API client configuration.
 */

import { describe, it, expect, beforeEach, afterEach } from "vitest";

// Mock localStorage
const localStorageMock = {
  store: {} as Record<string, string>,
  getItem(key: string) {
    return this.store[key] || null;
  },
  setItem(key: string, value: string) {
    this.store[key] = value;
  },
  removeItem(key: string) {
    delete this.store[key];
  },
  clear() {
    this.store = {};
  },
};

Object.defineProperty(global, "localStorage", {
  value: localStorageMock,
});

// Import after mocking
import {
  getConnectionConfig,
  saveConnectionConfig,
  clearConnectionConfig,
} from "../lib/api/client";

describe("API Client", () => {
  beforeEach(() => {
    localStorageMock.clear();
  });

  afterEach(() => {
    localStorageMock.clear();
  });

  describe("getConnectionConfig", () => {
    it("returns null when no config is stored", () => {
      const config = getConnectionConfig();
      expect(config).toBeNull();
    });

    it("returns stored config", () => {
      const config = { baseUrl: "http://localhost:9000", authToken: "test-token" };
      localStorageMock.setItem("ntm-connection", JSON.stringify(config));

      const result = getConnectionConfig();
      expect(result).toEqual(config);
    });

    it("returns null for invalid JSON", () => {
      localStorageMock.setItem("ntm-connection", "invalid-json");

      const result = getConnectionConfig();
      expect(result).toBeNull();
    });
  });

  describe("saveConnectionConfig", () => {
    it("saves config to localStorage", () => {
      const config = { baseUrl: "http://test:8080" };
      saveConnectionConfig(config);

      const stored = JSON.parse(localStorageMock.getItem("ntm-connection") || "{}");
      expect(stored.baseUrl).toBe("http://test:8080");
    });

    it("saves config with auth token", () => {
      const config = { baseUrl: "http://test:8080", authToken: "my-token" };
      saveConnectionConfig(config);

      const stored = JSON.parse(localStorageMock.getItem("ntm-connection") || "{}");
      expect(stored.authToken).toBe("my-token");
    });
  });

  describe("clearConnectionConfig", () => {
    it("removes config from localStorage", () => {
      localStorageMock.setItem("ntm-connection", '{"baseUrl":"http://test"}');
      clearConnectionConfig();

      expect(localStorageMock.getItem("ntm-connection")).toBeNull();
    });
  });
});
