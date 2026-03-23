import type { UnifiedDiffLineRow } from "./git-diff";

type ShikiModule = typeof import("shiki");

type HighlightedToken = {
  color?: string;
  content: string;
  fontStyle?: number;
};

export type HighlightedDiffLineMap = Record<string, HighlightedToken[]>;

const shikiTheme = "github-dark";
const shikiLangs = [
  "bash",
  "css",
  "go",
  "html",
  "javascript",
  "json",
  "jsx",
  "markdown",
  "text",
  "toml",
  "ts",
  "tsx",
  "yaml",
] as const;

let highlighterPromise: Promise<Awaited<ReturnType<ShikiModule["createHighlighter"]>>> | null = null;
const highlightedCodeCache = new Map<string, Promise<HighlightedToken[][]>>();

export async function highlightDiffRows(
  path: string,
  rows: UnifiedDiffLineRow[],
): Promise<HighlightedDiffLineMap> {
  const code = rows.map((row) => row.text).join("\n");
  const highlightedLines = await highlightCode(path, code);
  const lineMap: HighlightedDiffLineMap = {};
  for (const [index, row] of rows.entries()) {
    lineMap[row.id] = highlightedLines[index] ?? [{ content: row.text }];
  }
  return lineMap;
}

export function resolveShikiLanguage(path: string) {
  const normalizedPath = path.toLowerCase();
  if (
    normalizedPath.endsWith(".md") ||
    normalizedPath.endsWith(".mdx") ||
    normalizedPath.endsWith(".markdown")
  ) {
    return "markdown";
  }
  if (normalizedPath.endsWith(".tsx")) {
    return "tsx";
  }
  if (normalizedPath.endsWith(".ts")) {
    return "ts";
  }
  if (normalizedPath.endsWith(".jsx")) {
    return "jsx";
  }
  if (normalizedPath.endsWith(".js") || normalizedPath.endsWith(".mjs") || normalizedPath.endsWith(".cjs")) {
    return "javascript";
  }
  if (normalizedPath.endsWith(".json") || normalizedPath.endsWith(".jsonc")) {
    return "json";
  }
  if (normalizedPath.endsWith(".yml") || normalizedPath.endsWith(".yaml")) {
    return "yaml";
  }
  if (normalizedPath.endsWith(".go")) {
    return "go";
  }
  if (
    normalizedPath.endsWith(".sh") ||
    normalizedPath.endsWith(".bash") ||
    normalizedPath.endsWith(".zsh")
  ) {
    return "bash";
  }
  if (normalizedPath.endsWith(".html")) {
    return "html";
  }
  if (normalizedPath.endsWith(".css")) {
    return "css";
  }
  if (normalizedPath.endsWith(".toml")) {
    return "toml";
  }
  return "text";
}

async function highlightCode(path: string, code: string) {
  const language = resolveShikiLanguage(path);
  const cacheKey = `${language}\u0000${code}`;
  const cached = highlightedCodeCache.get(cacheKey);
  if (cached) {
    return cached;
  }
  const next = (async () => {
    const highlighter = await getHighlighter();
    try {
      const result = highlighter.codeToTokens(code, { lang: language, theme: shikiTheme });
      return result.tokens.map((line) =>
        line.map((token) => ({
          color: token.color,
          content: token.content,
          fontStyle: token.fontStyle,
        })),
      );
    } catch {
      if (language !== "text") {
        const fallback = highlighter.codeToTokens(code, { lang: "text", theme: shikiTheme });
        return fallback.tokens.map((line) =>
          line.map((token) => ({
            color: token.color,
            content: token.content,
            fontStyle: token.fontStyle,
          })),
        );
      }
      return code.split("\n").map((line) => [{ content: line }]);
    }
  })();
  highlightedCodeCache.set(cacheKey, next);
  return next;
}

async function getHighlighter() {
  if (!highlighterPromise) {
    highlighterPromise = import("shiki").then(({ createHighlighter }) =>
      createHighlighter({
        langs: [...shikiLangs],
        themes: [shikiTheme],
      }),
    );
  }
  return highlighterPromise;
}
