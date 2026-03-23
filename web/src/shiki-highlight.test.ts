import { describe, expect, it } from "vitest";
import { resolveShikiLanguage } from "./shiki-highlight";

describe("resolveShikiLanguage", () => {
  it("maps common source paths to bundled shiki grammars", () => {
    expect(resolveShikiLanguage("web/src/app.tsx")).toBe("tsx");
    expect(resolveShikiLanguage("web/src/api.ts")).toBe("ts");
    expect(resolveShikiLanguage("README.md")).toBe("markdown");
    expect(resolveShikiLanguage("ops/host/bootstrap.sh")).toBe("bash");
    expect(resolveShikiLanguage("cmd/fascinate/main.go")).toBe("go");
    expect(resolveShikiLanguage("config/settings.yaml")).toBe("yaml");
    expect(resolveShikiLanguage("package.json")).toBe("json");
  });

  it("falls back to plain text for unknown extensions", () => {
    expect(resolveShikiLanguage("AGENTS.unknown")).toBe("text");
  });
});
