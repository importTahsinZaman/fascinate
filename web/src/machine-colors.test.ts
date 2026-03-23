import { describe, expect, it } from "vitest";
import { getMachineColor, getMachineColorStyle, getMachineColorStyles, machineColorPalette } from "./machine-colors";

describe("machine colors", () => {
  it("uses a 15-color palette", () => {
    expect(machineColorPalette).toHaveLength(15);
  });

  it("does not include colors too close to the primary green or muted lavender", () => {
    expect(machineColorPalette).not.toContain("#3ecf8e");
    expect(machineColorPalette).not.toContain("#6fcf5b");
    expect(machineColorPalette).not.toContain("#2eb8a3");
    expect(machineColorPalette).not.toContain("#7ebd67");
    expect(machineColorPalette).not.toContain("#9b93a8");
  });

  it("maps the same machine name to the same color", () => {
    expect(getMachineColor("tic-tac-toe")).toEqual(getMachineColor("tic-tac-toe"));
  });

  it("keeps currently visible machine colors distinct", () => {
    const styles = getMachineColorStyles([
      "space-shooter",
      "tic-tac-toe",
      "tic-tac-toe-v2",
      "tic-tac-toe-v3",
      "todo-app",
      "notes-app",
    ]);

    const accents = Object.values(styles).map((style) => (style as Record<string, string>)["--machine-accent"]);
    expect(new Set(accents).size).toBe(accents.length);
  });

  it("produces CSS variables for shell and sidebar chrome", () => {
    expect(getMachineColorStyle("space-shooter")).toMatchObject({
      "--machine-accent": expect.any(String),
      "--machine-surface": expect.any(String),
      "--machine-shell-surface": expect.any(String),
      "--machine-border": expect.any(String),
      "--machine-strong-border": expect.any(String),
      "--machine-focus-surface": expect.any(String),
    });
  });
});
