import { describe, expect, it } from "vitest";
import { resolveShikiLanguage } from "./shiki-highlight";

describe("resolveShikiLanguage", () => {
  it("maps common source paths to bundled shiki grammars", () => {
    expect(resolveShikiLanguage("web/src/app.tsx")).toBe("tsx");
    expect(resolveShikiLanguage("web/src/api.ts")).toBe("ts");
    expect(resolveShikiLanguage("README.md")).toBe("markdown");
    expect(resolveShikiLanguage("ops/host/bootstrap.sh")).toBe("bash");
    expect(resolveShikiLanguage("cmd/fascinate/main.go")).toBe("go");
    expect(resolveShikiLanguage("Dockerfile")).toBe("docker");
    expect(resolveShikiLanguage("Makefile")).toBe("make");
    expect(resolveShikiLanguage("services/api/main.py")).toBe("python");
    expect(resolveShikiLanguage("backend/src/Main.java")).toBe("java");
    expect(resolveShikiLanguage("infra/main.tf")).toBe("terraform");
    expect(resolveShikiLanguage("src/lib.rs")).toBe("rust");
    expect(resolveShikiLanguage("src/server.cpp")).toBe("cpp");
    expect(resolveShikiLanguage("src/Program.cs")).toBe("csharp");
    expect(resolveShikiLanguage("app/schema.graphql")).toBe("graphql");
    expect(resolveShikiLanguage("ui/App.vue")).toBe("vue");
    expect(resolveShikiLanguage("ui/App.svelte")).toBe("svelte");
    expect(resolveShikiLanguage("config/settings.yaml")).toBe("yaml");
    expect(resolveShikiLanguage("package.json")).toBe("json");
  });

  it("falls back to plain text for unknown extensions", () => {
    expect(resolveShikiLanguage("AGENTS.unknown")).toBe("text");
  });
});
