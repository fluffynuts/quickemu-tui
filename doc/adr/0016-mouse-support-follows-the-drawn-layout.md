# 16. Mouse support maps clicks onto the same layout the views draw

Status: accepted (recorded retrospectively)

## Context

The UI is keyboard-first, with the mouse as an addition (`mouse.go`).

## Decision

- Click handling reuses the helpers the views draw with (`bodyArea`, `listWidth`,
  `detailHead`, `windowStart`, `menuItemAt`, `qsLayout`), so a click lands on
  what is drawn, and a layout change moves both.
- Every dialog has a close button, `[x]` in its top border, doing what esc does
  there; dialogs that ask something have buttons that press the key they
  stand for (`[ Save ]`, `[ Yes ]`/`[ No ]`, …).
- Clicking a selected VM again, or right-clicking any, opens its actions menu;
  the wheel scrolls the list under it and elsewhere stands in for ↑/↓.
- `-no-mouse` leaves the mouse to the terminal (so text can be selected without
  holding shift).

## Consequences

- Mouse tests (`mouse_test.go`) click on text found in the rendered screen, so
  they track the layout rather than hard-coded coordinates.
- A new dialog needs its layout helper shared between its view and its click
  handling.
