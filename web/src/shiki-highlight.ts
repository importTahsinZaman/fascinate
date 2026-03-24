import type { BundledLanguage, SpecialLanguage } from "shiki";
import type { UnifiedDiffLineRow } from "./git-diff";

type ShikiModule = typeof import("shiki");
type ShikiHighlighter = Awaited<ReturnType<ShikiModule["createHighlighter"]>>;
type ResolvedShikiLanguage = BundledLanguage | SpecialLanguage;

export type HighlightedToken = {
  color?: string;
  content: string;
  fontStyle?: number;
};

export type HighlightedDiffLineMap = Record<string, HighlightedToken[]>;

const shikiTheme = "github-dark";
const shikiFallbackLanguage: SpecialLanguage = "text";
const shikiFileNameLanguages: Array<[fileName: string, language: BundledLanguage]> = [
  ["dockerfile", "docker"],
  ["makefile", "make"],
];
const shikiExtensionLanguages: Array<[extension: string, language: BundledLanguage]> = [
  [".md", "markdown"],
  [".mdx", "markdown"],
  [".markdown", "markdown"],
  [".tsx", "tsx"],
  [".ts", "ts"],
  [".jsx", "jsx"],
  [".js", "javascript"],
  [".mjs", "javascript"],
  [".cjs", "javascript"],
  [".json", "json"],
  [".jsonc", "json"],
  [".yml", "yaml"],
  [".yaml", "yaml"],
  [".toml", "toml"],
  [".ini", "ini"],
  [".go", "go"],
  [".py", "python"],
  [".pyi", "python"],
  [".rb", "ruby"],
  [".erb", "erb"],
  [".php", "php"],
  [".phtml", "php"],
  [".java", "java"],
  [".kt", "kotlin"],
  [".kts", "kotlin"],
  [".swift", "swift"],
  [".c", "c"],
  [".h", "c"],
  [".cpp", "cpp"],
  [".cc", "cpp"],
  [".cxx", "cpp"],
  [".hpp", "cpp"],
  [".hh", "cpp"],
  [".hxx", "cpp"],
  [".cs", "csharp"],
  [".rs", "rust"],
  [".sql", "sql"],
  [".scala", "scala"],
  [".dart", "dart"],
  [".lua", "lua"],
  [".pl", "perl"],
  [".pm", "perl"],
  [".r", "r"],
  [".clj", "clojure"],
  [".cljs", "clojure"],
  [".cljc", "clojure"],
  [".edn", "clojure"],
  [".ex", "elixir"],
  [".exs", "elixir"],
  [".erl", "erlang"],
  [".hrl", "erlang"],
  [".hcl", "hcl"],
  [".tf", "terraform"],
  [".tfvars", "terraform"],
  [".sh", "bash"],
  [".bash", "bash"],
  [".zsh", "bash"],
  [".fish", "fish"],
  [".ps1", "powershell"],
  [".psm1", "powershell"],
  [".psd1", "powershell"],
  [".html", "html"],
  [".xml", "xml"],
  [".svg", "xml"],
  [".css", "css"],
  [".scss", "scss"],
  [".sass", "scss"],
  [".less", "less"],
  [".proto", "proto"],
  [".graphql", "graphql"],
  [".gql", "graphql"],
  [".vue", "vue"],
  [".svelte", "svelte"],
];

let highlighterPromise: Promise<ShikiHighlighter> | null = null;
const loadedShikiLanguages = new Set<ResolvedShikiLanguage>([shikiFallbackLanguage]);
const loadingShikiLanguages = new Map<ResolvedShikiLanguage, Promise<void>>();
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

export function resolveShikiLanguage(path: string): ResolvedShikiLanguage {
  const normalizedPath = path.toLowerCase();
  const fileName = normalizedPath.split("/").at(-1) ?? normalizedPath;
  for (const [candidate, language] of shikiFileNameLanguages) {
    if (fileName === candidate) {
      return language;
    }
  }
  for (const [extension, language] of shikiExtensionLanguages) {
    if (normalizedPath.endsWith(extension)) {
      return language;
    }
  }
  return shikiFallbackLanguage;
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
      await ensureLanguageLoaded(highlighter, language);
      const result = highlighter.codeToTokens(code, { lang: language, theme: shikiTheme });
      return result.tokens.map((line) =>
        line.map((token) => ({
          color: token.color,
          content: token.content,
          fontStyle: token.fontStyle,
        })),
      );
    } catch {
      if (language !== shikiFallbackLanguage) {
        const fallback = highlighter.codeToTokens(code, { lang: shikiFallbackLanguage, theme: shikiTheme });
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
        langs: [shikiFallbackLanguage],
        themes: [shikiTheme],
      }),
    );
  }
  return highlighterPromise;
}

async function ensureLanguageLoaded(highlighter: ShikiHighlighter, language: ResolvedShikiLanguage) {
  if (language === shikiFallbackLanguage || loadedShikiLanguages.has(language)) {
    return;
  }
  const inFlight = loadingShikiLanguages.get(language);
  if (inFlight) {
    await inFlight;
    return;
  }
  const next = highlighter.loadLanguage(language)
    .then(() => {
      loadedShikiLanguages.add(language);
      loadingShikiLanguages.delete(language);
    })
    .catch((error) => {
      loadingShikiLanguages.delete(language);
      throw error;
    });
  loadingShikiLanguages.set(language, next);
  await next;
}
