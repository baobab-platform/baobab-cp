import { cleanup } from "@testing-library/react";
import { afterEach } from "vitest";

afterEach(() => {
  if (typeof document !== "undefined") cleanup();
});

// jsdom does not implement modal dialogs. This stand-in exercises the
// components' own logic (open state, close event, focus return); the
// browser's inertness and focus trapping belong to end-to-end tests.
if (typeof HTMLDialogElement !== "undefined" && typeof HTMLDialogElement.prototype.showModal !== "function") {
  HTMLDialogElement.prototype.showModal = function showModal(this: HTMLDialogElement) {
    this.setAttribute("open", "");
  };
  HTMLDialogElement.prototype.close = function close(this: HTMLDialogElement) {
    if (!this.hasAttribute("open")) return;
    this.removeAttribute("open");
    this.dispatchEvent(new Event("close"));
  };
}
