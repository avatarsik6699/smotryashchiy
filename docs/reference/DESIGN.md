---
name: sre-kit
description: A quiet, object-first operating console for scanning source health and telemetry.
colors:
  canvas-ink: "#080b0d"
  graphite-surface: "#0c1114"
  raised-graphite: "#11181c"
  hairline: "#263137"
  text-primary: "#d8e0df"
  text-muted: "#7e8c8c"
  operator-green: "#8bb89d"
  focus-green: "#b6d4c0"
  status-ok: "#70c28d"
  status-warn: "#d6ad68"
  status-critical: "#de7b7f"
  status-unreachable: "#617074"
typography:
  title:
    fontFamily: "JetBrains Mono, IBM Plex Mono, ui-monospace, SFMono-Regular, Consolas, monospace"
    fontSize: "clamp(1.75rem, 4vw, 2rem)"
    fontWeight: 650
    lineHeight: 1.2
  body:
    fontFamily: "JetBrains Mono, IBM Plex Mono, ui-monospace, SFMono-Regular, Consolas, monospace"
    fontSize: "1rem"
    fontWeight: 400
    lineHeight: 1.45
  label:
    fontFamily: "JetBrains Mono, IBM Plex Mono, ui-monospace, SFMono-Regular, Consolas, monospace"
    fontSize: "0.75rem"
    fontWeight: 400
    lineHeight: 1.45
    letterSpacing: "0.05em"
rounded:
  control: "2px"
  pulse: "50%"
spacing:
  xs: "8px"
  sm: "12px"
  md: "16px"
  lg: "24px"
  xl: "32px"
components:
  button-primary:
    backgroundColor: "{colors.operator-green}"
    textColor: "{colors.canvas-ink}"
    typography: "{typography.body}"
    rounded: "{rounded.control}"
    padding: "8px 12px"
  button-secondary:
    backgroundColor: "transparent"
    textColor: "{colors.text-primary}"
    typography: "{typography.body}"
    rounded: "{rounded.control}"
    padding: "8px 12px"
  input:
    backgroundColor: "{colors.canvas-ink}"
    textColor: "{colors.text-primary}"
    typography: "{typography.body}"
    rounded: "{rounded.control}"
    padding: "9px 10px"
  ledger-row:
    backgroundColor: "transparent"
    textColor: "{colors.text-primary}"
    typography: "{typography.body}"
    rounded: "{rounded.control}"
    padding: "14px 0"
---

# Design System: sre-kit

## Overview

**Creative North Star: "The Quiet Source Ledger"**

sre-kit is an object-first operating console: the observed Source leads, while adapter identity,
purpose, target, status and freshness remain supporting evidence. Its world is a near-black working
surface with muted green operator cues, compact mono typography and rules thin enough to preserve
density. It should feel calm under continuous use, not theatrical.

Hierarchy comes from alignment, rhythm and disclosure rather than card chrome. A compact command
bar establishes location; aggregate status follows; dense ledgers connect named objects to their
signals. Terminal language is restrained to useful prompts, paths and prefixes. The interface does
not imitate a terminal window.

**Key Characteristics:**

- Near-black canvas with shallow graphite separation.
- Object names before integration and adapter detail.
- Mono-first, tabular, compact information density.
- Hairline-led hierarchy with nearly square controls.
- Muted green for operation; distinct semantic colors for health.

## Colors

The palette stays close to ink and graphite so labels, values and state—not surfaces—carry emphasis.

### Primary

- **Operator Green:** Marks active navigation, command prefixes and primary actions. Keep it muted;
  it identifies agency rather than general health.
- **Focus Green:** Reserved for the visible two-pixel keyboard focus outline.

### Secondary

- **Status OK:** Healthy or live state only.
- **Status Warning:** Degraded or cautionary state only.
- **Status Critical:** Failed or destructive state only.
- **Status Unreachable:** Missing connectivity or unavailable state only.

### Neutral

- **Canvas Ink:** The uninterrupted application background and input field ground.
- **Graphite Surface:** Bounded containers and the drawer surface.
- **Raised Graphite:** Hovered rows, alerts and measurement cells.
- **Hairline:** Dividers, table strokes, input boundaries and spatial grid lines.
- **Primary Text:** Object names, values and actionable labels.
- **Muted Text:** Technical context, timestamps, captions and non-current navigation.

### Named Rules

**The State Is Not Chrome Rule.** Semantic health colors describe state and are never repurposed
for navigation, decoration or general actions.

**The Unknown Stays Visible Rule.** Unreachable and absent data retain their own labeled state;
they never inherit a healthy treatment.

## Typography

**Display Font:** JetBrains Mono, with IBM Plex Mono and system monospace fallbacks  
**Body Font:** JetBrains Mono, with IBM Plex Mono and system monospace fallbacks  
**Label/Mono Font:** The same mono stack

**Character:** The shipped interface is mono-first from heading to measurement. Weight, size,
letter spacing and muted contrast create hierarchy while tabular numerals keep changing data stable.

### Hierarchy

