import type { CSSProperties } from "react";

type MachineColor = {
  accent: string;
  surface: string;
  shellSurface: string;
  border: string;
  strongBorder: string;
  focusSurface: string;
};

const machineColorPalette = [
  "#c95d4a",
  "#d98a2f",
  "#c9b449",
  "#e39b42",
  "#e17854",
  "#db6675",
  "#d45dd6",
  "#a56af2",
  "#796cf2",
  "#5f8ff5",
  "#49b4f2",
  "#34c7d5",
  "#2a9cb8",
  "#d1495b",
  "#5868d8",
] as const;

const paletteSpreadOrder = [0, 7, 14, 4, 11, 2, 9, 6, 13, 1, 8, 3, 10, 5, 12] as const;

export function getMachineColor(seed: string): MachineColor {
  const accent = machineColorPalette[Math.abs(hashString(seed)) % machineColorPalette.length];
  return buildMachineColor(accent);
}

export function getMachineColorStyles(seeds: string[]): Record<string, CSSProperties> {
  const orderedSeeds = Array.from(new Set(seeds.filter((seed) => seed.trim() !== "")));
  const usedPaletteIndices = new Set<number>();
  const styles: Record<string, CSSProperties> = {};

  for (const seed of orderedSeeds) {
    const preferredSlot = Math.abs(hashString(seed)) % paletteSpreadOrder.length;
    let accent = machineColorPalette[paletteSpreadOrder[preferredSlot]];

    for (let offset = 0; offset < paletteSpreadOrder.length; offset += 1) {
      const slot = paletteSpreadOrder[(preferredSlot + offset) % paletteSpreadOrder.length];
      if (usedPaletteIndices.has(slot)) {
        continue;
      }
      usedPaletteIndices.add(slot);
      accent = machineColorPalette[slot];
      break;
    }

    styles[seed] = toMachineColorStyle(buildMachineColor(accent));
  }

  return styles;
}

export function getMachineColorStyle(seed: string): CSSProperties {
  return toMachineColorStyle(getMachineColor(seed));
}

function buildMachineColor(accent: string): MachineColor {
  return {
    accent,
    surface: hexToRGBA(accent, 0.08),
    shellSurface: hexToRGBA(accent, 0.06),
    border: hexToRGBA(accent, 0.2),
    strongBorder: hexToRGBA(accent, 0.34),
    focusSurface: hexToRGBA(accent, 0.14),
  };
}

function toMachineColorStyle(color: MachineColor): CSSProperties {
  return {
    "--machine-accent": color.accent,
    "--machine-surface": color.surface,
    "--machine-shell-surface": color.shellSurface,
    "--machine-border": color.border,
    "--machine-strong-border": color.strongBorder,
    "--machine-focus-surface": color.focusSurface,
  } as CSSProperties;
}

function hashString(value: string) {
  let hash = 0;
  for (let index = 0; index < value.length; index += 1) {
    hash = (hash << 5) - hash + value.charCodeAt(index);
    hash |= 0;
  }
  return hash;
}

function hexToRGBA(hex: string, alpha: number) {
  const normalized = hex.replace("#", "");
  const red = Number.parseInt(normalized.slice(0, 2), 16);
  const green = Number.parseInt(normalized.slice(2, 4), 16);
  const blue = Number.parseInt(normalized.slice(4, 6), 16);
  return `rgba(${red}, ${green}, ${blue}, ${alpha})`;
}

export { machineColorPalette };
