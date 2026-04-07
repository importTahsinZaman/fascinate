import { beforeEach, describe, expect, it } from "vitest";
import {
  createMockShell,
  getMockDefaultWorkspace,
  getMockTerminalGitDiffBatch,
  getMockTerminalGitStatus,
  getMockTerminalPresentation,
  listMockShells,
  resetMockControlPlaneState,
} from "./mock-control-plane";

describe("mock control plane", () => {
  beforeEach(() => {
    resetMockControlPlaneState();
  });

  it("returns seeded workspace and terminal presentation data", async () => {
    const workspace = await getMockDefaultWorkspace();
    expect(workspace.windows).toHaveLength(2);
    expect(workspace.windows[0]?.shellId).toBe("mock-session-m1");

    const shells = await listMockShells();
    expect(shells).toHaveLength(2);

    const presentation = getMockTerminalPresentation("mock-session-m1", "m-1");
    expect(presentation.cwd).toBe("/home/ubuntu/aisi");
    expect(presentation.lines.join("\n")).toContain("feature/metadata-guardrails");
  });

  it("creates mock shells with machine-specific cwd defaults", async () => {
    const shell = await createMockShell("cool-space");
    const presentation = getMockTerminalPresentation(shell.id, "cool-space");
    expect(presentation.cwd).toBe("/home/ubuntu/project-alpha");
    expect(presentation.lines.join("\n")).toContain("pnpm dev");
  });

  it("returns mock git status and diff batches for seeded repos", async () => {
    const status = await getMockTerminalGitStatus("mock-session-m1", "/home/ubuntu/aisi");
    expect(status.state).toBe("ready");
    expect(status.files).toHaveLength(2);

    const batch = await getMockTerminalGitDiffBatch("mock-session-m1", {
      cwd: "/home/ubuntu/aisi",
      repo_root: "/home/ubuntu/aisi",
      files: [
        {
          path: "connector/src/main/java/com/aisi/connector/controller/MetadataIndexController.java",
          kind: "modified",
          worktree_status: "M",
        },
      ],
    });
    expect(batch.diffs).toHaveLength(1);
    expect(batch.diffs[0]?.state).toBe("ready");
    expect(batch.diffs[0]?.patch).toContain("@PostConstruct");
  });
});