- **Title:** Semi-bold and compact; names the current operational surface or observed object.
- **Body:** Regular-weight interface copy, object metadata and values with a measured line height.
- **Label:** Small, often uppercase with restrained tracking; used for table headers, section markers
  and compact operational metadata.
- **Measurement:** Semi-bold tabular numerals, larger than their muted labels, with no decorative
  display styling.

### Named Rules

**The Name Before Mechanism Rule.** Give the operator-defined object name the strongest type;
integration, purpose, target and identifiers follow at lower contrast or smaller scale.

**The Stable Numbers Rule.** Measurements, counts and timestamps use mono tabular alignment so live
changes do not disturb the scan path.

## Layout

The shell is a full-height canvas with a sticky, hairline-bottom command bar and a centered content
column capped at 1440px. Desktop content uses fluid side padding between 16px and 44px; the main
stack begins 28px below the bar and finishes with 48px of breathing room. The shared spacing rhythm
is 8, 12, 16, 24 and 32px.

The primary overview sequence is command context, operational heading, aggregate source state,
alerts, then project-grouped source ledgers. Rows align identity, current measurements and freshness
in three columns. At 900px and below each row becomes a single-column evidence stack. At 720px and
below the command bar wraps, the session indicator recedes, and content padding contracts to 12px.
Detail headers switch from object-plus-action columns to one column at 760px. Tables remain dense
and horizontally scroll when their evidence cannot be collapsed honestly.

**The Evidence Line Rule.** Keep identity, measurements and freshness on one aligned row when space
allows; stack them in that same order when it does not.

## Elevation & Depth

The system is flat by default and uses no box shadows. Depth comes from graphite tone changes,
one-pixel hairlines and the drawer overlay. Hovering a ledger row may reveal a quiet raised-graphite
wash, but the row remains part of the ledger rather than becoming a floating card. The sticky command
bar may use a restrained backdrop blur only to protect legibility over scrolling content.

**The Hairline Before Shadow Rule.** Establish containment with a one-pixel rule or tonal shift;
do not add drop shadows to routine surfaces.

## Shapes

Controls and containers are square in character with only a two-pixel radius to soften rendering.
Tables, drawers, fields, alerts, chips and buttons share this near-rectilinear language. The one
deliberate exception is the circular eight-pixel status pulse, whose silhouette makes state scannable
without turning the surrounding component into a badge.

**The Square Console Rule.** Rounded cards, pills and bubbly control groups do not belong in the
operating surface; reserve circles for status pulses and loaders.

## Components

### Buttons

- **Shape:** Compact and nearly square, with a thin boundary and two-pixel corner softening.
- **Primary:** Muted operator-green fill with an 8px by 12px inset; use for the single leading action
  in a region.
- **Secondary / Icon:** Transparent fill with a hairline boundary. Icon-only actions use a tighter
  6px by 9px inset and an explicit accessible label.
- **Focus / Disabled:** Keyboard focus is a two-pixel focus-green outline offset by two pixels.
  Disabled actions lower opacity and retain their shape.

### Cards / Containers

- **Corner Style:** Near-square, with no radius beyond the shared control softening.
- **Background:** Graphite only when a bounded surface is necessary; ledgers usually remain on the
  canvas.
- **Shadow Strategy:** None; use hairlines and tonal separation.
- **Internal Padding:** 16px normally and 12px on narrow screens.

### Inputs / Fields

- **Style:** Canvas-ink field, hairline border, two-pixel corners and 9px by 10px padding. Field labels
  are compact and stronger than their muted descriptions.
- **Focus:** The same visible focus outline as buttons; never rely on a color-only border shift.
- **Error / Disabled:** Preserve readable content and pair state color with text; do not erase the
  field's boundary.

### Navigation

The sticky command bar presents `$ sre-kit`, path-like `./` labels and a terse live-session status.
Links are muted at rest, primary text on hover, and current through a thin operator-green underline.
On narrow screens it wraps into two compact rows and removes the nonessential session line.

### Source Ledger

The signature ledger is a ruled sequence, not a card grid. Each row begins with a labeled status
pulse and the Source name, continues with integration purpose and target, then aligns declared
measurements and freshness. Project headers use a path prefix and tracked label; row hover adds only
a quiet tonal wash. Source registry tables follow the same identity-first reading order.

### Status Pulse

An eight-pixel circular dot carries semantic state beside an object or explicit label. Color is never
the only state signal: nearby text, accessible naming or the object's state copy must remain present.

## Do's and Don'ts

### Do:

- **Do** lead every operational record with its operator-defined name.
- **Do** use hairlines, spacing and tabular alignment to organize dense evidence.
- **Do** keep status textual or accessibly named in addition to its semantic color.
- **Do** preserve keyboard focus, narrow-width stacking and horizontally available evidence.
- **Do** use prompt and path motifs only when they communicate command context or location.

### Don't:

- **Don't** turn ledgers into a dashboard of elevated cards.
- **Don't** use health green, amber or red as general decoration or navigation chrome.
- **Don't** add gradients, glass effects, scanlines, CRT blur or fake terminal window controls.
- **Don't** hide missing or unreachable data behind a healthy default.
- **Don't** add ornamental animation; motion is limited to feedback such as the compact loader.
