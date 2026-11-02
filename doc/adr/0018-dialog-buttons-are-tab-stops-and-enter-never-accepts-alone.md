# 18. Dialog buttons are tab stops, and enter never accepts on its own

Status: accepted. Supersedes the bullet on buttons in 0016.

## Context

Buttons were only for the mouse: each stood for a key (`[ Yes ]` for `y`,
`[ Save ]` for enter) and had no place in the keyboard's tab order. Enter also
meant "accept" in most dialogs: a confirmation's default, quick settings'
Save, the custom ISO screen's Next, closing the error dialog. A stray enter
could therefore answer a question the user hadn't read, and dialogs behaved
differently from each other (esc saved in quick settings, discarded elsewhere;
tab switched log files but moved between settings elsewhere).

## Decision

One model for every dialog (`dialogbuttons.go`; the first-run setup, a separate
program, follows the same rules):

- A dialog has tab stops of its own (a text box; the settings of quick settings
  and the custom ISO screen; one for a list or picker) and then its buttons.
  Tab and shift-tab go through them in that order and wrap. A dialog with no
  stops of its own, such as a confirmation, goes straight to its buttons.
- The first button is the default. It is drawn bold green, the others blue,
  and the focused one reversed; the default is therefore also the first tab
  stop, and a confirmation whose default is No draws `[ No ]` before `[ Yes ]`.
  A hotkey is underlined in its button (`Y` in `Yes`).
- Enter or space presses the focused button, left and right move between
  buttons, and a click presses a button directly. Any other key goes back to
  the dialog's own controls first (typing on a button types in the text box).
- Enter does nothing else: it still submits a text box that is the only thing
  in the dialog, and still chooses the highlighted row of a list or picker. It no longer
  takes a confirmation's default, saves quick settings, advances the custom ISO
  screen or closes the error dialog.
- Esc cancels, meaning closes, whatever the dialog (in the install steps it
  steps back, as before). Every dialog has a Cancel or Close button as its last
  stop; `[x]` stays a mouse-only close button and isn't a tab stop. Quick
  settings' Cancel, esc and a click outside discard; only Save writes.
- `y` and `n` still answer a confirmation, and the hint text for them is gone.
- Where tab used to switch something, that moves to the arrows: in the logs
  dialog ← and → switch file, and the dialog and the key hints say so.
- Focus belongs to one dialog: a new dialog, or another question asked from
  one (`askConfirm`, `askPrompt`), starts with nothing focused
  (`dialogKey`, checked in `Update`).

## Consequences

- Reaching the default takes a tab: a user used to enter answering a
  confirmation presses tab, enter, or `y`.
- The colour is the only sign of the default besides its position, so it is
  also bold, to survive a terminal without colour.
- A new dialog needs its buttons in `dialogButtons`, its tab stops in
  `bodyStops`/`bodyStop`/`setBodyStop`, and its view to draw `buttonRow`.
