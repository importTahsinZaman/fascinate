# Fascinate Design System

This design system is derived from the review UI captured in `/Users/tahsin/Desktop/vmcloud/design_inspo.txt` and adapted for Fascinate's product shape: a browser command center for running coding agents on VMs.

## Product Mood

- Calm, dense, operator-grade.
- High-signal over decorative.
- Dark surfaces with restrained contrast.
- Accent color is used intentionally for state and primary actions, not decoration.
- Separation should come from layering, borders, and spacing instead of glow, gradients, or heavy shadows.

## Typography

### Sans

- Primary stack: `-apple-system`, `BlinkMacSystemFont`, `"SF Pro Text"`, `"SF Pro Display"`, `"Helvetica Neue"`, `"Segoe UI"`, `sans-serif`
- Default weight: `400`
- Action labels / control text: `500`
- Titles: `600`

### Mono

- Mono stack: `"SF Mono"`, `"SFMono-Regular"`, `ui-monospace`, `Menlo`, `Consolas`, `monospace`
- Use for terminals, environment previews, commands, and low-level data.

### Type Scale

- `11px / 16px`: overlines, eyebrows
- `12px / 16px`: metadata, pills, counts, status rows
- `13px / 18px`: default app text, controls, list items
- `14px / 20px`: section titles, shell titles
- `28-34px / 1.08`: login and major hero copy only

### Letter Spacing

- Normal UI copy: default
- Dense titles: `-0.01em` to `-0.03em`
- Eyebrows: `0.08em`, uppercase

## Color Tokens

All components should use semantic tokens instead of hardcoded colors.

### Core Surfaces

- `--bg-page`: app background
- `--bg-surface`: primary panel surface
- `--bg-elevated`: raised cards, rows, compact controls
- `--bg-hover`: hover state
- `--bg-active`: pressed/active state
- `--bg-tint`: subtle neutral tint for hover/selection

### Borders

- `--border-subtle`: default dividers and panel strokes
- `--border-medium`: stronger framed surfaces
- `--border-strong`: selected or high-emphasis framing

### Text

- `--text-primary`: main readable text
- `--text-secondary`: supporting copy
- `--text-tertiary`: labels, subdued metadata
- `--text-danger`: destructive status
- `--text-success`: healthy / connected / passed
- `--text-warning`: investigate / caution
- `--text-purple`: secondary semantic accent
- `--text-on-accent`: text on primary green buttons

### Accent

- `--accent-primary`: primary action green
- `--accent-primary-hover`
- `--accent-primary-pressed`

## Spacing

Spacing uses a compact `4px` base scale.

- `4px`
- `6px`
- `8px`
- `10px`
- `12px`
- `16px`
- `24px`
- `32px`

Rules:

- Tight interactive clusters use `4-6px`.
- Standard control and list padding uses `8-12px`.
- Section spacing uses `12-16px`.
- Large page-level spacing should be rare.

## Radius

- `6px`: compact controls, buttons, inputs
- `8px`: cards, list items, env previews
- `12px`: popovers, windows, login card
- `999px`: pills and status chips

The system should stay mostly in the `6-12px` range. Avoid oversized rounded corners.

## Elevation

- Base panels should feel flat.
- Use borders before shadows.
- Shadows should be soft and reserved for overlays only.
- No glow, glass, or blurred atmospheric effects in core product UI.

## Component Rules

### Toolbelt

- Compact floating control strip.
- Icon-first.
- Neutral by default.
- Active button gets a muted selected fill, not a bright accent.
- Logout/destructive affordances stay subtle until hover.

### Popovers

- Elevated dark surface with a subtle border.
- Internal sections are separated with borders, not large whitespace.
- Forms stay compact.
- Popovers should feel like dense operator panels, not marketing cards.

### Buttons

- Default buttons are neutral secondary controls.
- Primary actions use green.
- Destructive actions use muted red surfaces with warm red text.
- Use the same height rhythm as the rest of the app: compact by default.

### Inputs

- Dark filled surface.
- Thin border.
- Small radius.
- Focus ring is crisp and visible.

### Shell Windows

- Header is compact and restrained.
- Window chrome should feel like application UI, not a stylized terminal toy.
- Terminal content area can be slightly darker than the frame.
- Metadata row should be quiet and legible.

### Lists and Rows

- Rows are dense and compact.
- Item emphasis comes from text hierarchy, not large cards.
- Use `13px` primary text and `12px` metadata.

## Motion

- Keep motion quick and quiet.
- Preferred duration: `100-150ms`
- Prefer background, border, opacity, and subtle transform changes.
- Avoid bounce, overshoot, or ornamental easing.

## Fascinate-Specific Interpretation

This system should make Fascinate feel like:

- a command center
- an operator console
- a live workspace for many agents

It should not feel like:

- cyberpunk terminal theater
- generic glossy SaaS
- playful canvas software

The UI should stay serious, dense, and clear even as the workspace becomes more visual.
