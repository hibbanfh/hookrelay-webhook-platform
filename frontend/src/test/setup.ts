// Server env for route-handler tests. Values are fake.
process.env.NEON_AUTH_BASE_URL = "https://neon.test/auth";
process.env.NEON_AUTH_SESSION_COOKIE = "__Secure-neon-auth.session_token";
process.env.DASHBOARD_ORIGIN = "http://localhost:3000";
process.env.API_INTERNAL_URL = "http://api.test:8080";
process.env.SESSION_SECRET = "test-session-secret-0123456789-abcdefghij";

// jsdom has no <dialog>.showModal.
if (typeof HTMLDialogElement !== "undefined" && !HTMLDialogElement.prototype.showModal) {
  HTMLDialogElement.prototype.showModal = function showModal(this: HTMLDialogElement) {
    this.setAttribute("open", "");
  };
}

import { cleanup } from "@testing-library/react";
import { afterEach } from "vitest";

afterEach(() => cleanup());
