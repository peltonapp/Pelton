---
title: Shortcuts
description: Pelton's default keyboard shortcuts, and how to rebind them.
---

# Shortcuts

Pelton uses ++cmd++ on macOS and ++ctrl++ on Windows and Linux for the same bindings. The tables below write ++cmd++; substitute accordingly.

## App

| Shortcut | Action |
| -------- | ------ |
| ++cmd+n++ | Compose a new message |
| ++cmd+enter++ | Send the message you are writing |
| ++cmd+k++ | [Command palette](command-palette.md) |
| ++cmd+f++ | Search |
| ++cmd+r++ | Sync now |
| ++cmd+m++ | Add mailbox |
| ++cmd+comma++ | Settings |
| ++cmd+p++ | Export open message as PDF |
| ++cmd+z++ | Undo the last send, delete or archive |
| ++ctrl+cmd+f++ | Toggle fullscreen |
| ++cmd+h++ | Hide window (macOS) |
| ++cmd+w++ | Close the front compose window, settings, or the window itself |
| ++cmd+q++ | Quit |

++cmd+enter++ works from anywhere in the compose window: the editor, the subject, or an address field. An address you are still typing is added as a recipient before the message goes. A plain ++enter++ is always just a new line.

## Menu bar access keys

On Windows and Linux, where the in-app menu bar is the only menu there is, ++alt++ on its own focuses the bar and underlines one letter in each menu title. ++alt++ plus that letter opens the menu straight away. Arrow keys move between menus and items from there, ++esc++ leaves.

The letters are worked out from the titles themselves, so they follow your language and any menu you have renamed or added. A binding you set under **Settings, Shortcuts** always wins over an access key using the same combination.

macOS has no such convention and does not use them.

## Message actions

Each one acts on the messages you have selected, or on the open one when nothing is selected.

| Shortcut | Action |
| -------- | ------ |
| ++r++ | Reply |
| ++a++ | Reply all |
| ++f++ | Forward |
| ++e++ | Archive |
| ++s++ | Flag |
| ++u++ | Mark unread |
| ++m++ | Move to folder |
| ++z++ | Snooze |
| ++backspace++ | Delete |

These are the single letters webmail has used for years, so the keys you already know work here. They only fire when you are not typing: in a search box, an address field or the editor, the letter is just a letter.

++delete++ deletes as well as ++backspace++, until you change that binding. Delete moves to Trash, and ++cmd+z++ brings it back.

Mark read, download-for-offline and unsubscribe ship unbound. Bind them to whatever you like under **Settings, Shortcuts**, which is also where you can change any of the keys above.

Whatever you bind shows up next to the matching entry when you right-click a message, and in the toolbar tooltips. Turn that off under **Settings, Shortcuts** with "Show keyboard shortcut hints in the app".

## Selecting messages

With the message list focused:

| Shortcut | Action |
| -------- | ------ |
| ++shift+down++ / ++shift+up++ | Extend the selection to the next or previous message |
| ++space++ | Add or remove the highlighted message |
| ++cmd+a++ | Select all |
| ++esc++ | Clear the selection |

The message actions above then apply to everything selected, so ++shift+down++ a few times and then ++backspace++ deletes them all. ++shift+down++ measures from the message you last moved to, opened or picked with ++space++, and adds to what is already selected.

In the unified views ++cmd+a++ only works once **Select all in unified views** is on in **Settings**, since those lists span every account. All of this needs **Select multiple messages at once**, which is on by default.

## Reading tabs

Middle-click a message, or right-click it and pick **Open in new tab**, to park it in a tab. The tab bar only exists while a tab does.

| Shortcut | Action |
| -------- | ------ |
| ++cmd+1++ | Back to the reading pane |
| ++cmd+2++ to ++cmd+8++ | Jump to that tab |
| ++cmd+9++ | Jump to the last tab |
| ++cmd+w++ | Close the tab you are on |

Middle-clicking a tab closes it too. With no tab open, ++cmd+w++ closes the window as before.

Opening and closing a tab are also actions you can bind a key to under **Settings, Shortcuts**, and they sit in the **View** menu.

By default tabs last until you quit. **Settings, Reading** has a switch to bring them back next launch.

## Developer overlays

Only in a development run, started with `PELTON_DEV` (what `make run` sets) or `PELTON_DEVTOOLS=1`. A normal build does not bind these keys at all.

| Shortcut | Action |
| -------- | ------ |
| ++f6++ | Activity log: what the backend is doing, live |
| ++f7++ | Process: goroutines, heap, database and cache sizes |
| ++f8++ | Frames: frame timing for the ui |

You do not have to remember the keys. In a development run the **DEV** badge in
the status bar opens a menu listing each overlay, its key, and whether it is
open. Clicking an entry toggles it.

Everything the overlays show is read on the machine and displayed there. Nothing
is collected and nothing is sent anywhere.

The browser inspector is a separate thing and is not one of these. It is fixed
when the binary is built, so `make run` has it and a downloaded Pelton does not,
whatever environment variables are set.

The overlays are draggable and several can be open at once. They always start closed.

The activity log shows the same lines Pelton writes to its log, so it follows the log level. Set `PELTON_DEBUG=1` for the full sync detail.

The webview inspector is the browser's own, not Pelton's: ++f12++ on Windows, ++cmd+alt+i++ on macOS, right-click and Inspect on Linux. It only exists in a development build.

## Rebinding

**Settings, Shortcuts** lists every action with its current combo. Click one and press the new combination to rebind it, including the defaults above. If a combo is already taken, Pelton tells you what it is bound to.

## Vim modes

Two independent toggles for keyboard-centric use:

- **App vim mode** (**Settings, Shortcuts**): `h`/`j`/`k`/`l` style navigation across the message list and panes.
- **Compose vim mode** (**Settings, Composing**): vim keybindings inside the compose editor.

In app vim mode, ++v++ starts a visual selection at the current message, and `j`, `k`, `gg` and `G` extend it from there. A blue **VISUAL** badge shows in the status bar while it runs. ++esc++ ends it and keeps what it selected, so you can move past a few messages and press ++v++ again to add another range. ++shift+down++ and ++shift+up++ extend the selection without entering visual mode. With messages selected and visual mode off, ++esc++ clears the selection, and the next ++esc++ closes the reading pane.

## Need help?

See [Support](../support.md).
